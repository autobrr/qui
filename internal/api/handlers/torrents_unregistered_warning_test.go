// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type fakeUnregistered struct {
	notLoaded bool
	host      string
	health    map[string]qbittorrent.TrackerHealth
	since     map[string]time.Time
}

func (f fakeUnregistered) Health(hash string) qbittorrent.TrackerHealth {
	return f.health[hash]
}

func (f fakeUnregistered) UnregisteredSince(hash string) (time.Time, bool) {
	since, ok := f.since[hash]
	return since, ok
}

func (f fakeUnregistered) UnregisteredSinceLoaded() bool {
	return !f.notLoaded
}

func (f fakeUnregistered) UnregisteredBy(hash string) (qbittorrent.UnregisteredTracker, bool) {
	if f.health[hash] != qbittorrent.TrackerHealthUnregistered {
		return qbittorrent.UnregisteredTracker{}, false
	}
	return qbittorrent.UnregisteredTracker{Host: f.host, Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"}, true
}

type fakeUnregisteredSource map[int]fakeUnregistered

func (f fakeUnregisteredSource) unregisteredOf(instanceID int) unregisteredReader {
	return f[instanceID]
}

func warningHash(n int) string {
	return fmt.Sprintf("%040x", n)
}

func TestUnregisteredDeleteWarning_SelectAllCountsTheWholeFilterPastOnePage(t *testing.T) {
	t.Parallel()

	const inFilter = 320
	now := time.Now()
	torrents := make([]qbt.Torrent, 0, inFilter+10)
	health := map[string]qbittorrent.TrackerHealth{}
	since := map[string]time.Time{}
	for i := range inFilter + 10 {
		hash := warningHash(i + 1)
		category := "tv"
		if i >= inFilter {
			category = "movies"
		}
		torrents = append(torrents, qbt.Torrent{Name: "Torrent " + hash[34:], Hash: hash, Category: category, AddedOn: int64(1000 + i)})
		health[hash] = qbittorrent.TrackerHealthUnregistered
		since[hash] = now.Add(-time.Duration(i+1) * time.Minute)
	}
	oldest := warningHash(inFilter)
	since[oldest] = now.Add(-3 * 24 * time.Hour)
	excluded := warningHash(1)

	maindata := map[string]qbt.Torrent{}
	for _, torrent := range torrents {
		maindata[torrent.Hash] = torrent
	}
	body, err := json.Marshal(map[string]any{"rid": 1, "full_update": true, "torrents": maindata})
	require.NoError(t, err)

	db := testdb.NewMigratedSQLite(t, "torrents-unregistered-warning")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/sync/maindata" {
			_, _ = w.Write(body)
			return
		}
		_, _ = w.Write([]byte("Ok."))
	}))
	t.Cleanup(srv.Close)
	instance, err := instanceStore.Create(t.Context(), "alpha", srv.URL, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	testutil.SetUnexportedField(t, clientPool, "clients", map[int]*qbittorrent.Client{instance.ID: newStaleCachedClient(t, srv.URL, torrents)})

	handler := NewTorrentsHandler(qbittorrent.NewSyncManager(clientPool, nil), nil, instanceStore, nil)
	handler.unregistered = fakeUnregisteredSource{instance.ID: {host: "tracker.example.invalid", health: health, since: since}}
	req := newTorrentFieldRequest(t, instance.ID, map[string]any{
		"selectAll":     true,
		"filters":       map[string]any{"categories": []string{"tv"}},
		"excludeHashes": []string{excluded},
	})

	rec := httptest.NewRecorder()
	handler.GetUnregisteredDeleteWarning(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var warning UnregisteredDeleteWarning
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &warning))
	require.Equal(t, inFilter-2, warning.Count)
	require.True(t, warning.Since.Equal(since[warningHash(inFilter-1)]), "since %s", warning.Since)
	require.Equal(t, []UnregisteredWarningTracker{{Host: "tracker.example.invalid", Count: inFilter - 2}}, warning.Trackers)
}

func TestRecentlyUnregistered(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	unregistered := qbittorrent.TrackerHealthUnregistered
	source := fakeUnregisteredSource{
		1: {
			host: "a.example.invalid",
			health: map[string]qbittorrent.TrackerHealth{
				warningHash(1): unregistered,
				warningHash(2): unregistered,
				warningHash(3): unregistered,
				warningHash(4): qbittorrent.TrackerHealthDown,
				warningHash(5): unregistered,
			},
			since: map[string]time.Time{
				warningHash(1): now.Add(-3 * time.Hour),
				warningHash(2): now.Add(-48*time.Hour + time.Second),
				warningHash(3): now.Add(-48 * time.Hour),
				warningHash(4): now.Add(-time.Hour),
			},
		},
		2: {
			host:   "b.example.invalid",
			health: map[string]qbittorrent.TrackerHealth{warningHash(1): unregistered, warningHash(6): unregistered},
			since:  map[string]time.Time{warningHash(1): now.Add(-time.Hour), warningHash(6): now.Add(-2 * time.Hour)},
		},
		4: {
			notLoaded: true,
			host:      "c.example.invalid",
			health:    map[string]qbittorrent.TrackerHealth{warningHash(1): unregistered},
		},
	}

	tests := []struct {
		name      string
		selection map[int][]string
		want      UnregisteredDeleteWarning
	}{
		{
			name:      "counts each torrent unregistered for less than 2 days, from the earliest time",
			selection: map[int][]string{1: {warningHash(1), warningHash(2), warningHash(3), warningHash(4)}},
			want:      UnregisteredDeleteWarning{Count: 2, Since: now.Add(-48*time.Hour + time.Second), Trackers: []UnregisteredWarningTracker{{Host: "a.example.invalid", Count: 2}}},
		},
		{
			name:      "an unregistered torrent with no time counts as seen now",
			selection: map[int][]string{1: {warningHash(5)}},
			want:      UnregisteredDeleteWarning{Count: 1, Since: now, Trackers: []UnregisteredWarningTracker{{Host: "a.example.invalid", Count: 1}}},
		},
		{
			name:      "spans instances and ranks trackers by count",
			selection: map[int][]string{1: {warningHash(1)}, 2: {warningHash(1), warningHash(6), warningHash(6)}},
			want: UnregisteredDeleteWarning{Count: 3, Since: now.Add(-3 * time.Hour), Trackers: []UnregisteredWarningTracker{
				{Host: "b.example.invalid", Count: 2},
				{Host: "a.example.invalid", Count: 1},
			}},
		},
		{
			name:      "an instance whose times are not loaded yet counts nothing",
			selection: map[int][]string{1: {warningHash(1)}, 4: {warningHash(1)}},
			want:      UnregisteredDeleteWarning{Count: 1, Since: now.Add(-3 * time.Hour), Trackers: []UnregisteredWarningTracker{{Host: "a.example.invalid", Count: 1}}},
		},
		{
			name:      "nothing recent answers 0 and no time",
			selection: map[int][]string{1: {warningHash(3), warningHash(4)}, 3: {warningHash(1)}},
			want:      UnregisteredDeleteWarning{Trackers: []UnregisteredWarningTracker{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, recentlyUnregistered(source, tt.selection, now))
		})
	}
}
