// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package hardlinktree

// isPlatformDirNotEmpty has no additional native error outside Windows.
// syscall.ENOTEMPTY is handled by isDirNotEmpty itself.
func isPlatformDirNotEmpty(error) bool {
	return false
}
