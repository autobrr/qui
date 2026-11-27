// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"slices"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

const trackerDetailChunkSize = 100

type announceFlags struct {
	trackerWarning     bool
	trackerError       bool
	otherAnnounceError bool
}

func announceFlagsOf(t *qbt.Torrent) announceFlags {
	return announceFlags{
		trackerWarning:     t.HasTrackerWarning,
		trackerError:       t.HasTrackerError,
		otherAnnounceError: t.HasOtherAnnounceError,
	}
}

type TrackerLookup map[string][]qbt.TorrentTracker

func (l TrackerLookup) Trackers(hash string) []qbt.TorrentTracker {
	return l[hash]
}

type trackerDetail struct {
	flags     announceFlags
	trackers  []qbt.TorrentTracker
	fetchedAt time.Time
}

type trackerDetails struct {
	mu     sync.Mutex
	byHash map[string]trackerDetail
	edited map[string]struct{}
}

func (sm *SyncManager) trackerDetailsFor(caches map[int]*trackerDetails, instanceID int) *trackerDetails {
	sm.trackerHealthMu.Lock()
	defer sm.trackerHealthMu.Unlock()
	cache := caches[instanceID]
	if cache == nil {
		cache = &trackerDetails{}
		caches[instanceID] = cache
	}
	return cache
}

func (sm *SyncManager) hydrateTrackerDetails(ctx context.Context, client *Client, cache *trackerDetails, torrents []*qbt.Torrent, flaggedOnly bool) (TrackerLookup, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	sm.trackerHealthMu.Lock()
	edited := cache.edited
	cache.edited = nil
	sm.trackerHealthMu.Unlock()

	now := time.Now()
	next := make(map[string]trackerDetail, len(cache.byHash))
	var stale []string
	for _, torrent := range torrents {
		flags := announceFlagsOf(torrent)
		if flaggedOnly && flags == (announceFlags{}) {
			continue
		}
		hash := torrent.Hash
		prev, ok := cache.byHash[hash]
		_, wasEdited := edited[hash]
		if ok && !wasEdited && prev.flags == flags && now.Sub(prev.fetchedAt) < sm.trackerHealthDetailMaxAge {
			next[hash] = prev
			continue
		}
		next[hash] = trackerDetail{flags: flags}
		stale = append(stale, hash)
	}

	fetched, fetchErr := fetchTrackerDetails(ctx, client, stale)
	for _, hash := range stale {
		trackers, ok := fetched[hash]
		if !ok {
			delete(next, hash)
			continue
		}
		entry := next[hash]
		entry.trackers = trackers
		entry.fetchedAt = now
		next[hash] = entry
	}
	cache.byHash = next

	lookup := make(TrackerLookup, len(next))
	for hash, entry := range next {
		lookup[hash] = entry.trackers
	}
	return lookup, fetchErr
}

func fetchTrackerDetails(ctx context.Context, client *Client, hashes []string) (map[string][]qbt.TorrentTracker, error) {
	fetched := make(map[string][]qbt.TorrentTracker, len(hashes))
	for chunk := range slices.Chunk(hashes, trackerDetailChunkSize) {
		rows, err := client.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{Hashes: chunk, IncludeTrackers: true})
		if err != nil {
			return fetched, err
		}
		for i := range rows {
			fetched[rows[i].Hash] = rows[i].Trackers
		}
	}
	return fetched, nil
}
