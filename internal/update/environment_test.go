// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"io"
	"runtime"
	"testing"

	"github.com/rs/zerolog"
)

// TestCanSelfUpdateRespectsWindowsGuard verifies that the Service correctly blocks self-update on Windows
func TestCanSelfUpdateRespectsWindowsGuard(t *testing.T) {
	svc := NewService(
		noopLogger(),
		true, // enabled
		"v1.0.0",
		"test-agent",
	)

	canUpdate := svc.CanSelfUpdate()

	if runtime.GOOS == "windows" && canUpdate {
		t.Fatal("CRITICAL: CanSelfUpdate() must return false on Windows to prevent syscall.Exec panic")
	}
}

// noopLogger returns a zerolog.Logger that discards all output
func noopLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}
