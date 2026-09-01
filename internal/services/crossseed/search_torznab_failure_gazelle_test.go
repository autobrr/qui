// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

	resp, _, _, err := svc.searchTorrentMatches(context.Background(), instanceID, torznabGazelleSourceHash, TorrentSearchOptions{
		IndexerIDs: []int{1},
	}, clients)

	require.NoError(t, err, "torznab failure must not discard gazelle matches")
	require.NotNil(t, resp)
	require.True(t, resp.Partial, "response should be marked partial when the torznab leg failed")
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

	resp, _, _, err := svc.searchTorrentMatches(ctx, instanceID, torznabGazelleSourceHash, TorrentSearchOptions{
		IndexerIDs: []int{1},
	}, clients)

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

	resp, _, _, err := svc.searchTorrentMatches(context.Background(), instanceID, torznabGazelleSourceHash, TorrentSearchOptions{
		IndexerIDs: []int{1},
	}, clients)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Results, 1, "the gazelle match still reaches the response")
	require.NotNil(t, resp.DecisionTrace)
	require.Equal(t, 0, resp.DecisionTrace.FinalMatches, "no torznab candidate was accepted")
}
