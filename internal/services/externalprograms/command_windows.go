// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package externalprograms

import (
	"context"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func buildNativeCommand(ctx context.Context, path string, args []string, useTerminal bool) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // intentional external program execution
	cmd.SysProcAttr = newWindowsSysProcAttr(useTerminal)

	return cmd
}

func newWindowsSysProcAttr(useTerminal bool) *syscall.SysProcAttr {
	attr := &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS,
	}
	if useTerminal {
		attr.CreationFlags = windows.CREATE_NEW_CONSOLE
	}

	return attr
}
