// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package externalprograms

import (
	"context"
	"os/exec"
)

func buildNativeCommand(ctx context.Context, path string, args []string, _ bool) *exec.Cmd {
	return exec.CommandContext(ctx, path, args...) //nolint:gosec // intentional external program execution
}
