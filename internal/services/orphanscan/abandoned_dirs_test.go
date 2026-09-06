// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

// mkdirs creates each relative directory under root and returns root.
func mkdirs(t *testing.T, root string, rel ...string) string {
	t.Helper()
	for _, r := range rel {
		if err := os.MkdirAll(filepath.Join(root, r), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", r, err)
		}
	}
	return root
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// abandonedPaths runs a walk over root and returns the directories the
// abandoned-directory pass would remove.
func abandonedPaths(t *testing.T, root string, categoryPaths []string, ignorePaths []string) []string {
	t.Helper()

	backend := newTestBackend()
	_, dirs, _, err := walkScanRootCollectingDirs(context.Background(), root, NewTorrentFileMap(), ignorePaths, 0, 0, backend)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	candidates := abandonedDirCandidates(context.Background(), sortDeepestFirst(dirs), []string{root}, ignorePaths, categoryPaths, 0, backend)
	paths := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if !c.IsDir {
			t.Fatalf("candidate %q is not marked as a directory", c.Path)
		}
		paths = append(paths, c.Path)
	}
	return paths
}

func TestAbandonedDirs_EmptyTreeIsRemovedDeepestFirst(t *testing.T) {
	t.Parallel()

	root := mkdirs(t, t.TempDir(), filepath.Join("left", "deep", "deeper"), "right")

	got := abandonedPaths(t, root, nil, nil)

	want := []string{
		filepath.Join(root, "left"),
		filepath.Join(root, "left", "deep"),
		filepath.Join(root, "left", "deep", "deeper"),
		filepath.Join(root, "right"),
	}
	sorted := slices.Clone(got)
	slices.Sort(sorted)
	if !slices.Equal(sorted, want) {
		t.Fatalf("abandoned dirs = %v, want %v", sorted, want)
	}

	// Order is the contract deletion relies on: a directory is always listed
	// before the one that contains it, so each is empty when its turn comes.
	for i, dir := range got {
		for _, earlier := range got[:i] {
			if isPathUnderNormalized(normalizePath(dir), normalizePath(earlier)) {
				t.Fatalf("%q is listed after its own parent %q", dir, earlier)
			}
		}
	}
}

func TestAbandonedDirs_DirectoryHoldingAFileIsKept(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// A file anywhere below keeps the whole chain, even though the file itself
	// is reported separately as an orphan.
	writeFile(t, filepath.Join(root, "keep", "nested", "payload.mkv"))
	mkdirs(t, root, "gone")

	got := abandonedPaths(t, root, nil, nil)

	if !slices.Equal(got, []string{filepath.Join(root, "gone")}) {
		t.Fatalf("abandoned dirs = %v, want only the file-free directory", got)
	}
}

func TestAbandonedDirs_ScanRootIsNeverRemoved(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	if got := abandonedPaths(t, root, nil, nil); len(got) != 0 {
		t.Fatalf("an empty scan root must be kept, got %v", got)
	}
}

func TestAbandonedDirs_CategoryDestinationsAreProtected(t *testing.T) {
	t.Parallel()

	root := mkdirs(t, t.TempDir(), "movies", filepath.Join("movies", "staging"), "junk")
	category := filepath.Join(root, "movies")

	got := abandonedPaths(t, root, []string{category}, nil)

	// The category folder itself stays. So does anything above it, and the
	// staging directory inside it is fair game only because it is not the
	// destination qBittorrent writes to.
	if slices.Contains(got, category) {
		t.Fatalf("category destination %q must not be removed: %v", category, got)
	}
	if !slices.Contains(got, filepath.Join(root, "junk")) {
		t.Fatalf("unreferenced directory should still be reported: %v", got)
	}
}

func TestAbandonedDirs_ParentOfACategoryIsProtected(t *testing.T) {
	t.Parallel()

	root := mkdirs(t, t.TempDir(), filepath.Join("tv", "shows"))
	category := filepath.Join(root, "tv", "shows")

	got := abandonedPaths(t, root, []string{category}, nil)

	if len(got) != 0 {
		t.Fatalf("nothing above a category destination may be removed, got %v", got)
	}
}

func TestAbandonedDirs_IgnoredPathsAreProtected(t *testing.T) {
	t.Parallel()

	root := mkdirs(t, t.TempDir(), "preserve", "junk")
	ignored := filepath.Join(root, "preserve")

	got := abandonedPaths(t, root, nil, []string{ignored})

	if slices.Contains(got, ignored) {
		t.Fatalf("ignored path %q must not be removed: %v", ignored, got)
	}
	if !slices.Contains(got, filepath.Join(root, "junk")) {
		t.Fatalf("unreferenced directory should still be reported: %v", got)
	}
}

func TestAbandonedDirs_GracePeriodHoldsFreshDirectories(t *testing.T) {
	t.Parallel()

	root := mkdirs(t, t.TempDir(), "fresh")
	backend := newTestBackend()

	_, dirs, _, err := walkScanRootCollectingDirs(context.Background(), root, NewTorrentFileMap(), nil, 0, 0, backend)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	got := abandonedDirCandidates(context.Background(), sortDeepestFirst(dirs), []string{root}, nil, nil, time.Hour, backend)
	if len(got) != 0 {
		t.Fatalf("a directory younger than the grace period must be held, got %v", got)
	}
}

func TestCategoryPaths_ExplicitAndImplicitDestinations(t *testing.T) {
	t.Parallel()

	defaultSavePath := filepath.Join(string(filepath.Separator), "data", "torrents")
	explicit := filepath.Join(string(filepath.Separator), "mnt", "elsewhere", "films")

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
	svc.getCategoriesProvider = func(_ context.Context, _ int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{
			"movies": {Name: "movies", SavePath: explicit},
			// qBittorrent reports an empty save path for a category that
			// inherits the default; it still lands in defaultSavePath/<name>.
			"tv": {Name: "tv", SavePath: ""},
		}, nil
	}

	got, err := svc.categoryPaths(context.Background(), 1, defaultSavePath)
	if err != nil {
		t.Fatalf("categoryPaths: %v", err)
	}

	want := []string{explicit, filepath.Join(defaultSavePath, "tv")}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("categoryPaths = %v, want %v", got, want)
	}
}
