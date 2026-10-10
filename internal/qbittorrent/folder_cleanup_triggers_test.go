// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

	client, err := NewClientWithTimeout(instance.ID, r.stub.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)

	r.sm = NewSyncManager(&ClientPool{instanceStore: instanceStore, clients: map[int]*Client{instance.ID: client}}, nil)
	fc := NewFolderCleanup(fsops.NewPool(instanceStore, local.NewBackend()), r.sm, &fakeIgnores{})
	fc.Tick = rigTick
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

func TestFolderCleanupTriggerBulkDeleteWithFiles(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "tv"))

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

	fc.Tick = rigTick
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

// remoteRowStore answers every instance read with a row in remote mode.
type remoteRowStore struct{}

func (remoteRowStore) Get(_ context.Context, id int) (*models.Instance, error) {
	return &models.Instance{ID: id, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey"}, nil
}

// statSpy stands in for the SSH host and counts the stats that reached it.
type statSpy struct {
	fsops.Backend
	stats atomic.Int32
}

func (s *statSpy) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	s.stats.Add(1)
	return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
}

// The ClientPool's instance store still reads the row as local, the backend
// pool's store reads it as remote. The snapshot must decide from the backend
// pool's row alone and skip, rather than stat local paths on the SSH host.
func TestFolderCleanupSkipsAnInstanceThatLeftLocalMode(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "tv"))
	spy := &statSpy{}
	pool := fsops.NewPoolWithRemote(remoteRowStore{}, local.NewBackend(), func(*models.Instance) fsops.Backend { return spy })
	fc := NewFolderCleanup(pool, r.sm, &fakeIgnores{})
	r.sm.SetFolderCleanup(fc)

	batch := r.sm.PrepareFolderCleanup(t.Context(), r.instanceID, FolderCleanupOp{Kind: FolderCleanupDelete, Hashes: []string{hashA}})

	require.Empty(t, batch.snaps)
	require.Zero(t, spy.stats.Load(), "the cleanup must not read the SSH host")
}
