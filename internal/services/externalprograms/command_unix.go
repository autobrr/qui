// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package externalprograms

import (
	"context"
	"os/exec"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

func (s *Service) buildDirectCommand(ctx context.Context, program *models.ExternalProgram, args []string) (*exec.Cmd, executionPolicy) {
	return exec.CommandContext(ctx, program.Path, args...), executionPolicy{} //nolint:gosec // intentional external program execution
}

func (s *Service) buildTerminalCommand(ctx context.Context, program *models.ExternalProgram, args []string) (*exec.Cmd, executionPolicy) {
	allArgs := append([]string{program.Path}, args...)
	cmd, launcher := s.createTerminalCommand(ctx, shellJoin(allArgs))
	return cmd, executionPolicy{releaseOnStart: launcher}
}

func (s *Service) createTerminalCommand(ctx context.Context, cmdLine string) (cmd *exec.Cmd, launcher bool) {
	if terminal, found := detectTerminalFromEnv(); found {
		if isTerminalAvailable(terminal) {
			cmdName, args := buildTerminalArgs(terminal, cmdLine)
			if cmdName != "" {
				log.Debug().
					Str("terminal", terminal).
					Str("source", "TERM_PROGRAM").
					Str("command", cmdLine).
					Msg("using terminal emulator for external program")
				return exec.CommandContext(ctx, cmdName, args...), true
			}
		}
	}

	for _, tc := range getTerminalCandidates() {
		if isTerminalAvailable(tc.name) {
			cmdName, args := buildTerminalArgs(tc.name, cmdLine)
			if cmdName != "" {
				log.Debug().
					Str("terminal", tc.name).
					Str("source", "detection").
					Str("command", cmdLine).
					Msg("using terminal emulator for external program")
				return exec.CommandContext(ctx, cmdName, args...), true
			}
		}
	}

	log.Warn().
		Str("command", cmdLine).
		Msg("no terminal emulator found, running command in background")
	return exec.CommandContext(ctx, "sh", "-c", cmdLine), false
}
