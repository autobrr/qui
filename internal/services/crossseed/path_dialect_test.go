// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlink"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// slashBackend is a remote as cross-seed sees it on any host: the slash
// dialect over an in-memory tree. On a Windows host a host filepath call shows
// up as a backslashed path or as a path the tree does not hold. The nil
// embedded Backend makes any call the test does not fake panic.
type slashBackend struct {
	fsops.Backend
	files   map[string]hardlink.FileID
	dirs    map[string]struct{}
	devices []string // SameFilesystem is true for two paths under one of these
	stats   []string
	plans   []hardlinktree.TreePlan
	removed int   // RemoveTree calls
	size    int64 // file size Lstat reports, 5 when unset
}

func newSlashBackend(devices []string, files ...string) *slashBackend {
	b := &slashBackend{files: map[string]hardlink.FileID{}, dirs: map[string]struct{}{}, devices: devices}
	for i, f := range files {
		b.files[f] = testFileID(uint64(i + 1))
		for dir := path.Dir(f); ; dir = path.Dir(dir) {
			b.dirs[dir] = struct{}{}
			if dir == "/" {
				break
			}
		}
	}
	return b
}

// testFileID builds a distinct nonzero FileID for file n.
func testFileID(n uint64) hardlink.FileID {
	return hardlink.UnixFileID(1, n)
}

func (b *slashBackend) Paths() fsops.PathDialect { return fsops.SlashPaths }

func (b *slashBackend) Lstat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	b.stats = append(b.stats, p)
	if _, ok := b.dirs[p]; ok {
		return &fsops.LstatInfo{Path: p, IsDir: true, Mode: fs.ModeDir, Nlinks: 1}, nil
	}
	if id, ok := b.files[p]; ok {
		size := b.size
		if size == 0 {
			size = 5
		}
		return &fsops.LstatInfo{Path: p, Size: size, FileID: id, Nlinks: 2}, nil
	}
	return nil, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrNotExist}
}

func (b *slashBackend) Stat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	return b.Lstat(ctx, p)
}

func (b *slashBackend) device(p string) string {
	p = path.Clean(p)
	for _, root := range b.devices {
		if p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/") {
			return root
		}
	}
	return ""
}

func (b *slashBackend) SameFilesystem(_ context.Context, p1, p2 string) (bool, error) {
	_, isDir := b.dirs[p1]
	if _, isFile := b.files[p1]; !isFile && !isDir {
		return false, &fs.PathError{Op: "stat", Path: p1, Err: fs.ErrNotExist}
	}
	dev := b.device(p1)
	return dev != "" && dev == b.device(p2), nil
}

func (b *slashBackend) MkdirAll(_ context.Context, p string, _ fs.FileMode) error {
	b.dirs[p] = struct{}{}
	return nil
}

func (b *slashBackend) HardlinkTree(_ context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	recorded := hardlinktree.TreePlan{RootDir: plan.RootDir, Files: append([]hardlinktree.FilePlan(nil), plan.Files...)}
	b.plans = append(b.plans, recorded)
	created := &fsops.TreeCreateResult{Created: len(plan.Files)}
	for _, f := range plan.Files {
		created.Files = append(created.Files, f.TargetPath)
	}
	return created, nil
}

func (b *slashBackend) RemoveTree(context.Context, *fsops.TreeCreateResult) error {
	b.removed++
	return nil
}

func (b *slashBackend) Remove(context.Context, string, fsops.RemoveOptions) error { return nil }

func (b *slashBackend) Mkdir(_ context.Context, p string, _ fs.FileMode) error {
	_, isDir := b.dirs[p]
	if _, isFile := b.files[p]; isDir || isFile {
		return &fs.PathError{Op: "mkdir", Path: p, Err: fs.ErrExist}
	}
	b.dirs[p] = struct{}{}
	return nil
}

func slashBackendService(instance *models.Instance, backend fsops.Backend) *Service {
	s := &Service{instanceStore: &mockInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}}}
	// The pool hands a local-mode instance its local backend, so Require(Write)
	// passes and the fake stands in for the remote.
	s.SetBackendPool(fsops.NewPool(s.instanceStore, backend))
	return s
}

