// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package externalprograms

import (
	"context"
	"os/exec"

	"github.com/autobrr/qui/internal/models"
)

func (s *Service) buildDirectCommand(ctx context.Context, program *models.ExternalProgram, args []string) (*exec.Cmd, executionPolicy) {
	return exec.CommandContext(ctx, program.Path, args...), executionPolicy{} //nolint:gosec // intentional external program execution
}

// buildTerminalCommand creates a command that opens in a terminal window.
func (s *Service) buildTerminalCommand(ctx context.Context, program *models.ExternalProgram, args []string) (*exec.Cmd, executionPolicy) {
	allArgs := append([]string{program.Path}, args...)
	cmd, launcher := s.createTerminalCommand(ctx, shellJoin(allArgs))
	return cmd, executionPolicy{releaseOnStart: launcher}
}
