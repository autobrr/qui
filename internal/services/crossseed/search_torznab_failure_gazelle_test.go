// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// gettableIndexerStore also serves Get by ID so specific-indexer selection
// (resolveIndexerSelection) sees the configured indexers.
type gettableIndexerStore struct {
	failingEnabledIndexerStore
}

func (s *gettableIndexerStore) Get(_ context.Context, id int) (*models.TorznabIndexer, error) {
	for _, idx := range s.indexers {
		if idx != nil && idx.ID == id {
			return idx, nil
		}
	}
	return nil, nil
}

const torznabGazelleSourceHash = "223759985c562a644428312c8cd3585d04686847"

// torznabGazelleScope asks the Torznab indexer and OPS, the target of the RED source.
var torznabGazelleScope = searchScope{torznabIndexerIDs: []int{1}, gazelleHosts: []string{"orpheus.network"}}

// newTorznabGazelleFixture builds a Service with one music-capable Torznab
// indexer pointing at trackerURL and a RED-sourced music torrent whose Gazelle
// lookup is stubbed to return one match.
func newTorznabGazelleFixture(t *testing.T, dbName, trackerURL string) (*Service, int, *gazelleClientSet) {
	t.Helper()
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, dbName)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	sourceTorrent := qbt.Torrent{
		Hash:     torznabGazelleSourceHash,
		Name:     "During - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://flacsfor.me/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "During - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
	}

	type torrentInfoDict struct {
		Length int64  `bencode:"length"`
		Name   string `bencode:"name"`
	}
	type torrentDict struct {
		Announce string          `bencode:"announce"`
		Info     torrentInfoDict `bencode:"info"`
	}
	torrentBytes, err := bencode.Marshal(torrentDict{
		Announce: "https://flacsfor.me/announce",
		Info: torrentInfoDict{
			Length: 123,
			Name:   "During - LMK (2024 WF)",
		},
	})
	require.NoError(t, err)

	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	prevFindMatch := findGazelleMatch
	findGazelleMatch = func(_ context.Context, _ *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
		return &gazellemusic.Match{TorrentID: 4242, Title: "During - LMK (2024 WF)", Size: 123, Reason: "hash"}, nil
	}
	t.Cleanup(func() {
		findGazelleMatch = prevFindMatch
	})

	svc := &Service{
		instanceStore: instanceStore,
		jackettService: jackett.NewService(&gettableIndexerStore{failingEnabledIndexerStore{indexers: []*models.TorznabIndexer{
			{
				ID:           1,
				Name:         "Generic Indexer",
				Enabled:      true,
				BaseURL:      trackerURL,
				Backend:      models.TorznabBackendNative,
				Capabilities: []string{"search", "music-search", "audio-search"},
				Categories:   []models.TorznabIndexerCategory{{IndexerID: 1, CategoryID: 3000, CategoryName: "Audio"}},
			},
		}}}, jackett.WithMinRequestInterval(time.Millisecond)),
		syncManager: &hashFilteringSyncManager{
			torrents: []qbt.Torrent{sourceTorrent},
			filesByHash: map[string]qbt.TorrentFiles{
				strings.ToLower(torznabGazelleSourceHash): sourceFiles,
			},
			exportedTorrent: torrentBytes,
		},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	return svc, instance.ID, clients
}

// Regression: when the Torznab leg of a cross-seed search fails entirely (for
// example the only filtered indexer is a downed tracker), matches already
// found via Gazelle (RED/OPS) must be returned as a partial response instead
// of being discarded by a whole-search error.
func TestSearchTorrentMatches_TorznabFailureKeepsGazelleMatches(t *testing.T) {
	downedTracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	t.Cleanup(downedTracker.Close)

	svc, instanceID, clients := newTorznabGazelleFixture(t, "crossseed-torznab-fail-gazelle", downedTracker.URL)

	resp, _, _, err := svc.searchTorrentMatches(context.Background(), instanceID, torznabGazelleSourceHash, TorrentSearchOptions{}, torznabGazelleScope, clients)

	require.NoError(t, err, "torznab failure must not discard gazelle matches")
	require.NotNil(t, resp)
	require.True(t, resp.Partial, "response should be marked partial when the torznab leg failed")
	require.True(t, resp.TorznabFailed, "the dialog must show that Torznab failed")
	require.Len(t, resp.Results, 1)
	require.Equal(t, "gazelle:hash", resp.Results[0].MatchReason)
	require.Contains(t, resp.Results[0].DownloadURL, "4242", "download URL should reference the gazelle torrent")
}

// Regression (PR #2156 review): a caller-imposed deadline is a form of
// cancellation and must propagate as an error, not be degraded into a
// Gazelle-only partial success. The expired caller deadline surfaces through
// torznabFailed's "search timed out" call site with Gazelle matches already
// in hand, so this pins the closure's ctx.Err() guard, which distinguishes
// the caller's context from the internal wait timeout.
func TestSearchTorrentMatches_CallerDeadlineIsNotPartialSuccess(t *testing.T) {
	blockingTracker := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(blockingTracker.Close)

	svc, instanceID, clients := newTorznabGazelleFixture(t, "crossseed-torznab-cancel-gazelle", blockingTracker.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)

	resp, _, _, err := svc.searchTorrentMatches(ctx, instanceID, torznabGazelleSourceHash, TorrentSearchOptions{}, torznabGazelleScope, clients)

	require.Error(t, err, "caller deadline expiry must not be reported as partial success")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, resp)
}

