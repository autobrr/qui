// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package biglib serves a large synthetic library from a fake qBittorrent, for
// the memory budget tests of the torrent readers.
package biglib

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// Size is the number of torrents of the budget tests.
const Size = 20_000

const MB = 1 << 20

var words = strings.Fields("Silent Harbor Crimson Ledger Northern Vale Paper Lantern Hollow Crown Iron Orchard Glass Tide Amber Signal")

// Torrents returns n torrents on 40 trackers under .test. Each content is
// seeded one to four times, as in a library that cross-seeds.
func Torrents(n int) []qbt.Torrent {
	rnd := rand.New(rand.NewPCG(7, 7)) //nolint:gosec // a fixed seed gives the same library in each run
	categories := []string{"tv", "movies", "music", "books", "games", "tv-4k", "movies-4k", "cross-seed", "anime", "misc"}
	tags := []string{"", "cross-seed", "cross-seed, sonarr", "radarr, upgraded", "freeleech, keep"}
	states := []qbt.TorrentState{qbt.TorrentStateStalledUp, qbt.TorrentStateStalledUp, qbt.TorrentStateUploading, qbt.TorrentStatePausedUp}

	torrents := make([]qbt.Torrent, 0, n)
	for content := 0; len(torrents) < n; content++ {
		copies := min([]int{1, 1, 2, 3, 4}[rnd.IntN(5)], n-len(torrents))
		w := rnd.Perm(len(words))
		name := fmt.Sprintf("%s.%s.%s.%d.S%02dE%02d.1080p.WEB-DL.DDP5.1.H.264-GRP%d",
			words[w[0]], words[w[1]], words[w[2]], content, content%9+1, content%24+1, content%50)
		size := 200_000_000 + rnd.Int64N(60_000_000_000)
		category := categories[rnd.IntN(len(categories))]
		for c := range copies {
			i := len(torrents)
			hash := fmt.Sprintf("%040x", i+1)
			save := "/data/torrents/" + category
			added := int64(1_700_000_000 - i*600)
			torrents = append(torrents, qbt.Torrent{
				Hash: hash, InfohashV1: hash, Name: name, Size: size, TotalSize: size,
				Completed: size, Downloaded: size, Uploaded: size * 3 / 2, Ratio: 1.5,
				Progress: 1, State: states[rnd.IntN(len(states))], Category: category,
				Tags: tags[rnd.IntN(len(tags))], SavePath: save, ContentPath: save + "/" + name,
				AddedOn: added, CompletionOn: added + 900, LastActivity: added + 3600,
				Tracker:     fmt.Sprintf("https://tracker-%02d.test/announce", (content+c*7)%40),
				NumComplete: rnd.Int64N(200), Private: true, ETA: 8_640_000, TrackersCount: 1,
			})
		}
	}
	return torrents
}

// Serve starts a fake qBittorrent that serves torrents and returns its URL.
// The first sync gets a full update, and a later sync gets no change, as a
// quiet qBittorrent answers. Each torrent has one working tracker.
func Serve(t testing.TB, torrents []qbt.Torrent) string {
	t.Helper()

	byHash := make(map[string]qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		byHash[torrent.Hash] = torrent
	}
	mainData, err := json.Marshal(map[string]any{"rid": 1, "full_update": true, "torrents": byHash})
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			if r.URL.Query().Get("rid") != "0" {
				_, _ = io.WriteString(w, `{"rid":2}`)
				return
			}
			_, _ = w.Write(mainData)
		case "/api/v2/torrents/info":
			out := []qbt.Torrent{}
			for hash := range strings.SplitSeq(r.URL.Query().Get("hashes"), "|") {
				if torrent, ok := byHash[hash]; ok {
					torrent.Trackers = []qbt.TorrentTracker{{Url: torrent.Tracker, Status: qbt.TrackerStatusOK}}
					out = append(out, torrent)
				}
			}
			_ = json.MarshalWrite(w, out)
		case "/api/v2/torrents/files":
			_, _ = io.WriteString(w, "[]")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// AllocatedBytes returns the lowest number of bytes that one call of f
// allocated over runs calls. The lowest run leaves out a sync or a cache fill
// that ran at the same time.
func AllocatedBytes(runs int, f func()) uint64 {
	var lowest uint64
	var before, after runtime.MemStats
	for i := range runs {
		runtime.GC()
		runtime.ReadMemStats(&before)
		f()
		runtime.ReadMemStats(&after)
		if n := after.TotalAlloc - before.TotalAlloc; i == 0 || n < lowest {
			lowest = n
		}
	}
	return lowest
}
