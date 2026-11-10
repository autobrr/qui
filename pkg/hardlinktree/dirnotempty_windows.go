// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package hardlinktree

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isPlatformDirNotEmpty recognizes the code Windows returns for removing a
// folder that is not empty, whatever language its message is in.
func isPlatformDirNotEmpty(err error) bool {
	return errors.Is(err, windows.ERROR_DIR_NOT_EMPTY)
}
