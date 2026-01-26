// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
)

// InstanceLister lists all qBittorrent instances
type InstanceLister interface {
	ListInstanceIDs(ctx context.Context) ([]int, error)
}

// TorrentHashProvider provides current torrent hashes for an instance
type TorrentHashProvider interface {
	GetAllTorrentHashes(ctx context.Context, instanceID int) ([]string, error)
}

// Service manages cached torrent file information
type Service struct {
	db           dbinterface.Querier
	repo         *Repository
	mu           sync.Mutex
	lastCacheLog map[string]time.Time
}

const cacheLogThrottle = 30 * time.Second

func newCacheKey(instanceID int, hash string) string {
	return fmt.Sprintf("%d:%s", instanceID, hash)
}

// NewService creates a new files manager service
func NewService(db dbinterface.Querier) *Service {
	return &Service{
		db:           db,
		repo:         NewRepository(db),
		lastCacheLog: make(map[string]time.Time),
	}
}

// GetCachedFiles retrieves cached file information for a torrent.
// Returns nil if no cache exists or cache is stale.
//
// CONCURRENCY NOTE: This function does NOT use transactions to avoid deadlocks.
// There's a small TOCTOU race where cache could be invalidated between sync check
// and file retrieval, but this is acceptable because:
// 1. The worst case is serving slightly stale data (same as normal cache behavior)
// 2. Cache invalidation is triggered by user actions (rename, delete, etc.)
// 3. The cache has built-in freshness checks that limit staleness (5 min for active torrents, 30 min for completed)
// 4. Avoiding transactions prevents deadlocks during concurrent operations (backups, writes, etc.)
//
// If absolute consistency is required, the caller should invalidate the cache
// before calling this method, or use the qBittorrent API directly.
func (s *Service) GetCachedFiles(ctx context.Context, instanceID int, hash string) (qbt.TorrentFiles, error) {
	results, missing, err := s.GetCachedFilesBatch(ctx, instanceID, []string{hash})
	if err != nil {
		return nil, err
	}
	// If the requested hash was not returned or explicitly marked missing, behave like a cache miss.
	if _, found := lookupMissing(hash, missing); found {
		return nil, nil
	}
	files, ok := results[hash]
	if !ok {
		return nil, nil
	}
	return files, nil
}

// GetCachedFilesBatch retrieves cached file information for multiple torrents.
// Missing or stale entries are returned in the second slice so callers can decide what to refresh.
func (s *Service) GetCachedFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, []string, error) {
	unique := dedupeHashes(hashes)
	if len(unique) == 0 {
		return map[string]qbt.TorrentFiles{}, nil, nil
	}

	syncInfoMap, err := s.repo.GetSyncInfoBatch(ctx, instanceID, unique)
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, fmt.Errorf("failed to get sync info batch: %w", err)
	}

	freshHashes := make([]string, 0, len(unique))
	missing := make([]string, 0, len(unique))

	for _, hash := range unique {
		info := syncInfoMap[hash]
		if info == nil {
			missing = append(missing, hash)
			continue
		}

		if !cacheIsFresh(info) {
			missing = append(missing, hash)
			continue
		}

		freshHashes = append(freshHashes, hash)
	}

	results := make(map[string]qbt.TorrentFiles, len(freshHashes))
	if len(freshHashes) > 0 {
		cachedFiles, err := s.repo.GetFilesBatch(ctx, instanceID, freshHashes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get cached files batch: %w", err)
		}

		for hash, files := range cachedFiles {
			if len(files) == 0 {
				missing = append(missing, hash)
				continue
			}
			results[hash] = convertCachedFiles(files)
		}

		// Ensure fresh hashes that lacked rows are marked missing.
		for _, hash := range freshHashes {
			if _, ok := results[hash]; !ok {
				missing = append(missing, hash)
			}
		}
	}

	return results, missing, nil
}

// CacheFilesBatch stores file information for multiple torrents in the database
func (s *Service) CacheFilesBatch(ctx context.Context, instanceID int, files map[string]qbt.TorrentFiles) error {
	var allCachedFiles []CachedFile
	var allSyncInfos []SyncInfo

	for hash, torrentFiles := range files {
		if len(torrentFiles) == 0 {
			continue
		}

		// Convert to cache format
		cachedFiles := make([]CachedFile, len(torrentFiles))
		for i, f := range torrentFiles {
			pieceStart, pieceEnd := int64(0), int64(0)
			if len(f.PieceRange) >= 2 {
				pieceStart = int64(f.PieceRange[0])
				pieceEnd = int64(f.PieceRange[1])
			}

			isSeed := f.IsSeed
			cachedFiles[i] = CachedFile{
				InstanceID:      instanceID,
				TorrentHash:     hash,
				FileIndex:       f.Index,
				Name:            f.Name,
				Size:            f.Size,
				Progress:        float64(f.Progress),
				Priority:        f.Priority,
				IsSeed:          &isSeed,
				PieceRangeStart: pieceStart,
				PieceRangeEnd:   pieceEnd,
				Availability:    float64(f.Availability),
			}
		}

		allCachedFiles = append(allCachedFiles, cachedFiles...)

		// Collect sync metadata
		syncInfo := SyncInfo{
			InstanceID:      instanceID,
			TorrentHash:     hash,
			LastSyncedAt:    time.Now(),
			TorrentProgress: 0.0, // Not tracking progress anymore
			FileCount:       len(torrentFiles),
		}
		allSyncInfos = append(allSyncInfos, syncInfo)
	}

	if len(allCachedFiles) > 0 {
		// Store all files in database in one batch
		if err := s.repo.UpsertFiles(ctx, allCachedFiles); err != nil {
			return fmt.Errorf("failed to cache files: %w", err)
		}
	}

	if len(allSyncInfos) > 0 {
		// Update all sync metadata in one batch
		if err := s.repo.UpsertSyncInfoBatch(ctx, allSyncInfos); err != nil {
			return fmt.Errorf("failed to update sync info: %w", err)
		}
	}

	// Log each torrent individually
	for hash, torrentFiles := range files {
		if len(torrentFiles) == 0 {
			continue
		}

		now := time.Now()
		cacheKey := newCacheKey(instanceID, hash)
		//shouldLog := false

		s.mu.Lock()
		if last, ok := s.lastCacheLog[cacheKey]; !ok || now.Sub(last) >= cacheLogThrottle {
			s.lastCacheLog[cacheKey] = now
			//shouldLog = true
		}
		s.mu.Unlock()

		//if shouldLog {
		//	log.Trace().
		//		Int("instanceID", instanceID).
		//		Str("hash", hash).
		//		Int("fileCount", len(torrentFiles)).
		//		Msg("Cached torrent files")
		//}
	}

	return nil
}

