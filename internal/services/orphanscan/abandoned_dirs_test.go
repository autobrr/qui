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

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type directoryReadCounter struct {
	fsops.Backend
	reads int
}

func (b *directoryReadCounter) ReadDir(ctx context.Context, path string) ([]fsops.DirEntry, error) {
	b.reads++
	return b.Backend.ReadDir(ctx, path)
}

func TestAbandonedDirs_SkipsReadsForKnownKeptFiles(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		freshName string
		disc      bool
		wantReads int
		wantDirs  int
	}{
		{name: "orphan only", wantReads: 1, wantDirs: 1},
		{name: "fresh file before orphan", freshName: "a-fresh.mkv"},
		{name: "fresh file after orphan", freshName: "z-fresh.mkv"},
		{name: "disc count still requires reread", freshName: "z-fresh.mkv", disc: true, wantReads: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			dir := filepath.Join(root, "candidate")
			orphan := filepath.Join(dir, "orphan.mkv")
			if tt.disc {
				orphan = filepath.Join(dir, "disc", "BDMV", "STREAM", "00001.m2ts")
			}
			writeFile(t, orphan)
			backdate(t, orphan)
			if tt.freshName != "" {
				writeFile(t, filepath.Join(dir, tt.freshName))
			}
			backdate(t, dir)

			backend := &directoryReadCounter{Backend: newTestBackend()}
			orphans, dirs, err := walkScanRootCollectingDirs(t.Context(), root, newTestProtected(t, fsops.HostPaths, nil, NewTorrentFileMap(fsops.HostPaths)), time.Hour, backend, nil)
			if err != nil {
				t.Fatal(err)
			}
			backend.reads = 0
			got := abandonedDirCandidates(t.Context(), sortDeepestFirst(dirs), orphans, []string{root}, nil, newTestProtected(t, fsops.HostPaths, nil, nil), time.Hour, backend)
			if len(got) != tt.wantDirs || backend.reads != tt.wantReads {
				t.Fatalf("directories = %v, reads = %d; want %d directories and %d reads", got, backend.reads, tt.wantDirs, tt.wantReads)
			}
		})
	}
}

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
	orphans, dirs, err := walkScanRootCollectingDirs(context.Background(), root, newTestProtected(t, fsops.HostPaths, ignorePaths, NewTorrentFileMap(fsops.HostPaths)), 0, backend, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	candidates := abandonedDirCandidates(context.Background(), sortDeepestFirst(dirs), orphans, []string{root}, categoryPaths, newTestProtected(t, fsops.HostPaths, ignorePaths, nil), 0, backend)
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
			if isPathUnderNormalized(fsops.HostPaths, fsops.NormalizePath(fsops.HostPaths, dir), fsops.NormalizePath(fsops.HostPaths, earlier)) {
				t.Fatalf("%q is listed after its own parent %q", dir, earlier)
			}
		}
	}
}

func TestAbandonedDirs_DirectoryHoldingOnlyOrphansIsRemoved(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Nothing owns payload.mkv, so deleting it empties both directories above.
	writeFile(t, filepath.Join(root, "show", "season", "payload.mkv"))
	mkdirs(t, root, "gone")

	got := abandonedPaths(t, root, nil, nil)

	want := []string{
		filepath.Join(root, "gone"),
		filepath.Join(root, "show"),
		filepath.Join(root, "show", "season"),
	}
	sorted := slices.Clone(got)
	slices.Sort(sorted)
	if !slices.Equal(sorted, want) {
		t.Fatalf("abandoned dirs = %v, want %v", sorted, want)
	}
}

