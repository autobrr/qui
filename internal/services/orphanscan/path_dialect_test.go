// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

// slashBackend is a remote as orphan scan sees it on any host: the slash dialect
// over an in-memory tree. On a Windows host a host filepath call shows up as a
// backslashed path or as a file the tree does not hold.
type slashBackend struct {
	fsops.Backend
	files map[string]int64
	dirs  map[string]struct{}
}

func newSlashBackend(files map[string]int64, emptyDirs ...string) *slashBackend {
	b := &slashBackend{Backend: newTestBackend(), files: files, dirs: map[string]struct{}{"/": {}}}
	addDirs := func(dir string) {
		for ; dir != "/"; dir = path.Dir(dir) {
			b.dirs[dir] = struct{}{}
		}
	}
	for f := range files {
		addDirs(path.Dir(f))
	}
	for _, dir := range emptyDirs {
		addDirs(dir)
	}
	return b
}

func (b *slashBackend) Paths() fsops.PathDialect { return fsops.SlashPaths }

func (b *slashBackend) children(dir string) []fsops.DirEntry {
	var entries []fsops.DirEntry
	for d := range b.dirs {
		if d != dir && path.Dir(d) == dir {
			entries = append(entries, fsops.DirEntry{Name: path.Base(d), IsDir: true})
		}
	}
	for f := range b.files {
		if path.Dir(f) == dir {
			entries = append(entries, fsops.DirEntry{Name: path.Base(f)})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func (b *slashBackend) Lstat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	if _, ok := b.dirs[p]; ok {
		return &fsops.LstatInfo{Path: p, IsDir: true, Nlinks: 1}, nil
	}
	if size, ok := b.files[p]; ok {
		return &fsops.LstatInfo{Path: p, Size: size, Nlinks: 1}, nil
	}
	return nil, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrNotExist}
}

func (b *slashBackend) Stat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	return b.Lstat(ctx, p)
}

func (b *slashBackend) ReadDir(_ context.Context, dir string) ([]fsops.DirEntry, error) {
	if _, ok := b.dirs[dir]; !ok {
		return nil, &fs.PathError{Op: "readdir", Path: dir, Err: fs.ErrNotExist}
	}
	return b.children(dir), nil
}

