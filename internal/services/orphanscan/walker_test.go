// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
)

const orphanFileName = "orphan.txt"

type cancelAwareWalkBackend struct {
	fsops.Backend
	err error
}

type statFailureWalkBackend struct {
	fsops.Backend
	failedPath string
	entries    []fsops.WalkEntry
}

func (b *statFailureWalkBackend) WalkDir(_ context.Context, _ string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	entries := b.entries
	if opts.EmitStatErrors {
		entries = append([]fsops.WalkEntry{{Path: b.failedPath, StatErr: errors.New("metadata unavailable")}}, entries...)
	}

	ch := make(chan fsops.WalkEntry, len(entries))
	for _, entry := range entries {
		ch <- entry
	}
	close(ch)
	return ch, nil
}

func (b *cancelAwareWalkBackend) WalkDir(ctx context.Context, _ string, _ fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	ch := make(chan fsops.WalkEntry, 1)
	ch <- fsops.WalkEntry{Err: b.err}

	go func() {
		<-ctx.Done()
		close(ch)
	}()

	return ch, nil
}

func TestWalkScanRoot_CancelsProducerBeforeDrainingOnEntryError(t *testing.T) {
	walkErr := errors.New("walk entry failed")
	backend := &cancelAwareWalkBackend{
		Backend: newTestBackend(),
		err:     walkErr,
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	root := t.TempDir()

	result := make(chan error, 1)
	go func() {
		_, _, err := walkScanRoot(ctx, root, NewTorrentFileMap(), nil, 0, 0, backend)
		result <- err
	}()

	select {
	case err := <-result:
		if !errors.Is(err, walkErr) {
			t.Fatalf("walkScanRoot error = %v, want %v", err, walkErr)
		}
	case <-time.After(time.Second):
		cancel()
		err := <-result
		t.Fatalf("walk remained blocked until the parent context was canceled: %v", err)
	}
}

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
	orphans, truncated, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
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

	orphans, _, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("expected no orphans when disc unit contains an in-use file, got %d", len(orphans))
	}
}

func TestWalkScanRoot_DiscUnitSuppressedWhenOwnedFileMetadataFails(t *testing.T) {
	root := t.TempDir()
	movieDir := filepath.Join(root, "Movie.2024")
	bdmvDir := filepath.Join(movieDir, "BDMV")
	streamDir := filepath.Join(bdmvDir, "STREAM")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatal(err)
	}

	inUse := filepath.Join(bdmvDir, "index.bdmv")
	sibling := filepath.Join(streamDir, "00000.m2ts")
	for _, path := range []string{inUse, sibling} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	tfm := NewTorrentFileMap()
	tfm.Add(inUse)
	backend := &statFailureWalkBackend{
		Backend:    newTestBackend(),
		failedPath: inUse,
		entries: []fsops.WalkEntry{{
			Path:    sibling,
			Size:    1,
			ModTime: time.Now().Add(-time.Hour),
		}},
	}

	orphans, _, err := walkScanRoot(t.Context(), root, tfm, nil, 0, 100, backend)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 {
		t.Fatalf("owned disc unit was exposed as orphan: %#v", orphans)
	}
}

