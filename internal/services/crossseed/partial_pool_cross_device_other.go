// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package crossseed

// partialPoolPlatformCrossDeviceError has no additional native error outside
// Windows; syscall.EXDEV is handled by the shared classifier.
func partialPoolPlatformCrossDeviceError(error) bool {
	return false
}
