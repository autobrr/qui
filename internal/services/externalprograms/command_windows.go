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
	var cmd *exec.Cmd
	var commandLine string
	ext := strings.ToLower(filepath.Ext(programPath))
	launcher := ext == ".bat" || ext == ".cmd"
	policy := executionPolicy{}
	// Batch files require cmd.exe and release their slot after shell startup.
	if launcher {
		policy = executionPolicy{releaseOnStart: true, reportAfterExit: true}
		cmdArgs := append([]string{"/d", "/v:off", "/s", "/c", programPath}, args...)
		cmd = exec.CommandContext(ctx, "cmd.exe", cmdArgs...) //nolint:gosec // intentional external program execution
		// Expanding a quoted environment value keeps operators and percent
		// tokens in the path literal. Arguments retain their existing shell semantics.
		cmd.Env = append(os.Environ(), "QUI_EXTERNAL_PROGRAM_PATH="+programPath)
		var line strings.Builder
		line.WriteString(`cmd.exe /d /v:off /s /c ""%QUI_EXTERNAL_PROGRAM_PATH%"`)
		for _, arg := range args {
			line.WriteByte(' ')
			line.WriteString(syscall.EscapeArg(arg))
		}
		line.WriteByte('"')
		commandLine = line.String()
	} else {
		cmd = exec.CommandContext(ctx, programPath, args...) //nolint:gosec // intentional external program execution
		policy.releaseOnStart = isWindowsGUIExecutable(programPath)
	}
	// Suppress console windows without hiding the window of a GUI program.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW, CmdLine: commandLine}
	return cmd, policy
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
