// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package syncsnap builds Sync snapshots for tests of the consumers of sync
// updates. It sends no request over the network.
package syncsnap

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/testutil"
)

// Source is a fake qBittorrent that answers each sync with a full update.
// Create it outside a synctest bubble: its sync object starts a goroutine
// that never ends. Its updates can be read inside the bubble.
type Source struct {
	mu       sync.Mutex
	mainData []byte
	last     qbt.SyncUpdate
	owner    *syncowner.Owner
}

func NewSource() *Source {
	s := &Source{}
	client := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid"}).WithHTTPClient(&http.Client{
		Transport: testutil.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			if r.URL.Path != "/api/v2/sync/maindata" {
				rec.WriteHeader(http.StatusNotFound)
				return rec.Result(), nil
			}
			_, _ = rec.Write(s.mainData)
			return rec.Result(), nil
		}),
	})
	options := qbt.DefaultSyncOptions()
	options.OnSyncUpdate = func(update qbt.SyncUpdate) { s.last = update }
	s.owner = syncowner.New(client, options)
	return s
}

// Update returns the update that go-qbittorrent gives OnSyncUpdate after a
// full sync that holds torrents. Changes.Torrents names each torrent, and
// Changes.TorrentsRemoved names each torrent of the last update that torrents
// leave out. A torrent that is equal to one of the last update keeps its
// object, as in a real sync.
func (s *Source) Update(t testing.TB, torrents ...qbt.Torrent) qbt.SyncUpdate {
	t.Helper()

	byHash := make(map[string]qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		byHash[torrent.Hash] = torrent
	}
	mainData, err := json.Marshal(fullUpdate{Rid: 1, FullUpdate: true, Torrents: byHash})
	require.NoError(t, err)
	return s.sync(t, mainData)
}

// Partial returns the update after a sync that qBittorrent answers with
// mainData, a partial update in JSON. Changes.Torrents names only the
// torrents that mainData names.
func (s *Source) Partial(t testing.TB, mainData string) qbt.SyncUpdate {
	t.Helper()
	return s.sync(t, []byte(mainData))
}

func (s *Source) sync(t testing.TB, mainData []byte) qbt.SyncUpdate {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.mainData = mainData
	s.last = qbt.SyncUpdate{}
	require.NoError(t, s.owner.Sync(t.Context()))
	require.NotNil(t, s.last.Snapshot)
	return s.last
}

type fullUpdate struct {
	Rid        int64                  `json:"rid"`
	FullUpdate bool                   `json:"full_update"`
	Torrents   map[string]qbt.Torrent `json:"torrents"`
}

// Update returns the update of one full sync that holds torrents. Inside a
// synctest bubble, use a Source.
func Update(t testing.TB, torrents ...qbt.Torrent) qbt.SyncUpdate {
	t.Helper()
	return NewSource().Update(t, torrents...)
}
