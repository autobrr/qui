// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"encoding/json"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// TestTorrentViewJSONOmitsInternalFields pins the shadow fields on TorrentView:
// fields used only by the backend must never serialize in list/SSE rows. Marshaling
// the cross-instance view exercises TorrentView through its deepest embedding,
// covering both row types in one pass.
func TestTorrentViewJSONOmitsInternalFields(t *testing.T) {
	t.Parallel()

	hasMetadata := false
	torrent := &qbt.Torrent{
		Hash:        "aaa",
		HasMetadata: &hasMetadata,
		Name:        "Alpha",
		MagnetURI:   "magnet:?xt=urn:btih:aaa",

		HasTrackerWarning:     true,
		HasTrackerError:       true,
		HasOtherAnnounceError: true,
	}

	data, err := json.Marshal(CrossInstanceTorrentView{
		TorrentView: &TorrentView{Torrent: torrent},
		InstanceID:  1,
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotContains(t, decoded, "magnet_uri")
	require.NotContains(t, decoded, "has_metadata")
	require.NotContains(t, decoded, "has_tracker_warning")
	require.NotContains(t, decoded, "has_tracker_error")
	require.NotContains(t, decoded, "has_other_announce_error")
	require.Equal(t, "aaa", decoded["hash"])
	require.Equal(t, "Alpha", decoded["name"])
}

// Before Web API 2.10.0, qBittorrent leaves reannounce out of the torrent list, and
// go-qbittorrent decodes the missing key as 0. A row must leave the key out then,
// because 0 means "now" on an instance that sends it.
func TestTorrentViewJSONReannounceFollowsInstance(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		live bool
		want any
	}{
		{name: "before 2.10.0", live: false, want: nil},
		{name: "2.10.0 and later", live: true, want: float64(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(newTorrentView(&qbt.Torrent{Hash: "aaa"}, "", tc.live))
			require.NoError(t, err)

			var decoded map[string]any
			require.NoError(t, json.Unmarshal(data, &decoded))
			require.Equal(t, tc.want, decoded["reannounce"])
		})
	}
}

// The SSE stream keeps page-sized copies of the rows. A pointer into the source
// torrent would keep the whole library array alive behind each page.
func TestNewTorrentViewCopiesReannounce(t *testing.T) {
	t.Parallel()

	torrent := &qbt.Torrent{Hash: "aaa", Reannounce: 840}
	tv := newTorrentView(torrent, "", true)
	require.NotSame(t, &torrent.Reannounce, tv.Reannounce)
	require.Equal(t, int64(840), *tv.Reannounce)
}
