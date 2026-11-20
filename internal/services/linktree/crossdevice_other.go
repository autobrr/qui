// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package linktree

// isPlatformCrossDevice has no error to add to EXDEV outside Windows.
func isPlatformCrossDevice(error) bool {
	return false
}
