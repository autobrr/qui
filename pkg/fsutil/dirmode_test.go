// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsutil

import "testing"

// TestDirModeInvariants pins the directory-mode constants so an accidental edit
// is caught. ContentDirMode requests all bits and lets umask decide (discussion
// #1704); LinkTreeBaseDirMode must never carry "other" bits so it cannot grant
// world access regardless of umask.
func TestDirModeInvariants(t *testing.T) {
	if ContentDirMode != 0o777 {
		t.Errorf("ContentDirMode = %04o, want 0777", ContentDirMode)
	}

	if LinkTreeBaseDirMode != 0o770 {
		t.Errorf("LinkTreeBaseDirMode = %04o, want 0770", LinkTreeBaseDirMode)
	}

	// The whole point of LinkTreeBaseDirMode is that it can never expose the
	// directory to "other" users, even under a permissive umask like 000.
	if other := LinkTreeBaseDirMode & 0o007; other != 0 {
		t.Errorf("LinkTreeBaseDirMode grants 'other' bits %03o; must be 0 so umask can never expose it", other)
	}
}
