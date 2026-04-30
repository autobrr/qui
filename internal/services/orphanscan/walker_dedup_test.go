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

	// Single-link file (nlink=1): never dedup — can't appear twice in a walk.
	if w.shouldSkipDuplicate(fid, 1) {
		t.Fatal("single-link file should not be skipped")
	}
	if w.shouldSkipDuplicate(fid, 1) {
		t.Fatal("single-link file should not be skipped even with same FileID")
	}

	// Hardlinked file (nlink > 1): first occurrence recorded, second is a dup.
	fid2 := hardlink.FileID{Dev: 1, Ino: 99}
	if w.shouldSkipDuplicate(fid2, 3) {
		t.Fatal("first hardlinked file should not be skipped")
	}
	if !w.shouldSkipDuplicate(fid2, 3) {
		t.Fatal("duplicate hardlinked file should be skipped")
	}

	// Zero FileID: never skip.
	if w.shouldSkipDuplicate(hardlink.FileID{}, 2) {
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
	// They share the same inode with nlink=2, so shouldSkipDuplicate deduplicates
	// the second occurrence — only one orphan unit should be produced.

	orphans, _, err := walkScanRoot(
		t.Context(), root, tfm, nil, 0, 100,
		newTestBackend(),
	)
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}

	if len(orphans) != 1 {
		t.Fatalf("expected 1 deduped orphan unit, got %d", len(orphans))
	}
}
