//go:build !windows

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import "testing"

// TestSelfUpdateSupportedOnNonWindows ensures platforms other than Windows remain eligible for self-update.
func TestSelfUpdateSupportedOnNonWindows(t *testing.T) {
	if !isSelfUpdateSupportedPlatform() {
		t.Fatal("non-Windows platforms must support self-update")
	}
}
