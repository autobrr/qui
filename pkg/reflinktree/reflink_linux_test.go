//go:build linux

// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package reflinktree

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCloneFile_RetriesEAGAIN(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.txt")
	dstPath := filepath.Join(tmpDir, "dst.txt")

	if err := os.WriteFile(srcPath, []byte("reflink test"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	originalClone := ioctlFileClone
	originalCloneRange := ioctlFileCloneRange
	t.Cleanup(func() {
		ioctlFileClone = originalClone
		ioctlFileCloneRange = originalCloneRange
	})

	attempts := 0
	ioctlFileClone = func(_, _ int) error {
		attempts++
		if attempts < 3 {
			return unix.EAGAIN
		}
		return nil
	}
	ioctlFileCloneRange = func(_ int, _ *unix.FileCloneRange) error {
		t.Fatalf("unexpected FICLONERANGE fallback")
		return nil
	}

	if err := cloneFile(srcPath, dstPath); err != nil {
		t.Fatalf("expected reflink clone to succeed after retries: %v", err)
	}

	if attempts != 3 {
		t.Fatalf("expected 3 clone attempts, got %d", attempts)
	}

	if _, err := os.Stat(dstPath); err != nil {
		t.Fatalf("expected destination file to exist: %v", err)
	}
}
