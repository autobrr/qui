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
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
)

const folderCleanupStatConcurrency = 8

const folderCleanupContentProbes = 16

type FolderCleanupKind uint8

const (
	FolderCleanupDelete FolderCleanupKind = iota
	FolderCleanupSetLocation
	FolderCleanupSetSavePath
	FolderCleanupSetDownloadPath
	FolderCleanupSetCategory
	FolderCleanupEnableATM
	FolderCleanupEditCategory
)

type FolderCleanupOp struct {
	Kind           FolderCleanupKind
	Hashes         []string
	Path           string
	Category       string
	EditedCategory string
}

type FolderCleanupBatch struct {
	fc      *FolderCleanup
	snaps   []folderSnapshot
	dropped int
}

func (b FolderCleanupBatch) Queue() bool {
	if b.fc == nil {
		return false
	}
	return b.fc.enqueue(b.snaps, b.dropped)
}

func (sm *SyncManager) SetFolderCleanup(fc *FolderCleanup) {
	sm.folderCleanup.Store(fc)
}

func (sm *SyncManager) PrepareFolderCleanup(ctx context.Context, instanceID int, op FolderCleanupOp) FolderCleanupBatch {
	return sm.prepareFolderCleanup(ctx, instanceID, op, false)
}

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
	if room := max(fc.room(), 0); len(torrents) > room {
		batch.dropped = len(torrents) - room
		torrents = torrents[:room]
	}
	batch.snaps = sm.snapshotFolders(ctx, instanceID, backend, torrents, op.Kind != FolderCleanupDelete)
	return batch
}

func folderCleanupCandidates(syncManager *syncowner.Owner, d fsops.PathDialect, op FolderCleanupOp) []*qbt.Torrent {
	snapshot := snapshotIndex(syncManager.SnapshotUnchecked())
	mayMove := func(t *qbt.Torrent) bool { return op.mayMove(d, t) }
	switch {
	case op.Kind == FolderCleanupEditCategory:
		if category, ok := syncManager.GetCategoriesUnchecked()[op.EditedCategory]; ok && d.Clean(category.SavePath) == d.Clean(op.Path) {
			return nil
		}
		return snapshot.Matching(mayMove)
	case len(op.Hashes) == 1 && op.Hashes[0] == "all":
		return snapshot.Matching(mayMove)
	}
	return slices.DeleteFunc(lookupTorrents(snapshot, op.Hashes), func(t *qbt.Torrent) bool { return !mayMove(t) })
}

func lookupTorrents(snapshot torrentLookup, hashes []string) []*qbt.Torrent {
	var found []*qbt.Torrent
	seen := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		id := strings.ToLower(hash)
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if t, ok := snapshot.Torrent(id); ok {
			found = append(found, t)
		}
	}
	return found
}

func (op FolderCleanupOp) mayMove(d fsops.PathDialect, t *qbt.Torrent) bool {
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

func (sm *SyncManager) snapshotFolders(ctx context.Context, instanceID int, backend fsops.Backend, torrents []*qbt.Torrent, move bool) []folderSnapshot {
	if len(torrents) == 0 {
		return nil
	}
	d := backend.Paths()
	snaps := make([]folderSnapshot, len(torrents))
	var noRoot []string
	for i, t := range torrents {
		snaps[i] = folderSnapshot{InstanceID: instanceID, Hash: t.Hash, SavePath: t.SavePath,
			DownloadPath: t.DownloadPath, ContentPath: t.ContentPath, Layout: layoutRootFolder, Move: move}
		content := fsops.NormalizePath(d, t.ContentPath)
		if content == fsops.NormalizePath(d, t.SavePath) || (t.DownloadPath != "" && content == fsops.NormalizePath(d, t.DownloadPath)) {
			snaps[i].Layout = layoutNoRootFolder
			noRoot = append(noRoot, t.Hash)
		}
	}

	if len(noRoot) > 0 {
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

	if batch, ok := backend.(fsops.BatchStater); ok {
		markContentBatch(ctx, instanceID, batch, d, snaps)
		return snaps
	}

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

func (s *folderSnapshot) markContent(ctx context.Context, backend fsops.Backend) bool {
	for p := range s.contentProbes(backend.Paths()) {
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
		top = slices.DeleteFunc(top, isJunkName)
		save := d.Clean(s.ContentPath)
		for _, name := range top[:min(len(top), folderCleanupContentProbes)] {
			if !yield(d.Join(save, d.FromSlash(name))) {
				return
			}
		}
	}
}

func (s *folderSnapshot) markExisted(info *fsops.LstatInfo) {
	s.ContentExisted = true
	if s.Layout == layoutRootFolder && !info.IsDir {
		s.Layout = layoutSingleFile
	}
}

func (sm *SyncManager) syncedMainData(ctx context.Context, instanceID int) (*Client, *syncowner.Owner, error) {
	client, syncManager, _, err := sm.readMainData(ctx, instanceID, mainDataReadCached)
	if err != nil {
		return nil, nil, err
	}
	if client == nil || syncManager == nil || client.GetLastSyncUpdate().IsZero() {
		return nil, nil, fmt.Errorf("instance %d has not synced yet", instanceID)
	}
	return client, syncManager, nil
}

func (sm *SyncManager) liveTorrents(ctx context.Context, instanceID int) ([]*qbt.Torrent, error) {
	_, syncManager, err := sm.syncedMainData(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return snapshotTorrents(syncManager.SnapshotUnchecked()), nil
}

func (sm *SyncManager) ownRows(ctx context.Context, instanceID int, hashes []string) (map[string]*qbt.Torrent, error) {
	_, syncManager, err := sm.syncedMainData(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	snapshot := snapshotIndex(syncManager.SnapshotUnchecked())
	rows := make(map[string]*qbt.Torrent, len(hashes))
	for _, hash := range hashes {
		hash = canonicalizeHash(hash)
		if t, ok := snapshot.Torrent(hash); ok {
			rows[hash] = t
		}
	}
	return rows, nil
}

func (sm *SyncManager) hintRefresh(instanceID int) {
	sm.HintMainDataRefresh(instanceID, "folder_cleanup")
}

func (sm *SyncManager) lastSync(ctx context.Context, instanceID int) (time.Time, error) {
	client, _, err := sm.syncedMainData(ctx, instanceID)
	if err != nil {
		return time.Time{}, err
	}
	return client.GetLastSyncUpdate(), nil
}
