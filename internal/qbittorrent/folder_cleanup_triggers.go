// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

// folderCleanupStatConcurrency bounds the content checks a request waits on
// before it is sent to qBittorrent.
const folderCleanupStatConcurrency = 8

// folderCleanupContentProbes bounds the top-level entries checked for one
// torrent without a root folder. When none of them exists the content reads as
// missing, which leaves the folder to orphan scan, as for an unmounted disk.
const folderCleanupContentProbes = 16

// FolderCleanupKind names a qui operation that can leave folders behind.
type FolderCleanupKind uint8

const (
	// FolderCleanupDelete is a delete with files.
	FolderCleanupDelete FolderCleanupKind = iota
	FolderCleanupSetLocation
	// FolderCleanupSetSavePath is the WebAPI's setSavePath, which applies to
	// torrents with ATM off only.
	FolderCleanupSetSavePath
	FolderCleanupSetDownloadPath
	FolderCleanupSetCategory
	FolderCleanupEnableATM
	// FolderCleanupEditCategory moves every torrent in the category, and in
	// the subcategories that inherit its path, that uses Automatic Torrent
	// Management.
	FolderCleanupEditCategory
)

// FolderCleanupOp is an operation about to be sent to qBittorrent.
type FolderCleanupOp struct {
	Kind FolderCleanupKind
	// Hashes are the torrents; ["all"] means every torrent. EditCategory
	// ignores it.
	Hashes []string
	// Path is the new location, save path or download path, or for
	// EditCategory the category's new save path.
	Path string
	// Category is SetCategory's new category.
	Category string
	// EditedCategory is the category EditCategory changes.
	EditedCategory string
}

// FolderCleanupBatch holds the snapshots taken before an operation.
type FolderCleanupBatch struct {
	fc      *FolderCleanup
	snaps   []folderSnapshot
	dropped int
}

// Queue hands the batch to folder cleanup. Call it only after qBittorrent
// accepted the operation. A zero batch does nothing. It reports whether
// anything was queued, which is when the cleanup will read the sync cache.
func (b FolderCleanupBatch) Queue() bool {
	if b.fc == nil {
		return false
	}
	return b.fc.enqueue(b.snaps, b.dropped)
}

// SetFolderCleanup wires the folder cleanup that operations hand their
// snapshots to.
func (sm *SyncManager) SetFolderCleanup(fc *FolderCleanup) {
	sm.folderCleanup.Store(fc)
}

// PrepareFolderCleanup snapshots the torrents op is about to delete or move,
// from the sync cache plus one Stat per torrent, or one batch stat on a backend
// that batches. Call it before sending the request, and Queue the result only
// once qBittorrent accepted it. It returns within 30 s even while a Stat hangs:
// content not checked by then reads as missing, which leaves its folder to
// orphan scan. ctx itself is left as it was, for the request.
func (sm *SyncManager) PrepareFolderCleanup(ctx context.Context, instanceID int, op FolderCleanupOp) FolderCleanupBatch {
	return sm.prepareFolderCleanup(ctx, instanceID, op, false)
}

// PrepareProxiedFolderCleanup is PrepareFolderCleanup for a proxied request,
// whose client gives up on its own timeout, so it returns within 5 s.
func (sm *SyncManager) PrepareProxiedFolderCleanup(ctx context.Context, instanceID int, op FolderCleanupOp) FolderCleanupBatch {
	return sm.prepareFolderCleanup(ctx, instanceID, op, true)
}

