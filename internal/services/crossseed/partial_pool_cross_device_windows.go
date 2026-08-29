// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package crossseed

import (
	"errors"

	"golang.org/x/sys/windows"
)

// partialPoolPlatformCrossDeviceError recognizes the native error returned by
// Windows hardlink creation across volumes.
func partialPoolPlatformCrossDeviceError(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}
