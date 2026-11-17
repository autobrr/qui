// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

const lookupHash = "dddd000000000000000000000000000000000001"

// The tracker conditions of automations read the trackers from the lookup.
// It must give the trackers that qBittorrent reports for the hash, on each
// version that the old write into the torrents supported, and leave the
// shared torrents of the Sync snapshot as they are.
func TestTrackerDetailsGivesTheTrackersOfEachHash(t *testing.T) {
	t.Parallel()

	reported := []qbt.TorrentTracker{
		{Url: "https://alpha.example.invalid/announce", Status: qbt.TrackerStatusNotWorking, Message: "timed out"},
		{Url: "https://beta.example.invalid/announce", Status: qbt.TrackerStatusOK},
	}

	for _, tc := range []struct {
		name      string
		webAPI    string
		want      []qbt.TorrentTracker
		wantURLs  []string
		wantFetch bool
	}{
		{name: "below qBittorrent 5.1", webAPI: "2.11.3"},
		{name: "qBittorrent 5.1", webAPI: "2.11.4", want: reported, wantURLs: []string{"https://alpha.example.invalid/announce"}, wantFetch: true},
		{name: "qBittorrent 5.2", webAPI: "2.15.1", want: reported, wantURLs: []string{"https://alpha.example.invalid/announce"}, wantFetch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var infoRequests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/auth/login":
					_, _ = io.WriteString(w, "Ok.")
				case "/api/v2/app/webapiVersion":
					_, _ = io.WriteString(w, tc.webAPI)
				case "/api/v2/sync/maindata":
					_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Quiet.Harbor.2024.1080p.WEB-DL-GRP","added_on":1,"has_other_announce_error":true}},"trackers":{"https://alpha.example.invalid/announce":[%[1]q]}}`, lookupHash)
				case "/api/v2/torrents/info":
					infoRequests.Add(1)
					_, _ = fmt.Fprintf(w, `[{"hash":%q,"trackers":[{"url":"https://alpha.example.invalid/announce","status":4,"msg":"timed out"},{"url":"https://beta.example.invalid/announce","status":2}]}]`, lookupHash)
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

			shared := client.SyncOwner().SnapshotUnchecked().Torrents()
			lookup, err := sm.TrackerDetails(t.Context(), 1, shared)
			require.NoError(t, err)
			require.Equal(t, tc.want, lookup.Trackers(lookupHash))
			require.Equal(t, tc.wantFetch, infoRequests.Load() > 0)

			var urls []string
			for _, tracker := range sm.TrackerURLs(t.Context(), 1).Trackers(lookupHash) {
				urls = append(urls, tracker.Url)
			}
			require.Equal(t, tc.wantURLs, urls)

			// The seal panics in this sync when a lookup wrote to a shared torrent.
			require.NoError(t, client.SyncOwner().Sync(t.Context()))
			require.Empty(t, shared[0].Trackers)
		})
	}
}
