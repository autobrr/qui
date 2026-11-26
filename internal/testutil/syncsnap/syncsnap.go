// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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

func Update(t testing.TB, torrents ...qbt.Torrent) qbt.SyncUpdate {
	t.Helper()
	return NewSource().Update(t, torrents...)
}
