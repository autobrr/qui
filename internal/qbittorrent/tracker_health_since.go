// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/pkg/infohash"
)

type UnregisteredSinceStore interface {
	ListUnregisteredSince(ctx context.Context, instanceID int) (map[string]time.Time, error)
	PutUnregisteredSince(ctx context.Context, instanceID int, since map[string]time.Time) error
	DeleteUnregisteredSince(ctx context.Context, instanceID int, hashes []string) error
}

func (sm *SyncManager) SetUnregisteredSinceStore(store UnregisteredSinceStore) {
	sm.sinceStore.Store(&store)
}

type unregisteredSince struct {
	loaded bool
	times  map[string]time.Time
	stored map[string]time.Time
	edited map[string]time.Time
}

func (sm *SyncManager) sinceFor(instanceID int) *unregisteredSince {
	state := sm.trackerHealthSince[instanceID]
	if state == nil {
		state = &unregisteredSince{}
		if sm.trackerHealthSince == nil {
			sm.trackerHealthSince = make(map[int]*unregisteredSince)
		}
		sm.trackerHealthSince[instanceID] = state
	}
	return state
}

func (sm *SyncManager) healthNow() time.Time {
	if sm.trackerHealthNow == nil {
		return time.Now()
	}
	return sm.trackerHealthNow()
}

func (sm *SyncManager) loadUnregisteredSince(ctx context.Context, instanceID int) {
	sm.trackerHealthMu.RLock()
	loaded := sm.trackerHealthSince[instanceID].isLoaded()
	sm.trackerHealthMu.RUnlock()
	if loaded {
		return
	}

	var rows map[string]time.Time
	if store := sm.sinceStore.Load(); store != nil {
		var err error
		rows, err = (*store).ListUnregisteredSince(ctx, instanceID)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to load unregistered since times, delete rules that read the unregistered verdict wait")
			return
		}
	}

	sm.trackerHealthMu.Lock()
	state := sm.sinceFor(instanceID)
	state.loaded = true
	state.stored = maps.Clone(rows)
	maps.DeleteFunc(rows, func(hash string, _ time.Time) bool {
		_, edited := state.edited[hash]
		return edited
	})
	state.times = rows
	sm.trackerHealthMu.Unlock()
}

func (state *unregisteredSince) isLoaded() bool {
	return state != nil && state.loaded
}

func (state *unregisteredSince) current() map[string]time.Time {
	if state == nil {
		return nil
	}
	return state.times
}

func (sm *SyncManager) markTrackersEdited(instanceID int, hashes []string, at time.Time) {
	state := sm.sinceFor(instanceID)
	if state.edited == nil {
		state.edited = make(map[string]time.Time, len(hashes))
	}
	times := maps.Clone(state.times)
	for _, hash := range hashes {
		state.edited[hash] = at
		delete(times, hash)
	}
	state.times = times
}

func (sm *SyncManager) forgetTrackerHealth(ctx context.Context, instanceID int, hashes []string, trackersChanged bool) {
	sm.trackerHealthMu.Lock()
	if trackersChanged {
		for _, cache := range []*trackerDetails{sm.trackerHealthFlagged[instanceID], sm.automationTrackerDetails[instanceID]} {
			if cache == nil {
				continue
			}
			if cache.edited == nil {
				cache.edited = make(map[string]struct{}, len(hashes))
			}
			for _, hash := range hashes {
				cache.edited[hash] = struct{}{}
			}
		}
	}

	sm.markTrackersEdited(instanceID, hashes, sm.healthNow())
	snapshot := sm.trackerHealthCache[instanceID]
	changed := snapshot != nil && slices.ContainsFunc(hashes, func(hash string) bool {
		_, has := snapshot.since[hash]
		return has || trackersChanged && snapshot.byHash[hash] != ""
	})
	if changed {
		byHash := snapshot.byHash
		if trackersChanged {
			byHash = maps.Clone(byHash)
			for _, hash := range hashes {
				delete(byHash, hash)
			}
		}
		state := sm.sinceFor(instanceID)
		sm.trackerHealthCache[instanceID] = newTrackerHealthSnapshot(byHash, snapshot.unregisteredBy, state.times, state.loaded)
	}
	sm.trackerHealthMu.Unlock()

	sm.storeUnregisteredSince(ctx, instanceID)
	if changed {
		sm.notifyTrackerHealthUpdated(instanceID)
	}
}

