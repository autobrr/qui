// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

const (
	rideMaxAttemptAge = 120 * time.Second
	rideWriteQuiet    = 60 * time.Second
)

// ReadInstanceTorrents returns what [SyncManager.GetCachedInstanceTorrents]
// returns. It reads the torrents in memory and sends no request while syncs
// of other readers keep them fresh. It reads as GetCachedInstanceTorrents does,
// which can start a sync, when the last sync attempt is over 120 seconds old,
// when qui wrote to the instance in the last 60 seconds, when a torrent is
// moving, or when the instance is down or never synced.
func (sm *SyncManager) ReadInstanceTorrents(ctx context.Context, instanceID int) ([]CrossInstanceTorrentView, error) {
	client, err := sm.clientPool.GetClientOffline(ctx, instanceID)
	if errors.Is(err, ErrPoolClosed) {
		return nil, err
	}

	facts := rideFacts{lastWrite: sm.lastWriteAt(instanceID)}
	var syncManager *qbt.SyncManager
	if err == nil {
		facts.hasClient = true
		facts.healthy = client.IsHealthy()
		syncManager = client.GetSyncManager()
	}
	if syncManager != nil {
		facts.lastAttempt = syncManager.LastSyncTime()
		facts.synced = !syncManager.LastSuccessfulSyncTime().IsZero()
		facts.anyMoving = len(syncManager.GetTorrentsMatchingUnchecked(func(t *qbt.Torrent) bool {
			return t.State == qbt.TorrentStateMoving
		})) > 0
	}
	// The 120 s rule relies on the tracker health loop, which syncs each
	// connected instance every 60 s (refreshTrackerHealthCounts in sync_manager.go).
	if !rideOnSyncs(facts, time.Now()) {
		return sm.GetCachedInstanceTorrents(ctx, instanceID)
	}

	name, err := sm.lookupInstanceName(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return sm.instanceTorrentViews(client, instanceID, name, syncManager.GetTorrentsUnchecked(qbt.TorrentFilterOptions{})), nil
}

// rideFacts is what ReadInstanceTorrents knows about an instance before it picks a path.
type rideFacts struct {
	lastAttempt time.Time
	lastWrite   time.Time
	anyMoving   bool
	hasClient   bool
	healthy     bool
	synced      bool
}

// rideOnSyncs reports whether a read may use the torrents in memory and start no sync.
func rideOnSyncs(f rideFacts, now time.Time) bool {
	switch {
	case !f.hasClient, !f.healthy, !f.synced, f.anyMoving:
		return false
	case f.lastAttempt.IsZero(), now.Sub(f.lastAttempt) > rideMaxAttemptAge:
		return false
	case !f.lastWrite.IsZero() && now.Sub(f.lastWrite) < rideWriteQuiet:
		return false
	default:
		return true
	}
}
