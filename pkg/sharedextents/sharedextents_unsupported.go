// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package sharedextents

// Supported reports whether the current platform can inspect shared allocation.
const Supported = false

// FilesShareAllocation reports that shared-extent detection is unsupported
// without probing either path.
func FilesShareAllocation(_, _ string) (bool, error) {
	return false, ErrUnsupported
}