type sinceStep struct {
	torrent  *qbt.Torrent
	trackers []qbt.TorrentTracker
	health   TrackerHealth
	now      time.Time
	started  time.Time
}

func (state *unregisteredSince) next(prev time.Time, has bool, step sinceStep) (time.Time, bool) {
	torrent := step.torrent
	if torrentStopped(torrent) || anyTrackerWorking(step.trackers) {
		return time.Time{}, false
	}
	if has && time.Unix(torrent.AddedOn, 0).After(prev) {
		has = false
	}
	if has {
		return prev, true
	}
	if step.health != TrackerHealthUnregistered {
		return time.Time{}, false
	}
	if editedAt, ok := state.edited[torrent.Hash]; ok && !step.started.After(editedAt) {
		return time.Time{}, false
	}
	return step.now, true
}

func (sm *SyncManager) advanceUnregisteredSince(instanceID int, torrents []*qbt.Torrent, details TrackerLookup, byHash map[string]TrackerHealth, complete bool, started, now time.Time) {
	state := sm.sinceFor(instanceID)
	if !state.loaded {
		return
	}
	next := make(map[string]time.Time, len(state.times))
	if !complete {
		maps.Copy(next, state.times)
	}
	for _, torrent := range torrents {
		prev, has := state.times[torrent.Hash]
		if !has && byHash[torrent.Hash] != TrackerHealthUnregistered {
			continue
		}
		since, keep := state.next(prev, has, sinceStep{torrent: torrent, trackers: details.Trackers(torrent.Hash), health: byHash[torrent.Hash], now: now, started: started})
		if keep {
			next[torrent.Hash] = since
		} else {
			delete(next, torrent.Hash)
		}
	}
	state.times = next
	maps.DeleteFunc(state.edited, func(_ string, at time.Time) bool { return at.Before(started) })
}

func (sm *SyncManager) advanceOneUnregisteredSince(instanceID int, torrent *qbt.Torrent, trackers []qbt.TorrentTracker, health TrackerHealth, at time.Time) bool {
	state := sm.sinceFor(instanceID)
	if !state.loaded {
		return false
	}
	prev, has := state.times[torrent.Hash]
	since, keep := state.next(prev, has, sinceStep{torrent: torrent, trackers: trackers, health: health, now: at, started: at})
	if keep == has && since.Equal(prev) {
		return false
	}
	times := maps.Clone(state.times)
	if times == nil {
		times = make(map[string]time.Time, 1)
	}
	if keep {
		times[torrent.Hash] = since
	} else {
		delete(times, torrent.Hash)
	}
	state.times = times
	return true
}

func (sm *SyncManager) hasUnregisteredSince(instanceID int, hash string) bool {
	sm.trackerHealthMu.RLock()
	defer sm.trackerHealthMu.RUnlock()
	_, has := sm.trackerHealthSince[instanceID].current()[hash]
	return has
}

func (sm *SyncManager) unregisteredSinceHashes(instanceID int) map[string]time.Time {
	sm.trackerHealthMu.RLock()
	defer sm.trackerHealthMu.RUnlock()
	return sm.trackerHealthSince[instanceID].current()
}

func (sm *SyncManager) storeUnregisteredSince(ctx context.Context, instanceID int) {
	store := sm.sinceStore.Load()
	if store == nil {
		return
	}
	sm.sinceStoreMu.Lock()
	defer sm.sinceStoreMu.Unlock()

	sm.trackerHealthMu.RLock()
	state := sm.trackerHealthSince[instanceID]
	if !state.isLoaded() {
		sm.trackerHealthMu.RUnlock()
		return
	}
	times, stored := state.times, state.stored
	sm.trackerHealthMu.RUnlock()

	var removed []string
	for hash := range stored {
		if _, ok := times[hash]; !ok {
			removed = append(removed, hash)
		}
	}
	added := make(map[string]time.Time)
	for hash, since := range times {
		if prev, ok := stored[hash]; !ok || !prev.Equal(since) {
			added[hash] = since
		}
	}
	if len(removed) == 0 && len(added) == 0 {
		return
	}

	next := maps.Clone(stored)
	if next == nil {
		next = make(map[string]time.Time, len(added))
	}
	if len(removed) > 0 {
		slices.Sort(removed)
		if err := (*store).DeleteUnregisteredSince(ctx, instanceID, removed); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Int("count", len(removed)).Msg("Failed to remove unregistered since times, retrying on the next pass")
		} else {
			for _, hash := range removed {
				delete(next, hash)
			}
		}
	}
	if len(added) > 0 {
		if err := (*store).PutUnregisteredSince(ctx, instanceID, added); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Int("count", len(added)).Msg("Failed to store unregistered since times, retrying on the next pass")
		} else {
			maps.Copy(next, added)
		}
	}

	sm.trackerHealthMu.Lock()
	sm.sinceFor(instanceID).stored = next
	sm.trackerHealthMu.Unlock()
}

