// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// rigTick is the worker tick in the rig. It stays well above the 200 ms
// debounced sync after a write: a hint on every tick closer than that would
// keep postponing the sync the worker waits on.
const rigTick = 500 * time.Millisecond

// rigKeptWait outlasts two full ticks plus the debounced sync, so a folder
// removed by mistake is gone by the time requireKept looks, even when the
// worker first waits a tick for the sync to drop the torrent's row. In the
// rig a removal lands about 0.5 s after the request.
const rigKeptWait = 2*rigTick + 500*time.Millisecond

const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hashC = "cccccccccccccccccccccccccccccccccccccccc"
)

// triggerRig is a SyncManager wired to a real FolderCleanup, a real backend
// pool over the local filesystem, and a qBittorrent stub that deletes and
// moves files in a temp dir.
type triggerRig struct {
	t          *testing.T
	root       string
	stub       *qbtstub.Server
	sm         *SyncManager
	instanceID int
	instances  *models.InstanceStore
}

func newTriggerRig(t *testing.T) *triggerRig {
	root := t.TempDir()
	r := &triggerRig{t: t, root: root, stub: qbtstub.New(t, filepath.Join(root, "torrents"))}
	r.stub.AddCategory("tv", "")
	r.stub.AddCategory("movies", "")
	for _, dir := range []string{"torrents/incomplete", "torrents/tv", "torrents/movies", "torrents/qui-links"} {
		require.NoError(t, os.MkdirAll(r.p(dir), 0o755))
	}

	db := testdb.NewMigratedSQLite(t, "qbittorrent-folder-cleanup-triggers")
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "test", r.stub.URL, "", "", nil, nil, false, new(true))
	require.NoError(t, err)
	baseDir := r.p("torrents/qui-links")
	_, err = instanceStore.Update(t.Context(), instance.ID, instance.Name, r.stub.URL, "", "", nil, nil,
		&models.InstanceUpdateParams{HardlinkBaseDir: &baseDir})
	require.NoError(t, err)
	r.instanceID = instance.ID
	r.instances = instanceStore

	client, err := NewClientWithTimeout(instance.ID, r.stub.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)

	r.sm = NewSyncManager(&ClientPool{instanceStore: instanceStore, clients: map[int]*Client{instance.ID: client}}, nil)
	fc := NewFolderCleanup(fsops.NewPool(instanceStore, local.NewBackend()), r.sm, &fakeIgnores{})
	fc.tick = rigTick
	fc.Start(t.Context())
	t.Cleanup(fc.Stop)
	r.sm.SetFolderCleanup(fc)
	return r
}

func (r *triggerRig) p(rel string) string { return filepath.Join(r.root, filepath.FromSlash(rel)) }

// add puts torrents in the stub and syncs them into the cache.
func (r *triggerRig) add(torrents ...qbtstub.Torrent) {
	for _, t := range torrents {
		r.stub.Add(t)
	}
	client, err := r.sm.clientPool.GetClientOffline(r.t.Context(), r.instanceID)
	require.NoError(r.t, err)
	require.NoError(r.t, client.GetSyncManager().Sync(r.t.Context()))
}

func (r *triggerRig) requireGone(rel string) {
	r.t.Helper()
	require.Eventually(r.t, func() bool {
		_, err := os.Lstat(r.p(rel))
		return err != nil
	}, 5*time.Second, 20*time.Millisecond, "%s must be removed", rel)
}

// requireKept waits out a tick and the sync, then checks rels are still there.
func (r *triggerRig) requireKept(rels ...string) {
	r.t.Helper()
	time.Sleep(rigKeptWait)
	for _, rel := range rels {
		require.DirExists(r.t, r.p(rel))
	}
}

// perRelease is the autobrr layout: a save path per release, no subfolder.
func (r *triggerRig) perRelease(hash, category string) qbtstub.Torrent {
	return qbtstub.Torrent{Hash: hash, Name: "Show.S01E01-GRP", SavePath: r.p("torrents/tv/Show.S01E01-GRP"),
		Category: category, Layout: qbtstub.NoRootFolder, Files: []string{"Show.S01E01.mkv"}}
}

