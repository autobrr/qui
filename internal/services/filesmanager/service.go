// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
)

// Service manages cached torrent file information
type Service struct {
	db   dbinterface.Querier
	repo *Repository
}

// NewService creates a new files manager service
func NewService(db dbinterface.Querier) *Service {
	return &Service{
		db:   db,
		repo: NewRepository(db),
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
	// Check if we have sync metadata (without transaction to avoid deadlocks)
	syncInfo, err := s.repo.GetSyncInfo(ctx, instanceID, hash)
	if err != nil {
		if err == sql.ErrNoRows {
			// No cache exists
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get sync info: %w", err)
	}

	// If torrent is 100% complete and we have a cache, use it
	// Otherwise, check if cache is fresh enough (less than 5 minutes old for active torrents)
	cacheFreshDuration := 5 * time.Minute
	if torrentProgress >= 1.0 && syncInfo.TorrentProgress < 1.0 {
		// Torrent reached completion since we cached it; bypass stale snapshot
		return nil, nil
	}
	if torrentProgress >= 1.0 {
		// Completed torrents change infrequently, but still refresh periodically to pick up external renames
		cacheFreshDuration = 30 * time.Minute
	}

	cacheAge := time.Since(syncInfo.LastSyncedAt)
	if cacheAge > cacheFreshDuration {
		// Cache is stale
		return nil, nil
	}

	// For in-progress torrents, check if progress has advanced significantly
	// This ensures the UI shows up-to-date progress for actively downloading torrents
	if torrentProgress < 1.0 {
		const progressThreshold = 0.01 // 1% progress change triggers cache refresh
		progressDelta := torrentProgress - syncInfo.TorrentProgress
		if progressDelta > progressThreshold || progressDelta < 0 {
			// Progress has advanced significantly or regressed (torrent deleted/reset), bypass cache to get fresh data
			return nil, nil
		}
	}

	// Retrieve cached files (without transaction to avoid deadlocks)
	cachedFiles, err := s.repo.GetFiles(ctx, instanceID, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get cached files: %w", err)
	}

	if len(cachedFiles) == 0 {
		return nil, nil
	}

	// Convert to qBittorrent format
	return convertCachedFiles(cachedFiles), nil
}

// GetCachedFilesBatch retrieves cached files for multiple torrents in a single pass.
// The returned map only contains entries for torrents with valid cache snapshots.
func (s *Service) GetCachedFilesBatch(ctx context.Context, instanceID int, requests []BatchRequest) (map[string]qbt.TorrentFiles, error) {
	results := make(map[string]qbt.TorrentFiles)
	if len(requests) == 0 {
		return results, nil
	}

	unique := make(map[string]float64, len(requests))
	for _, req := range requests {
		hash := strings.TrimSpace(req.Hash)
		if hash == "" {
			continue
		}
		if current, ok := unique[hash]; !ok || req.Progress > current {
			unique[hash] = req.Progress
		}
	}

	if len(unique) == 0 {
		return results, nil
	}

	const activeCacheTTL = 5 * time.Minute
	const progressThreshold = 0.01

	completeHashes := make([]string, 0, len(unique))
	activeHashes := make([]string, 0, len(unique))
	for hash, progress := range unique {
		if progress >= 1.0 {
			completeHashes = append(completeHashes, hash)
		} else {
			activeHashes = append(activeHashes, hash)
		}
	}

	eligible := make(map[string]struct{}, len(unique))
	for _, hash := range completeHashes {
		eligible[hash] = struct{}{}
	}

	if len(activeHashes) > 0 {
		syncInfos, err := s.repo.GetSyncInfos(ctx, instanceID, activeHashes)
		if err != nil {
			return nil, fmt.Errorf("failed to get sync info batch: %w", err)
		}

		for _, hash := range activeHashes {
			progress := unique[hash]
			info, ok := syncInfos[hash]
			if !ok {
				continue
			}

			cacheAge := time.Since(info.LastSyncedAt)
			if cacheAge > activeCacheTTL {
				continue
			}
			if progress >= 1.0 && info.TorrentProgress < 1.0 {
				continue
			}

			if progress < 1.0 {
				delta := progress - info.TorrentProgress
				if delta > progressThreshold || delta < 0 {
					continue
				}
			}

			eligible[hash] = struct{}{}
		}
	}

	if len(eligible) == 0 {
		return results, nil
	}

	lookup := make([]string, 0, len(eligible))
	for hash := range eligible {
		lookup = append(lookup, hash)
	}

	rows, err := s.repo.GetFilesForHashes(ctx, instanceID, lookup)
	if err != nil {
		return nil, fmt.Errorf("failed to get cached files batch: %w", err)
	}

	for hash, cachedFiles := range rows {
		if len(cachedFiles) == 0 {
			continue
		}
		results[hash] = convertCachedFiles(cachedFiles)
	}

	return results, nil
}

// CacheFiles stores file information in the database
func (s *Service) CacheFiles(ctx context.Context, instanceID int, hash string, torrentProgress float64, files qbt.TorrentFiles) error {
	if len(files) == 0 {
		return nil
	}

	// Convert to cache format
	cachedFiles := make([]CachedFile, len(files))
	for i, f := range files {
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

	// Store in database
	if err := s.repo.UpsertFiles(ctx, cachedFiles); err != nil {
		return fmt.Errorf("failed to cache files: %w", err)
	}

	// Update sync metadata
	syncInfo := SyncInfo{
		InstanceID:      instanceID,
		TorrentHash:     hash,
		LastSyncedAt:    time.Now(),
		TorrentProgress: torrentProgress,
		FileCount:       len(files),
	}

	if err := s.repo.UpsertSyncInfo(ctx, syncInfo); err != nil {
		return fmt.Errorf("failed to update sync info: %w", err)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Str("hash", hash).
		Int("fileCount", len(files)).
		Float64("progress", torrentProgress).
		Msg("Cached torrent files")

	return nil
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

func convertCachedFiles(rows []CachedFile) qbt.TorrentFiles {
	if len(rows) == 0 {
		return nil
	}

	files := make(qbt.TorrentFiles, len(rows))
	for i, cf := range rows {
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
