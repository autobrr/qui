// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsutil

import "testing"

func TestDirModeInvariants(t *testing.T) {
	if ContentDirMode != 0o777 {
		t.Errorf("ContentDirMode = %04o, want 0777", ContentDirMode)
	}
}