// The monitored folders arrive with the preferences qui already reads.
func TestFolderCleanupTriggerKeepsMonitoredFolders(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.stub.ScanDirs = map[string]any{r.p("torrents/watch"): 0, r.p("media/watch"): r.p("torrents/from-watch")}
	watched := r.perRelease(hashA, "")
	watched.SavePath = r.p("torrents/watch/Show.S01E01-GRP")
	custom := r.perRelease(hashB, "")
	custom.SavePath = r.p("torrents/from-watch/Show.S01E01-GRP")
	r.add(watched, custom)

	require.NoError(t, r.sm.BulkAction(t.Context(), r.instanceID, []string{hashA, hashB}, "deleteWithFiles"))
	r.requireGone("torrents/watch/Show.S01E01-GRP")
	r.requireGone("torrents/from-watch/Show.S01E01-GRP")
	r.requireKept("torrents/watch", "torrents/from-watch")
}

// A default save path changed in qBittorrent's WebUI is a stop folder on the
// next check, while qui's cached preferences still hold the old one, which
// lies above it.
func TestFolderCleanupTriggerReadsPreferencesFresh(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	client, err := r.sm.clientPool.GetClientOffline(t.Context(), r.instanceID)
	require.NoError(t, err)
	_, err = client.GetAppPreferences(t.Context())
	require.NoError(t, err)

	r.stub.SetDefaultSavePath(r.p("torrents/new"))
	r.add(qbtstub.Torrent{Hash: hashA, Name: "Rel", SavePath: r.p("torrents/new"), Layout: qbtstub.RootFolder, Files: []string{"a.mkv"}})

	require.NoError(t, r.sm.BulkAction(t.Context(), r.instanceID, []string{hashA}, "deleteWithFiles"))
	r.requireGone("torrents/new/Rel")
	r.requireKept("torrents/new")
}

// The worker reads the view on every check, so the requests it costs must not
// grow with the torrents in the cache or the hashes it asks about.
func TestFolderCleanupViewRequestsDoNotGrowWithTorrents(t *testing.T) {
	t.Parallel()
	requests := func(hashes ...string) map[string]int {
		r := newTriggerRig(t)
		for _, hash := range hashes {
			r.add(qbtstub.Torrent{Hash: hash, Name: hash[:4], SavePath: r.p("torrents/tv"), Layout: qbtstub.RootFolder, Files: []string{"a.mkv"}})
		}
		ctx := t.Context()
		count := func(call func() error) int {
			before := r.stub.Requests()
			require.NoError(t, call())
			return r.stub.Requests() - before
		}
		return map[string]int{
			"liveTorrents": count(func() error { _, err := r.sm.liveTorrents(ctx, r.instanceID); return err }),
			"ownRows":      count(func() error { _, err := r.sm.ownRows(ctx, r.instanceID, hashes); return err }),
			"stopFolderInputs": count(func() error {
				_, err := r.sm.stopFolderInputs(ctx, r.instanceID)
				return err
			}),
			"categoryNesting": count(func() error { _, err := r.sm.categoryNesting(ctx, r.instanceID); return err }),
		}
	}
	require.Equal(t, requests(hashA), requests(hashA, hashB, hashC))
}

func TestFolderCleanupTriggerBulkDeleteWithFiles(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "tv"))

	require.NoError(t, r.sm.BulkAction(t.Context(), r.instanceID, []string{hashA}, "deleteWithFiles"))
	r.requireGone("torrents/tv/Show.S01E01-GRP")
	require.DirExists(t, r.p("torrents/tv"))
}

