// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWalkScanRoot_CollapsesDiscLayoutIntoSingleOrphanUnit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Create a Blu-ray style disc folder under a movie directory.
	movieDir := filepath.Join(root, "Movie.2024")
	bdmvDir := filepath.Join(movieDir, "BDMV")
	streamDir := filepath.Join(bdmvDir, "STREAM")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Multiple files in disc layout should collapse to one orphan.
	paths := []string{
		filepath.Join(bdmvDir, "index.bdmv"),
		filepath.Join(streamDir, "00000.m2ts"),
		filepath.Join(streamDir, "00001.m2ts"),
	}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		// Make sure grace period does not filter these out.
		old := time.Now().Add(-2 * time.Hour)
		_ = os.Chtimes(p, old, old)
	}

	tfm := NewTorrentFileMap()
	orphans, truncated, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100)
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if truncated {
		t.Fatalf("expected not truncated")
	}
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan unit, got %d", len(orphans))
	}
	if filepath.Clean(orphans[0].Path) != filepath.Clean(movieDir) {
		t.Fatalf("expected orphan unit path %q, got %q", movieDir, orphans[0].Path)
	}
	if orphans[0].Size <= 0 {
		t.Fatalf("expected aggregated size > 0")
	}
}

func TestWalkScanRoot_DiscUnitSuppressedWhenAnyContainedFileInUse(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	movieDir := filepath.Join(root, "Movie.2024")
	bdmvDir := filepath.Join(movieDir, "BDMV")
	streamDir := filepath.Join(bdmvDir, "STREAM")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inUse := filepath.Join(bdmvDir, "index.bdmv")
	other := filepath.Join(streamDir, "00000.m2ts")
	for _, p := range []string{inUse, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		old := time.Now().Add(-2 * time.Hour)
		_ = os.Chtimes(p, old, old)
	}

	tfm := NewTorrentFileMap()
	tfm.Add(normalizePath(inUse))

	orphans, _, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100)
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("expected no orphans when disc unit contains an in-use file, got %d", len(orphans))
	}
}

func TestWalkScanRoot_UsesMarkerDirWhenMarkerIsDirectlyUnderScanRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bdmvDir := filepath.Join(root, "BDMV")
	if err := os.MkdirAll(filepath.Join(bdmvDir, "STREAM"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	p := filepath.Join(bdmvDir, "STREAM", "00000.m2ts")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(p, old, old)

	tfm := NewTorrentFileMap()
	orphans, _, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100)
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(orphans))
	}
	if filepath.Clean(orphans[0].Path) != filepath.Clean(bdmvDir) {
		t.Fatalf("expected orphan unit path %q, got %q", bdmvDir, orphans[0].Path)
	}
}

func TestWalkScanRoot_DiscUnitFallsBackToMarkerDirWhenParentHasOtherContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	movieDir := filepath.Join(root, "Movie.2024")
	bdmvDir := filepath.Join(movieDir, "BDMV")
	streamDir := filepath.Join(bdmvDir, "STREAM")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Disc files.
	for _, p := range []string{
		filepath.Join(bdmvDir, "index.bdmv"),
		filepath.Join(streamDir, "00000.m2ts"),
	} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		old := time.Now().Add(-2 * time.Hour)
		_ = os.Chtimes(p, old, old)
	}

	// Extra sibling content in parent folder means we should not delete the parent as a unit.
	extra := filepath.Join(movieDir, "readme.txt")
	if err := os.WriteFile(extra, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(extra, old, old)

	tfm := NewTorrentFileMap()
	orphans, truncated, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100)
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if truncated {
		t.Fatalf("expected not truncated")
	}
	if len(orphans) != 2 {
		t.Fatalf("expected 2 orphan units (marker dir + extra file), got %d", len(orphans))
	}

	// Confirm the disc unit is the marker directory, not the movieDir.
	expectedDiscUnit := filepath.Clean(bdmvDir)
	foundDiscUnit := false
	for _, o := range orphans {
		if filepath.Clean(o.Path) == expectedDiscUnit {
			foundDiscUnit = true
			break
		}
	}
	if !foundDiscUnit {
		t.Fatalf("expected disc unit %q in orphans, got %+v", expectedDiscUnit, orphans)
	}
}