func TestWalkScanRoot_SkipsUnownedFileWhenMetadataFails(t *testing.T) {
	root := t.TempDir()
	backend := &statFailureWalkBackend{
		Backend:    newTestBackend(),
		failedPath: filepath.Join(root, "unreadable.mkv"),
	}

	orphans, _, err := walkScanRoot(t.Context(), root, NewTorrentFileMap(), nil, 0, 100, backend)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 {
		t.Fatalf("metadata failure was exposed as orphan: %#v", orphans)
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
	orphans, _, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
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

func TestWalkScanRoot_DiscUnitUsesParentWhenSiblingContentNotInUse(t *testing.T) {
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

	// Extra sibling content that is NOT referenced by any torrent should not prevent deleting
	// the parent as a single unit.
	extra := filepath.Join(movieDir, "readme.txt")
	if err := os.WriteFile(extra, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(extra, old, old)

	tfm := NewTorrentFileMap()
	orphans, truncated, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if truncated {
		t.Fatalf("expected not truncated")
	}
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan unit (parent folder), got %d", len(orphans))
	}
	if filepath.Clean(orphans[0].Path) != filepath.Clean(movieDir) {
		t.Fatalf("expected orphan unit path %q, got %q", movieDir, orphans[0].Path)
	}
}

func TestWalkScanRoot_DiscUnitFallsBackToMarkerDirWhenSiblingContentInUse(t *testing.T) {
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

	// Sibling content exists and is referenced by a torrent => unsafe to delete parent.
	extra := filepath.Join(movieDir, "readme.txt")
	if err := os.WriteFile(extra, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(extra, old, old)

	tfm := NewTorrentFileMap()
	tfm.Add(normalizePath(extra))

	orphans, truncated, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if truncated {
		t.Fatalf("expected not truncated")
	}
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan unit (marker dir), got %d", len(orphans))
	}
	if filepath.Clean(orphans[0].Path) != filepath.Clean(bdmvDir) {
		t.Fatalf("expected orphan unit path %q, got %q", bdmvDir, orphans[0].Path)
	}
}

func TestWalkScanRoot_IgnoresFuseHiddenFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	fuseHidden := filepath.Join(root, ".fuse_hidden0005c2cb00000025")
	if err := os.WriteFile(fuseHidden, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	normal := filepath.Join(root, orphanFileName)
	if err := os.WriteFile(normal, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(fuseHidden, old, old)
	_ = os.Chtimes(normal, old, old)

	tfm := NewTorrentFileMap()
	orphans, truncated, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if truncated {
		t.Fatalf("expected not truncated")
	}

	paths := make([]string, 0, len(orphans))
	for _, o := range orphans {
		paths = append(paths, filepath.Base(o.Path))
	}
	for _, p := range paths {
		if strings.HasPrefix(p, ".fuse") {
			t.Fatalf("expected .fuse* to be ignored, got orphan %q", p)
		}
	}
	foundNormal := false
	for _, p := range paths {
		if p == orphanFileName {
			foundNormal = true
		}
	}
	if !foundNormal {
		t.Fatalf("expected orphan.txt to be included, got %v", paths)
	}
}

func TestWalkScanRoot_IgnoresPartsFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	partsFile := filepath.Join(root, "movie.mkv.parts")
	if err := os.WriteFile(partsFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	normal := filepath.Join(root, orphanFileName)
	if err := os.WriteFile(normal, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(partsFile, old, old)
	_ = os.Chtimes(normal, old, old)

	tfm := NewTorrentFileMap()
	orphans, truncated, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if truncated {
		t.Fatalf("expected not truncated")
	}

	paths := make([]string, 0, len(orphans))
	for _, o := range orphans {
		paths = append(paths, filepath.Base(o.Path))
	}
	for _, p := range paths {
		if strings.HasSuffix(p, ".parts") {
			t.Fatalf("expected *.parts to be ignored, got orphan %q", p)
		}
	}
	foundNormal := false
	for _, p := range paths {
		if p == orphanFileName {
			foundNormal = true
		}
	}
	if !foundNormal {
		t.Fatalf("expected orphan.txt to be included, got %v", paths)
	}
}

func writeOldFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(path, old, old)
}

func orphanPaths(orphans []OrphanFile) []string {
	paths := make([]string, 0, len(orphans))
	for _, o := range orphans {
		paths = append(paths, normalizePath(o.Path))
	}
	return paths
}

func TestWalkScanRoot_IgnoresTrashDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	inTrash := filepath.Join(root, ".Trash-1000", "trash.txt")
	writeOldFile(t, inTrash)

	normal := filepath.Join(root, orphanFileName)
	writeOldFile(t, normal)

	tfm := NewTorrentFileMap()
	orphans, truncated, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if truncated {
		t.Fatalf("expected not truncated")
	}

	paths := orphanPaths(orphans)
	for _, p := range paths {
		if strings.Contains(p, normalizePath(filepath.Join(root, ".Trash-1000"))) {
			t.Fatalf("expected .Trash-* to be ignored, got orphan %q", p)
		}
	}

	foundNormal := false
	for _, p := range paths {
		if filepath.Base(p) == orphanFileName {
			foundNormal = true
		}
	}
	if !foundNormal {
		t.Fatalf("expected orphan.txt to be included, got %v", paths)
	}
}

func TestWalkScanRoot_IgnoresKubernetesInternalDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	inKube := filepath.Join(root, "..data", "kube.txt")
	writeOldFile(t, inKube)

	normal := filepath.Join(root, orphanFileName)
	writeOldFile(t, normal)

	tfm := NewTorrentFileMap()
	orphans, truncated, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if truncated {
		t.Fatalf("expected not truncated")
	}

	paths := orphanPaths(orphans)
	for _, p := range paths {
		if strings.Contains(p, normalizePath(filepath.Join(root, "..data"))) {
			t.Fatalf("expected k8s internal dirs to be ignored, got orphan %q", p)
		}
	}

	foundNormal := false
	for _, p := range paths {
		if filepath.Base(p) == orphanFileName {
			foundNormal = true
		}
	}
	if !foundNormal {
		t.Fatalf("expected orphan.txt to be included, got %v", paths)
	}
}

func TestWalkScanRoot_IgnorePathSiblingPreventsParentDiscUnit(t *testing.T) {
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

	// Sibling content that is protected by ignore paths.
	extra := filepath.Join(movieDir, "readme.txt")
	if err := os.WriteFile(extra, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(extra, old, old)

	// Ignore the sibling file - this should prevent parent unit selection.
	ignorePaths := []string{extra}

	tfm := NewTorrentFileMap()
	orphans, truncated, err := walkScanRoot(context.Background(), root, tfm, ignorePaths, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if truncated {
		t.Fatalf("expected not truncated")
	}
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan unit (marker dir fallback), got %d", len(orphans))
	}
	// Should use marker dir, not parent, because parent contains ignored sibling.
	if filepath.Clean(orphans[0].Path) != filepath.Clean(bdmvDir) {
		t.Fatalf("expected orphan unit path %q, got %q", bdmvDir, orphans[0].Path)
	}
}

func TestWalkScanRoot_IgnorePathInsideMarkerDisablesDiscGrouping(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	movieDir := filepath.Join(root, "Movie.2024")
	bdmvDir := filepath.Join(movieDir, "BDMV")
	streamDir := filepath.Join(bdmvDir, "STREAM")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Disc files.
	fileA := filepath.Join(bdmvDir, "index.bdmv")
	fileB := filepath.Join(streamDir, "00000.m2ts")
	for _, p := range []string{fileA, fileB} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		old := time.Now().Add(-2 * time.Hour)
		_ = os.Chtimes(p, old, old)
	}

	// Ignore a file inside the marker directory - this should disable disc-unit grouping entirely.
	ignorePaths := []string{fileA}

	tfm := NewTorrentFileMap()
	orphans, _, err := walkScanRoot(context.Background(), root, tfm, ignorePaths, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	// Should have individual file orphans, not a disc unit.
	// Only fileB should be reported (fileA is ignored).
	if len(orphans) != 1 {
		t.Fatalf("expected 1 individual file orphan, got %d", len(orphans))
	}
	if filepath.Clean(orphans[0].Path) != filepath.Clean(fileB) {
		t.Fatalf("expected orphan path %q, got %q", fileB, orphans[0].Path)
	}
}

func TestWalkScanRoot_IgnorePathInsideMarkerMultipleFiles(t *testing.T) {
	// Regression test: when disc grouping is disabled due to ignore paths,
	// multiple non-ignored files should each be reported as individual orphans,
	// not collapsed into a single unit.
	t.Parallel()

	root := t.TempDir()
	movieDir := filepath.Join(root, "Movie.2024")
	bdmvDir := filepath.Join(movieDir, "BDMV")
	streamDir := filepath.Join(bdmvDir, "STREAM")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Multiple disc files.
	fileIgnored := filepath.Join(bdmvDir, "index.bdmv")
	fileA := filepath.Join(streamDir, "00000.m2ts")
	fileB := filepath.Join(streamDir, "00001.m2ts")
	fileC := filepath.Join(streamDir, "00002.m2ts")
	for _, p := range []string{fileIgnored, fileA, fileB, fileC} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		old := time.Now().Add(-2 * time.Hour)
		_ = os.Chtimes(p, old, old)
	}

	// Ignore one file inside marker - should disable disc-unit grouping for all files.
	ignorePaths := []string{fileIgnored}

	tfm := NewTorrentFileMap()
	orphans, _, err := walkScanRoot(context.Background(), root, tfm, ignorePaths, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	// Should have 3 individual file orphans (fileA, fileB, fileC), not 1 disc unit.
	if len(orphans) != 3 {
		t.Fatalf("expected 3 individual file orphans, got %d", len(orphans))
	}
	// Verify each orphan is an individual file path, not a directory.
	orphanPaths := make(map[string]bool)
	for _, o := range orphans {
		orphanPaths[filepath.Clean(o.Path)] = true
	}
	for _, expected := range []string{fileA, fileB, fileC} {
		if !orphanPaths[filepath.Clean(expected)] {
			t.Errorf("expected orphan path %q not found", expected)
		}
	}
}

func TestWalkScanRoot_MixedCaseMarkerOnDisk(t *testing.T) {
	t.Parallel()

	// This test ensures the actual on-disk marker casing is preserved.
	root := t.TempDir()
	movieDir := filepath.Join(root, "Movie.2024")
	// Use lowercase marker directory name.
	bdmvDir := filepath.Join(movieDir, "bdmv")
	streamDir := filepath.Join(bdmvDir, "STREAM")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fileA := filepath.Join(bdmvDir, "index.bdmv")
	if err := os.WriteFile(fileA, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(fileA, old, old)

	tfm := NewTorrentFileMap()
	orphans, _, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan unit, got %d", len(orphans))
	}
	// The returned unit path should match the actual on-disk directory.
	expectedUnit := movieDir
	gotUnit := filepath.Clean(orphans[0].Path)
	// On case-sensitive systems, exact match. On case-insensitive (Windows/macOS), normalizePath makes them equal.
	if normalizePath(gotUnit) != normalizePath(expectedUnit) {
		t.Fatalf("expected orphan unit path %q (normalized), got %q (normalized)", normalizePath(expectedUnit), normalizePath(gotUnit))
	}
	// Verify the marker directory exists with the actual on-disk casing.
	if _, err := os.Stat(bdmvDir); err != nil {
		t.Fatalf("expected marker directory %q to exist: %v", bdmvDir, err)
	}
}

func TestWalkScanRoot_MixedCaseMarkerDirectlyUnderScanRoot(t *testing.T) {
	// This test validates that when the marker is directly under scan root,
	// the returned path uses the actual on-disk casing (not uppercase BDMV).
	t.Parallel()

	root := t.TempDir()
	// Use lowercase marker directory name directly under scan root.
	bdmvDir := filepath.Join(root, "bdmv")
	streamDir := filepath.Join(bdmvDir, "STREAM")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fileA := filepath.Join(bdmvDir, "index.bdmv")
	if err := os.WriteFile(fileA, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(fileA, old, old)

	tfm := NewTorrentFileMap()
	orphans, _, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan unit, got %d", len(orphans))
	}
	// The returned unit path should be the marker directory with actual on-disk casing.
	gotUnit := filepath.Clean(orphans[0].Path)
	// Verify it matches the exact on-disk path (lowercase "bdmv").
	if gotUnit != filepath.Clean(bdmvDir) {
		t.Fatalf("expected orphan unit path %q (exact casing), got %q", bdmvDir, gotUnit)
	}
}

func TestWalkScanRoot_UnicodeCanonicalEquivalenceDoesNotFalseOrphan(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Decomposed form on disk; composed form in qBittorrent file list (or vice versa).
	dirDecomposed := filepath.Join(root, "La\u030apsley")
	if err := os.MkdirAll(dirDecomposed, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fileDecomposed := filepath.Join(dirDecomposed, "track.flac")
	if err := os.WriteFile(fileDecomposed, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(fileDecomposed, old, old)

	// Equivalent composed path string.
	fileComposed := filepath.Join(root, "Låpsley", "track.flac")
	if fileComposed == fileDecomposed {
		t.Fatalf("expected composed and decomposed paths to differ (test bug): %q", fileComposed)
	}

	tfm := NewTorrentFileMap()
	tfm.Add(fileComposed)

	orphans, _, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("expected no orphans for canonical-equivalent unicode paths, got %d: %v", len(orphans), orphans)
	}
}

// On a case-insensitive filesystem qBittorrent can report two save paths that
// differ only by case for one physical directory. The walker sees one spelling
// on disk; the owned-file map holds the other. See issue #2314.
func TestWalkScanRoot_CaseDifferenceDoesNotFalseOrphan(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	trackerDir := filepath.Join(root, "TrackerName")
	if err := os.MkdirAll(trackerDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	onDisk := filepath.Join(trackerDir, "Show.S01E01.mkv")
	if err := os.WriteFile(onDisk, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(onDisk, old, old)

	// qBittorrent reports the same file under the other casing of the same dir.
	tfm := NewTorrentFileMap()
	tfm.Add(filepath.Join(root, "trackername", "Show.S01E01.mkv"))

	orphans, _, err := walkScanRoot(context.Background(), root, tfm, nil, 0, 100, local.NewBackend())
	if err != nil {
		t.Fatalf("walkScanRoot: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("expected no orphans for case-different owned path, got %d: %v", len(orphans), orphans)
	}
}

func TestNormalizeIgnorePaths_KeepsUserCasing(t *testing.T) {
	t.Parallel()

	typed := filepath.Join(t.TempDir(), "downloads", "Keep", "Archive")

	got, err := NormalizeIgnorePaths([]string{typed})
	if err != nil {
		t.Fatalf("NormalizeIgnorePaths: %v", err)
	}
	if len(got) != 1 || got[0] != typed {
		t.Fatalf("expected ignore path stored verbatim %q, got %v", typed, got)
	}

	// Matching still folds, so the differently-cased on-disk path stays protected.
	other := filepath.Join(strings.ToLower(typed), "movie.mkv")
	if !isIgnoredPath(other, got) {
		t.Fatalf("expected %q to be ignored by %q", other, got[0])
	}
}

// An ignore path protects a directory the user typed. A file below it whose name
// carries a non-UTF-8 byte must still be protected: the prefix is folded for
// comparison, so the file path has to fold the same way or the protection
// silently does nothing and the file is deleted.
func TestIsIgnoredPath_InvalidUTF8UnderMixedCaseDir(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)
	ignore := filepath.Join(sep, "data", "downloads", "Keep")
	file := filepath.Join(ignore, "mo"+string([]byte{0xff})+"vie.mkv")

	if !isIgnoredPath(file, []string{ignore}) {
		t.Fatalf("expected file under ignored directory to be protected: %q", file)
	}
}

// Two sibling disc directories that differ only by case are two real directories
// on a case-sensitive filesystem. The decision cache must keep them apart, or the
// second directory's files are attributed to the first, and deleting the first
// takes files the second one's scan never approved.
func TestDiscUnitFromParentMarker_CaseVariantSiblingsDoNotShareCache(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)
	upper := filepath.Join(sep, "rips", "Pack")
	lower := filepath.Join(sep, "rips", "pack")
	cache := make(map[string]discUnitDecision)

	unitFor := func(dir string) string {
		unit, ok := discUnitFromParentMarker(
			context.Background(),
			filepath.Join(dir, "BDMV", "STREAM", "a.m2ts"),
			dir, filepath.Join(dir, "BDMV"), "BDMV", nil, cache, nil,
			local.NewBackend())
		if !ok {
			t.Fatalf("expected a disc unit for %q", dir)
		}
		return unit
	}

	if u, l := unitFor(upper), unitFor(lower); u == l {
		t.Fatalf("distinct disc directories must not share a unit path: %q", u)
	}
}
