// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"encoding/json/v2"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files in testdata")

const sortGoldenPath = "testdata/single_instance_sort.golden"

func sortGoldenTorrents() []qbt.Torrent {
	words := []string{"alpha", "Alpha", "ALPHA", "beta", "Beta"}
	names := []string{"Show.S01", "Film.2020", "Album", "show.s01", "film.2020", "ALBUM"}
	states := []qbt.TorrentState{
		qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateDownloading,
		qbt.TorrentStateStoppedDl, qbt.TorrentStateError, "futureState",
	}
	trackers := []string{
		"https://tracker.example.invalid/announce",
		"https://TRACKER.example.invalid/announce",
		"udp://other.example.invalid:6969/announce",
		"",
	}

	torrents := make([]qbt.Torrent, 24)
	for i := range torrents {
		v := reflect.ValueOf(&torrents[i]).Elem()
		for f := range v.NumField() {
			n := (i*(f%4+1) + f) % 5
			field := v.Field(f)
			switch field.Kind() {
			case reflect.Int64:
				field.SetInt(int64(n))
			case reflect.Float64:
				field.SetFloat(float64(n) / 4)
			case reflect.Bool:
				field.SetBool(n%2 == 0)
			case reflect.String:
				field.SetString(words[n])
			default:
			}
		}

		t := &torrents[i]
		t.Hash = strings.Repeat(fmt.Sprintf("%02x", (i*7)%24), 20)
		t.Name = names[i%len(names)]
		t.State = states[(i/6+i%3)%len(states)]
		t.Tracker = trackers[(i/2)%len(trackers)]
		t.Priority = int64(i % 4)
		if i%4 == 1 {
			t.ETA = 8640000
		}
		base := int64(1_700_000_000)
		step := int64(i % 3)
		t.AddedOn = base + step*3600
		t.LastActivity = base + step*600
		t.CompletionOn = base + step*3600
		t.SeenComplete = base + step*3600
		if step == 0 {
			t.CompletionOn = -1
			t.SeenComplete = 0
		}
	}
	return torrents
}

func sortGoldenTrackers(i int) []qbt.TorrentTracker {
	url := "https://tracker.example.invalid/announce"
	switch i / 6 {
	case 1:
		return []qbt.TorrentTracker{{Url: url, Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"}}
	case 2:
		return []qbt.TorrentTracker{{Url: url, Status: qbt.TrackerStatusUnreachable, Message: "timed out"}}
	case 3:
		return []qbt.TorrentTracker{{Url: url, Status: qbt.TrackerStatusTrackerError, Message: "ratio too low"}}
	default:
		return []qbt.TorrentTracker{{Url: url, Status: qbt.TrackerStatusOK}}
	}
}

func sortGoldenColumns() []string {
	typ := reflect.TypeFor[qbt.Torrent]()
	columns := make([]string, 0, typ.NumField())
	for field := range typ.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			columns = append(columns, name)
		}
	}
	return columns
}

func newSortTestClient(t *testing.T, instanceID int, trackers func(i int) []qbt.TorrentTracker) *Client {
	t.Helper()
	return newListTestClient(t, instanceID, sortGoldenTorrents(), trackers)
}

func newListTestClient(t testing.TB, instanceID int, torrents []qbt.Torrent, trackers func(i int) []qbt.TorrentTracker) *Client {
	t.Helper()

	trackersByHash := make(map[string][]qbt.TorrentTracker, len(torrents))
	mainTorrents := make(map[string]qbt.Torrent, len(torrents))
	for i, torrent := range torrents {
		trackersByHash[torrent.Hash] = trackers(i)
		mainTorrents[torrent.Hash] = torrent
	}
	mainData, err := json.Marshal(map[string]any{"rid": 1, "full_update": true, "torrents": mainTorrents})
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			_, _ = w.Write(mainData)
		case "/api/v2/torrents/info":
			var out []qbt.Torrent
			if r.URL.Query().Get("hashes") == "" {
				for hash, trackers := range trackersByHash {
					out = append(out, qbt.Torrent{Hash: hash, Trackers: trackers})
				}
			}
			for hash := range strings.SplitSeq(r.URL.Query().Get("hashes"), "|") {
				if hash != "" {
					out = append(out, qbt.Torrent{Hash: hash, Trackers: trackersByHash[hash]})
				}
			}
			_ = json.MarshalWrite(w, out)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClientWithTimeout(instanceID, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	require.True(t, client.Supports(CapTrackerHealth), "state sort must see tracker health")
	return client
}

func TestSingleInstanceSortGolden(t *testing.T) {
	t.Parallel()

	sm := NewSyncManager(&ClientPool{clients: map[int]*Client{1: newSortTestClient(t, 1, sortGoldenTrackers)}}, nil)
	sm.refreshTrackerHealthCounts(t.Context(), 1)
	torrents := sortGoldenTorrents()

	var got strings.Builder
	for _, column := range sortGoldenColumns() {
		for _, order := range []string{"asc", "desc"} {
			resp, err := sm.ListTorrents(t.Context(), TorrentListView{InstanceID: 1, Sort: column, Order: order})
			require.NoError(t, err, "%s %s", column, order)
			require.Len(t, resp.Torrents, len(torrents), "%s %s", column, order)

			hashes := make([]string, len(resp.Torrents))
			for i, torrent := range resp.Torrents {
				hashes[i] = torrent.Hash[:2]
			}
			fmt.Fprintf(&got, "%s %s: %s\n", column, order, strings.Join(hashes, " "))
		}
	}

	if *updateGolden {
		require.NoError(t, os.WriteFile(sortGoldenPath, []byte(got.String()), 0o600))
		return
	}
	want, err := os.ReadFile(sortGoldenPath)
	require.NoError(t, err, "run with -update to create the golden file")
	require.Equal(t, string(want), got.String(), "single-instance sort order changed; run with -update if intended")
}