func (b *slashBackend) WalkDir(ctx context.Context, root string, _ fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	if _, ok := b.dirs[root]; !ok {
		return nil, &fs.PathError{Op: "walk", Path: root, Err: fs.ErrNotExist}
	}
	var entries []fsops.WalkEntry
	var walk func(dir string)
	walk = func(dir string) {
		info, _ := b.Lstat(ctx, dir)
		entries = append(entries, fsops.WalkEntry{LstatInfo: *info})
		for _, e := range b.children(dir) {
			child := path.Join(dir, e.Name)
			if e.IsDir {
				walk(child)
				continue
			}
			info, _ := b.Lstat(ctx, child)
			entries = append(entries, fsops.WalkEntry{LstatInfo: *info})
		}
	}
	walk(root)
	ch := make(chan fsops.WalkEntry, len(entries))
	for _, e := range entries {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func (b *slashBackend) Remove(_ context.Context, p string, opts fsops.RemoveOptions) error {
	if _, ok := b.files[p]; ok {
		delete(b.files, p)
		return nil
	}
	if _, ok := b.dirs[p]; !ok {
		return &fs.PathError{Op: "remove", Path: p, Err: fs.ErrNotExist}
	}
	if !opts.Recursive && len(b.children(p)) > 0 {
		return &fs.PathError{Op: "remove", Path: p, Err: errors.New("directory not empty")}
	}
	for f := range b.files {
		if strings.HasPrefix(f, p+"/") {
			delete(b.files, f)
		}
	}
	for d := range b.dirs {
		if d == p || strings.HasPrefix(d, p+"/") {
			delete(b.dirs, d)
		}
	}
	return nil
}

// On a Windows host a remote save path is not host-absolute, and host Clean
// backslashes it or reads a leading // as a UNC volume.
func TestPathDialect_RootBuilders(t *testing.T) {
	t.Parallel()

	saved, err := validDefaultSavePath(fsops.SlashPaths, "/data/torrents/")
	require.NoError(t, err)
	require.Equal(t, "/data/torrents", saved)

	categories := map[string]qbt.Category{
		"tv":     {},
		"tv/hd":  {},
		"movies": {SavePath: "//media/movies/"},
		"music":  {SavePath: "music"},
	}
	for name, want := range map[string]string{
		"tv/hd":  "/data/tv/hd",
		"movies": "/media/movies",
		"music":  "/data/music",
	} {
		require.Equal(t, want, resolveCategoryPath(fsops.SlashPaths, name, categories, "/data", true), name)
	}

	files := qbt.TorrentFiles{{Name: "Show/e1.mkv"}, {Name: "Show/e2.mkv"}}
	require.Equal(t, "/data/real", actualSavePathFromContentPath(fsops.SlashPaths, "/data/tv", "/data/real/Show", files))
	single := qbt.TorrentFiles{{Name: "a\\b.mkv"}}
	require.Equal(t, "/data/real", actualSavePathFromContentPath(fsops.SlashPaths, "/data/tv", "/data/real/a\\b.mkv", single))

	roots := scanRootsFromTorrents(fsops.SlashPaths, []qbt.Torrent{{SavePath: "//srv/data/", ContentPath: "/srv/data/x\\y"}})
	slices.Sort(roots)
	require.Equal(t, []string{"/srv/data", "/srv/data/x\\y"}, roots)

	ignore, err := NormalizeIgnorePaths(fsops.SlashPaths, []string{"/data/skip/", "//srv/keep"})
	require.NoError(t, err)
	require.Equal(t, []string{"/data/skip", "/srv/keep"}, ignore)
	_, err = NormalizeIgnorePaths(fsops.SlashPaths, []string{"data/skip"})
	require.Error(t, err)
}

// A backslash in a remote name is a byte of that name, never a separator.
func TestPathDialect_FileMapKeys(t *testing.T) {
	t.Parallel()

	torrents := []qbt.Torrent{{Hash: "h", SavePath: "//srv/data/", State: qbt.TorrentStatePausedUp}}
	files := map[string]qbt.TorrentFiles{"h": {{Name: "Show/E01.mkv"}, {Name: "a\\b.mkv"}}}
	result, err := buildFileMapFromTorrents(fsops.SlashPaths, torrents, files)
	require.NoError(t, err)

	require.Equal(t, []string{"/srv/data"}, result.scanRoots)
	require.True(t, result.fileMap.Has("/srv/data/show/e01.mkv"))
	require.True(t, result.fileMap.Has("/srv/data/a\\b.mkv"))
	require.False(t, result.fileMap.Has("/srv/data/a/b.mkv"))
	require.True(t, result.fileMap.HasAnyInDir("/srv/data/show"))
	require.True(t, result.fileMap.HasAnyInDir("/srv"))
	require.False(t, result.fileMap.HasAnyInDir("/"))
	require.False(t, result.fileMap.HasAnyInDir("/srv/data/a"))
}

func TestPathDialect_DiscAndPlainUnits(t *testing.T) {
	t.Parallel()

	b := newSlashBackend(map[string]int64{
		"/data/Movie/BDMV/STREAM/00000.m2ts": 10,
		"/data/d/f.mkv":                      1,
		"/data/Owned/BDMV/STREAM/00000.m2ts": 10,
		"/data/Owned/extra.mkv":              1,
		"/data/Owned/sub/x.mkv":              1,
	})
	tfm := NewTorrentFileMap(fsops.SlashPaths)
	tfm.Add("/data/Owned/extra.mkv")
	tfm.Add("/data/Owned/sub/x.mkv")
	cache := map[string]discUnitDecision{}

	unit, isDisc := discOrphanUnitWithContext(t.Context(), "/data", "/data/Movie/BDMV/STREAM/00000.m2ts", tfm, cache, nil, b)
	require.True(t, isDisc)
	require.Equal(t, "/data/Movie", unit)

	unit, isDisc = discOrphanUnitWithContext(t.Context(), "/data", "/data/d/f.mkv", tfm, cache, nil, b)
	require.False(t, isDisc)
	require.Equal(t, "/data/d/f.mkv", unit)

	// A disc parent holding owned files is not a disc root, so the unit stays at BDMV.
	unit, isDisc = discOrphanUnitWithContext(t.Context(), "/data", "/data/Owned/BDMV/STREAM/00000.m2ts", tfm, cache, nil, b)
	require.True(t, isDisc)
	require.Equal(t, "/data/Owned/BDMV", unit)

	discRoots := map[string]string{"/data/Movie": "/data/Movie"}
	outer, ok := outermostDiscUnit(fsops.SlashPaths, "/data/Movie/BDMV", discRoots)
	require.True(t, ok)
	require.Equal(t, "/data/Movie", outer)
}

func TestPathDialect_AbandonedDirs(t *testing.T) {
	t.Parallel()

	b := newSlashBackend(map[string]int64{"/data/old/sub/x.mkv": 1}, "/data/empty")
	dirs := []AbandonedDir{{Path: "/data/old/sub"}, {Path: "/data/old"}, {Path: "/data/empty"}}
	deleted := []OrphanFile{{Path: "/data/old/sub/x.mkv"}}

	got := abandonedDirCandidates(t.Context(), dirs, deleted, []string{"/data"}, nil, nil, 0, b)
	paths := make([]string, 0, len(got))
	for _, o := range got {
		paths = append(paths, o.Path)
	}
	require.Equal(t, []string{"/data/old/sub", "/data/old", "/data/empty"}, paths)

	require.True(t, underDeletedPath(fsops.SlashPaths, "/data/Movie/BDMV", map[string]struct{}{"/data/Movie": {}}))
}

func TestPathDialect_ScanRootLookup(t *testing.T) {
	t.Parallel()

	roots := []string{"/data", "/data/a"}
	require.Equal(t, "/data/a", findScanRoot(fsops.SlashPaths, "/data/a/x.mkv", roots))
	// "a\b" is a sibling of "a", not a directory inside it.
	require.Equal(t, "/data", findScanRoot(fsops.SlashPaths, "/data/a\\b/x.mkv", roots))

	require.NoError(t, withinScanRoot(fsops.SlashPaths, "/data/a", "/data/a/x.mkv"))
	require.Error(t, withinScanRoot(fsops.SlashPaths, "/data/a", "/data/a\\x.mkv"))
}

func TestPathDialect_IgnorePathMatching(t *testing.T) {
	t.Parallel()

	ignore := []string{"/data/skip"}
	require.True(t, isIgnoredPath(fsops.SlashPaths, "/data/skip/x.mkv", ignore))
	require.False(t, isIgnoredPath(fsops.SlashPaths, "/data/skip\\x.mkv", ignore))
	require.True(t, isPathProtectedByIgnorePaths(fsops.SlashPaths, "/data", ignore))
	require.False(t, isPathProtectedByIgnorePaths(fsops.SlashPaths, "/data/skip\\x", ignore))
	require.False(t, isPathUnderNormalized(fsops.SlashPaths, "/data/a\\b", "/data/a"))
}

// A lexical slash walk visits "AUX/x" before "AUX.d/y", and a name holding
// a backslash sorts by that byte.
func TestPathDialect_WalksBefore(t *testing.T) {
	t.Parallel()

	require.True(t, walksBefore(fsops.SlashPaths, "/d/AUX/x", "/d/AUX.d/y"))
	require.False(t, walksBefore(fsops.SlashPaths, "/d/AUX.d/y", "/d/AUX/x"))
	require.False(t, walksBefore(fsops.SlashPaths, "/d/A\\x", "/d/A/y"))
	require.True(t, walksBefore(fsops.SlashPaths, "/d/A/y", "/d/A\\x"))
}

// A remote scan stores the paths the remote walk produced, byte for byte, on
// any host.
func TestExecuteScan_RemoteStoresSlashPaths(t *testing.T) {
	t.Parallel()

	b := newSlashBackend(map[string]int64{
		"/srv/data/owned.mkv":                    1,
		"/srv/data/a\\b.mkv":                     2,
		"/srv/data/a\\c.mkv":                     3,
		"/srv/data/x\\.DS_Store":                 4,
		"/srv/data/orphan.mkv":                   5,
		"/srv/data/keep/k.mkv":                   6,
		"/srv/data/Movie/BDMV/STREAM/00000.m2ts": 7,
		"/srv/data/old/x.mkv":                    8,
	}, "/srv/data/empty", "/srv/data/x/y")
	f := newModeFixture(t, "orphanscan-slash-paths", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend { return b })
	stubSync(f.svc).getAllTorrents = func(context.Context, int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "owned", SavePath: "//srv/data/", State: qbt.TorrentStatePausedUp}}, nil
	}
	stubSync(f.svc).getTorrentFilesBatch = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv"}, {Name: "a\\b.mkv"}}}, nil
	}
	stubSync(f.svc).getAppPreferences = func(context.Context, int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: "//srv/data"}, nil
	}
	// Without subcategories "x/y" is one name, and qBittorrent still saves it to x/y.
	stubSync(f.svc).getCategories = func(context.Context, int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{"x/y": {Name: "x/y"}}, nil
	}
	_, err := f.store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID:          1,
		IgnorePaths:         []string{"/srv/data/keep/"},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupMaxFiles: 100,
		DeleteAbandonedDirs: true,
	})
	require.NoError(t, err)

	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.Equal(t, []string{"/srv/data"}, run.ScanPaths)

	files, err := f.store.GetFilesForDeletion(t.Context(), run.ID)
	require.NoError(t, err)
	got := map[string]bool{}
	for _, file := range files {
		got[file.FilePath] = file.IsAbandonedDir
	}
	require.Equal(t, map[string]bool{
		"/srv/data/a\\c.mkv":     false,
		"/srv/data/x\\.DS_Store": false,
		"/srv/data/orphan.mkv":   false,
		"/srv/data/Movie":        false,
		"/srv/data/old/x.mkv":    false,
		"/srv/data/old":          true,
		"/srv/data/empty":        true,
	}, got)
}

