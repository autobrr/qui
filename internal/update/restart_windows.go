// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import "errors"

// restartSupported hides the Restart on Windows until #2858 adds it through a
// supervisor process.
const restartSupported = false

var errWindowsRestart = errors.New("restart is not supported on Windows yet")

func checkBinary(string) error {
	return errWindowsRestart
}

func execBinary(string) error {
	return errWindowsRestart
}
