// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autobrr/qui/pkg/hardlink"
)

func TestScanWalker_ShouldSkipDuplicate(t *testing.T) {
	t.Parallel()

	w := &scanWalker{
		seenFileIDs: make(map[hardlink.FileID]struct{}),
	}

	fid := hardlink.FileID{Dev: 1, Ino: 42}

	// First time seeing this FileID with nlink=1: not a dup, should be recorded.
	if w.shouldSkipDuplicate(fid, 1) {
		t.Fatal("first file should not be skipped")
	}

	// Second time with same FileID: duplicate, should be skipped.
	if !w.shouldSkipDuplicate(fid, 1) {
		t.Fatal("duplicate file should be skipped")
	}

	// File with nlink > 1: never skip (hardlinked files tracked differently).
	fid2 := hardlink.FileID{Dev: 1, Ino: 99}
	if w.shouldSkipDuplicate(fid2, 3) {
		t.Fatal("hardlinked file (nlink > 1) should not be skipped")
	}

	// Zero FileID: never skip.
	if w.shouldSkipDuplicate(hardlink.FileID{}, 1) {
		t.Fatal("zero FileID should not be skipped")
	}
}

func TestScanWalker_DedupThroughWalkScanRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "original.mkv")
	if err := os.WriteFile(src, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	dup := filepath.Join(root, "duplicate.mkv")
	if err := os.Link(src, dup); err != nil {
		t.Skip("hardlinks not supported on this filesystem")
	}

	tfm := NewTorrentFileMap()
	// Neither file is in the TFM, so both are orphans.
	// But they share the same inode, and nlink=2 so dedup doesn't apply (nlink > 1).
	// Both should appear as orphan units.

	orphans, _, err := walkScanRoot(
		t.Context(), root, tfm, nil, 0, 100,
		newTestBackend(),
	)
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}

	// With nlink=2, dedup is disabled, so both files should produce orphan entries.
	// They may be grouped into one unit (the file path itself) depending on disc-unit logic.
	if len(orphans) == 0 {
		t.Fatal("expected orphans, got none")
	}
}
