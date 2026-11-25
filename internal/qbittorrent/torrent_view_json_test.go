// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"encoding/json"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

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
			data, err := json.Marshal((&rowBuilder{reannounce: tc.live}).view(0, &qbt.Torrent{Hash: "aaa"}, ""))
			require.NoError(t, err)

			var decoded map[string]any
			require.NoError(t, json.Unmarshal(data, &decoded))
			require.Equal(t, tc.want, decoded["reannounce"])
		})
	}
}

func TestRowBuilderCopiesReannounce(t *testing.T) {
	t.Parallel()

	torrent := &qbt.Torrent{Hash: "aaa", Reannounce: 840}
	rows := rowBuilder{reannounce: true, values: make([]rowValues, 1)}
	tv := rows.view(0, torrent, "")
	require.NotSame(t, &torrent.Reannounce, tv.Reannounce)
	require.Equal(t, int64(840), *tv.Reannounce)
}

func rowJSON(t *testing.T, row any) map[string]any {
	t.Helper()
	data, err := json.Marshal(row)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	return decoded
}

func TestTorrentListLeavesOutPopularityAndPrivateThatTheInstanceDoesNotSend(t *testing.T) {
	t.Parallel()
	store, err := models.NewInstanceStore(testdb.NewMigratedSQLite(t, "qbittorrent-unknown-list-fields"), make([]byte, 32))
	require.NoError(t, err)

	type fields struct {
		popularity, private any
	}
	want := map[string]map[string]fields{
		"2.9.3":  {hashA: {}, hashB: {}},
		"2.15.1": {hashA: {popularity: 1.5, private: true}, hashB: {popularity: float64(0)}},
	}
	clients := map[int]*Client{}
	versions := map[int]string{}
	for version := range want {
		instance, err := store.Create(t.Context(), version, "http://127.0.0.1:8080", "", "", nil, nil, false, nil)
		require.NoError(t, err)
		stub := qbtstub.New(t, "/data")
		stub.WebAPIVersion = version
		stub.Add(qbtstub.Torrent{Hash: hashA, Name: "Show.S01E01-GRP", SavePath: "/data", Popularity: 1.5, Private: true})
		stub.Add(qbtstub.Torrent{Hash: hashB, Name: "Show.S01E02-GRP", SavePath: "/data", NoMetadata: true})
		client, err := NewClientWithTimeout(instance.ID, stub.URL, "", "", "", nil, nil, false, time.Second, time.Second)
		require.NoError(t, err)
		t.Cleanup(client.optimisticUpdates.Close)
		require.NoError(t, client.SyncOwner().Sync(t.Context()))
		clients[instance.ID] = client
		versions[instance.ID] = version
	}
	sm := NewSyncManager(&ClientPool{instanceStore: store, clients: clients}, nil)

	check := func(t *testing.T, version string, row map[string]any) {
		t.Helper()
		hash, _ := row["hash"].(string)
		w := want[version][hash]
		for key, value := range map[string]any{"popularity": w.popularity, "private": w.private} {
			got, ok := row[key]
			require.Equal(t, value != nil, ok, "%s key of %s at Web API %s", key, hash, version)
			require.Equal(t, value, got, "%s of %s at Web API %s", key, hash, version)
		}
	}

	for id, version := range versions {
		resp, err := sm.ListTorrents(t.Context(), TorrentListView{InstanceID: id})
		require.NoError(t, err)
		require.Len(t, resp.Torrents, 2)
		for _, row := range resp.Torrents {
			check(t, version, rowJSON(t, row))
		}
	}

	resp, err := sm.ListTorrents(t.Context(), TorrentListView{Unified: true})
	require.NoError(t, err)
	require.Len(t, resp.CrossInstanceTorrents, 4)
	for _, row := range resp.CrossInstanceTorrents {
		check(t, versions[row.InstanceID], rowJSON(t, row))
	}
}
