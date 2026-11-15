// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"context"
	"debug/pe"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/internal/models"
)

func (s *Service) buildDirectCommand(ctx context.Context, program *models.ExternalProgram, args []string) (*exec.Cmd, executionPolicy) {
	programPath := program.Path
	// Command keeps absolute PATHEXT resolution private, so cmd.Path is not sufficient.
	if resolved, err := exec.LookPath(programPath); err == nil {
		programPath = resolved
	}
	ext := strings.ToLower(filepath.Ext(programPath))
	// Batch files require cmd.exe and release their slot after shell startup.
	if ext == ".bat" || ext == ".cmd" {
		cmd := cmdCommand(ctx, "/c", programPath, args)
		cmd.SysProcAttr.CreationFlags = windows.CREATE_NO_WINDOW
		return cmd, executionPolicy{releaseOnStart: true, reportAfterExit: true}
	}
	cmd := exec.CommandContext(ctx, programPath, args...) //nolint:gosec // intentional external program execution
	// Suppress console windows without hiding the window of a GUI program.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd, executionPolicy{releaseOnStart: isWindowsGUIExecutable(programPath)}
}

// buildTerminalCommand opens a new console window that stays open after the program exits.
func (s *Service) buildTerminalCommand(ctx context.Context, program *models.ExternalProgram, args []string) (*exec.Cmd, executionPolicy) {
	cmd := cmdCommand(ctx, "/k", program.Path, args)
	cmd.SysProcAttr.CreationFlags = windows.CREATE_NEW_CONSOLE
	// Nil streams would connect the shell to NUL. Null handles let it use the new console instead.
	console := os.NewFile(0, "console")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = console, console, console
	return cmd, executionPolicy{releaseOnStart: true}
}

// cmdCommand runs programPath through one cmd.exe parse with literal arguments.
// The path comes from an environment variable, so operators and percent tokens in it stay literal.
func cmdCommand(ctx context.Context, mode, programPath string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd.exe", append([]string{"/d", "/e:on", "/v:off", "/s", mode, programPath}, args...)...) //nolint:gosec // intentional external program execution
	cmd.Env = append(os.Environ(), "QUI_EXTERNAL_PROGRAM_PATH="+programPath)
	var line strings.Builder
	line.WriteString(`cmd.exe /d /e:on /v:off /s ` + mode + ` ""%QUI_EXTERNAL_PROGRAM_PATH%"`)
	for _, arg := range args {
		// cmd.exe ends the command at a line break, so the rest of the line would be lost.
		if strings.ContainsAny(arg, "\r\n") {
			cmd.Err = errors.New("cmd.exe argument contains a line break")
		}
		line.WriteByte(' ')
		line.WriteString(cmdQuote(arg))
	}
	line.WriteByte('"')
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line.String()}
	return cmd
}

// cmdQuote quotes arg so that cmd.exe and the program both read it literally.
// It ports append_bat_arg from the Rust standard library (library/std/src/sys/pal/windows/args.rs).
// A doubled quote keeps the quoted region open, and %%cd:~, before each % makes cmd.exe expand
// an empty substring instead of a variable, so no operator or variable in the value takes effect.
func cmdQuote(arg string) string {
	quote := arg == "" || strings.HasSuffix(arg, `\`)
	for _, r := range arg {
		special := r < utf8.RuneSelf && !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(`#$*+-./:?@\_`, r)
		if special || unicode.IsControl(r) {
			quote = true
		}
	}
	var b strings.Builder
	if quote {
		b.WriteByte('"')
	}
	backslashes := 0
	for _, r := range arg {
		if r == '\\' {
			backslashes++
			b.WriteRune(r)
			continue
		}
		switch r {
		case '"':
			b.WriteString(strings.Repeat(`\`, backslashes) + `"`)
		case '%':
			b.WriteString("%%cd:~,")
		}
		backslashes = 0
		b.WriteRune(r)
	}
	if quote {
		b.WriteString(strings.Repeat(`\`, backslashes) + `"`)
	}
	return b.String()
}

var shGetFileInfo = windows.NewLazySystemDLL("shell32.dll").NewProc("SHGetFileInfoW")

// isWindowsGUIExecutable queries executable type without launching an association.
// The PE fallback covers SHGetFileInfo's MAX_PATH limit and unavailable shell metadata.
func isWindowsGUIExecutable(path string) bool {
	if len(path) >= windows.MAX_PATH {
		return isPEGUIExecutable(path)
	}
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err = windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	if err == nil || errors.Is(err, syscall.Errno(1)) { // S_OK or S_FALSE both own an initialization count.
		defer windows.CoUninitialize()
	} else if !errors.Is(err, syscall.Errno(windows.RPC_E_CHANGED_MODE)) {
		return isPEGUIExecutable(path)
	}
	const shgfiExeType = 0x2000
	kind, _, _ := shGetFileInfo.Call(uintptr(unsafe.Pointer(pointer)), 0, 0, 0, shgfiExeType)
	runtime.KeepAlive(pointer)
	if kind == 0 {
		return isPEGUIExecutable(path)
	}
	// SHGFI_EXETYPE returns PE in the low word and a Windows version only for GUI images.
	const peSignature = 0x4550
	return uint16(kind) == peSignature && uint16(kind>>16) != 0
}

// Unreadable or non-PE paths keep the owned-process policy and normal start errors.
func isPEGUIExecutable(path string) bool {
	image, err := pe.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = image.Close() }()
	switch header := image.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		return header.Subsystem == pe.IMAGE_SUBSYSTEM_WINDOWS_GUI
	case *pe.OptionalHeader64:
		return header.Subsystem == pe.IMAGE_SUBSYSTEM_WINDOWS_GUI
	}
	return false
}
