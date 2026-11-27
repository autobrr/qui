// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/infohash"
)

const (
	refRigHash1   = "aa01aa01aa01aa01aa01aa01aa01aa01aa01aa01"
	refRigHash2   = "aa02aa02aa02aa02aa02aa02aa02aa02aa02aa02"
	refRigHash3   = "aa03aa03aa03aa03aa03aa03aa03aa03aa03aa03"
	refRigHash4   = "aa04aa04aa04aa04aa04aa04aa04aa04aa04aa04"
	refRigHash5   = "aa05aa05aa05aa05aa05aa05aa05aa05aa05aa05"
	refRigMissing = "ffffffffffffffffffffffffffffffffffffffff"
)

func newRefRig(t *testing.T) (*SyncManager, *Client) {
	t.Helper()

	maindata := fmt.Sprintf(`{"rid":1,"full_update":true,"torrents":{
		%q:{"name":"Show.S01E01-GRP","state":"stalledUP","category":"tv","tags":"keep, cross-seed"},
		%q:{"name":"Show.S01E02-GRP","state":"downloading","category":"tv","tags":"keep"},
		%q:{"name":"Film.2024-GRP","state":"pausedUP","category":"movies","tags":""},
		%q:{"name":"Film.2025-GRP","state":"missingFiles","category":"movies","tags":"cross-seed"},
		%q:{"name":"Album-GRP","state":"checkingUP","category":"","tags":"keeper"}
	}}`, refRigHash1, refRigHash2, refRigHash3, refRigHash4, refRigHash5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			_, _ = io.WriteString(w, maindata)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClientWithTimeout(1, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	sm := NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
	sm.instanceName = func(context.Context, int) (string, error) { return "seedbox", nil }
	return sm, client
}

func TestGetTorrentRefsSelectsSyncSnapshotTorrents(t *testing.T) {
	t.Parallel()
	sm, client := newRefRig(t)
	snapshot := client.SyncOwner().SnapshotUnchecked()

	for _, tc := range []struct {
		name   string
		filter TorrentRefFilter
		want   []string
	}{
		{"all", TorrentRefFilter{}, []string{refRigHash1, refRigHash2, refRigHash3, refRigHash4, refRigHash5}},
		{"one hash", TorrentRefFilter{Hashes: infohash.ParseAll([]string{refRigHash3})}, []string{refRigHash3}},
		{"hashes out of order, a repeat, and a missing one", TorrentRefFilter{Hashes: infohash.ParseAll([]string{refRigHash4, refRigHash1, refRigMissing, refRigHash4})}, []string{refRigHash4, refRigHash1}},
		{"a missing hash", TorrentRefFilter{Hashes: infohash.ParseAll([]string{refRigMissing})}, []string{}},
		{"category", TorrentRefFilter{Category: "movies"}, []string{refRigHash3, refRigHash4}},
		{"tag", TorrentRefFilter{Tag: "keep"}, []string{refRigHash1, refRigHash2}},
		{"completed", TorrentRefFilter{Completed: true}, []string{refRigHash1, refRigHash3, refRigHash5}},
		{"completed in a category", TorrentRefFilter{Category: "tv", Completed: true}, []string{refRigHash1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refs, err := sm.GetTorrentRefs(t.Context(), 1, tc.filter)
			require.NoError(t, err)

			got := make([]string, 0, len(refs))
			for _, ref := range refs {
				shared, ok := snapshot.Torrent(ref.Hash)
				require.True(t, ok)
				require.Same(t, shared, ref, "a read returns the torrent of the Sync snapshot")
				got = append(got, ref.Hash)
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestInstanceTorrentViewsShareTheSyncSnapshot(t *testing.T) {
	t.Parallel()
	sm, client := newRefRig(t)
	snapshot := client.SyncOwner().SnapshotUnchecked()

	cached, err := sm.GetCachedInstanceTorrents(t.Context(), 1)
	require.NoError(t, err)
	ridden, err := sm.ReadInstanceTorrents(t.Context(), 1)
	require.NoError(t, err)

	require.Len(t, cached, snapshot.Len())
	require.Len(t, ridden, snapshot.Len())
	for i := range cached {
		shared, ok := snapshot.Torrent(cached[i].Hash)
		require.True(t, ok)
		require.Same(t, shared, cached[i].Torrent)
		require.Same(t, shared, ridden[i].Torrent)
		require.Equal(t, "seedbox", cached[i].InstanceName)
	}
}
