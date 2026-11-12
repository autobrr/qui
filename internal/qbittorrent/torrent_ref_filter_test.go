// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newRefRig is a sync manager over a qBittorrent stub that holds torrents in
// several states, categories, and tags.
func newRefRig(t *testing.T) (*SyncManager, *Client) {
	t.Helper()

	const maindata = `{"rid":1,"full_update":true,"torrents":{
		"aa01":{"name":"Show.S01E01-GRP","state":"stalledUP","category":"tv","tags":"keep, cross-seed"},
		"aa02":{"name":"Show.S01E02-GRP","state":"downloading","category":"tv","tags":"keep"},
		"aa03":{"name":"Film.2024-GRP","state":"pausedUP","category":"movies","tags":""},
		"aa04":{"name":"Film.2025-GRP","state":"missingFiles","category":"movies","tags":"cross-seed"},
		"aa05":{"name":"Album-GRP","state":"checkingUP","category":"","tags":"keeper"}
	}}`
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

	// Each want holds what the qBittorrent filter of go-qbittorrent selects for
	// the same options, except that a read by hash keeps the order of the hashes.
	for _, tc := range []struct {
		name   string
		filter TorrentRefFilter
		want   []string
	}{
		{"all", TorrentRefFilter{}, []string{"aa01", "aa02", "aa03", "aa04", "aa05"}},
		{"one hash", TorrentRefFilter{Hashes: []string{"aa03"}}, []string{"aa03"}},
		{"hashes out of order, a repeat, and a missing one", TorrentRefFilter{Hashes: []string{"aa04", "aa01", "ffff", "aa04"}}, []string{"aa04", "aa01"}},
		{"a missing hash", TorrentRefFilter{Hashes: []string{"ffff"}}, []string{}},
		{"category", TorrentRefFilter{Category: "movies"}, []string{"aa03", "aa04"}},
		{"tag", TorrentRefFilter{Tag: "keep"}, []string{"aa01", "aa02"}},
		{"completed", TorrentRefFilter{Completed: true}, []string{"aa01", "aa03", "aa05"}},
		{"completed in a category", TorrentRefFilter{Category: "tv", Completed: true}, []string{"aa01"}},
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
