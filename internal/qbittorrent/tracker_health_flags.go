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

// trackerDetailChunkSize is the number of hashes in one torrents/info request.
const trackerDetailChunkSize = 100

// announceFlags are the per-torrent announce flags that qBittorrent 5.2+ sends
// in sync/maindata. The zero value means no tracker problem.
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

type trackerDetail struct {
	flags     announceFlags
	trackers  []qbt.TorrentTracker
	fetchedAt time.Time
}

// trackerDetails caches the tracker details of the torrents of one instance.
// mu also keeps two hydrations of the same cache from running at once.
type trackerDetails struct {
	mu     sync.Mutex
	byHash map[string]trackerDetail
	// edited holds the hashes whose trackers changed in qui since the last
	// hydration, so their details are fetched again. SyncManager.trackerHealthMu
	// guards it, so a tracker edit does not wait for a hydration to finish.
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

// hydrateTrackerDetails sets Trackers on the torrents from cache. It fetches
// details only for a torrent whose flags changed since the last hydration, or
// whose details are older than trackerHealthDetailMaxAge. With flaggedOnly, a
// torrent without an announce flag gets no Trackers, so its tracker health is
// healthy. When the fetch fails, it keeps and sets the details that it fetched
// before the error.
func (sm *SyncManager) hydrateTrackerDetails(ctx context.Context, client *Client, cache *trackerDetails, torrents []qbt.Torrent, flaggedOnly bool) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	sm.trackerHealthMu.Lock()
	edited := cache.edited
	cache.edited = nil
	sm.trackerHealthMu.Unlock()

	now := time.Now()
	next := make(map[string]trackerDetail, len(cache.byHash))
	var stale []string
	for i := range torrents {
		flags := announceFlagsOf(&torrents[i])
		if flaggedOnly && flags == (announceFlags{}) {
			continue
		}
		hash := torrents[i].Hash
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
			// Either deleted after the sync, or not fetched before an error.
			// Both leave no entry, so the next hydration fetches it.
			delete(next, hash)
			continue
		}
		entry := next[hash]
		entry.trackers = trackers
		entry.fetchedAt = now
		next[hash] = entry
	}
	// Keep the fetched details also after an error, so a large first hydration
	// that times out continues where it stopped.
	cache.byHash = next

	for i := range torrents {
		if entry, ok := next[torrents[i].Hash]; ok {
			torrents[i].Trackers = entry.trackers
		}
	}
	return fetchErr
}

// fetchTrackerDetails fetches the trackers of the given torrents in requests of
// trackerDetailChunkSize hashes. Each request names its hashes: a request
// without a hash filter returns the whole library and blocks qBittorrent for
// seconds in one stretch, while a request of 100 hashes blocks it for about
// 70 ms at 50,000 torrents. After an error it returns what it fetched so far.
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