// A delete with files of a torrent whose first file was never downloaded
// still removes the save path and the subfolder it leaves.
func TestFolderCleanupTriggerDeleteWithFirstFileMissing(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(qbtstub.Torrent{Hash: hashA, Name: "Show.S01E01-GRP", SavePath: r.p("torrents/tv/Show.S01E01-GRP"), Category: "tv",
		Layout: qbtstub.NoRootFolder, Files: []string{"Show.S01E01.nfo", "Sample/sample.mkv", "Show.S01E01.mkv"}})
	require.NoError(t, os.Remove(r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.nfo")))

	require.NoError(t, r.sm.BulkAction(t.Context(), r.instanceID, []string{hashA}, "deleteWithFiles"))
	r.requireGone("torrents/tv/Show.S01E01-GRP")
	require.DirExists(t, r.p("torrents/tv"))
}

func TestFolderCleanupTriggerBulkDeleteKeepsFiles(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "tv"))

	require.NoError(t, r.sm.BulkAction(t.Context(), r.instanceID, []string{hashA}, "delete"))
	require.FileExists(t, r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv"))
	// Had the delete queued work, an emptied folder would now be removed.
	require.NoError(t, os.Remove(r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")))
	r.requireKept("torrents/tv/Show.S01E01-GRP")
}

// The worker owns the cleanup, so a client that disconnects after qBittorrent
// accepted the delete still gets its folders removed.
func TestFolderCleanupTriggerRunsAfterRequestCancellation(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "tv"))

	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, r.sm.BulkAction(ctx, r.instanceID, []string{hashA}, "deleteWithFiles"))
	cancel()
	r.requireGone("torrents/tv/Show.S01E01-GRP")
}

func TestFolderCleanupTriggerSetLocation(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "tv"))

	require.NoError(t, r.sm.SetLocation(t.Context(), r.instanceID, []string{hashA}, r.p("torrents/movies")))
	r.requireGone("torrents/tv/Show.S01E01-GRP")
	require.FileExists(t, r.p("torrents/movies/Show.S01E01.mkv"))
}

// An ATM torrent without a root folder: libtorrent moves the files and leaves
// the subfolder they sat in.
func TestFolderCleanupTriggerSetCategory(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(qbtstub.Torrent{Hash: hashA, Name: "Show", SavePath: r.p("torrents/tv"), Category: "tv", AutoTMM: true,
		Layout: qbtstub.NoRootFolder, Files: []string{"Show/Show.S01E01.mkv"}})

	require.NoError(t, r.sm.SetCategory(t.Context(), r.instanceID, []string{hashA}, "movies"))
	r.requireGone("torrents/tv/Show")
	require.DirExists(t, r.p("torrents/tv"))
	require.FileExists(t, r.p("torrents/movies/Show/Show.S01E01.mkv"))
}

func TestFolderCleanupTriggerEnableATM(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "movies"))

	require.NoError(t, r.sm.SetAutoTMM(t.Context(), r.instanceID, []string{hashA}, true))
	r.requireGone("torrents/tv/Show.S01E01-GRP")
	require.FileExists(t, r.p("torrents/movies/Show.S01E01.mkv"))
}

// Turning ATM off never moves a torrent, so it queues nothing. The snapshot
// already skips a torrent in ATM for the ATM kind, so only a torrent with ATM
// off, as in a mixed selection, could show a wrongly queued item. Another
// client then deletes it with its files: had the request queued it, its
// emptied folder would be removed, though qui deleted nothing.
func TestFolderCleanupTriggerDisableATMIsNotATrigger(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "movies"))

	require.NoError(t, r.sm.SetAutoTMM(t.Context(), r.instanceID, []string{hashA}, false))
	r.stub.DeleteElsewhere(hashA)
	r.requireKept("torrents/tv/Show.S01E01-GRP")
}

// Editing a category's save path moves its ATM torrents; the old category
// tree is removed up to the default save path, and a manual torrent in the
// category keeps its folder.
func TestFolderCleanupTriggerEditCategory(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.stub.AddCategory("archive", r.p("torrents/old/archive"))
	r.add(
		qbtstub.Torrent{Hash: hashA, Name: "A", SavePath: r.p("torrents/old/archive"), Category: "archive", AutoTMM: true,
			Layout: qbtstub.NoRootFolder, Files: []string{"A/a.mkv"}},
		qbtstub.Torrent{Hash: hashB, Name: "B", SavePath: r.p("torrents/old/archive"), Category: "archive", AutoTMM: true,
			Layout: qbtstub.RootFolder, Files: []string{"b.mkv"}},
		qbtstub.Torrent{Hash: hashC, Name: "C", SavePath: r.p("torrents/manual/C-GRP"), Category: "archive",
			Layout: qbtstub.NoRootFolder, Files: []string{"c.mkv"}},
	)

	require.NoError(t, r.sm.EditCategory(t.Context(), r.instanceID, "archive", r.p("torrents/new")))
	r.requireGone("torrents/old")
	require.FileExists(t, r.p("torrents/new/A/a.mkv"))
	require.FileExists(t, r.p("torrents/new/B/b.mkv"))
	require.FileExists(t, r.p("torrents/manual/C-GRP/c.mkv"))
}