// On a Windows host link mode built every target with host filepath, so a
// remote's path.Dir of it was "." and the link failed.
func TestPathDialect_LinkModePlan(t *testing.T) {
	tests := []struct {
		name         string
		preset       string
		baseDir      string
		savePath     string
		file         string
		existing     string // a folder another torrent already holds
		wantRoot     string
		wantCategory string // "" skips it: pathcmp keys rewrite "\" on every host
	}{
		{
			name: "by-tracker", preset: "by-tracker", baseDir: "/data/hl", savePath: "/data/torrents",
			file: "Movie/movie.mkv", wantRoot: "/data/hl/Tracker", wantCategory: "/data/hl/tracker",
		},
		{
			name: "flat isolation folder", preset: "flat", baseDir: "/data/hl", savePath: "/data/torrents",
			file: "movie.mkv", wantRoot: "/data/hl/TorrentName", wantCategory: "/data/hl",
		},
		{
			name: "flat isolation folder taken", preset: "flat", baseDir: "/data/hl", savePath: "/data/torrents",
			file: "movie.mkv", existing: "/data/hl/TorrentName", wantRoot: "/data/hl/TorrentName--hash123", wantCategory: "/data/hl",
		},
		{
			name: "backslash in save path", preset: "by-tracker", baseDir: "/data/hl", savePath: `/data/torrents\old`,
			file: "Movie/movie.mkv", wantRoot: "/data/hl/Tracker",
		},
		{
			name: "double slash base dir", preset: "by-tracker", baseDir: "//data/hl", savePath: "/data/torrents",
			file: "Movie/movie.mkv", wantRoot: "/data/hl/Tracker", wantCategory: "/data/hl/tracker",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := tt.savePath + "/" + tt.file
			backend := newSlashBackend([]string{"/data"}, source)
			if tt.existing != "" {
				backend.dirs[tt.existing] = struct{}{}
			}
			sync := &rootlessSavePathSyncManager{}
			instance := &models.Instance{
				ID: 1, Name: "qbt1", HasLocalFilesystemAccess: true, UseHardlinks: true,
				HardlinkBaseDir: tt.baseDir, HardlinkDirPreset: tt.preset,
			}
			s := slashBackendService(instance, backend)
			s.syncManager = sync

			files := qbt.TorrentFiles{{Name: tt.file, Size: 5}}
			result := s.processHardlinkMode(
				t.Context(),
				CrossSeedCandidate{InstanceID: 1, InstanceName: "qbt1"},
				[]byte("torrent"), "hash123", "", "TorrentName", &CrossSeedRequest{IndexerName: "Tracker"},
				&qbt.Torrent{Hash: "matched", ContentPath: path.Dir(source)},
				"exact", files, files,
				&qbt.TorrentProperties{SavePath: tt.savePath},
				"category", "category.cross",
			)

			require.True(t, result.Success, "result: %+v", result)
			require.Len(t, backend.plans, 1)
			require.Equal(t, hardlinktree.TreePlan{
				RootDir: tt.wantRoot,
				Files:   []hardlinktree.FilePlan{{SourcePath: source, TargetPath: tt.wantRoot + "/" + tt.file}},
			}, backend.plans[0])
			require.Equal(t, tt.wantRoot, sync.addedOptions["savepath"])
			if tt.wantCategory != "" {
				categorySavePath, ok := s.createdCategories.Load("1:category.cross")
				require.True(t, ok)
				require.Equal(t, tt.wantCategory, categorySavePath)
			}
		})
	}
}

// Host Clean on Windows splits a remote "a\b" name into two segments, which
// ranks the wrong base dir first.
func TestPathDialect_LinkIntoMatchingBaseDirRanksSlashSegments(t *testing.T) {
	t.Parallel()

	source := `/data/a\b/x/file.mkv`
	backend := newSlashBackend([]string{"/data"}, source)
	var visited []string
	err := LinkIntoMatchingBaseDir(t.Context(), `/data/a/b/links,/data/a\b/links`, source, backend, func(baseDir string) error {
		visited = append(visited, baseDir)
		return crossDeviceLinkError(baseDir)
	})

	require.Error(t, err)
	require.Equal(t, []string{`/data/a\b/links`, "/data/a/b/links"}, visited)
}

