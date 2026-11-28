// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package verdict

import (
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/infohash"
)

const (
	unregisteredHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	workingHash      = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestWaitedUnregisteredByGivesTheTrackerOfAnUnregisteredTorrentThatIsNotCleared(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	snapshot := qbittorrent.NewTrackerHealthSnapshotWithTrackers(
		map[string]qbittorrent.TrackerHealth{unregisteredHash: qbittorrent.TrackerHealthUnregistered},
		map[string]time.Time{unregisteredHash: now.Add(-time.Hour)},
		map[string][]qbt.TorrentTracker{unregisteredHash: {
			{Url: "** [DHT] **", Status: qbt.TrackerStatusOK},
			{Url: "https://tracker.example.invalid/announce", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"},
		}},
	)
	cleared := infohash.NewSet()
	hash, err := infohash.Parse(unregisteredHash)
	require.NoError(t, err)
	cleared.Add(hash)

	tests := []struct {
		name   string
		waited Waited
		hash   string
		want   qbittorrent.UnregisteredTracker
		found  bool
	}{
		{name: "unregistered", waited: ForDelete(snapshot, now), hash: unregisteredHash,
			want: qbittorrent.UnregisteredTracker{Host: "tracker.example.invalid", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"}, found: true},
		{name: "cleared by the fresh read", waited: ForDelete(snapshot, now).Without(cleared), hash: unregisteredHash},
		{name: "not unregistered", waited: ForDelete(snapshot, now), hash: workingHash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := tt.waited.UnregisteredBy(tt.hash)
			require.Equal(t, tt.found, found)
			require.Equal(t, tt.want, got)
		})
	}
}