// qBittorrent sets an ATM torrent's save path at once and its content path
// only once the files have moved, so the sync right after the request caches
// the new save path with the old content path. The cleanup waits for the
// content path, or the stale one would protect the folder it is to remove.
func TestFolderCleanupTriggerSlowATMMove(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		torrent func(r *triggerRig) qbtstub.Torrent
		op      func(r *triggerRig) error
		left    string
		moved   string
	}{
		{
			name: "set category",
			torrent: func(r *triggerRig) qbtstub.Torrent {
				torrent := r.perRelease(hashA, "tv")
				torrent.AutoTMM = true
				return torrent
			},
			op: func(r *triggerRig) error {
				return r.sm.SetCategory(r.t.Context(), r.instanceID, []string{hashA}, "movies")
			},
			left:  "torrents/tv/Show.S01E01-GRP",
			moved: "torrents/movies/Show.S01E01.mkv",
		},
		{
			name:    "enable ATM",
			torrent: func(r *triggerRig) qbtstub.Torrent { return r.perRelease(hashA, "movies") },
			op:      func(r *triggerRig) error { return r.sm.SetAutoTMM(r.t.Context(), r.instanceID, []string{hashA}, true) },
			left:    "torrents/tv/Show.S01E01-GRP",
			moved:   "torrents/movies/Show.S01E01.mkv",
		},
		{
			name: "edit category",
			torrent: func(r *triggerRig) qbtstub.Torrent {
				r.stub.AddCategory("archive", r.p("torrents/old/archive"))
				return qbtstub.Torrent{Hash: hashA, Name: "A", SavePath: r.p("torrents/old/archive"), Category: "archive", AutoTMM: true,
					Layout: qbtstub.NoRootFolder, Files: []string{"a.mkv"}}
			},
			op: func(r *triggerRig) error {
				return r.sm.EditCategory(r.t.Context(), r.instanceID, "archive", r.p("torrents/new"))
			},
			left:  "torrents/old",
			moved: "torrents/new/a.mkv",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newTriggerRig(t)
			r.add(tc.torrent(r))
			r.stub.SlowMoves = true
			client, err := r.sm.clientPool.GetClientOffline(t.Context(), r.instanceID)
			require.NoError(t, err)
			before := client.GetSyncManager().GetTorrentsUnchecked(qbt.TorrentFilterOptions{})[0]

			require.NoError(t, tc.op(r))
			require.Eventually(t, func() bool {
				row := client.GetSyncManager().GetTorrentsUnchecked(qbt.TorrentFilterOptions{})[0]
				return row.SavePath != before.SavePath && row.ContentPath == before.ContentPath
			}, 5*time.Second, 20*time.Millisecond, "the cache must show the new save path with the old content path")
			// Let the second sync after the request land before the files move.
			time.Sleep(300 * time.Millisecond)

			r.stub.FinishMoves()
			r.requireGone(tc.left)
			require.FileExists(t, r.p(tc.moved))
		})
	}
}

// The request returns once qBittorrent accepted it; the folders wait in the
// queue for the worker.
func TestFolderCleanupTriggerDoesNotWaitForCleanup(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "tv"))
	fc := NewFolderCleanup(r.sm.folderCleanup.Load().pool, r.sm, &fakeIgnores{})
	r.sm.SetFolderCleanup(fc)

	require.NoError(t, r.sm.BulkAction(t.Context(), r.instanceID, []string{hashA}, "deleteWithFiles"))
	require.DirExists(t, r.p("torrents/tv/Show.S01E01-GRP"))
	require.Len(t, fc.pending, 1)

	fc.tick = rigTick
	fc.Start(t.Context())
	t.Cleanup(fc.Stop)
	r.requireGone("torrents/tv/Show.S01E01-GRP")
}

