// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"

	qbt "github.com/autobrr/go-qbittorrent"
)

type TorrentFacts struct {
	TrackerDomains []string
	Private        *bool
	Complete       bool
}

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
