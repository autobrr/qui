// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import "os"

func checkExecutable(string) error {
	return nil
}

func execBinary(string) error {
	os.Exit(restartExitCode)
	return nil
}
