// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/internal/models"
)

func (s *Service) buildDirectCommand(ctx context.Context, program *models.ExternalProgram, args []string) (*exec.Cmd, bool) {
	var cmd *exec.Cmd
	ext := strings.ToLower(filepath.Ext(program.Path))
	launcher := ext == ".bat" || ext == ".cmd"
	// Batch files require cmd.exe; preserve the existing launcher behavior for them.
	if launcher {
		cmdArgs := append([]string{"/d", "/c", "start", "", "/b", program.Path}, args...)
		cmd = exec.CommandContext(ctx, "cmd.exe", cmdArgs...) //nolint:gosec // intentional external program execution
	} else {
		cmd = exec.CommandContext(ctx, program.Path, args...) //nolint:gosec // intentional external program execution
	}
	// Suppress console windows without hiding the window of a GUI program.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd, launcher
}
