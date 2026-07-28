// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package sharedextents

import (
	"errors"
	"testing"
)

func TestFilesShareAllocationUnsupportedPlatform(t *testing.T) {
	shared, err := FilesShareAllocation("source", "candidate")
	if shared {
		t.Fatal("unexpected shared allocation match")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}
