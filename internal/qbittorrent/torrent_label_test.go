// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/infohash"
)

func TestTorrentLabelReadsTheSyncedNameWithoutAQBittorrentRequest(t *testing.T) {
	t.Parallel()
	sm, stub, _ := newHelperRig(t)
	syncs := stub.Calls("sync/maindata")

	hash, err := infohash.Parse(hashA)
	require.NoError(t, err)
	require.Equal(t, TorrentLabel{Name: "Show.S01E01-GRP"}, sm.TorrentLabel(t.Context(), 1, hash))
	require.Equal(t, syncs, stub.Calls("sync/maindata"))

	missing, err := infohash.Parse("cccccccccccccccccccccccccccccccccccccccc")
	require.NoError(t, err)
	require.Zero(t, sm.TorrentLabel(t.Context(), 1, missing))

	require.Zero(t, sm.TorrentLabel(t.Context(), 2, hash), "an instance without a client gives no label")
}

func TestLabelTrackerDomainFallsBackToTheTrackerListOfTheTorrent(t *testing.T) {
	t.Parallel()
	mainData := &qbt.MainData{Trackers: map[string][]string{
		"https://tracker.example.invalid/announce/passkey123":    {hashA},
		"udp://alpha.example.invalid:6969/announce":              {hashA},
		"https://other.example.invalid:2710/passkey456/announce": {hashB},
	}}

	require.Equal(t, "working.example.invalid", labelTrackerDomain(&qbt.Torrent{Hash: hashA, Tracker: "https://working.example.invalid/announce/key"}, mainData))
	for range 20 {
		require.Equal(t, "tracker.example.invalid", labelTrackerDomain(&qbt.Torrent{Hash: hashA}, mainData), "the first URL in sort order wins")
	}
	require.Equal(t, "other.example.invalid", labelTrackerDomain(&qbt.Torrent{Hash: hashB}, mainData))
	require.Empty(t, labelTrackerDomain(&qbt.Torrent{Hash: "cccccccccccccccccccccccccccccccccccccccc"}, mainData))
	require.Empty(t, labelTrackerDomain(&qbt.Torrent{Hash: hashA}, nil))
}
