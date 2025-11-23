// Copyright (c) 2025, s0up and the autobrr contributors.
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
func (s *Service) GetCachedFiles(ctx context.Context, instanceID int, hash string, torrentProgress float64) (qbt.TorrentFiles, error) {
	results, missing, err := s.GetCachedFilesBatch(ctx, instanceID, []string{hash}, map[string]float64{hash: torrentProgress})
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
func (s *Service) GetCachedFilesBatch(ctx context.Context, instanceID int, hashes []string, torrentProgress map[string]float64) (map[string]qbt.TorrentFiles, []string, error) {
	unique := dedupeHashes(hashes)
	if len(unique) == 0 {
		return map[string]qbt.TorrentFiles{}, nil, nil
	}

	if torrentProgress == nil {
		torrentProgress = make(map[string]float64)
	}

	syncInfoMap, err := s.repo.GetSyncInfoBatch(ctx, instanceID, unique)
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, fmt.Errorf("failed to get sync info batch: %w", err)
	}

	freshHashes := make([]string, 0, len(unique))
	missing := make([]string, 0, len(unique))

	for _, hash := range unique {
		info := syncInfoMap[hash]
		progress, hasProgress := torrentProgress[hash]
		if !hasProgress || info == nil {
			missing = append(missing, hash)
			continue
		}

		if !cacheIsFresh(info, progress) {
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
func (s *Service) CacheFilesBatch(ctx context.Context, instanceID int, torrentProgress map[string]float64, files map[string]qbt.TorrentFiles) error {
	var allCachedFiles []CachedFile
	var allSyncInfos []SyncInfo

	for hash, torrentFiles := range files {
		if len(torrentFiles) == 0 {
			continue
		}

		progress, ok := torrentProgress[hash]
		if !ok {
			progress = 0.0
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
			TorrentProgress: progress,
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

		progress, ok := torrentProgress[hash]
		if !ok {
			progress = 0.0
		}

		now := time.Now()
		cacheKey := newCacheKey(instanceID, hash)
		shouldLog := false

		s.mu.Lock()
		if last, ok := s.lastCacheLog[cacheKey]; !ok || now.Sub(last) >= cacheLogThrottle {
			s.lastCacheLog[cacheKey] = now
			shouldLog = true
		}
		s.mu.Unlock()

		if shouldLog {
			log.Trace().
				Int("instanceID", instanceID).
				Str("hash", hash).
				Int("fileCount", len(torrentFiles)).
				Float64("progress", progress).
				Msg("Cached torrent files")
		}
	}

	return nil
}

// CacheFiles stores file information in the database
func (s *Service) CacheFiles(ctx context.Context, instanceID int, hash string, torrentProgress float64, files qbt.TorrentFiles) error {
	return s.CacheFilesBatch(ctx, instanceID, map[string]float64{hash: torrentProgress}, map[string]qbt.TorrentFiles{hash: files})
}

// InvalidateCache removes cached file information for a torrent
func (s *Service) InvalidateCache(ctx context.Context, instanceID int, hash string) error {
	if err := s.repo.DeleteFiles(ctx, instanceID, hash); err != nil {
		return fmt.Errorf("failed to invalidate file cache: %w", err)
	}

	if err := s.repo.DeleteSyncInfo(ctx, instanceID, hash); err != nil {
		return fmt.Errorf("failed to delete sync info: %w", err)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Str("hash", hash).
		Msg("Invalidated torrent files cache")

	return nil
}

// CleanupStaleCache removes old cache entries
func (s *Service) CleanupStaleCache(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	deleted, err := s.repo.DeleteOldCache(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup stale cache: %w", err)
	}

	if deleted > 0 {
		log.Info().
			Int("deleted", deleted).
			Dur("olderThan", olderThan).
			Msg("Cleaned up stale torrent files cache")
	}

	return deleted, nil
}

// GetCacheStats returns statistics about the cache
func (s *Service) GetCacheStats(ctx context.Context, instanceID int) (*CacheStats, error) {
	return s.repo.GetCacheStats(ctx, instanceID)
}

func cacheIsFresh(info *SyncInfo, torrentProgress float64) bool {
	if info == nil {
		return false
	}

	cacheFreshDuration := 5 * time.Minute
	if torrentProgress >= 1.0 && info.TorrentProgress < 1.0 {
		return false
	}
	if torrentProgress >= 1.0 {
		// Completed torrents change infrequently; refresh occasionally to pick up external renames.
		cacheFreshDuration = 30 * time.Minute
	}

	if time.Since(info.LastSyncedAt) > cacheFreshDuration {
		return false
	}

	if torrentProgress < 1.0 {
		const progressThreshold = 0.01
		progressDelta := torrentProgress - info.TorrentProgress
		if progressDelta > progressThreshold || progressDelta < 0 {
			return false
		}
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
