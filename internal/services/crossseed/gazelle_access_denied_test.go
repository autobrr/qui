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