func TestPathDialect_MatchedFilesystemProbePath(t *testing.T) {
	t.Parallel()

	backend := newSlashBackend([]string{"/data"}, "/data/torrents/Movie/movie.mkv")
	got, ok := matchedFilesystemProbePath(t.Context(), backend,
		&qbt.Torrent{ContentPath: "/data/torrents/Movie"},
		&qbt.TorrentProperties{SavePath: "/data/torrents"},
		qbt.TorrentFiles{{Name: "Movie/movie.mkv"}},
	)

	require.True(t, ok)
	require.Equal(t, "/data/torrents/Movie/movie.mkv", got)
}

// Host IsAbs on Windows rejects "/data/tv", which read as "not linked" with no
// error.
func TestPathDialect_ForEachLocalFileIDOnSlashSavePath(t *testing.T) {
	t.Parallel()

	backend := newSlashBackend([]string{"/data"}, "/data/tv/Show/ep1.mkv", "/data/tv/ep2.mkv")

	var ids []hardlink.FileID
	err := forEachLocalFileID(t.Context(), backend, "/data/tv", qbt.TorrentFiles{{Name: "Show/ep1.mkv"}, {Name: "ep2.mkv"}},
		func(id hardlink.FileID, _ uint64) bool {
			ids = append(ids, id)
			return true
		})
	require.NoError(t, err)
	require.Equal(t, []hardlink.FileID{testFileID(1), testFileID(2)}, ids)

	err = forEachLocalFileID(t.Context(), backend, "/data/tv", qbt.TorrentFiles{{Name: "../x"}},
		func(hardlink.FileID, uint64) bool { return true })
	require.ErrorContains(t, err, "cannot be resolved under the save path")
}

// batchSlashBackend is a slashBackend that also lstats in batches, the way
// the remote backend does on the exec tier.
type batchSlashBackend struct {
	*slashBackend
	batches [][]string
}

func (b *batchSlashBackend) LstatBatch(ctx context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	b.batches = append(b.batches, paths)
	infos := make([]*fsops.LstatInfo, len(paths))
	errs := make([]error, len(paths))
	for i, p := range paths {
		infos[i], errs[i] = b.Lstat(ctx, p)
	}
	return infos, errs
}

// StatBatch makes this a fsops.BatchStater. Cross-seed never stats with
// links followed, so a call fails the test.
func (b *batchSlashBackend) StatBatch(context.Context, []string) ([]*fsops.LstatInfo, []error) {
	panic("cross-seed does not use StatBatch")
}

func TestForEachLocalTorrentFileBatchesOnABatchBackend(t *testing.T) {
	t.Parallel()

	backend := &batchSlashBackend{slashBackend: newSlashBackend([]string{"/data"}, "/data/tv/ep1.mkv", "/data/tv/ep3.mkv")}
	files := qbt.TorrentFiles{{Name: "ep1.mkv"}, {Name: "../x"}, {Name: "ep2.mkv"}, {Name: "ep3.mkv"}}

	var ids []hardlink.FileID
	err := forEachLocalFileID(t.Context(), backend, "/data/tv", files, func(id hardlink.FileID, _ uint64) bool {
		ids = append(ids, id)
		return true
	})
	require.ErrorContains(t, err, "cannot be resolved under the save path")
	require.Equal(t, []hardlink.FileID{testFileID(1), testFileID(2)}, ids, "a missing file is skipped")
	want := []string{"/data/tv/ep1.mkv", "/data/tv/ep2.mkv", "/data/tv/ep3.mkv"}
	require.Equal(t, [][]string{want}, backend.batches, "one batch of the files that resolve")
	require.Equal(t, want, backend.stats, "no Lstat outside the batch")
}