func TestAbandonedDirs_DirectoryHoldingAKeptFileIsKept(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	owned := filepath.Join(root, "keep", "nested", "payload.mkv")
	writeFile(t, owned)
	mkdirs(t, root, "gone")

	backend := newTestBackend()
	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(fsops.NormalizePath(fsops.HostPaths, owned))

	orphans, dirs, err := walkScanRootCollectingDirs(context.Background(), root, newTestProtected(t, fsops.HostPaths, nil, tfm), 0, backend, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	got := make([]string, 0, len(dirs))
	for _, c := range abandonedDirCandidates(context.Background(), sortDeepestFirst(dirs), orphans, []string{root}, nil, newTestProtected(t, fsops.HostPaths, nil, nil), 0, backend) {
		got = append(got, c.Path)
	}

	if !slices.Equal(got, []string{filepath.Join(root, "gone")}) {
		t.Fatalf("abandoned dirs = %v, want only the directory the run empties", got)
	}
}

// TestAbandonedDirs_DiscUnitInternalsFollowTheUnit covers an empty directory
// inside a disc layout. When the unit is deleted the file pass takes the whole
// tree, so the directory is not listed on its own. When a torrent owns part of
// the layout the unit stays, and the empty directory is a plain candidate, in
// whichever order the walk met the owned and the orphan file.
func TestAbandonedDirs_DiscUnitInternalsFollowTheUnit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		owned      string // relative to Feature/BDMV; "" means the whole layout is orphaned
		wantBackup bool
	}{
		{name: "whole layout orphaned", owned: "", wantBackup: false},
		// Lexical walk order: BACKUP/, STREAM/00001.m2ts, index.bdmv.
		{name: "owned file met after the orphan", owned: "index.bdmv", wantBackup: true},
		{name: "owned file met before the orphan", owned: filepath.Join("STREAM", "00001.m2ts"), wantBackup: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			bdmv := filepath.Join(root, "Feature", "BDMV")
			writeFile(t, filepath.Join(bdmv, "STREAM", "00001.m2ts"))
			writeFile(t, filepath.Join(bdmv, "index.bdmv"))
			backup := filepath.Join(bdmv, "BACKUP")
			mkdirs(t, root, filepath.Join("Feature", "BDMV", "BACKUP"))

			tfm := NewTorrentFileMap(fsops.HostPaths)
			if tt.owned != "" {
				tfm.Add(fsops.NormalizePath(fsops.HostPaths, filepath.Join(bdmv, tt.owned)))
			}

			backend := newTestBackend()
			orphans, dirs, err := walkScanRootCollectingDirs(context.Background(), root, newTestProtected(t, fsops.HostPaths, nil, tfm), 0, backend, nil)
			if err != nil {
				t.Fatalf("walk: %v", err)
			}

			got := make([]string, 0, len(dirs))
			for _, c := range abandonedDirCandidates(context.Background(), sortDeepestFirst(dirs), orphans, []string{root}, nil, newTestProtected(t, fsops.HostPaths, nil, nil), 0, backend) {
				got = append(got, c.Path)
			}
			if slices.Contains(got, backup) != tt.wantBackup {
				t.Fatalf("BACKUP listed = %v, want %v (orphans %v, candidates %v)", !tt.wantBackup, tt.wantBackup, orphans, got)
			}
			if tt.owned == "" {
				for _, c := range got {
					if c == filepath.Join(root, "Feature") || isPathUnderNormalized(fsops.HostPaths, fsops.NormalizePath(fsops.HostPaths, c), fsops.NormalizePath(fsops.HostPaths, filepath.Join(root, "Feature"))) {
						t.Fatalf("%q is inside the deleted unit and must not be listed separately: %v", c, got)
					}
				}
			}
		})
	}
}

// TestAbandonedDirs_NestedScanRootIsNeverRemoved covers a scan root that sits
// inside another root's walk. It holds only orphans, so nothing but the root
// check keeps it off the preview.
func TestAbandonedDirs_NestedScanRootIsNeverRemoved(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "torrents")
	writeFile(t, filepath.Join(nested, "stale.mkv"))

	backend := newTestBackend()
	orphans, dirs, err := walkScanRootCollectingDirs(context.Background(), root, newTestProtected(t, fsops.HostPaths, nil, NewTorrentFileMap(fsops.HostPaths)), 0, backend, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	got := abandonedDirCandidates(context.Background(), sortDeepestFirst(dirs), orphans, []string{root, nested}, nil, newTestProtected(t, fsops.HostPaths, nil, nil), 0, backend)
	if len(got) != 0 {
		t.Fatalf("a nested scan root must be kept even when it holds only orphans, got %v", got)
	}
}

