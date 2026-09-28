// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"fmt"
	"os"
)

func checkBinary(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

// execBinary ends the child. Its supervisor starts the binary again, which
// holds the new release after a Self-update.
func execBinary(string) error {
	os.Exit(restartExitCode)
	return nil
}
