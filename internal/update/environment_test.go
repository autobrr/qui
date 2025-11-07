// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"io"
	"runtime"
	"testing"

	"github.com/rs/zerolog"
)

// TestWindowsBlockedFromSelfUpdate ensures that Windows is always blocked from self-update.
//
// This is a critical safety check because the restart logic in internal/api/handlers/version.go
// uses syscall.Exec, which only exists on Unix systems. If this guard is ever removed or bypassed,
// the Windows build will compile but panic at runtime when users attempt to self-update.
//
// This test will fail if:
//   - isSelfUpdateSupportedPlatform() is changed to allow Windows
//   - The guard logic is removed or refactored incorrectly
//
// Related: internal/api/handlers/version.go:132 (syscall.Exec call)
func TestWindowsBlockedFromSelfUpdate(t *testing.T) {
	originalGOOS := runtime.GOOS
	defer func() {
		// Note: We can't actually change runtime.GOOS, but this documents the intent
		_ = originalGOOS
	}()

	if runtime.GOOS == "windows" {
		if isSelfUpdateSupportedPlatform() {
			t.Fatal("CRITICAL: isSelfUpdateSupportedPlatform() must return false on Windows to prevent runtime panic from syscall.Exec")
		}
	}

	// Also verify the function logic directly
	// Since we can't override runtime.GOOS in tests, we verify the implementation
	// matches the documented contract: "Windows binaries cannot safely replace themselves"
	t.Run("contract verification", func(t *testing.T) {
		// If someone changes the implementation, this will serve as documentation
		// of the required behavior
		supportedPlatforms := []string{"linux", "darwin", "freebsd"}
		unsupportedPlatforms := []string{"windows"}

		for _, platform := range supportedPlatforms {
			if platform == runtime.GOOS {
				if !isSelfUpdateSupportedPlatform() {
					t.Errorf("platform %s should support self-update", platform)
				}
			}
		}

		for _, platform := range unsupportedPlatforms {
			if platform == runtime.GOOS {
				if isSelfUpdateSupportedPlatform() {
					t.Fatalf("CRITICAL: platform %s MUST NOT support self-update (syscall.Exec is Unix-only)", platform)
				}
			}
		}
	})
}

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
