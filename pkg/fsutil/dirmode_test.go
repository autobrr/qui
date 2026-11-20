// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsutil

import "testing"

// TestDirModeInvariants pins the directory-mode constant so an accidental edit
// is caught. ContentDirMode requests all bits and lets umask decide (discussion
// #1704).
func TestDirModeInvariants(t *testing.T) {
	if ContentDirMode != 0o777 {
		t.Errorf("ContentDirMode = %04o, want 0777", ContentDirMode)
	}
}