// TestAbandonedDirs_CaseTwinOfADeletedOrphanStillBlocks covers a symlink whose
// name differs only by case from an orphan in the same directory. The walk skips
// symlinks, so only childrenAllKept sees it, and it must not pass for the
// orphan. Needs a case-sensitive filesystem.
func TestAbandonedDirs_CaseTwinOfADeletedOrphanStillBlocks(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	root := mkdirs(t, filepath.Join(base, "root"), "a")
	dir := filepath.Join(root, "a")
	writeFile(t, filepath.Join(dir, "Junk.mkv"))
	target := filepath.Join(base, "outside.txt")
	writeFile(t, target)
	if err := os.Symlink(target, filepath.Join(dir, "junk.mkv")); err != nil {
		if os.IsExist(err) {
			t.Skip("case-insensitive filesystem")
		}
		t.Skipf("symlinks are unavailable on this host: %v", err)
	}

	backend := newTestBackend()
	orphans, dirs, err := walkScanRootCollectingDirs(context.Background(), root, newTestProtected(t, fsops.HostPaths, nil, NewTorrentFileMap(fsops.HostPaths)), 0, backend, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	got := abandonedDirCandidates(context.Background(), sortDeepestFirst(dirs), orphans, []string{root}, nil, newTestProtected(t, fsops.HostPaths, nil, nil), 0, backend)
	if len(got) != 0 {
		t.Fatalf("a directory still holding the symlink must be kept, got %v", got)
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

	_, dirs, err := walkScanRootCollectingDirs(context.Background(), root, newTestProtected(t, fsops.HostPaths, nil, NewTorrentFileMap(fsops.HostPaths)), 0, backend, nil)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	got := abandonedDirCandidates(context.Background(), sortDeepestFirst(dirs), nil, []string{root}, nil, newTestProtected(t, fsops.HostPaths, nil, nil), time.Hour, backend)
	if len(got) != 0 {
		t.Fatalf("a directory younger than the grace period must be held, got %v", got)
	}
}

// qBittorrent downloads into the download path and each category's download
// folder again, so the abandoned-directory pass protects them, but they are not
// scan roots. The category save folders are both.
func TestDeclaredScanRoots_ProtectsTheDownloadFolders(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	defaultSavePath := filepath.Join(base, "torrents")
	tv := filepath.Join(defaultSavePath, "tv")
	tempPath := filepath.Join(base, "incomplete")
	fast := filepath.Join(base, "fast", "tv")

	for _, tc := range []struct {
		name         string
		downloadPath string
		want         []string
	}{
		{name: "download path on", downloadPath: tempPath, want: []string{tv, fast, tempPath}},
		{name: "download path off", want: []string{tv, fast}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
			stubSync(svc).folders(qbittorrent.InstanceFolders{
				DefaultSavePath:     defaultSavePath,
				DownloadPath:        tc.downloadPath,
				CategorySaveFolders: []string{tv},
				DownloadFolders:     []string{fast},
			})

			roots, protected, err := svc.declaredScanRoots(t.Context(), fsops.HostPaths, 1, scanScope{CategoryPaths: true, AbandonedDirs: true})
			if err != nil {
				t.Fatalf("declaredScanRoots: %v", err)
			}
			if !slices.Equal(roots, []string{tv}) {
				t.Fatalf("roots = %v, want only the category save folder", roots)
			}
			if !slices.Equal(protected, tc.want) {
				t.Fatalf("protected = %v, want %v", protected, tc.want)
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
