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

	qbt "github.com/autobrr/go-qbittorrent"
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

func TestGetTorrentRefsSelectsWhatGetTorrentsSelects(t *testing.T) {
	t.Parallel()
	sm, client := newRefRig(t)
	snapshot := client.SyncOwner().SnapshotUnchecked()

	for _, tc := range []struct {
		name string
		refs TorrentRefFilter
		old  qbt.TorrentFilterOptions
		// With hashes, the refs come in the order of the hashes and the old
		// read in the order of the snapshot.
		hashOrder bool
	}{
		{"all", TorrentRefFilter{}, qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterAll}, false},
		{"one hash", TorrentRefFilter{Hashes: []string{"aa03"}}, qbt.TorrentFilterOptions{Hashes: []string{"aa03"}}, false},
		{"hashes out of order, a repeat, and a missing one", TorrentRefFilter{Hashes: []string{"aa04", "aa01", "ffff", "aa04"}}, qbt.TorrentFilterOptions{Hashes: []string{"aa04", "aa01", "ffff", "aa04"}}, true},
		{"a missing hash", TorrentRefFilter{Hashes: []string{"ffff"}}, qbt.TorrentFilterOptions{Hashes: []string{"ffff"}}, false},
		{"category", TorrentRefFilter{Category: "movies"}, qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterAll, Category: "movies"}, false},
		{"tag", TorrentRefFilter{Tag: "keep"}, qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterAll, Tag: "keep"}, false},
		{"completed", TorrentRefFilter{Completed: true}, qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterCompleted}, false},
		{"completed in a category", TorrentRefFilter{Category: "tv", Completed: true}, qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterCompleted, Category: "tv"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refs, err := sm.GetTorrentRefs(t.Context(), 1, tc.refs)
			require.NoError(t, err)
			want, err := sm.GetTorrents(t.Context(), 1, tc.old)
			require.NoError(t, err)

			got := make([]qbt.Torrent, 0, len(refs))
			for _, ref := range refs {
				shared, ok := snapshot.Torrent(ref.Hash)
				require.True(t, ok)
				require.Same(t, shared, ref, "a read returns the torrent of the Sync snapshot")
				got = append(got, *ref)
			}
			if tc.hashOrder {
				order := []string{got[0].Hash, got[1].Hash}
				require.Equal(t, []string{"aa04", "aa01"}, order)
				require.ElementsMatch(t, want, got)
				return
			}
			require.Equal(t, want, got)
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
