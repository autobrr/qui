// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"maps"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
)

const seenTrackersQueue = 16

type seenTrackers struct {
	hash     string
	flags    announceFlags
	trackers []qbt.TorrentTracker
	at       time.Time
}

func (sm *SyncManager) beginTrackersRead(instanceID int, owner *syncowner.Owner, hash string) func([]qbt.TorrentTracker) {
	snapshot := owner.SnapshotUnchecked()
	if snapshot == nil {
		return func([]qbt.TorrentTracker) {}
	}
	torrent, ok := snapshot.Torrent(hash)
	if !ok {
		return func([]qbt.TorrentTracker) {}
	}
	flags, at := announceFlagsOf(torrent), time.Now()

	return func(trackers []qbt.TorrentTracker) {
		sm.trackerHealthMu.RLock()
		seen := sm.trackerHealthSeen[instanceID]
		sm.trackerHealthMu.RUnlock()

		select {
		case seen <- seenTrackers{hash: hash, flags: flags, trackers: trackers, at: at}:
		default:
		}
	}
}

func (sm *SyncManager) applySeenTrackers(ctx context.Context, instanceID int, seen seenTrackers) {
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil || !client.Supports(CapTrackerHealth) {
		return
	}
	snapshot := syncManager.SnapshotUnchecked()
	if snapshot == nil {
		return
	}
	torrent, ok := snapshot.Torrent(seen.hash)
	if !ok || announceFlagsOf(torrent) != seen.flags {
		return
	}

	trackers := seen.trackers
	if client.Supports(CapAnnounceFlags) {
		if seen.flags == (announceFlags{}) {
			trackers = nil
		} else {
			cache := sm.trackerDetailsFor(sm.trackerHealthFlagged, instanceID)
			cache.mu.Lock()
			if prev, ok := cache.byHash[seen.hash]; ok && seen.at.Before(prev.fetchedAt) {
				cache.mu.Unlock()
				return
			}
			if cache.byHash == nil {
				cache.byHash = make(map[string]trackerDetail)
			}
			cache.byHash[seen.hash] = trackerDetail{flags: seen.flags, trackers: trackers, fetchedAt: seen.at}
			cache.mu.Unlock()
		}
	} else {
		sm.trackerHealthMu.RLock()
		passAt := sm.trackerHealthPassAt[instanceID]
		sm.trackerHealthMu.RUnlock()
		if seen.at.Before(passAt) {
			return
		}
	}

	if sm.setTrackerHealth(instanceID, seen.hash, trackerHealthOf(torrent, trackers, seen.at)) {
		sm.notifyTrackerHealthUpdated(instanceID)
	}
}

func (sm *SyncManager) setTrackerHealth(instanceID int, hash string, health TrackerHealth) bool {
	sm.trackerHealthMu.Lock()
	defer sm.trackerHealthMu.Unlock()

	snapshot := sm.trackerHealthCache[instanceID]
	if snapshot == nil || snapshot.Health(hash) == health {
		return false
	}
	byHash := make(map[string]TrackerHealth, len(snapshot.byHash)+1)
	maps.Copy(byHash, snapshot.byHash)
	if health == "" {
		delete(byHash, hash)
	} else {
		byHash[hash] = health
	}
	sm.trackerHealthCache[instanceID] = NewTrackerHealthSnapshot(byHash)
	return true
}
