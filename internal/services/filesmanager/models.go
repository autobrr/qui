// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"time"
)

// CachedFile represents a cached torrent file
type CachedFile struct {
	ID              int
	InstanceID      int
	TorrentHash     string
	FileIndex       int
	Name            string
	Size            int64
	Progress        float64
	Priority        int
	IsSeed          *bool
	PieceRangeStart int64
	PieceRangeEnd   int64
	Availability    float64
	CachedAt        time.Time
}

// SyncInfo tracks when a torrent's files were last synced
type SyncInfo struct {
	InstanceID      int
	TorrentHash     string
	LastSyncedAt    time.Time
	TorrentProgress float64
	FileCount       int
}

// CacheStats provides statistics about the file cache
type CacheStats struct {
	TotalTorrents   int
	TotalFiles      int
	CachedTorrents  int
	OldestCacheAge  *time.Duration
	NewestCacheAge  *time.Duration
	AverageCacheAge *time.Duration
}