// qBittorrent can move some torrents and then reject the request. Nothing is
// queued; the moved torrents' folders are left to orphan scan.
func TestFolderCleanupTriggerPartialSetCategory(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(
		qbtstub.Torrent{Hash: hashA, Name: "A", SavePath: r.p("torrents/tv"), Category: "tv", AutoTMM: true,
			Layout: qbtstub.NoRootFolder, Files: []string{"A/a.mkv"}},
		qbtstub.Torrent{Hash: hashB, Name: "B", SavePath: r.p("torrents/tv"), Category: "tv", AutoTMM: true,
			Layout: qbtstub.NoRootFolder, Files: []string{"B/b.mkv"}},
	)
	r.stub.ConflictAfter = 1

	require.Error(t, r.sm.SetCategory(t.Context(), r.instanceID, []string{hashA, hashB}, "movies"))
	require.FileExists(t, r.p("torrents/movies/A/a.mkv"))
	r.requireKept("torrents/tv/A")
}

// A rejected delete: a rejected move would also be held by its unmoved row,
// so only a delete shows that nothing was queued.
func TestFolderCleanupTriggerRejectedRequest(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "tv"))
	r.stub.Reject = 500

	require.Error(t, r.sm.BulkAction(t.Context(), r.instanceID, []string{hashA}, "deleteWithFiles"))
	// Had the request queued work, the emptied folder would now be removed.
	require.NoError(t, os.Remove(r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")))
	r.requireKept("torrents/tv/Show.S01E01-GRP")
}

// qBittorrent acts only on a hash of exactly 40 hex characters naming the
// torrent's own hash (TorrentID::fromString, then Session::getTorrent), and
// answers 200 for any other. The snapshot takes no other torrent, so a
// request qBittorrent ignores queues nothing.
func TestFolderCleanupSnapshotsOnlyHashesQBittorrentActsOn(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	torrent := r.perRelease(hashA, "tv")
	torrent.InfohashV1 = hashB
	torrent.InfohashV2 = hashA + strings.Repeat("c", 24)
	r.add(torrent)

	for _, tc := range []struct {
		hashes []string
		want   int
	}{
		{hashes: []string{hashA}, want: 1},
		{hashes: []string{strings.ToUpper(hashA)}, want: 1},
		{hashes: []string{"all"}, want: 1},
		{hashes: []string{"all", hashA}, want: 1},
		{hashes: []string{" " + hashA}},
		{hashes: []string{hashA + "\n"}},
		{hashes: []string{torrent.InfohashV2}},
		{hashes: []string{hashB}},
		{hashes: []string{"ALL"}},
		{hashes: []string{"zz" + hashA[2:]}},
	} {
		batch := r.sm.PrepareFolderCleanup(t.Context(), r.instanceID, FolderCleanupOp{Kind: FolderCleanupDelete, Hashes: tc.hashes})
		require.Len(t, batch.snaps, tc.want, "%q", tc.hashes)
	}
}

// Without a root folder the content reads as there when any top-level entry
// is, so a first file set to Do Not Download or removed by hand does not leave
// the folder behind. Only the first folderCleanupContentProbes entries are
// checked, a junk name such as Thumbs.db never counts, since an empty mount
// point can hold one, and a passed deadline reads as missing.
func TestFolderCleanupMarkContentChecksEachTopLevelEntry(t *testing.T) {
	t.Parallel()
	files := []string{"Show.S01E01.nfo", "Sample/sample.mkv", "Show.S01E01.mkv"}
	many := make([]string, folderCleanupContentProbes+1)
	for i := range many {
		many[i] = fmt.Sprintf("e%02d.mkv", i)
	}
	expired, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		name   string
		files  []string
		onDisk []string
		ctx    context.Context
		want   bool
	}{
		{name: "first file there", files: files, onDisk: []string{"Show.S01E01.nfo"}, want: true},
		{name: "first file missing", files: files, onDisk: []string{"Sample/sample.mkv", "Show.S01E01.mkv"}, want: true},
		{name: "first file's folder missing", files: files[1:], onDisk: []string{"Show.S01E01.mkv"}, want: true},
		{name: "all missing", files: files},
		{name: "past the probe limit", files: many, onDisk: many[len(many)-1:]},
		{name: "only a junk name there", files: []string{"Thumbs.db", ".DS_Store", "Show.S01E01.mkv"}, onDisk: []string{"Thumbs.db", ".DS_Store"}},
		{name: "a junk name and real content there", files: []string{"desktop.ini", "Show.S01E01.mkv"}, onDisk: []string{"desktop.ini", "Show.S01E01.mkv"}, want: true},
		{name: "deadline passed", files: files, onDisk: files, ctx: expired},
	} {
		save := filepath.Join(t.TempDir(), "Rel-GRP")
		require.NoError(t, os.MkdirAll(save, 0o755))
		for _, f := range tc.onDisk {
			p := filepath.Join(save, filepath.FromSlash(f))
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
		}
		ctx := tc.ctx
		if ctx == nil {
			ctx = t.Context()
		}
		snap := folderSnapshot{SavePath: save, ContentPath: save, Layout: layoutNoRootFolder, Files: tc.files}
		snap.markContent(ctx, local.NewBackend())
		require.Equal(t, tc.want, snap.ContentExisted, tc.name)
	}
}