// CacheFiles stores file information in the database
func (s *Service) CacheFiles(ctx context.Context, instanceID int, hash string, files qbt.TorrentFiles) error {
	return s.CacheFilesBatch(ctx, instanceID, map[string]qbt.TorrentFiles{hash: files})
}

// InvalidateCache removes cached file information for a torrent
func (s *Service) InvalidateCache(ctx context.Context, instanceID int, hash string) error {
	if err := s.repo.DeleteTorrentCache(ctx, instanceID, []string{hash}); err != nil {
		return fmt.Errorf("failed to invalidate torrent cache: %w", err)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Str("hash", hash).
		Msg("Invalidated torrent files cache")

	return nil
}

// CleanupRemovedTorrentsCache removes cache entries for torrents that no longer exist
func (s *Service) CleanupRemovedTorrentsCache(ctx context.Context, instanceID int, currentHashes []string) (int, error) {
	deleted, err := s.repo.DeleteCacheForRemovedTorrents(ctx, instanceID, currentHashes)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup cache for removed torrents: %w", err)
	}

	if deleted > 0 {
		log.Info().
			Int("instanceID", instanceID).
			Int("deleted", deleted).
			Msg("Cleaned up cache for removed torrents")
	}

	return deleted, nil
}

// StartOrphanCleanup starts a background goroutine that periodically removes orphaned
// cache entries (entries for torrents that no longer exist in qBittorrent).
// Runs cleanup on startup and then hourly.
func (s *Service) StartOrphanCleanup(ctx context.Context, instances InstanceLister, hashes TorrentHashProvider) {
	go func() {
		// Run cleanup on startup after a short delay to let instances connect
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}

		s.runOrphanCleanup(ctx, instances, hashes)

		// Then run hourly
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runOrphanCleanup(ctx, instances, hashes)
			}
		}
	}()
}

func (s *Service) runOrphanCleanup(ctx context.Context, instances InstanceLister, hashes TorrentHashProvider) {
	instanceIDs, err := instances.ListInstanceIDs(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("filesmanager: cache maintenance skipped, failed to list instances")
		return
	}

	var totalDeleted int
	for _, instanceID := range instanceIDs {
		currentHashes, err := hashes.GetAllTorrentHashes(ctx, instanceID)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", instanceID).
				Msg("filesmanager: cache maintenance skipped for instance")
			continue
		}

		deleted, err := s.CleanupRemovedTorrentsCache(ctx, instanceID, currentHashes)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).
				Msg("filesmanager: cache maintenance failed for instance")
			continue
		}
		totalDeleted += deleted
	}

	if totalDeleted > 0 {
		log.Info().Int("totalDeleted", totalDeleted).Msg("filesmanager: pruned stale cache entries")
	}
}

// GetCacheStats returns statistics about the cache
func (s *Service) GetCacheStats(ctx context.Context, instanceID int) (*CacheStats, error) {
	return s.repo.GetCacheStats(ctx, instanceID)
}

func cacheIsFresh(info *SyncInfo) bool {
	if info == nil {
		return false
	}

	// Use a fixed cache duration for simplicity
	cacheFreshDuration := 5 * time.Minute

	if time.Since(info.LastSyncedAt) > cacheFreshDuration {
		return false
	}

	return true
}

func convertCachedFiles(cached []CachedFile) qbt.TorrentFiles {
	if len(cached) == 0 {
		return nil
	}

	files := make(qbt.TorrentFiles, len(cached))
	for i, cf := range cached {
		isSeed := false
		if cf.IsSeed != nil {
			isSeed = *cf.IsSeed
		}

		files[i] = struct {
			Availability float32 `json:"availability"`
			Index        int     `json:"index"`
			IsSeed       bool    `json:"is_seed,omitempty"`
			Name         string  `json:"name"`
			PieceRange   []int   `json:"piece_range"`
			Priority     int     `json:"priority"`
			Progress     float32 `json:"progress"`
			Size         int64   `json:"size"`
		}{
			Availability: float32(cf.Availability),
			Index:        cf.FileIndex,
			IsSeed:       isSeed,
			Name:         cf.Name,
			PieceRange:   []int{int(cf.PieceRangeStart), int(cf.PieceRangeEnd)},
			Priority:     cf.Priority,
			Progress:     float32(cf.Progress),
			Size:         cf.Size,
		}
	}
	return files
}

func lookupMissing(hash string, missing []string) (string, bool) {
	for _, m := range missing {
		if m == hash {
			return m, true
		}
	}
	return "", false
}
