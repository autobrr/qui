// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestTrackerOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, in, want string }{
		{"passkey in the path", "https://tracker.example.invalid/abc123xyz/announce", "https://tracker.example.invalid"},
		{"passkey in the query", "http://tracker.example.invalid/announce?passkey=abc", "http://tracker.example.invalid"},
		{"passkey in the user info", "https://user:abc@tracker.example.invalid/announce", "https://tracker.example.invalid"},
		{"port stays", "udp://tracker.example.invalid:1337/abc/announce", "udp://tracker.example.invalid:1337"},
		{"DHT row stays", "** [DHT] **", "** [DHT] **"},
		{"no scheme can hold a passkey", "tracker.example.invalid/abc/announce", ""},
		{"no host can hold a passkey", "https:///abc/announce", ""},
		{"not a URL can hold a passkey", "https://tracker.example.invalid:abc/announce", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, TrackerOrigin(tc.in))
		})
	}
}

func TestWithoutTrackerPasskeysKeepsTheCachedRows(t *testing.T) {
	t.Parallel()
	const full = "https://tracker.example.invalid/abc/announce"
	const origin = "https://tracker.example.invalid"
	cached := func() *qbt.Torrent {
		return &qbt.Torrent{Tracker: full, Trackers: []qbt.TorrentTracker{{Url: full}}}
	}
	single, cross := cached(), cached()
	response := &TorrentResponse{
		Torrents:              []TorrentView{{Torrent: single}},
		CrossInstanceTorrents: []CrossInstanceTorrentView{{TorrentView: &TorrentView{Torrent: cross}, InstanceID: 1}},
	}

	hidden := response.withoutTrackerPasskeys()

	want := &qbt.Torrent{Tracker: origin, Trackers: []qbt.TorrentTracker{{Url: origin}}}
	require.Equal(t, want, hidden.Torrents[0].Torrent)
	require.Equal(t, want, hidden.CrossInstanceTorrents[0].Torrent)
	require.Equal(t, cached(), single)
	require.Equal(t, cached(), cross)
	require.Same(t, single, response.Torrents[0].Torrent)
	require.Same(t, cross, response.CrossInstanceTorrents[0].Torrent)
}

func TestCleanTrackerMessageRemovesEveryPassKeyForm(t *testing.T) {
	t.Parallel()
	announce := []string{"https://tracker.example.invalid/f00dfeedcafe1234/announce?torrent_pass=beefbeefbeef5678", "udp://other.example.invalid:1337/announce"}
	for _, tc := range []struct{ name, in, want string }{
		{"plain message stays", "Unregistered torrent", "Unregistered torrent"},
		{"https URL", "Torrent not registered, see https://tracker.example.invalid/f00dfeedcafe1234/announce", "Torrent not registered, see"},
		{"udp URL", "unregistered (udp://other.example.invalid:1337/xyz9876543)", "unregistered ("},
		{"address without a scheme", "failed for //tracker.example.invalid/secret999", "failed for"},
		{"bare path key", "passkey f00dfeedcafe1234 is not registered", "passkey is not registered"},
		{"bare query key", "torrent_pass beefbeefbeef5678 unknown", "torrent_pass unknown"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, CleanTrackerMessage(tc.in, announce))
		})
	}
}

func TestTrackersWithoutPasskeysCleansTheMessage(t *testing.T) {
	t.Parallel()
	const full = "https://tracker.example.invalid/f00dfeedcafe1234/announce"
	hidden := TrackersWithoutPasskeys([]qbt.TorrentTracker{{Url: full, Message: "f00dfeedcafe1234: Unregistered torrent https://tracker.example.invalid/x"}})
	require.Equal(t, []qbt.TorrentTracker{{Url: "https://tracker.example.invalid", Message: ": Unregistered torrent"}}, hidden)
}