// qBittorrent's setDownloadPath moves an incomplete torrent with ATM off from
// where its content sits into the new path. A complete torrent does not move,
// and admitting every one of a bulk change over seeding torrents would hold
// the queue for the deadline.
func TestFolderCleanupSetDownloadPathAdmits(t *testing.T) {
	t.Parallel()
	const save, download = "/data/torrents/tv/Rel-GRP", "/data/torrents/incomplete/Rel-GRP"
	for _, tc := range []struct {
		name    string
		torrent qbt.Torrent
		target  string
		want    bool
	}{
		{"from the save path", qbt.Torrent{SavePath: save, ContentPath: save, Progress: 0.5}, "/data/torrents/incomplete/tv", true},
		{"from the save path, root folder", qbt.Torrent{SavePath: save, ContentPath: save + "/Rel"}, "/data/torrents/incomplete/tv", true},
		{"complete, in the save path", qbt.Torrent{SavePath: save, ContentPath: save, Progress: 1}, "/data/torrents/incomplete/tv", false},
		{"complete root folder, in the save path", qbt.Torrent{SavePath: save, ContentPath: save + "/Rel", Progress: 1}, "/data/torrents/incomplete/tv", false},
		{"to the save path itself", qbt.Torrent{SavePath: save, ContentPath: save}, save + "/", false},
		{"empty target without a download path", qbt.Torrent{SavePath: save, ContentPath: save}, "", false},
		{"content outside the save path", qbt.Torrent{SavePath: save, ContentPath: "/data/elsewhere"}, "/data/torrents/incomplete/tv", false},
		{"ATM", qbt.Torrent{SavePath: save, ContentPath: save, AutoManaged: true}, "/data/torrents/incomplete/tv", false},
		{"from the download path", qbt.Torrent{SavePath: save, DownloadPath: download, ContentPath: download}, "/data/torrents/incomplete/tv", true},
		{"back to the save path", qbt.Torrent{SavePath: save, DownloadPath: download, ContentPath: download}, "", true},
		{"same download path", qbt.Torrent{SavePath: save, DownloadPath: download, ContentPath: download}, download, false},
		{"complete, already in the save path", qbt.Torrent{SavePath: save, DownloadPath: download, ContentPath: save}, "/data/torrents/incomplete/tv", false},
	} {
		op := FolderCleanupOp{Kind: FolderCleanupSetDownloadPath, Path: tc.target}
		require.Equal(t, tc.want, op.mayMove(fsops.SlashPaths, &tc.torrent), tc.name)
	}
}