func torrentStopped(t *qbt.Torrent) bool {
	switch t.State {
	case qbt.TorrentStateStoppedUp, qbt.TorrentStateStoppedDl, qbt.TorrentStatePausedUp, qbt.TorrentStatePausedDl:
		return true
	default:
		return false
	}
}

func anyTrackerWorking(trackers []qbt.TorrentTracker) bool {
	return slices.ContainsFunc(trackers, func(tracker qbt.TorrentTracker) bool {
		return tracker.Status == qbt.TrackerStatusOK && !pseudoTrackerRow(tracker.Url)
	})
}

func pseudoTrackerRow(url string) bool {
	if strings.Contains(url, "://") {
		return false
	}
	label := strings.Trim(url, "*[] ")
	return strings.EqualFold(label, "dht") || strings.EqualFold(label, "pex") || strings.EqualFold(label, "lsd")
}

type UnregisteredTracker struct {
	Host    string
	Status  qbt.TrackerStatus
	Message string
}

type unregisteredRow struct {
	trackers []qbt.TorrentTracker
	by       int
}

func unregisteredTrackerOf(trackers []qbt.TorrentTracker, i int) UnregisteredTracker {
	urls := make([]string, len(trackers))
	for j := range trackers {
		urls[j] = trackers[j].Url
	}
	var host string
	if u, err := url.Parse(trackers[i].Url); err == nil {
		host = u.Hostname()
	}
	return UnregisteredTracker{Host: host, Status: trackers[i].Status, Message: CleanTrackerMessage(trackers[i].Message, urls)}
}

func (s *TrackerHealthSnapshot) UnregisteredSince(hash string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	since, ok := s.since[hash]
	return since, ok
}

func (s *TrackerHealthSnapshot) UnregisteredSinceLoaded() bool {
	return s != nil && s.sinceLoaded
}

func (s *TrackerHealthSnapshot) UnregisteredBy(hash string) (UnregisteredTracker, bool) {
	if s == nil {
		return UnregisteredTracker{}, false
	}
	row, ok := s.unregisteredBy[hash]
	if !ok {
		return UnregisteredTracker{}, false
	}
	return unregisteredTrackerOf(row.trackers, row.by), true
}

func NewTrackerHealthSnapshotWithSince(byHash map[string]TrackerHealth, since map[string]time.Time) *TrackerHealthSnapshot {
	return newTrackerHealthSnapshot(byHash, nil, since, true)
}

func (sm *SyncManager) StillUnregistered(ctx context.Context, instanceID int, hashes []string) (*infohash.Set, error) {
	client, owner, err := sm.getClientAndSyncManager(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	now := sm.healthNow()
	index := NewHashIndex(owner.SnapshotUnchecked())
	still := infohash.NewSet()
	for chunk := range slices.Chunk(hashes, trackerDetailChunkSize) {
		rows, err := client.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{Hashes: chunk, IncludeTrackers: true})
		if err != nil {
			return nil, err
		}
		var recovered []seenTrackers
		var stopped []string
		for i := range rows {
			hash, err := infohash.Parse(rows[i].Hash)
			if err != nil {
				continue
			}
			if trackerHealthOf(&rows[i], rows[i].Trackers, now) == TrackerHealthUnregistered {
				still.Add(hash)
				continue
			}
			if torrent, ok := index.Torrent(hash); ok {
				recovered = append(recovered, seenTrackers{hash: torrent.Hash, flags: announceFlagsOf(torrent), trackers: rows[i].Trackers, at: now})
				if torrentStopped(&rows[i]) {
					stopped = append(stopped, torrent.Hash)
				}
			}
		}
		if len(recovered) > 0 {
			sm.applySeenTrackers(ctx, instanceID, recovered...)
		}
		if len(stopped) > 0 {
			sm.forgetTrackerHealth(ctx, instanceID, stopped, false)
		}
	}
	return still, nil
}
