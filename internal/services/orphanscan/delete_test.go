// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeDeleteFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap()

	disp, err := safeDeleteFile(root, target, tfm)
	if err != nil {
		t.Fatalf("safeDeleteFile error: %v", err)
	}
	if disp != deleteDispositionDeleted {
		t.Fatalf("expected deleted disposition, got %v", disp)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err=%v", err)
	}
}

func TestSafeDeleteFile_SkipsWhenInUse(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap()
	tfm.Add(normalizePath(target))

	disp, err := safeDeleteFile(root, target, tfm)
	if err != nil {
		t.Fatalf("safeDeleteFile error: %v", err)
	}
	if disp != deleteDispositionSkippedInUse {
		t.Fatalf("expected skipped-in-use disposition, got %v", disp)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected file to remain, stat err=%v", err)
	}
}

func TestSafeDeleteFile_RefusesScanRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tfm := NewTorrentFileMap()

	if _, err := safeDeleteFile(root, root, tfm); err == nil {
		t.Fatalf("expected error deleting scan root")
	}
}

func TestSafeDeleteFile_RefusesEscapingPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tfm := NewTorrentFileMap()

	outside := filepath.Join(root, "..", "escape.txt")
	if _, err := safeDeleteFile(root, outside, tfm); err == nil {
		t.Fatalf("expected error for path escaping scan root")
	}
}

func TestSafeDeleteTarget_DeletesDirectoryRecursively(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	discDir := filepath.Join(root, "Movie.2024", "BDMV", "STREAM")
	if err := os.MkdirAll(discDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fileA := filepath.Join(root, "Movie.2024", "BDMV", "index.bdmv")
	fileB := filepath.Join(discDir, "00000.m2ts")
	if err := os.WriteFile(fileA, []byte("a"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap()

	unit := filepath.Join(root, "Movie.2024")
	disp, err := safeDeleteTarget(root, unit, tfm)
	if err != nil {
		t.Fatalf("safeDeleteTarget error: %v", err)
	}
	if disp != deleteDispositionDeleted {
		t.Fatalf("expected deleted disposition, got %v", disp)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("expected directory removed, stat err=%v", err)
	}
}

func TestSafeDeleteTarget_SkipsDirectoryWhenAnyFileInUse(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "Movie.2024", "BDMV")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fileInUse := filepath.Join(dir, "index.bdmv")
	fileOther := filepath.Join(dir, "STREAM", "00000.m2ts")
	if err := os.MkdirAll(filepath.Dir(fileOther), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fileInUse, []byte("a"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(fileOther, []byte("b"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap()
	tfm.Add(normalizePath(fileInUse))

	unit := filepath.Join(root, "Movie.2024")
	disp, err := safeDeleteTarget(root, unit, tfm)
	if err != nil {
		t.Fatalf("safeDeleteTarget error: %v", err)
	}
	if disp != deleteDispositionSkippedInUse {
		t.Fatalf("expected skipped-in-use disposition, got %v", disp)
	}
	if _, err := os.Stat(fileInUse); err != nil {
		t.Fatalf("expected in-use file to remain, stat err=%v", err)
	}
}

func TestSafeDeleteTarget_DeletingMarkerDirDoesNotDeleteSiblingFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	movieDir := filepath.Join(root, "Movie.2024")
	bdmvDir := filepath.Join(movieDir, "BDMV", "STREAM")
	if err := os.MkdirAll(bdmvDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Disc content.
	fileA := filepath.Join(movieDir, "BDMV", "index.bdmv")
	fileB := filepath.Join(bdmvDir, "00000.m2ts")
	if err := os.WriteFile(fileA, []byte("a"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Sibling content in the parent folder.
	sibling := filepath.Join(movieDir, "readme.txt")
	if err := os.WriteFile(sibling, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap()
	markerUnit := filepath.Join(movieDir, "BDMV")
	disp, err := safeDeleteTarget(root, markerUnit, tfm)
	if err != nil {
		t.Fatalf("safeDeleteTarget error: %v", err)
	}
	if disp != deleteDispositionDeleted {
		t.Fatalf("expected deleted disposition, got %v", disp)
	}
	if _, err := os.Stat(markerUnit); !os.IsNotExist(err) {
		t.Fatalf("expected marker directory removed, stat err=%v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("expected sibling file to remain, stat err=%v", err)
	}
}

func TestCollectCandidateDirsForCleanup_CascadesToParents(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	scanRoot := filepath.Join(base, "tv")
	showDir := filepath.Join(scanRoot, "ShowName")
	seasonDir := filepath.Join(showDir, "Season1")

	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	target := filepath.Join(seasonDir, "episode.mkv")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tfm := NewTorrentFileMap()
	disp, err := safeDeleteFile(scanRoot, target, tfm)
	if err != nil {
		t.Fatalf("safeDeleteFile error: %v", err)
	}
	if disp != deleteDispositionDeleted {
		t.Fatalf("expected deleted disposition, got %v", disp)
	}

	candidates := collectCandidateDirsForCleanup([]string{target}, []string{scanRoot}, nil)
	for _, dir := range candidates {
		_ = safeDeleteEmptyDir(scanRoot, dir)
	}

	if _, err := os.Stat(seasonDir); !os.IsNotExist(err) {
		t.Fatalf("expected season dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(showDir); !os.IsNotExist(err) {
		t.Fatalf("expected show dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(scanRoot); err != nil {
		t.Fatalf("expected scan root to remain, stat err=%v", err)
	}
}

func TestFindScanRoot_PrefersLongestMatch(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	rootA := filepath.Clean(base)
	rootB := filepath.Join(base, "tv")

	path := filepath.Join(rootB, "ShowName", "Season1", "episode.mkv")

	got := findScanRoot(path, []string{rootA, rootB})
	if filepath.Clean(got) != filepath.Clean(rootB) {
		t.Fatalf("expected longest root %q, got %q", rootB, got)
	}
}

func TestCollectCandidateDirsForCleanup_StopsAtNestedScanRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	rootA := filepath.Join(base, "tv")
	rootB := filepath.Join(rootA, "ShowName")
	target := filepath.Join(rootB, "Season1", "episode.mkv")

	candidates := collectCandidateDirsForCleanup([]string{target}, []string{rootA, rootB}, nil)
	for _, dir := range candidates {
		if filepath.Clean(dir) == filepath.Clean(rootB) {
			t.Fatalf("did not expect nested scan root in candidates: %q", dir)
		}
	}
}
