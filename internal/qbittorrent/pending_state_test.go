// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

const (
	pendingHashPaused = "cccc000000000000000000000000000000000001"
	pendingHashOther  = "cccc000000000000000000000000000000000002"
)

// newPendingStateRig is a sync manager over a qBittorrent stub with two
// seeding torrents. The stub accepts each write and keeps both torrents
// seeding, as qBittorrent does before the next sync shows a pause.
func newPendingStateRig(t *testing.T) (*SyncManager, *Client) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Quiet.Harbor.2024.1080p.WEB-DL-GRP","state":"stalledUP","progress":1},%q:{"name":"Night.Ferry.2023.720p.BluRay-GRP","state":"uploading","progress":1}}}`,
				pendingHashPaused, pendingHashOther)
		default:
			_, _ = io.WriteString(w, "Ok.")
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClientWithTimeout(1, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	return NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil), client
}

// An automation pass right after a pause in qui must see the torrent as
// paused, also before the next sync shows it. The torrent in the Sync snapshot
// keeps its state for the other readers.
func TestGetAllTorrentRefsShowsAPauseBeforeTheNextSync(t *testing.T) {
	t.Parallel()

	sm, client := newPendingStateRig(t)
	require.NoError(t, sm.BulkAction(t.Context(), 1, []string{pendingHashPaused}, "pause"))

	torrents, err := sm.GetAllTorrentRefs(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, torrents, 2)

	byHash := make(map[string]*qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		byHash[torrent.Hash] = torrent
	}
	require.Equal(t, qbt.TorrentStatePausedUp, byHash[pendingHashPaused].State)

	shared, ok := client.SyncOwner().SnapshotUnchecked().Torrent(pendingHashPaused)
	require.True(t, ok)
	require.Equal(t, qbt.TorrentStateStalledUp, shared.State, "the Sync snapshot keeps the state of the last sync")
	require.NotSame(t, shared, byHash[pendingHashPaused], "the paused torrent is a private copy")
}

// A torrent without a pending change is the torrent of the Sync snapshot, not
// a copy.
func TestGetAllTorrentRefsSharesTorrentsWithoutAPendingChange(t *testing.T) {
	t.Parallel()

	sm, client := newPendingStateRig(t)
	require.NoError(t, sm.BulkAction(t.Context(), 1, []string{pendingHashPaused}, "pause"))

	torrents, err := sm.GetAllTorrentRefs(t.Context(), 1)
	require.NoError(t, err)

	shared, ok := client.SyncOwner().SnapshotUnchecked().Torrent(pendingHashOther)
	require.True(t, ok)
	var got *qbt.Torrent
	for _, torrent := range torrents {
		if torrent.Hash == pendingHashOther {
			got = torrent
		}
	}
	require.Same(t, shared, got)
}