func TestPathDialect_ManualMatchPreview(t *testing.T) {
	t.Parallel()

	backend := newSlashBackend([]string{"/"}, "/data/torrents/Movie/movie.mkv")
	instance := &models.Instance{
		ID: 1, Name: "qbt1", HasLocalFilesystemAccess: true, UseHardlinks: true,
		HardlinkBaseDir: "/links", HardlinkDirPreset: "by-tracker",
	}
	s := slashBackendService(instance, backend)

	got := s.manualMatchEffectiveSavePath(t.Context(), instance, "/data/torrents", "Movie", "tracker.example",
		qbt.TorrentFiles{{Name: "Movie/movie.mkv", Size: 5}})
	require.Equal(t, "/links/tracker.example", got)
}

// Host Dir on Windows walks `\data\hl`, `\data`, `\` and never finds the
// remote's "/data/hl".
func TestPathDialect_SelectSeasonPackBaseDirWalksSlashParents(t *testing.T) {
	t.Parallel()

	backend := newSlashBackend([]string{"/data"}, "/data/tv/ep.mkv")
	backend.dirs["/data/hl"] = struct{}{}

	got, _, err := selectSeasonPackBaseDir(t.Context(), backend, "/data/hl/new",
		map[episodeIdentity]seasonPackLocalFile{{series: 1, episode: 1}: {sourcePath: "/data/tv/ep.mkv"}})

	require.NoError(t, err)
	require.Equal(t, "/data/hl/new", got)
	require.Contains(t, backend.stats, "/data/hl")
}

// Season pack base dirs follow rankBaseDirs like single cross-seeds (#3156): a
// missing dir listed first no longer wins over one nearer the sources.
func TestSelectSeasonPackBaseDir_RanksBySharedPrefix(t *testing.T) {
	t.Parallel()

	backend := newSlashBackend([]string{"/data"}, "/data/tv/ep.mkv")
	backend.dirs["/data/tv/hl"] = struct{}{}
	backend.dirs["/mnt/hl"] = struct{}{}

	got, rest, err := selectSeasonPackBaseDir(t.Context(), backend, "/data/hl/new, /mnt/hl, /data/tv/hl",
		map[episodeIdentity]seasonPackLocalFile{{series: 1, episode: 1}: {sourcePath: "/data/tv/ep.mkv"}})

	require.NoError(t, err)
	require.Equal(t, "/data/tv/hl", got)
	require.Equal(t, []string{"/data/hl/new", "/mnt/hl"}, rest)
}

