// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build unix

package update

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func checkExecutable(path string) error {
	if err := unix.Access(path, unix.X_OK); err != nil {
		return fmt.Errorf("%s is not executable: %w", path, err)
	}
	return nil
}

func execBinary(path string) error {
	//nolint:gosec // G204: path is qui's own binary, resolved at startup, and argv is qui's own
	return syscall.Exec(path, os.Args, os.Environ())
}

func Supervise(func(msg string)) {}