// A torrent still fetching metadata reports no content path. Clean would
// make that ".", qui's working folder, so no operation snapshots it.
func TestFolderCleanupSkipsTorrentsWithoutContentPath(t *testing.T) {
	t.Parallel()
	torrent := qbt.Torrent{SavePath: "/data/torrents/tv", Category: "tv"}
	for kind := FolderCleanupDelete; kind <= FolderCleanupEditCategory; kind++ {
		op := FolderCleanupOp{Kind: kind, Path: "/data/torrents/movies", Category: "movies", EditedCategory: "tv"}
		require.False(t, op.mayMove(fsops.SlashPaths, &torrent), "kind %d", kind)
	}
}

// hungBackend is a mount where a Stat under hang returns only once release is
// closed, whatever its context, as os.Stat does on a hung NFS mount. stats
// counts every Stat that reached it.
type hungBackend struct {
	fsops.Backend
	hang    string
	mu      sync.Mutex
	release chan struct{}
	stats   atomic.Int32
	inside  atomic.Int32
}

func (b *hungBackend) unhang() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.release:
	default:
		close(b.release)
	}
}

func (b *hungBackend) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	b.stats.Add(1)
	if strings.HasPrefix(p, b.hang) {
		b.inside.Add(1)
		defer b.inside.Add(-1)
		b.mu.Lock()
		release := b.release
		b.mu.Unlock()
		<-release
	}
	return b.Backend.Stat(context.Background(), p)
}

type hungRequirer struct {
	backendRequirer
	backend *hungBackend
}

func (h hungRequirer) Require(ctx context.Context, instanceID int, capability models.FilesystemCapability, _ ...models.FilesystemCapability) (fsops.Backend, *models.Instance, error) {
	_, instance, err := h.backendRequirer.Require(ctx, instanceID, capability)
	if err != nil {
		return nil, nil, err
	}
	return h.backend, instance, nil
}

// hungRig holds hashA in tv and hashB in movies, with every Stat under hashB's
// save path hung until the test ends.
func hungRig(t *testing.T) (*triggerRig, *hungBackend) {
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "tv"), qbtstub.Torrent{Hash: hashB, Name: "Film-GRP", SavePath: r.p("torrents/movies/Film-GRP"),
		Category: "movies", Layout: qbtstub.NoRootFolder, Files: []string{"Film.mkv"}})
	backend := &hungBackend{Backend: local.NewBackend(), hang: r.p("torrents/movies/Film-GRP"), release: make(chan struct{})}
	fc := NewFolderCleanup(hungRequirer{r.sm.folderCleanup.Load().pool, backend}, r.sm, &fakeIgnores{})
	fc.tick = rigTick
	fc.prepareBudget = 200 * time.Millisecond
	fc.proxyPrepareBudget = 200 * time.Millisecond
	fc.Start(t.Context())
	t.Cleanup(fc.Stop)
	t.Cleanup(backend.unhang)
	r.sm.SetFolderCleanup(fc)
	return r, backend
}

// A prepare returns at its budget even while a Stat that ignores its context
// hangs. The torrent checked by then reads as existing, the hung one as
// missing, and the abandoned Stat changes nothing once it returns.
func TestFolderCleanupPrepareReturnsPastAHungStat(t *testing.T) {
	t.Parallel()
	for _, proxied := range []bool{false, true} {
		t.Run(fmt.Sprintf("proxied=%v", proxied), func(t *testing.T) {
			t.Parallel()
			r, backend := hungRig(t)
			prepare := r.sm.PrepareFolderCleanup
			if proxied {
				prepare = r.sm.PrepareProxiedFolderCleanup
			}

			start := time.Now()
			batch := prepare(t.Context(), r.instanceID, FolderCleanupOp{Kind: FolderCleanupDelete, Hashes: []string{hashA, hashB}})
			require.Less(t, time.Since(start), 2*time.Second)
			existed := map[string]bool{}
			for _, snap := range batch.snaps {
				existed[snap.Hash] = snap.ContentExisted
			}
			require.Equal(t, map[string]bool{hashA: true, hashB: false}, existed)
			require.EqualValues(t, 1, backend.inside.Load(), "hashB's Stat is still hung")

			backend.unhang()
			require.Eventually(t, func() bool { return backend.inside.Load() == 0 }, 5*time.Second, 10*time.Millisecond)
			for _, snap := range batch.snaps {
				require.Equal(t, snap.Hash == hashA, snap.ContentExisted, snap.Hash)
			}
		})
	}
}

