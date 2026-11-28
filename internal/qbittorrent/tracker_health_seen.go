// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"maps"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/pkg/infohash"
)

const seenTrackersQueue = 16

type seenTrackers struct {
	hash     string
	flags    announceFlags
	trackers []qbt.TorrentTracker
	at       time.Time
}

func (sm *SyncManager) beginTrackersRead(instanceID int, owner *syncowner.Owner, hash infohash.Hash) func([]qbt.TorrentTracker) {
	torrent, ok := NewHashIndex(owner.SnapshotUnchecked()).Torrent(hash)
	if !ok {
		return func([]qbt.TorrentTracker) {}
	}
	flags, at := announceFlagsOf(torrent), sm.healthNow()

	return func(trackers []qbt.TorrentTracker) {
		sm.trackerHealthMu.RLock()
		seen := sm.trackerHealthSeen[instanceID]
		sm.trackerHealthMu.RUnlock()

		select {
		case seen <- seenTrackers{hash: torrent.Hash, flags: flags, trackers: trackers, at: at}:
		default:
		}
	}
}

func (sm *SyncManager) applySeenTrackers(ctx context.Context, instanceID int, seen ...seenTrackers) {
	client, syncManager, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil || !client.Supports(CapTrackerHealth) {
		return
	}
	index := NewHashIndex(syncManager.SnapshotUnchecked())
	changed := false
	for _, one := range seen {
		changed = sm.applyOneSeen(client, index, instanceID, one) || changed
	}
	if changed {
		sm.storeUnregisteredSince(ctx, instanceID)
		sm.notifyTrackerHealthUpdated(instanceID)
	}
}

func (sm *SyncManager) applyOneSeen(client *Client, index HashIndex, instanceID int, seen seenTrackers) bool {
	id, err := infohash.Parse(seen.hash)
	if err != nil {
		return false
	}
	torrent, ok := index.Torrent(id)
	if !ok || announceFlagsOf(torrent) != seen.flags {
		return false
	}

	trackers := seen.trackers
	if client.Supports(CapAnnounceFlags) {
		if seen.flags == (announceFlags{}) && !sm.hasUnregisteredSince(instanceID, seen.hash) {
			trackers = nil
		} else {
			cache := sm.trackerDetailsFor(sm.trackerHealthFlagged, instanceID)
			cache.mu.Lock()
			if prev, ok := cache.byHash[seen.hash]; ok && seen.at.Before(prev.fetchedAt) {
				cache.mu.Unlock()
				return false
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
			return false
		}
	}

	health, by := trackerVerdictOf(torrent, trackers, seen.at)
	var reason *unregisteredRow
	if by >= 0 {
		reason = &unregisteredRow{trackers: trackers, by: by}
	}
	return sm.setTrackerHealth(instanceID, torrent, trackers, health, reason, seen.at)
}

func (sm *SyncManager) setTrackerHealth(instanceID int, torrent *qbt.Torrent, trackers []qbt.TorrentTracker, health TrackerHealth, reason *unregisteredRow, at time.Time) bool {
	sm.trackerHealthMu.Lock()
	defer sm.trackerHealthMu.Unlock()

	snapshot := sm.trackerHealthCache[instanceID]
	if snapshot == nil {
		return false
	}
	hash := torrent.Hash
	sinceChanged := sm.advanceOneUnregisteredSince(instanceID, torrent, trackers, health, at)
	if snapshot.Health(hash) == health && !sinceChanged {
		return false
	}
	byHash := make(map[string]TrackerHealth, len(snapshot.byHash)+1)
	maps.Copy(byHash, snapshot.byHash)
	unregisteredBy := maps.Clone(snapshot.unregisteredBy)
	delete(unregisteredBy, hash)
	if health == "" {
		delete(byHash, hash)
	} else {
		byHash[hash] = health
	}
	if reason != nil {
		if unregisteredBy == nil {
			unregisteredBy = make(map[string]unregisteredRow, 1)
		}
		unregisteredBy[hash] = *reason
	}
	state := sm.sinceFor(instanceID)
	sm.trackerHealthCache[instanceID] = newTrackerHealthSnapshot(byHash, unregisteredBy, state.times, state.loaded)
	return true
}
