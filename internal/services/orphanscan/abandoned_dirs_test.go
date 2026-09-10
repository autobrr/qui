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
		if !c.IsAbandonedDir {
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

func TestCategoryPaths_ResolvesTheWayQBittorrentDoes(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	defaultSavePath := filepath.Join(base, "torrents")
	archive := filepath.Join(base, "archive")

	tests := []struct {
		name             string
		useSubcategories bool
		categories       map[string]qbt.Category
		want             []string
	}{
		{
			name:       "absolute save path is used as-is",
			categories: map[string]qbt.Category{"movies": {Name: "movies", SavePath: archive}},
			want:       []string{archive},
		},
		{
			name: "a category with no save path lands under the default save path",
			// qBittorrent reports an empty savePath for a category that inherits.
			categories: map[string]qbt.Category{"tv": {Name: "tv", SavePath: ""}},
			want:       []string{filepath.Join(defaultSavePath, "tv")},
		},
		{
			name:       "a relative save path is taken against the default save path",
			categories: map[string]qbt.Category{"music": {Name: "music", SavePath: filepath.Join("sorted", "audio")}},
			want:       []string{filepath.Join(defaultSavePath, "sorted", "audio")},
		},
		{
			name:       "an explicit dot uses the default save path",
			categories: map[string]qbt.Category{"movies": {Name: "movies", SavePath: "."}},
			want:       []string{defaultSavePath},
		},
		{
			name:       "spaces in an explicit save path are preserved",
			categories: map[string]qbt.Category{"movies": {Name: "movies", SavePath: " archive "}},
			want:       []string{filepath.Join(defaultSavePath, " archive ")},
		},
		{
			name:             "a child inherits an explicit dot destination",
			useSubcategories: true,
			categories: map[string]qbt.Category{
				"movies":    {Name: "movies", SavePath: "."},
				"movies/hd": {Name: "movies/hd", SavePath: ""},
			},
			want: []string{defaultSavePath, filepath.Join(defaultSavePath, "hd")},
		},
		{
			name:             "an inheriting subcategory follows its parent, not the default save path",
			useSubcategories: true,
			categories: map[string]qbt.Category{
				"movies":    {Name: "movies", SavePath: archive},
				"movies/hd": {Name: "movies/hd", SavePath: ""},
			},
			want: []string{archive, filepath.Join(archive, "hd")},
		},
		{
			name:             "nested inheritance walks the whole parent chain",
			useSubcategories: true,
			categories: map[string]qbt.Category{
				"a":     {Name: "a", SavePath: archive},
				"a/b":   {Name: "a/b", SavePath: ""},
				"a/b/c": {Name: "a/b/c", SavePath: ""},
			},
			want: []string{archive, filepath.Join(archive, "b"), filepath.Join(archive, "b", "c")},
		},
		{
			name: "a name qBittorrent cannot use as a directory is converted",
			// qBittorrent accepts "movies:hd" and creates "movies hd" for it.
			categories: map[string]qbt.Category{"movies:hd": {Name: "movies:hd", SavePath: ""}},
			want:       []string{filepath.Join(defaultSavePath, "movies hd")},
		},
		{
			name: "a run of invalid characters collapses into one space",
			// Utils::Fs::toValidPath replaces [:?"*<>|]+ , so the run is one pad.
			categories: map[string]qbt.Category{`films:?"x`: {Name: `films:?"x`}},
			want:       []string{filepath.Join(defaultSavePath, "films x")},
		},
		{
			name:       "a backslash is left alone, since toValidPath does not replace it",
			categories: map[string]qbt.Category{`a\b`: {Name: `a\b`}},
			want:       []string{filepath.Join(defaultSavePath, `a\b`)},
		},
		{
			name:             "only the last segment is converted when nesting",
			useSubcategories: true,
			categories: map[string]qbt.Category{
				"movies":     {Name: "movies", SavePath: archive},
				"movies/h:d": {Name: "movies/h:d"},
			},
			want: []string{archive, filepath.Join(archive, "h d")},
		},
		{
			name:             "with subcategories off the whole name is a path under the default save path",
			useSubcategories: false,
			categories: map[string]qbt.Category{
				"movies":    {Name: "movies", SavePath: archive},
				"movies/hd": {Name: "movies/hd", SavePath: ""},
			},
			want: []string{archive, filepath.Join(defaultSavePath, "movies", "hd")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
			svc.getCategoriesProvider = func(_ context.Context, _ int) (map[string]qbt.Category, error) {
				return tt.categories, nil
			}

			got, err := svc.categoryPaths(context.Background(), 1, defaultSavePath, tt.useSubcategories)
			if err != nil {
				t.Fatalf("categoryPaths: %v", err)
			}

			want := slices.Clone(tt.want)
			slices.Sort(want)
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Fatalf("categoryPaths = %v, want %v", got, want)
			}
		})
	}
}

