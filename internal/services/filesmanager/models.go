// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"time"
)

// CachedFile represents a cached torrent file
type CachedFile struct {
	CachedAt        time.Time
	TorrentHash     string
	Name            string
	Size            int64
	Progress        float64
	PieceRangeStart int64
	PieceRangeEnd   int64
	Availability    float64
	IsSeed          *bool
	ID              int
	InstanceID      int
	FileIndex       int
	Priority        int
}

// SyncInfo tracks when a torrent's files were last synced
type SyncInfo struct {
	LastSyncedAt    time.Time
	TorrentHash     string
	TorrentProgress float64
	InstanceID      int
	FileCount       int
}

// CacheStats provides statistics about the file cache
type CacheStats struct {
	OldestCacheAge  *time.Duration
	NewestCacheAge  *time.Duration
	AverageCacheAge *time.Duration
	TotalTorrents   int
	TotalFiles      int
	CachedTorrents  int
}
