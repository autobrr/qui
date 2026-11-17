// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"slices"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// The first sync after a start of qui comes before the tracker mapping, so the
// facts must not depend on the mapping.
// regression: #4100
func TestTorrentFactsOfGivesEachDomainOfTheSyncBeforeTheMapping(t *testing.T) {
	t.Parallel()

	pool, err := NewClientPool(nil, nil, time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	sm := NewSyncManager(pool, nil)

	torrents := []qbt.Torrent{{
		Hash:          "aaaa000000000000000000000000000000000001",
		Tracker:       "https://charlie.example/announce",
		TrackersCount: 3,
	}}
	trackers := map[string][]string{
		"https://alpha.example/announce":   {torrents[0].Hash},
		"https://bravo.example/announce":   {torrents[0].Hash},
		"https://charlie.example/announce": {torrents[0].Hash},
	}
	domains := func() []string {
		got := sm.TorrentFactsOf(1, torrentRefs(torrents), trackers)[0].TrackerDomains
		slices.Sort(got)
		return got
	}

	want := []string{"alpha.example", "bravo.example", "charlie.example"}
	require.Equal(t, want, domains(), "no mapping yet")

	sm.storeTrackerMapping(1, sm.trackerMappingFromMainData(torrentRefs(torrents), &qbt.MainData{Trackers: trackers}))
	require.Equal(t, want, domains(), "the mapping holds the torrent")
}