func (sm *SyncManager) prepareFolderCleanup(ctx context.Context, instanceID int, op FolderCleanupOp, proxied bool) FolderCleanupBatch {
	fc := sm.folderCleanup.Load()
	if fc == nil {
		return FolderCleanupBatch{}
	}
	budget := fc.prepareBudget
	if proxied {
		budget = fc.proxyPrepareBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	backend, _, err := fc.pool.Require(ctx, instanceID, models.CapabilityWrite)
	if err != nil {
		if !errors.Is(err, fsops.ErrNotCapable) {
			log.Debug().Err(err).Int("instanceID", instanceID).Msg("folder cleanup: no backend, skipping")
		}
		return FolderCleanupBatch{}
	}
	_, syncManager, _, err := sm.readMainData(ctx, instanceID, mainDataReadCached)
	if err != nil || syncManager == nil {
		return FolderCleanupBatch{}
	}

	d := backend.Paths()
	torrents := folderCleanupCandidates(syncManager, d, op)
	batch := FolderCleanupBatch{fc: fc}
	// Each torrent holds at least one folder, so those past the room left
	// would be dropped anyway; skip their Stat and file list.
	if room := max(fc.room(), 0); len(torrents) > room {
		batch.dropped = len(torrents) - room
		torrents = torrents[:room]
	}
	batch.snaps = sm.snapshotFolders(ctx, instanceID, backend, torrents, op.Kind != FolderCleanupDelete)
	return batch
}

func folderCleanupCandidates(syncManager *qbt.SyncManager, d fsops.PathDialect, op FolderCleanupOp) []qbt.Torrent {
	var torrents []qbt.Torrent
	switch {
	case op.Kind == FolderCleanupEditCategory:
		if category, ok := syncManager.GetCategoriesUnchecked()[op.EditedCategory]; ok && d.Clean(category.SavePath) == d.Clean(op.Path) {
			return nil
		}
		torrents = syncManager.GetTorrentsUnchecked(qbt.TorrentFilterOptions{})
	case len(op.Hashes) == 1 && op.Hashes[0] == "all":
		torrents = syncManager.GetTorrentsUnchecked(qbt.TorrentFilterOptions{})
	default:
		torrents = lookupTorrents(syncManager, op.Hashes)
	}
	return slices.DeleteFunc(torrents, func(t qbt.Torrent) bool { return !op.mayMove(d, &t) })
}

// lookupTorrents resolves hashes as qBittorrent's WebAPI does: only a
// torrent's own hash, in either case, names it. A padded or v2 hash, which
// BulkAction accepts but qBittorrent ignores, is not a cache key.
func lookupTorrents(syncManager *qbt.SyncManager, hashes []string) []qbt.Torrent {
	var found []qbt.Torrent
	seen := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		id := strings.ToLower(hash)
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if t, ok := syncManager.GetTorrentUnchecked(id); ok {
			found = append(found, t)
		}
	}
	return found
}

// mayMove filters on the cached row only, so it admits some torrents that do
// not move, such as turning ATM on for a torrent already in its category
// folder. Their own row protects their folder until the deadline (indexLive).
// Preferences are not read, because their 30 s cache can say "no move" for a
// torrent that then moves, and that miss would be silent.
func (op FolderCleanupOp) mayMove(d fsops.PathDialect, t *qbt.Torrent) bool {
	// A torrent still fetching metadata has no content path, which Clean
	// would turn into qui's own working folder.
	if t.ContentPath == "" {
		return false
	}
	switch op.Kind {
	case FolderCleanupDelete:
		return true
	case FolderCleanupSetLocation:
		return d.Clean(t.SavePath) != d.Clean(op.Path)
	case FolderCleanupSetSavePath:
		return !t.AutoManaged && d.Clean(t.SavePath) != d.Clean(op.Path)
	case FolderCleanupSetDownloadPath:
		// TorrentImpl::setDownloadPath moves an incomplete torrent from where
		// its content sits, its download path or, without one, its save path,
		// into the new path. A complete torrent stays put: without a download
		// path only Progress tells it apart, with one its content path does.
		if t.AutoManaged {
			return false
		}
		if t.DownloadPath == "" {
			return t.Progress < 1 && op.Path != "" && d.Clean(t.SavePath) != d.Clean(op.Path) &&
				pathAtOrUnder(d, fsops.NormalizePath(d, t.ContentPath), fsops.NormalizePath(d, t.SavePath))
		}
		return d.Clean(t.DownloadPath) != d.Clean(op.Path) &&
			pathAtOrUnder(d, fsops.NormalizePath(d, t.ContentPath), fsops.NormalizePath(d, t.DownloadPath))
	case FolderCleanupSetCategory:
		return t.AutoManaged && t.Category != op.Category
	case FolderCleanupEnableATM:
		return !t.AutoManaged
	case FolderCleanupEditCategory:
		return t.AutoManaged && (t.Category == op.EditedCategory || strings.HasPrefix(t.Category, op.EditedCategory+"/"))
	}
	return false
}

