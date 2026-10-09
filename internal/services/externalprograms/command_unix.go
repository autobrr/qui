// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package externalprograms

import (
	"context"
	"os/exec"

	"github.com/autobrr/qui/internal/models"
)

func (s *Service) buildDirectCommand(ctx context.Context, program *models.ExternalProgram, args []string) (*exec.Cmd, bool) {
	return exec.CommandContext(ctx, program.Path, args...), false //nolint:gosec // intentional external program execution
}
