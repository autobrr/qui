// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/internal/models"
)

func (s *Service) buildDirectCommand(ctx context.Context, program *models.ExternalProgram, args []string) (*exec.Cmd, bool) {
	programPath := program.Path
	// Command keeps absolute PATHEXT resolution private, so cmd.Path is not sufficient.
	if resolved, err := exec.LookPath(programPath); err == nil {
		programPath = resolved
	}
	var cmd *exec.Cmd
	var commandLine string
	ext := strings.ToLower(filepath.Ext(programPath))
	launcher := ext == ".bat" || ext == ".cmd"
	// Batch files require cmd.exe and release their slot after shell startup.
	if launcher {
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
	}
	// Suppress console windows without hiding the window of a GUI program.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW, CmdLine: commandLine}
	return cmd, launcher
}