// Deletion rechecks a slash preview at confirm time. A backslash is a byte of a name:
// a\b is a sibling of root a, skip\y is outside ignore path skip, and c is not the parent of category c\x.
func TestPathDialect_DeletionRechecks(t *testing.T) {
	t.Parallel()

	b := newSlashBackend(map[string]int64{
		"/srv/data/owned.mkv":                    1,
		"/srv/data/orphan.mkv":                   2,
		"/srv/data/late.mkv":                     3,
		"/srv/data/Movie/BDMV/STREAM/00000.m2ts": 4,
		"/srv/data/skip/x.mkv":                   5,
		"/srv/data/skip\\y.mkv":                  6,
		"/srv/data/a/n.mkv":                      7,
		"/srv/data/a\\b.mkv":                     8,
	}, "/srv/data/empty", "/srv/data/cat", "/srv/data/c", "/srv/data/skip\\d", "/srv/data/a\\d")
	// Only a local instance may delete; the dialect comes from the backend all the same.
	f := newModeFixture(t, "orphanscan-slash-delete", models.FilesystemModeLocal, nil)
	f.svc.backendPool = fsops.NewPool(f.instance, b)
	// late and disc are added between the preview and the confirm.
	torrents := []qbt.Torrent{
		{Hash: "owned", SavePath: "/srv/data", State: qbt.TorrentStatePausedUp},
		{Hash: "nested", SavePath: "/srv/data/a", State: qbt.TorrentStatePausedUp},
		{Hash: "late", SavePath: "/srv/data", State: qbt.TorrentStatePausedUp},
		{Hash: "disc", SavePath: "/srv/data", State: qbt.TorrentStatePausedUp},
	}
	live := 2
	files := map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv"}}, "nested": {{Name: "n.mkv"}}}
	categories := map[string]qbt.Category{}
	stubSync(f.svc).getAllTorrents = func(context.Context, int) ([]qbt.Torrent, error) { return torrents[:live], nil }
	stubSync(f.svc).getTorrentFilesBatch = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
		return files, nil
	}
	stubSync(f.svc).getAppPreferences = func(context.Context, int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: "/srv/data"}, nil
	}
	stubSync(f.svc).getCategories = func(context.Context, int) (map[string]qbt.Category, error) { return categories, nil }
	settings := &models.OrphanScanSettings{
		InstanceID:          1,
		IgnorePaths:         []string{},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupMaxFiles: 100,
		DeleteAbandonedDirs: true,
	}
	_, err := f.store.UpsertSettings(t.Context(), settings)
	require.NoError(t, err)

	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	live = len(torrents)
	files["late"] = qbt.TorrentFiles{{Name: "late.mkv"}}
	files["disc"] = qbt.TorrentFiles{{Name: "Movie/BDMV/STREAM/00000.m2ts"}}
	categories["cat"] = qbt.Category{Name: "cat"}
	categories["c\\x"] = qbt.Category{Name: "c\\x"}
	settings.IgnorePaths = []string{"/srv/data/skip"}
	_, err = f.store.UpsertSettings(t.Context(), settings)
	require.NoError(t, err)

	f.svc.executeDeletion(t.Context(), 1, run.ID)

	listed, err := f.store.ListFiles(t.Context(), run.ID, 1000, 0, "")
	require.NoError(t, err)
	got := map[string]string{}
	for _, file := range listed {
		got[file.FilePath] = file.Status + ": " + file.ErrorMessage
	}
	require.Equal(t, map[string]string{
		"/srv/data/orphan.mkv":  "deleted: ",
		"/srv/data/a\\b.mkv":    "deleted: ",
		"/srv/data/skip\\y.mkv": "deleted: ",
		"/srv/data/empty":       "deleted: ",
		"/srv/data/a\\d":        "deleted: ",
		"/srv/data/skip\\d":     "deleted: ",
		"/srv/data/c":           "deleted: ",
		"/srv/data/late.mkv":    "skipped: file is now in use by a torrent",
		"/srv/data/Movie":       "skipped: file is now in use by a torrent",
		"/srv/data/skip/x.mkv":  "skipped: path is protected by ignore paths",
		"/srv/data/skip":        "skipped: path is protected by ignore paths",
		"/srv/data/cat":         "skipped: directory is now a category destination",
	}, got)
	require.Equal(t, map[string]int64{
		"/srv/data/owned.mkv":                    1,
		"/srv/data/late.mkv":                     3,
		"/srv/data/Movie/BDMV/STREAM/00000.m2ts": 4,
		"/srv/data/skip/x.mkv":                   5,
		"/srv/data/a/n.mkv":                      7,
	}, b.files)
	require.Contains(t, b.dirs, "/srv/data/cat")
	require.NotContains(t, b.dirs, "/srv/data/empty")
}

func TestService_PathDialectWithoutABackendPool(t *testing.T) {
	t.Parallel()

	svc := NewService(DefaultConfig(), nil, nil, nil, nil, nil)
	_, err := svc.PathDialect(t.Context(), 1)
	require.Error(t, err)
}