// A cross-device error in one base dir rolls back and retries in the next ranked
// one on the sources' filesystem; any other link error fails at once (#3156).
func TestApplySeasonPackWebhook_RetriesNextBaseDirAfterCrossDeviceError(t *testing.T) {
	tests := []struct {
		name        string
		firstErr    error
		manual      bool
		oldPackDir  bool // the first pack dir already holds files
		allFail     bool // every base dir fails with firstErr
		fails       int  // base dirs that fail with firstErr before one succeeds; 0 means 1
		baseDirs    string
		wantApplied bool
		wantRoots   []string
	}{
		{name: "cross-device", firstErr: &os.LinkError{Op: "link", Err: syscall.EXDEV}, wantApplied: true, wantRoots: []string{"/data/tv/hl", "/data/hl2"}},
		{name: "other error", firstErr: &os.LinkError{Op: "link", Err: syscall.EACCES}, wantRoots: []string{"/data/tv/hl"}},
		{name: "cross-device with failed creator rollback", firstErr: fmt.Errorf("%w (%w: %w)", &os.LinkError{Op: "link", Err: syscall.EXDEV}, hardlinktree.ErrRollbackFailed, fs.ErrPermission), wantRoots: []string{"/data/tv/hl"}},
		{name: "cross-device with files already in the pack dir", firstErr: &os.LinkError{Op: "link", Err: syscall.EXDEV}, oldPackDir: true, wantApplied: true, wantRoots: []string{"/data/tv/hl", "/data/hl2"}},
		{name: "every base dir cross-device", firstErr: &os.LinkError{Op: "link", Err: syscall.EXDEV}, allFail: true, wantRoots: []string{"/data/tv/hl", "/data/hl2"}},
		{name: "manual assemble cross-device", firstErr: &os.LinkError{Op: "link", Err: syscall.EXDEV}, manual: true, wantApplied: true, wantRoots: []string{"/data/tv/hl", "/data/hl2"}},
		{name: "cross-device twice", firstErr: &os.LinkError{Op: "link", Err: syscall.EXDEV}, fails: 2, baseDirs: "/data/tv/hl, /data/usb/hl, /data/hl2, /data/hl3", wantApplied: true, wantRoots: []string{"/data/tv/hl", "/data/hl2", "/data/hl3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fix := newSeasonPackFixture(t)
			baseDirs := tt.baseDirs
			if baseDirs == "" {
				baseDirs = "/data/tv/hl, /data/usb/hl, /data/hl2"
			}
			inst := &models.Instance{
				ID: 1, Name: "SlashInst", IsActive: true, HasLocalFilesystemAccess: true, UseHardlinks: true,
				HardlinkBaseDir: baseDirs,
			}
			hashes := []string{"e01", "e02", "e03", "e04"}
			sources := make([]string, 0, len(fix.packFiles))
			torrents := make([]qbt.Torrent, 0, len(fix.packFiles))
			for i, fileName := range fix.packFiles {
				source := "/data/tv/" + fileName
				sources = append(sources, source)
				torrents = append(torrents, qbt.Torrent{Hash: hashes[i], Name: strings.TrimSuffix(fileName, path.Ext(fileName)), ContentPath: source, Progress: 1.0})
			}
			// /data/usb is another disk, so the retry skips it.
			backend := newSlashBackend([]string{"/data/usb", "/data"}, sources...)
			backend.dirs["/data/tv/hl"] = struct{}{}
			backend.dirs["/data/usb/hl"] = struct{}{}
			backend.dirs["/data/hl2"] = struct{}{}
			backend.dirs["/data/hl3"] = struct{}{}

			baseSM := newMultiFakeSyncManager(map[int][]qbt.Torrent{inst.ID: torrents}, map[int]*models.Instance{inst.ID: inst})
			baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, hashes...)
			sm := &seasonPackSyncManager{fakeSyncManager: baseSM}
			var roots []string
			var failed int
			s := &Service{
				instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
				syncManager:              sm,
				releaseCache:             NewReleaseCache(),
				automationSettingsLoader: defaultSettings(true, 0.75),
				seasonPackLinkCreator: func(_ context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
					roots = append(roots, plan.RootDir)
					if len(roots) <= max(tt.fails, 1) || tt.allFail {
						failed++
						if tt.oldPackDir {
							// Files from before this run keep the pack dir after the rollback.
							for dir := path.Dir(plan.Files[0].TargetPath); dir != plan.RootDir; dir = path.Dir(dir) {
								backend.dirs[dir] = struct{}{}
							}
						}
						return &fsops.TreeCreateResult{Created: 1}, tt.firstErr
					}
					return &fsops.TreeCreateResult{Created: len(plan.Files)}, nil
				},
			}
			s.settler = idleSettler(s.syncManager)
			s.SetBackendPool(fsops.NewPool(s.instanceStore, backend))

			if tt.manual {
				// Manual assembly stats each source, so report the torrent's size.
				backend.size = baseSM.files["e01"][0].Size
				resp, err := s.ApplyManualAssemble(t.Context(), &ManualAssembleRequest{
					InstanceID: inst.ID, TorrentData: fix.torrentData, TargetHashes: hashes,
				})
				require.NoError(t, err)
				require.True(t, resp.Applied, "%+v", resp)
				require.Equal(t, tt.wantRoots, roots)
				require.Equal(t, tt.wantRoots[len(tt.wantRoots)-1], resp.Destination, "the response names the dir that was used")
				return
			}
			resp, err := s.ApplySeasonPackWebhook(t.Context(), &SeasonPackApplyRequest{
				TorrentName: fix.packName,
				TorrentData: fix.torrentData,
				InstanceIDs: []int{inst.ID},
			})

			require.NoError(t, err)
			require.Equal(t, tt.wantApplied, resp.Applied, "%+v", resp)
			require.Equal(t, tt.wantRoots, roots)
			require.Equal(t, failed, backend.removed, "each partial tree is rolled back")
			if tt.wantApplied {
				require.Equal(t, tt.wantRoots[len(tt.wantRoots)-1], sm.addCalls[0].options["savepath"])
			} else {
				require.Equal(t, "link_failed", resp.Reason)
				require.Empty(t, sm.addCalls)
			}
		})
	}
}

