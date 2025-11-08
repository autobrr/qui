//go:build windows

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import "testing"

// TestSelfUpdateUnsupportedOnWindows ensures the guard remains enforced on Windows where syscall.Exec is unavailable.
func TestSelfUpdateUnsupportedOnWindows(t *testing.T) {
	if isSelfUpdateSupportedPlatform() {
		t.Fatal("isSelfUpdateSupportedPlatform() must return false on Windows to prevent syscall.Exec panic")
	}
}