// snapshotFolders records each torrent's layout and whether its content
// existed. Only a torrent without a root folder needs its file list: a root
// folder is removed whole, and a single file has nothing below it.
func (sm *SyncManager) snapshotFolders(ctx context.Context, instanceID int, backend fsops.Backend, torrents []qbt.Torrent, move bool) []folderSnapshot {
	if len(torrents) == 0 {
		return nil
	}
	d := backend.Paths()
	snaps := make([]folderSnapshot, len(torrents))
	var noRoot []string
	for i := range torrents {
		t := &torrents[i]
		snaps[i] = folderSnapshot{InstanceID: instanceID, Hash: t.Hash, SavePath: t.SavePath,
			DownloadPath: t.DownloadPath, ContentPath: t.ContentPath, Layout: layoutRootFolder, Move: move}
		content := fsops.NormalizePath(d, t.ContentPath)
		if content == fsops.NormalizePath(d, t.SavePath) || (t.DownloadPath != "" && content == fsops.NormalizePath(d, t.DownloadPath)) {
			snaps[i].Layout = layoutNoRootFolder
			noRoot = append(noRoot, t.Hash)
		}
	}

	if len(noRoot) > 0 {
		// A failed fetch leaves Files empty, which reads as "did not exist".
		files, err := sm.GetTorrentFilesBatch(ctx, instanceID, noRoot)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", instanceID).Msg("folder cleanup: file lists unavailable")
		}
		for i := range snaps {
			if snaps[i].Layout != layoutNoRootFolder {
				continue
			}
			list := files[canonicalizeHash(snaps[i].Hash)]
			names := make([]string, len(list))
			for j := range list {
				names[j] = list[j].Name
			}
			snaps[i].Files = names
		}
	}

	// Not fsops.StatAll: its fallback runs one Stat after another, and a
	// local Stat on a hung mount ignores ctx.
	if batch, ok := backend.(fsops.BatchStater); ok {
		markContentBatch(ctx, instanceID, batch, d, snaps)
		return snaps
	}

	// Each Stat marks its own copy and reports it on a channel with room for
	// all of them, so one still running when ctx ends, as os.Stat on a hung
	// mount does, writes nothing the caller reads and never blocks.
	type marked struct {
		i       int
		snap    folderSnapshot
		checked bool
	}
	results := make(chan marked, len(snaps))
	limit := make(chan struct{}, folderCleanupStatConcurrency)
	started, received, checked := 0, 0, 0
	for i := range snaps {
		select {
		case limit <- struct{}{}:
		case <-ctx.Done():
		}
		// select picks at random when both are ready, so a done ctx starts nothing.
		if ctx.Err() != nil {
			break
		}
		started++
		go func(i int, snap folderSnapshot) {
			ok := snap.markContent(ctx, backend)
			<-limit
			results <- marked{i, snap, ok}
		}(i, snaps[i])
	}
collect:
	for received < started {
		select {
		case m := <-results:
			received++
			if m.checked {
				snaps[m.i] = m.snap
				checked++
			}
		case <-ctx.Done():
			break collect
		}
	}
	// Results that landed as ctx ended are read too, so the outcome does not
	// depend on which ready case select picked.
	for drained := false; !drained && received < started; {
		select {
		case m := <-results:
			received++
			if m.checked {
				snaps[m.i] = m.snap
				checked++
			}
		default:
			drained = true
		}
	}
	if checked < len(snaps) {
		log.Info().Err(ctx.Err()).Int("instanceID", instanceID).Int("torrents", len(snaps)-checked).
			Msg("folder cleanup: content not checked in time, leaving these torrents' folders to orphan scan")
	}
	return snaps
}