// Regression (PR #2427 review): the Search breakdown explains the Torznab
// funnel only. A Gazelle match reaches the response without passing through
// that funnel, so it must not raise the reported match count.
func TestSearchTorrentMatches_TraceExcludesGazelleFromFinalMatches(t *testing.T) {
	emptyFeed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><title>Empty</title></channel></rss>`))
	}))
	t.Cleanup(emptyFeed.Close)

	svc, instanceID, clients := newTorznabGazelleFixture(t, "crossseed-trace-gazelle-matches", emptyFeed.URL)

	resp, _, _, err := svc.searchTorrentMatches(context.Background(), instanceID, torznabGazelleSourceHash, TorrentSearchOptions{}, torznabGazelleScope, clients)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Results, 1, "the gazelle match still reaches the response")
	require.NotNil(t, resp.DecisionTrace)
	require.Equal(t, 0, resp.DecisionTrace.FinalMatches, "no torznab candidate was accepted")
}

// slowGazelleClients returns OPS clients whose fake answers no request, and
// shortens the RED/OPS limit of each search. started closes at the first
// RED/OPS request; active counts the requests that still wait.
func slowGazelleClients(t *testing.T) (clients *gazelleClientSet, started <-chan struct{}, active *atomic.Int32) {
	t.Helper()
	// Not parallel: the test replaces package-level vars.
	prevTimeout := gazelleSearchTimeout
	// The OPS queue lets one request through every 2 s, and other tests share
	// it. A limit above 2 s makes the request reach the fake.
	gazelleSearchTimeout = 3 * time.Second
	t.Cleanup(func() { gazelleSearchTimeout = prevTimeout })
	prevFindMatch := findGazelleMatch
	findGazelleMatch = gazellemusic.FindMatch
	t.Cleanup(func() { findGazelleMatch = prevFindMatch })

	startedCh := make(chan struct{})
	var once sync.Once
	active = &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Add(-1)
		once.Do(func() { close(startedCh) })
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	client, err := gazellemusic.NewClient("orpheus.network", server.URL, "ops-key")
	require.NoError(t, err)
	return &gazelleClientSet{byHost: map[string]*gazellemusic.Client{"orpheus.network": client}}, startedCh, active
}

// RED/OPS and Torznab run at the same time. When RED/OPS does not answer in
// time, the search returns the Torznab results with the timeout note and starts
// no RED/OPS cooldown.
func TestSearchTorrentMatches_SlowGazelleKeepsTorznabResults(t *testing.T) {
	var torznabSawGazelleWaiting atomic.Bool
	var gazelleStarted <-chan struct{}
	var gazelleActive *atomic.Int32
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-gazelleStarted:
		case <-time.After(5 * time.Second):
		}
		torznabSawGazelleWaiting.Store(gazelleActive.Load() > 0)
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, `<rss version="2.0"><channel><title>Generic</title><item><title>During - LMK (2024 WF)</title><guid>torznab-guid</guid><size>123</size><enclosure url="https://example.invalid/candidate.torrent" length="123" type="application/x-bittorrent" /></item></channel></rss>`)
	}))
	t.Cleanup(tracker.Close)

	svc, instanceID, _ := newTorznabGazelleFixture(t, "crossseed-slow-gazelle-torznab", tracker.URL)
	clients, started, active := slowGazelleClients(t)
	gazelleStarted, gazelleActive = started, active

	resp, gazelleLookupCompleted, _, err := svc.searchTorrentMatches(t.Context(), instanceID, torznabGazelleSourceHash, TorrentSearchOptions{}, torznabGazelleScope, clients)

	require.NoError(t, err)
	require.True(t, torznabSawGazelleWaiting.Load(), "Torznab must run while RED/OPS waits")
	require.True(t, resp.GazelleTimedOut)
	require.Len(t, resp.Results, 1)
	require.Equal(t, "torznab-guid", resp.Results[0].GUID)
	require.False(t, gazelleLookupCompleted, "a RED/OPS timeout must start no cooldown")
}

// A RED/OPS-only dialog search that runs out of time returns an empty result
// with the timeout note, not an error.
func TestSearchTorrentMatches_SlowGazelleOnlyReturnsEmptyResultWithNote(t *testing.T) {
	svc, instanceID, _ := newTorznabGazelleFixture(t, "crossseed-slow-gazelle-only", "http://127.0.0.1:1")
	clients, _, _ := slowGazelleClients(t)

	resp, gazelleLookupCompleted, _, err := svc.searchTorrentMatches(t.Context(), instanceID, torznabGazelleSourceHash, TorrentSearchOptions{}, searchScope{gazelleHosts: []string{"orpheus.network"}}, clients)

	require.NoError(t, err)
	require.True(t, resp.GazelleTimedOut)
	require.Empty(t, resp.Results)
	require.False(t, gazelleLookupCompleted)
}
