// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"encoding/json"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// TestTorrentViewJSONOmitsMagnetURI pins the shadow field on TorrentView: the
// embedded qbt.Torrent.MagnetURI must never serialize in list/SSE rows (issue
// #2328), while the rest of the row stays intact. Marshaling the cross-instance
// view exercises TorrentView through its deepest embedding, covering both row
// types in one pass.
func TestTorrentViewJSONOmitsMagnetURI(t *testing.T) {
	t.Parallel()

	torrent := &qbt.Torrent{
		Hash:      "aaa",
		Name:      "Alpha",
		MagnetURI: "magnet:?xt=urn:btih:aaa",
	}

	data, err := json.Marshal(CrossInstanceTorrentView{
		TorrentView: &TorrentView{Torrent: torrent},
		InstanceID:  1,
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotContains(t, decoded, "magnet_uri")
	require.Equal(t, "aaa", decoded["hash"])
	require.Equal(t, "Alpha", decoded["name"])
}
