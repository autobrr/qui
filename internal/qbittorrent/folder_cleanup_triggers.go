// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

// folderCleanupStatConcurrency bounds the content checks a request waits on
// before it is sent to qBittorrent.
const folderCleanupStatConcurrency = 8

// FolderCleanupKind names a qui operation that can leave folders behind.
type FolderCleanupKind uint8

const (
	// FolderCleanupDelete is a delete with files.
	FolderCleanupDelete FolderCleanupKind = iota
	FolderCleanupSetLocation
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
	// Target is the new location or path, the new category, or for
	// EditCategory the category being edited.
	Target string
	// NewPath is EditCategory's new save path.
	NewPath string
}

// FolderCleanupBatch holds the snapshots taken before an operation.
type FolderCleanupBatch struct {
	fc      *FolderCleanup
	snaps   []folderSnapshot
	dropped int
}

// Queue hands the batch to folder cleanup. Call it only after qBittorrent
// accepted the operation; a zero batch does nothing.
func (b FolderCleanupBatch) Queue() {
	if b.fc != nil {
		b.fc.enqueue(b.snaps, b.dropped)
	}
}

// SetFolderCleanup wires the folder cleanup that operations hand their
// snapshots to.
func (sm *SyncManager) SetFolderCleanup(fc *FolderCleanup) {
	sm.folderCleanup.Store(fc)
}

// PrepareFolderCleanup snapshots the torrents op is about to delete or move,
// from the sync cache plus one Stat per torrent. Call it before sending the
// request, and Queue the result only once qBittorrent accepted it.
func (sm *SyncManager) PrepareFolderCleanup(ctx context.Context, instanceID int, op FolderCleanupOp) FolderCleanupBatch {
	fc := sm.folderCleanup.Load()
	if fc == nil {
		return FolderCleanupBatch{}
	}
	backend, instance, err := fc.pool.Require(ctx, instanceID, models.CapabilityWrite)
	if err != nil {
		if !errors.Is(err, fsops.ErrNotCapable) {
			log.Debug().Err(err).Int("instanceID", instanceID).Msg("folder cleanup: no backend, skipping")
		}
		return FolderCleanupBatch{}
	}
	if models.FilesystemAccessMode(instance) == models.FilesystemModeRemote {
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
		if category, ok := syncManager.GetCategoriesUnchecked()[op.Target]; ok && d.Clean(category.SavePath) == d.Clean(op.NewPath) {
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

// lookupTorrents resolves hashes from the cache the way qBittorrent's WebAPI
// does (TorrentID::fromString, then Session::getTorrent): a hash names a
// torrent only when it is exactly that torrent's own 40-character hash, in
// either case. No trimming and no v1 or v2 variant, unlike BulkAction:
// qBittorrent leaves such a torrent in place, so it must not be snapshotted.
// The cache is keyed by that hash, in lower case as qBittorrent reports it, so
// one exact-key read per hash both finds the torrent and rejects every other
// string: a padded hash, a v2 hash or another variant of it is not a key.
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
	switch op.Kind {
	case FolderCleanupDelete:
		return true
	case FolderCleanupSetLocation:
		return d.Clean(t.SavePath) != d.Clean(op.Target)
	case FolderCleanupSetCategory:
		return t.AutoManaged && t.Category != op.Target
	case FolderCleanupEnableATM:
		return !t.AutoManaged
	case FolderCleanupEditCategory:
		return t.AutoManaged && (t.Category == op.Target || strings.HasPrefix(t.Category, op.Target+"/"))
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
		content := foldKey(d, t.ContentPath)
		if content == foldKey(d, t.SavePath) || (t.DownloadPath != "" && content == foldKey(d, t.DownloadPath)) {
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

	var g errgroup.Group
	g.SetLimit(folderCleanupStatConcurrency)
	for i := range snaps {
		g.Go(func() error {
			snaps[i].markContent(ctx, backend)
			return nil
		})
	}
	_ = g.Wait()
	return snaps
}

// markContent Stats the content. Without a root folder the content path is the
// save path, which still exists as an empty mount point when the disk below it
// is gone, so the first listed entry is checked instead.
func (s *folderSnapshot) markContent(ctx context.Context, backend fsops.Backend) {
	d := backend.Paths()
	target := d.Clean(s.ContentPath)
	if s.Layout == layoutNoRootFolder {
		if len(s.Files) == 0 {
			return
		}
		first, ok := cleanTorrentFileName(s.Files[0])
		if !ok {
			return
		}
		first, _, _ = strings.Cut(first, "/")
		target = d.Join(target, d.FromSlash(first))
	}
	info, err := backend.Stat(ctx, target)
	if err != nil {
		return
	}
	s.ContentExisted = true
	if s.Layout == layoutRootFolder && !info.IsDir {
		s.Layout = layoutSingleFile
	}
}

// liveTorrents reads the cached torrents without a request. An instance that
// has not synced yet is an error, not an empty list, which would protect
// nothing.
func (sm *SyncManager) liveTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error) {
	client, syncManager, _, err := sm.readMainData(ctx, instanceID, mainDataReadCached)
	if err != nil {
		return nil, err
	}
	if client == nil || syncManager == nil || client.GetLastSyncUpdate().IsZero() {
		return nil, fmt.Errorf("instance %d has not synced yet", instanceID)
	}
	return syncManager.GetTorrentsUnchecked(qbt.TorrentFilterOptions{}), nil
}

func (sm *SyncManager) hintRefresh(instanceID int) {
	sm.HintMainDataRefresh(instanceID, "folder_cleanup")
}

// categoryNesting is the version half of CategorySavePathsNest, which
// refreshes qBittorrent's capabilities with a request.
func (sm *SyncManager) categoryNesting(ctx context.Context, instanceID int) (nestingSupport, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nestingSupport{}, fmt.Errorf("failed to get client: %w", err)
	}
	if err := client.RefreshCapabilities(ctx); err != nil {
		return nestingSupport{}, fmt.Errorf("failed to refresh qBittorrent capabilities: %w", err)
	}
	return nestingSupport{Nests: client.NestsCategorySavePaths(), Always: client.SubcategoriesAlwaysEnabled()}, nil
}

// stopFolderInputs reads the cached preferences and categories. The worker
// combines the use_subcategories preference read here with categoryNesting
// by CategorySavePathsNest's rule, as orphan scan does, so the two agree on
// a category folder, except for up to a minute after the instance's
// qBittorrent version changes, through an upgrade in place or an edit that
// points the instance at another qBittorrent, while the worker still reuses
// the old version's answer.
func (sm *SyncManager) stopFolderInputs(ctx context.Context, instanceID int) (stopInputs, error) {
	client, syncManager, _, err := sm.readMainData(ctx, instanceID, mainDataReadCached)
	if err != nil {
		return stopInputs{}, err
	}
	if client == nil || syncManager == nil || client.GetLastSyncUpdate().IsZero() {
		return stopInputs{}, fmt.Errorf("instance %d has not synced yet", instanceID)
	}
	prefs, err := client.GetAppPreferences(ctx)
	if err != nil {
		return stopInputs{}, fmt.Errorf("failed to get app preferences: %w", err)
	}
	return stopInputs{
		DefaultSavePath:  prefs.SavePath,
		TempPath:         prefs.TempPath,
		TempPathEnabled:  prefs.TempPathEnabled,
		Categories:       syncManager.GetCategoriesUnchecked(),
		UseSubcategories: prefs.UseSubcategories,
		ScanDirs:         prefs.ScanDirs,
	}, nil
}
