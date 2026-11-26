// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
)

const (
	rideMaxAttemptAge = 120 * time.Second
	rideWriteQuiet    = 60 * time.Second
)

func (sm *SyncManager) ReadInstanceTorrents(ctx context.Context, instanceID int) ([]CrossInstanceTorrentView, error) {
	client, err := sm.clientPool.GetClientOffline(ctx, instanceID)
	if errors.Is(err, ErrPoolClosed) {
		return nil, err
	}

	facts := rideFacts{lastWrite: sm.lastWriteAt(instanceID)}
	var syncManager *syncowner.Owner
	var snapshot *qbt.SyncSnapshot
	if err == nil {
		facts.hasClient = true
		facts.healthy = client.IsHealthy()
		syncManager = client.SyncOwner()
	}
	if syncManager != nil {
		facts.lastAttempt = syncManager.LastSyncTime()
		facts.synced = !syncManager.LastSuccessfulSyncTime().IsZero()
		snapshot = syncManager.SnapshotUnchecked()
		facts.anyMoving = len(snapshotMatching(snapshot, func(t *qbt.Torrent) bool { return t.State == qbt.TorrentStateMoving })) > 0
	}
	if !rideOnSyncs(facts, time.Now()) {
		return sm.GetCachedInstanceTorrents(ctx, instanceID)
	}

	name, err := sm.lookupInstanceName(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return sm.instanceTorrentViews(client, instanceID, name, snapshot), nil
}

type rideFacts struct {
	lastAttempt time.Time
	lastWrite   time.Time
	anyMoving   bool
	hasClient   bool
	healthy     bool
	synced      bool
}

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

func (sm *SyncManager) InstanceSnapshot(ctx context.Context, instanceID int) *qbt.SyncSnapshot {
	client, err := sm.clientPool.GetClientOffline(ctx, instanceID)
	if err != nil || client.SyncOwner() == nil {
		return nil
	}
	return client.SyncOwner().SnapshotUnchecked()
}