// markContentBatch is markContent for every snapshot, with one batch stat of
// all their probes, so a remote host costs a few round trips per batch.
func markContentBatch(ctx context.Context, instanceID int, batch fsops.BatchStater, d fsops.PathDialect, snaps []folderSnapshot) {
	var paths []string
	var owner []int
	for i := range snaps {
		for p := range snaps[i].contentProbes(d) {
			paths = append(paths, p)
			owner = append(owner, i)
		}
	}
	infos, errs := batch.StatBatch(ctx, paths)
	for j, err := range errs {
		if err == nil {
			snaps[owner[j]].markExisted(infos[j])
		}
	}
	if ctx.Err() != nil {
		log.Info().Err(ctx.Err()).Int("instanceID", instanceID).
			Msg("folder cleanup: content not checked in time, leaving the folders of torrents not checked to orphan scan")
	}
}

// markContent Stats the probes until one exists. It reports false when ctx
// ended before the check finished.
func (s *folderSnapshot) markContent(ctx context.Context, backend fsops.Backend) bool {
	for p := range s.contentProbes(backend.Paths()) {
		// ctx ended, so the content counts as not checked.
		if ctx.Err() != nil {
			return false
		}
		if info, err := backend.Stat(ctx, p); err == nil {
			s.markExisted(info)
			return true
		}
	}
	return true
}

// contentProbes are the paths of which one must exist for the content to have
// existed. Without a root folder the content path is the save path, which
// still exists as an empty mount point when the disk below it is gone, so the
// top-level entries are probed instead: a file set to Do Not Download or
// removed by hand is missing on its own.
func (s *folderSnapshot) contentProbes(d fsops.PathDialect) iter.Seq[string] {
	return func(yield func(string) bool) {
		if s.Layout != layoutNoRootFolder {
			yield(d.Clean(s.ContentPath))
			return
		}
		top, err := topLevelNames(s.Files)
		if err != nil {
			return
		}
		// An empty mount point can hold one of these, written by a file manager.
		top = slices.DeleteFunc(top, isJunkName)
		save := d.Clean(s.ContentPath)
		for _, name := range top[:min(len(top), folderCleanupContentProbes)] {
			if !yield(d.Join(save, d.FromSlash(name))) {
				return
			}
		}
	}
}

// markExisted records that a probe exists.
func (s *folderSnapshot) markExisted(info *fsops.LstatInfo) {
	s.ContentExisted = true
	if s.Layout == layoutRootFolder && !info.IsDir {
		s.Layout = layoutSingleFile
	}
}

// syncedMainData reads the cache without a request. An instance that has not
// synced yet is an error, not an empty cache, which would protect nothing.
func (sm *SyncManager) syncedMainData(ctx context.Context, instanceID int) (*Client, *qbt.SyncManager, error) {
	client, syncManager, _, err := sm.readMainData(ctx, instanceID, mainDataReadCached)
	if err != nil {
		return nil, nil, err
	}
	if client == nil || syncManager == nil || client.GetLastSyncUpdate().IsZero() {
		return nil, nil, fmt.Errorf("instance %d has not synced yet", instanceID)
	}
	return client, syncManager, nil
}

func (sm *SyncManager) liveTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error) {
	_, syncManager, err := sm.syncedMainData(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return syncManager.GetTorrentsUnchecked(qbt.TorrentFilterOptions{}), nil
}

// ownRows reads the cached rows of hashes, one map read each, keyed by
// canonicalizeHash. A hash without a row is left out.
func (sm *SyncManager) ownRows(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.Torrent, error) {
	_, syncManager, err := sm.syncedMainData(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	rows := make(map[string]qbt.Torrent, len(hashes))
	for _, hash := range hashes {
		hash = canonicalizeHash(hash)
		if t, ok := syncManager.GetTorrentUnchecked(hash); ok {
			rows[hash] = t
		}
	}
	return rows, nil
}

func (sm *SyncManager) hintRefresh(instanceID int) {
	sm.HintMainDataRefresh(instanceID, "folder_cleanup")
}

// lastSync is the last successful sync, with or without changes. A failed
// sync does not count, so it cannot pass for a fresh one.
func (sm *SyncManager) lastSync(ctx context.Context, instanceID int) (time.Time, error) {
	client, _, err := sm.syncedMainData(ctx, instanceID)
	if err != nil {
		return time.Time{}, err
	}
	return client.GetLastSyncUpdate(), nil
}
