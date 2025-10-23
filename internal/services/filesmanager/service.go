// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"database/sql"
	"fmt"
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

// GetCachedFiles retrieves cached file information for a torrent
// Returns nil if no cache exists or cache is stale
func (s *Service) GetCachedFiles(ctx context.Context, instanceID int, hash string, torrentProgress float64) (qbt.TorrentFiles, error) {
	// Check if we have sync metadata
	syncInfo, err := s.repo.GetSyncInfo(ctx, instanceID, hash)
	if err != nil {
		if err == sql.ErrNoRows {
			// No cache exists
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get sync info: %w", err)
	}

	// If torrent is 100% complete and we have a cache, use it
	// Otherwise, check if cache is fresh enough (less than 5 minutes old)
	cacheFreshDuration := 5 * time.Minute
	if torrentProgress >= 1.0 {
		// For complete torrents, cache is valid indefinitely
		cacheFreshDuration = 365 * 24 * time.Hour
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
		if progressDelta > progressThreshold {
			// Progress has advanced, bypass cache to get fresh data
			return nil, nil
		}
	}

	// Retrieve cached files
	cachedFiles, err := s.repo.GetFiles(ctx, instanceID, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get cached files: %w", err)
	}

	if len(cachedFiles) == 0 {
		return nil, nil
	}

	// Convert to qBittorrent format
	files := make(qbt.TorrentFiles, len(cachedFiles))
	for i, cf := range cachedFiles {
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

	return files, nil
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
