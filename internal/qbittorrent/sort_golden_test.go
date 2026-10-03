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

// sortGoldenTorrents builds a library where each column has repeated values.
// Reflection fills each scalar field from five values, so a field that
// go-qbittorrent adds later also gets data. Then each field with its own sort
// rule gets the edge cases that rule handles.
func sortGoldenTorrents() []qbt.Torrent {
	words := []string{"alpha", "Alpha", "ALPHA", "beta", "Beta"}
	// Index i and i+3 differ only in letter case.
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
				// Pointers and slices keep their zero value; trackers come from torrents/info.
			}
		}

		t := &torrents[i]
		// gcd(7, 24) == 1, so hash order is a permutation of index order.
		t.Hash = strings.Repeat(fmt.Sprintf("%02x", (i*7)%24), 20)
		t.Name = names[i%len(names)]
		// i and i+3 in one block of six share tracker health, state, and
		// timestamps, so the name tiebreaks compare names that differ only in case.
		t.State = states[(i/6+i%3)%len(states)]
		t.Tracker = trackers[(i/2)%len(trackers)]
		t.Priority = int64(i % 4) // 0 means not queued
		if i%4 == 1 {
			t.ETA = 8640000 // infinity
		}
		// These timestamps are old, so the one-hour grace for unregistered
		// trackers has ended.
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

// sortGoldenTrackers gives each block of six torrents a tracker that is
// working, unregistered, down, or in error.
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

// sortGoldenColumns lists every qbt.Torrent JSON field, so a new field from
// go-qbittorrent is sorted here without an edit.
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

// TestSingleInstanceSortGolden pins the order GetTorrentsWithFilters gives each
// sort column. Rewrite the golden file with:
//
//	go test ./internal/qbittorrent -run TestSingleInstanceSortGolden -update
func TestSingleInstanceSortGolden(t *testing.T) {
	t.Parallel()

	torrents := sortGoldenTorrents()
	trackersByHash := make(map[string][]qbt.TorrentTracker, len(torrents))
	mainTorrents := make(map[string]qbt.Torrent, len(torrents))
	for i, torrent := range torrents {
		trackersByHash[torrent.Hash] = sortGoldenTrackers(i)
		mainTorrents[torrent.Hash] = torrent
	}
	mainData, err := json.Marshal(map[string]any{"rid": 1, "full_update": true, "torrents": mainTorrents})
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4") // the lowest version with tracker health
		case "/api/v2/sync/maindata":
			_, _ = w.Write(mainData)
		case "/api/v2/torrents/info":
			var out []qbt.Torrent
			for hash := range strings.SplitSeq(r.URL.Query().Get("hashes"), "|") {
				out = append(out, qbt.Torrent{Hash: hash, Trackers: trackersByHash[hash]})
			}
			_ = json.MarshalWrite(w, out)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClientWithTimeout(1, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))
	require.True(t, client.supportsTrackerInclude(), "state sort must see tracker health")
	sm := NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)

	var got strings.Builder
	for _, column := range sortGoldenColumns() {
		for _, order := range []string{"asc", "desc"} {
			resp, err := sm.GetTorrentsWithFilters(t.Context(), 1, 0, 0, column, order, "", FilterOptions{})
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