// An automation deletes under a 60 s timeout, so a hung Stat may cost it the
// cleanup but not the delete. The healthy torrent's folder is still cleaned.
func TestFolderCleanupBulkDeletePastAHungStat(t *testing.T) {
	t.Parallel()
	r, _ := hungRig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	require.NoError(t, r.sm.BulkAction(ctx, r.instanceID, []string{hashA, hashB}, "deleteWithFiles"))
	require.Equal(t, 1, r.stub.Writes("torrents/delete"))
	r.requireGone("torrents/tv/Show.S01E01-GRP")
	r.requireKept("torrents/movies/Film-GRP")
	require.NoFileExists(t, r.p("torrents/movies/Film-GRP/Film.mkv"))
}

// With ctx already done no Stat starts, so the outcome at the deadline does
// not depend on which ready case select picks.
func TestFolderCleanupSnapshotStartsNoStatPastTheDeadline(t *testing.T) {
	t.Parallel()
	r, backend := hungRig(t)
	stats := backend.stats.Load()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	torrents := []qbt.Torrent{
		{Hash: hashA, SavePath: r.p("torrents/tv"), ContentPath: r.p("torrents/tv/Rel-A")},
		{Hash: hashB, SavePath: r.p("torrents/tv"), ContentPath: r.p("torrents/tv/Rel-B")},
	}

	for range 50 {
		snaps := r.sm.snapshotFolders(ctx, r.instanceID, backend, torrents, false)
		require.Len(t, snaps, 2)
	}
	require.Equal(t, stats, backend.stats.Load())
}

// remoteRowStore answers every instance read with a row in remote mode.
type remoteRowStore struct{}

func (remoteRowStore) Get(_ context.Context, id int) (*models.Instance, error) {
	return &models.Instance{ID: id, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey"}, nil
}

// batchSpy stands in for the SSH host: it answers from the local disk and
// counts the batch stats and the single stats that reached it.
type batchSpy struct {
	fsops.Backend
	batches atomic.Int32
	stats   atomic.Int32
}

func (s *batchSpy) Stat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	s.stats.Add(1)
	return s.Backend.Stat(ctx, p)
}

func (s *batchSpy) StatBatch(ctx context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	s.batches.Add(1)
	return fsops.StatAll(ctx, s.Backend, paths)
}

func (s *batchSpy) LstatBatch(ctx context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	s.batches.Add(1)
	return fsops.LstatAll(ctx, s.Backend, paths)
}

// Over SSH a Stat per torrent costs a round trip each, so a remote instance
// checks the content of the whole batch with one batch stat.
func TestFolderCleanupPrepareTakesOneBatchStatOnARemoteInstance(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "tv"),
		qbtstub.Torrent{Hash: hashB, Name: "Film-GRP", SavePath: r.p("torrents/movies"), Category: "movies",
			Layout: qbtstub.RootFolder, Files: []string{"Film.mkv"}},
		qbtstub.Torrent{Hash: hashC, Name: "Clip.mkv", SavePath: r.p("torrents/movies"), Category: "movies",
			Layout: qbtstub.SingleFile})
	require.NoError(t, os.Remove(r.p("torrents/movies/Clip.mkv")))
	spy := &batchSpy{Backend: local.NewBackend()}
	pool := fsops.NewPoolWithRemote(remoteRowStore{}, local.NewBackend(), func(*models.Instance) fsops.Backend { return spy })
	r.sm.SetFolderCleanup(NewFolderCleanup(pool, r.sm, &fakeIgnores{}))

	batch := r.sm.PrepareFolderCleanup(t.Context(), r.instanceID,
		FolderCleanupOp{Kind: FolderCleanupDelete, Hashes: []string{hashA, hashB, hashC}})

	require.EqualValues(t, 1, spy.batches.Load())
	require.Zero(t, spy.stats.Load())
	existed := map[string]bool{}
	for _, snap := range batch.snaps {
		existed[snap.Hash] = snap.ContentExisted
	}
	require.Equal(t, map[string]bool{hashA: true, hashB: true, hashC: false}, existed)
}
