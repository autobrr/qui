// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
)

// A banned client must stop calling the tracker after the first rejection,
// leave every torrent unstamped, and tell the user on the run record (#2807).
func TestSearchRunLoop_GazelleAccessDeniedStopsQueryingTracker(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"status":"failure","error":"Your IP address has been banned."}`))
	}))
	t.Cleanup(server.Close)

	torrents := make([]qbt.Torrent, 0, 3)
	filesByHash := make(map[string]qbt.TorrentFiles, 3)
	for i := range 3 {
		name := fmt.Sprintf("Artist %d - Album (2024 WF)", i)
		torrent := qbt.Torrent{
			Hash:     fmt.Sprintf("%040x", i+1),
			Name:     name,
			Progress: 1.0,
			Size:     246,
			Tracker:  "https://flacsfor.me/announce",
		}
		torrents = append(torrents, torrent)
		// Two files, so a lookup that ignored the rejection would search twice.
		filesByHash[torrent.Hash] = qbt.TorrentFiles{
			{Name: name + "/01 - Track One.flac", Size: 123},
			{Name: name + "/02 - Track Two.flac", Size: 123},
		}
	}

	svc, state := newSearchRunLoopFixture(t, "crossseed-runloop-gazelle-denied", &hashFilteringSyncManager{
		torrents: torrents, filesByHash: filesByHash,
	})
	findGazelleMatch = gazellemusic.FindMatch // undo the fixture's stub; the cleanup restores it
	client, err := gazellemusic.NewClient("orpheus.network", server.URL, "ops-key")
	require.NoError(t, err)
	state.gazelleClients = &gazelleClientSet{byHost: map[string]*gazellemusic.Client{"orpheus.network": client}}

	runSearchLoopToCompletion(t, svc, state)

	require.EqualValues(t, 1, requests.Load(), "a rejected tracker must get no further requests in the run")
	require.Equal(t, 3, state.run.Processed)
	require.Equal(t, models.CrossSeedSearchRunStatusFailed, state.run.Status, "the rejected tracker was the run's only source")
	require.NotNil(t, state.run.ErrorMessage)
	require.Equal(t, "OPS rejected the API key or this IP: Your IP address has been banned.", *state.run.ErrorMessage)

	for _, torrent := range torrents {
		_, found, err := svc.automationStore.GetSearchHistory(t.Context(), state.opts.InstanceID, torrent.Hash)
		require.NoError(t, err)
		require.False(t, found, "torrent %s was never checked and must stay due", strings.ToLower(torrent.Hash))
	}
}

// A run with another source left still completes, and reports the rejection.
func TestFinalizeSearchRun_GazelleAccessDeniedWithTorznabSucceeds(t *testing.T) {
	svc, state := newSearchRunLoopFixture(t, "crossseed-finalize-gazelle-denied", &hashFilteringSyncManager{})
	state.opts.DisableTorznab = false
	state.resolvedTorznabIndexerIDs = []int{1}
	state.torznabSearched = true
	state.gazelleClients.queried = map[string]struct{}{"orpheus.network": {}}
	state.gazelleClients.denied = map[string]error{
		"orpheus.network": fmt.Errorf("%w: status 401: bad credentials", gazellemusic.ErrAccessDenied),
	}

	svc.finalizeSearchRun(state, false)

	require.Equal(t, models.CrossSeedSearchRunStatusSuccess, state.run.Status)
	require.NotNil(t, state.run.ErrorMessage)
	require.Equal(t, "OPS rejected the API key or this IP: status 401: bad credentials", *state.run.ErrorMessage)
}

// Sources from OPS only target RED, so a denied RED leaves nothing to search
// even while the OPS key is configured.
func TestFinalizeSearchRun_GazelleOnlyQueriedTrackerDeniedFails(t *testing.T) {
	svc, state := newSearchRunLoopFixture(t, "crossseed-finalize-gazelle-target-denied", &hashFilteringSyncManager{})
	red, err := gazellemusic.NewClient("redacted.sh", "http://127.0.0.1:9", "red-key")
	require.NoError(t, err)
	state.gazelleClients.byHost["redacted.sh"] = red
	state.gazelleClients.queried = map[string]struct{}{"redacted.sh": {}}
	state.gazelleClients.denied = map[string]error{
		"redacted.sh": fmt.Errorf("%w: status 401: bad credentials", gazellemusic.ErrAccessDenied),
	}

	svc.finalizeSearchRun(state, false)

	require.Equal(t, models.CrossSeedSearchRunStatusFailed, state.run.Status)
	require.NotNil(t, state.run.ErrorMessage)
	require.Equal(t, "RED rejected the API key or this IP: status 401: bad credentials", *state.run.ErrorMessage)
}

// The filter or cooldown can skip Torznab for every candidate. Resolved
// indexers then leave Gazelle as the only source the run searched.
func TestFinalizeSearchRun_GazelleDeniedWithUnsearchedTorznabFails(t *testing.T) {
	svc, state := newSearchRunLoopFixture(t, "crossseed-finalize-gazelle-denied-torznab-skipped", &hashFilteringSyncManager{})
	state.opts.DisableTorznab = false
	state.resolvedTorznabIndexerIDs = []int{1}
	state.gazelleClients.queried = map[string]struct{}{"orpheus.network": {}}
	state.gazelleClients.denied = map[string]error{
		"orpheus.network": fmt.Errorf("%w: status 401: bad credentials", gazellemusic.ErrAccessDenied),
	}

	svc.finalizeSearchRun(state, false)

	require.Equal(t, models.CrossSeedSearchRunStatusFailed, state.run.Status)
}

// searchTorrentMatches can return before the Torznab search, as when no
// Torznab backend is set up. Only an indexer that answered counts as a
// search, so a run whose Gazelle tracker is denied then fails.
func TestSearchRunLoop_GazelleDeniedTorznabSearchDecidesStatus(t *testing.T) {
	tests := []struct {
		name    string
		torznab bool
		want    models.CrossSeedSearchRunStatus
	}{
		{name: "torznab skipped inside the search", want: models.CrossSeedSearchRunStatusFailed},
		{name: "torznab indexer answered", torznab: true, want: models.CrossSeedSearchRunStatusSuccess},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gazelle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"status":"failure","error":"bad credentials"}`))
			}))
			t.Cleanup(gazelle.Close)

			name := "Artist - Album (2024 WF)"
			torrent := qbt.Torrent{Hash: fmt.Sprintf("%040x", 1), Name: name, Progress: 1.0, Size: 123, Tracker: "https://flacsfor.me/announce"}
			svc, state := newSearchRunLoopFixture(t, "crossseed-runloop-gazelle-denied-torznab", &hashFilteringSyncManager{
				torrents:    []qbt.Torrent{torrent},
				filesByHash: map[string]qbt.TorrentFiles{torrent.Hash: {{Name: name + "/01 - Track One.flac", Size: 123}}},
			})
			findGazelleMatch = gazellemusic.FindMatch
			client, err := gazellemusic.NewClient("orpheus.network", gazelle.URL, "ops-key")
			require.NoError(t, err)
			state.gazelleClients = &gazelleClientSet{byHost: map[string]*gazellemusic.Client{"orpheus.network": client}}
			state.opts.DisableTorznab = false
			state.resolvedTorznabIndexerIDs = []int{1}
			// The cached filter state lets indexer 1 through the candidate pre-filter.
			svc.asyncFilteringCache = ttlcache.New[string, *AsyncIndexerFilteringState]()
			svc.asyncFilteringCache.Set(asyncFilteringCacheKey(state.opts.InstanceID, torrent.Hash), &AsyncIndexerFilteringState{
				CapabilitiesCompleted: true,
				ContentCompleted:      true,
				CapabilityIndexers:    []int{1},
				FilteredIndexers:      []int{1},
				contentType:           "music",
			}, ttlcache.DefaultTTL)
			if tt.torznab {
				indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/rss+xml")
					_, _ = w.Write([]byte(`<rss version="2.0"><channel><title>Tracker One</title></channel></rss>`))
				}))
				t.Cleanup(indexer.Close)
				svc.jackettService = newJackettServiceWithIndexers([]*models.TorznabIndexer{
					{ID: 1, Name: "Tracker One", BaseURL: indexer.URL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5, Enabled: true},
				})
			}

			runSearchLoopToCompletion(t, svc, state)

			require.Equal(t, 1, state.run.Processed)
			require.Equal(t, tt.want, state.run.Status)
		})
	}
}