// TestDeclaredScanRoots_FollowsTheEffectiveSubcategoryState covers both nesting
// states through the one accessor. Reading the use_subcategories preference
// directly would be wrong on qBittorrent 5.2, which dropped it and always
// inherits through the parent, so the absent field decodes as false.
func TestDeclaredScanRoots_FollowsTheEffectiveSubcategoryState(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	defaultSavePath := filepath.Join(base, "torrents")
	archive := filepath.Join(base, "archive")

	for _, tc := range []struct {
		name                 string
		subcategoriesEnabled bool
		want                 string
	}{
		{name: "nesting enabled follows the parent category", subcategoriesEnabled: true, want: filepath.Join(archive, "hd")},
		{name: "nesting disabled keeps the whole name under the default save path", want: filepath.Join(defaultSavePath, "movies", "hd")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
			svc.subcategoriesEnabledProvider = func(_ context.Context, _ int) (bool, error) {
				return tc.subcategoriesEnabled, nil
			}
			svc.getAppPreferencesProvider = func(_ context.Context, _ int) (qbt.AppPreferences, error) {
				return qbt.AppPreferences{SavePath: defaultSavePath}, nil
			}
			svc.getCategoriesProvider = func(_ context.Context, _ int) (map[string]qbt.Category, error) {
				return map[string]qbt.Category{
					"movies":    {Name: "movies", SavePath: archive},
					"movies/hd": {Name: "movies/hd", SavePath: ""},
				}, nil
			}

			_, protected, err := svc.declaredScanRoots(context.Background(), 1, scanScope{AbandonedDirs: true})
			if err != nil {
				t.Fatalf("declaredScanRoots: %v", err)
			}
			if !slices.Contains(protected, tc.want) {
				t.Fatalf("protected = %v, want %q", protected, tc.want)
			}
		})
	}
}

// TestAbandonedDirs_DirectoryHoldingASymlinkIsKept covers the promise that a
// directory is only removed when everything in it is also going. The walk skips
// symlinks, so a directory holding nothing else looks file-free; childrenAllKept
// is what stops it being reported.
func TestAbandonedDirs_DirectoryHoldingASymlinkIsKept(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	root := mkdirs(t, filepath.Join(base, "root"), "withlink", "junk")

	target := filepath.Join(base, "outside.txt")
	writeFile(t, target)
	withLink := filepath.Join(root, "withlink")
	if err := os.Symlink(target, filepath.Join(withLink, "link.txt")); err != nil {
		t.Skipf("symlinks are unavailable on this host: %v", err)
	}

	got := abandonedPaths(t, root, nil, nil)

	if slices.Contains(got, withLink) {
		t.Fatalf("a directory holding a symlink must be kept: %v", got)
	}
	if !slices.Contains(got, filepath.Join(root, "junk")) {
		t.Fatalf("a genuinely empty directory should still be reported: %v", got)
	}
}
