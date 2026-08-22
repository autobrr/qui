// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// The FileID literals below spell unix Dev/Ino fields; the scenarios they
// drive (bind-mount aliases) are unix-only anyway.
//go:build !windows

package orphanscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlink"
)

func TestScanWalker_ShouldSkipDuplicate(t *testing.T) {
	t.Parallel()

	w := &scanWalker{
		seenFileIDs: make(map[hardlink.FileID]struct{}),
	}

	// Single-link file (nlink=1) seen twice: same file reachable through a
	// second path (bind mount, mergerfs branch) — the alias is a dup (#1212).
	fid := hardlink.FileID{Dev: 1, Ino: 42}
	if w.shouldSkipDuplicate(fid, 1) {
		t.Fatal("first occurrence should not be skipped")
	}
	if !w.shouldSkipDuplicate(fid, 1) {
		t.Fatal("alias of an already-seen single-link file should be skipped")
	}

	// Hardlinked file (nlink > 1): distinct directory entries, never deduped.
	fid2 := hardlink.FileID{Dev: 1, Ino: 99}
	if w.shouldSkipDuplicate(fid2, 3) {
		t.Fatal("hardlinked file should not be skipped")
	}
	if w.shouldSkipDuplicate(fid2, 3) {
		t.Fatal("hardlinked file should not be skipped on repeat sighting")
	}

	// Zero FileID: never skip.
	if w.shouldSkipDuplicate(hardlink.FileID{}, 1) {
		t.Fatal("zero FileID should not be skipped")
	}
}

// fakeWalkBackend serves canned WalkDir entries so tests can simulate the same
// file appearing at two paths with nlink=1 (a bind-mount/mergerfs alias),
// which cannot be constructed on a real test filesystem.
type fakeWalkBackend struct {
	fsops.Backend
	entries []fsops.WalkEntry
}

func (b *fakeWalkBackend) WalkDir(_ context.Context, _ string, _ fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	ch := make(chan fsops.WalkEntry, len(b.entries))
	for _, e := range b.entries {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func TestScanWalker_RecordsInUseFileIDsForDedup(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inUsePath := filepath.Join(root, "in-use.mkv")
	aliasPath := filepath.Join(root, "alias.mkv")

	tfm := NewTorrentFileMap()
	tfm.Add(inUsePath)

	fid := hardlink.FileID{Dev: 1, Ino: 2}
	old := time.Now().Add(-time.Hour)
	backend := &fakeWalkBackend{
		Backend: newTestBackend(),
		entries: []fsops.WalkEntry{
			{Path: inUsePath, Size: 123, ModTime: old, FileID: fid, Nlinks: 1},
			{Path: aliasPath, Size: 123, ModTime: old, FileID: fid, Nlinks: 1},
		},
	}

	orphans, _, err := walkScanRoot(t.Context(), root, tfm, nil, 0, 0, backend)
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}

	// The alias shares the in-use file's identity — it must not be reported
	// as a deletable orphan.
	if got := len(orphans); got != 0 {
		t.Fatalf("expected no orphans, got %d: %v", got, orphans)
	}
}

func TestScanWalker_HardlinkOrphansReportedPerPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "original.mkv")
	if err := os.WriteFile(src, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	dup := filepath.Join(root, "duplicate.mkv")
	if err := os.Link(src, dup); err != nil {
		if errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skip("hardlinks not supported on this filesystem")
		}
		t.Fatalf("os.Link(%s, %s): %v", src, dup, err)
	}

	tfm := NewTorrentFileMap()
	// Neither file is in the TFM. Hardlinks (nlink=2) are distinct directory
	// entries, so both paths are reported as orphan units.

	orphans, _, err := walkScanRoot(
		t.Context(), root, tfm, nil, 0, 100,
		newTestBackend(),
	)
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}

	if len(orphans) != 2 {
		t.Fatalf("expected 2 orphan units (one per hardlink path), got %d", len(orphans))
	}
}
