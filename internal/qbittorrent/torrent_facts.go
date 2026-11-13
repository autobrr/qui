// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"

	qbt "github.com/autobrr/go-qbittorrent"
)

// TorrentFacts is what qui decides about one torrent beyond its fields.
type TorrentFacts struct {
	// TrackerDomains are the Trackers of the torrent, by the rules of the
	// tracker rows of the sidebar.
	TrackerDomains []string
	// Private is nil when qBittorrent does not report the private flag.
	Private  *bool
	Complete bool
}

// TorrentFactsOf returns the facts of each torrent of a sync of an instance,
// in the order of torrents. trackers is the tracker list of the sync.
func (sm *SyncManager) TorrentFactsOf(instanceID int, torrents []*qbt.Torrent, trackers map[string][]string) []TorrentFacts {
	client, _ := sm.clientPool.GetClientOffline(context.Background(), instanceID)
	in := torrentCountsInput{
		torrents:       torrents,
		domainToHashes: sm.getAuthoritativeDomainToHashes(instanceID),
		trackers:       sm.trackerSources(client, &qbt.MainData{Trackers: trackers}),
	}
	facts := make([]TorrentFacts, len(torrents))
	resolveTrackers(in, func(i int, domain, _ string) {
		facts[i].TrackerDomains = append(facts[i].TrackerDomains, domain)
	}, func(int) {})
	for i, t := range torrents {
		if !PrivateUnknown(t) {
			facts[i].Private = new(t.Private)
		}
		facts[i].Complete = isTorrentComplete(t)
	}
	return facts
}

// UniqueSizeOf returns the Unique size (GLOSSARY.md) of torrents, the full
// torrent list of a sync of an instance. ok is false until a Copy groups
// build that read the instance ends.
func (sm *SyncManager) UniqueSizeOf(instanceID int, torrents []*qbt.Torrent) (size int64, ok bool) {
	unique := sm.copyGroups.Load().uniqueSizes(instanceID)
	if unique == nil {
		return 0, false
	}
	shared := findSharedContentPaths(torrents)
	for i, t := range torrents {
		unique.add(t, shared[i], false)
	}
	size, _ = unique.totals()
	return size, true
}