// On a Windows host the season pack resolved remote sources as `\data\tv\...`,
// which the remote does not hold, so the apply failed as layout_mismatch.
func TestPathDialect_SeasonPackPlan(t *testing.T) {
	tests := []struct {
		name     string
		preset   string
		baseDir  string
		wantRoot string // also the add's savepath; "" skips both
		wantDir  string // where the pack folder lands
	}{
		{name: "by-tracker", preset: "by-tracker", baseDir: "/data/hl", wantRoot: "/data/hl/tracker.example.com", wantDir: "/data/hl/tracker.example.com"},
		// A flat RootDir passes the configured base dir through as given, so
		// the row pins only the targets.
		{name: "flat double slash", preset: "flat", baseDir: "//data/hl", wantDir: "/data/hl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fix := newSeasonPackFixture(t)
			inst := &models.Instance{
				ID: 1, Name: "SlashInst", IsActive: true, HasLocalFilesystemAccess: true, UseHardlinks: true,
				HardlinkBaseDir: tt.baseDir, HardlinkDirPreset: tt.preset,
			}
			hashes := []string{"e01", "e02", "e03", "e04"}
			sources := make([]string, 0, len(fix.packFiles))
			torrents := make([]qbt.Torrent, 0, len(fix.packFiles))
			for i, fileName := range fix.packFiles {
				source := "/data/tv/" + fileName
				sources = append(sources, source)
				torrents = append(torrents, qbt.Torrent{Hash: hashes[i], Name: strings.TrimSuffix(fileName, path.Ext(fileName)), ContentPath: source, Progress: 1.0})
			}
			backend := newSlashBackend([]string{"/data"}, sources...)
			backend.dirs["/data/hl"] = struct{}{}

			baseSM := newMultiFakeSyncManager(map[int][]qbt.Torrent{inst.ID: torrents}, map[int]*models.Instance{inst.ID: inst})
			baseSM.files = seasonPackEpisodeFiles(t, fix.torrentData, hashes...)
			sm := &seasonPackSyncManager{fakeSyncManager: baseSM}
			s := &Service{
				instanceStore:            &fakeInstanceStore{instances: map[int]*models.Instance{inst.ID: inst}},
				syncManager:              sm,
				releaseCache:             NewReleaseCache(),
				automationSettingsLoader: defaultSettings(true, 0.75),
			}
			s.SetBackendPool(fsops.NewPool(s.instanceStore, backend))

			resp, err := s.ApplySeasonPackWebhook(t.Context(), &SeasonPackApplyRequest{
				TorrentName: fix.packName,
				TorrentData: fix.torrentData,
				InstanceIDs: []int{inst.ID},
			})

			require.NoError(t, err)
			require.True(t, resp.Applied, "%+v", resp)
			require.Len(t, backend.plans, 1)
			want := make([]hardlinktree.FilePlan, 0, len(fix.packFiles))
			for i, fileName := range fix.packFiles {
				want = append(want, hardlinktree.FilePlan{SourcePath: sources[i], TargetPath: tt.wantDir + "/" + fix.packName + "/" + fileName})
			}
			require.ElementsMatch(t, want, backend.plans[0].Files)
			if tt.wantRoot != "" {
				require.Equal(t, tt.wantRoot, backend.plans[0].RootDir)
				require.Equal(t, tt.wantRoot, sm.addCalls[0].options["savepath"])
			}
		})
	}
}
