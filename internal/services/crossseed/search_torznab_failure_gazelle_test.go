// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	qbt "github.com/autobrr/go-qbittorrent"
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

// Regression: when the Torznab leg of a cross-seed search fails entirely (for
// example the only filtered indexer is a downed tracker), matches already
// found via Gazelle (RED/OPS) must be returned as a partial response instead
// of being discarded by a whole-search error.
func TestSearchTorrentMatches_TorznabFailureKeepsGazelleMatches(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-torznab-fail-gazelle")

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	sourceHash := "223759985c562a644428312c8cd3585d04686847"
	sourceHashNorm := strings.ToLower(sourceHash)
	sourceTorrent := qbt.Torrent{
		Hash:     sourceHash,
		Name:     "During - LMK (2024 WF)",
		Progress: 1.0,
		Size:     123,
		Tracker:  "https://flacsfor.me/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "During - LMK (2024 WF)/01 - Durante - Track.flac", Size: 123},
	}

	torrentDict := map[string]any{
		"announce": "https://flacsfor.me/announce",
		"info": map[string]any{
			"length": int64(123),
			"name":   "During - LMK (2024 WF)",
		},
	}
	torrentBytes, err := bencode.Marshal(torrentDict)
	require.NoError(t, err)

	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	prevFindMatch := findGazelleMatch
	findGazelleMatch = func(_ context.Context, _ *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
		return &gazellemusic.Match{TorrentID: 4242, Title: "During - LMK (2024 WF)", Size: 123, Reason: "hash"}, nil
	}
	defer func() {
		findGazelleMatch = prevFindMatch
	}()

	downedTracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	t.Cleanup(downedTracker.Close)

	svc := &Service{
		instanceStore: instanceStore,
		jackettService: jackett.NewService(&gettableIndexerStore{failingEnabledIndexerStore{indexers: []*models.TorznabIndexer{
			{
				ID:           1,
				Name:         "Generic Indexer",
				Enabled:      true,
				BaseURL:      downedTracker.URL,
				Backend:      models.TorznabBackendNative,
				Capabilities: []string{"search", "music-search", "audio-search"},
				Categories:   []models.TorznabIndexerCategory{{IndexerID: 1, CategoryID: 3000, CategoryName: "Audio"}},
			},
		}}}),
		syncManager: &hashFilteringSyncManager{
			gazelleSkipHashSyncManager: gazelleSkipHashSyncManager{
				torrents: []qbt.Torrent{sourceTorrent},
				filesByHash: map[string]qbt.TorrentFiles{
					sourceHashNorm: sourceFiles,
				},
				exportedTorrent: torrentBytes,
			},
		},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	resp, _, _, err := svc.searchTorrentMatches(ctx, instance.ID, sourceHash, TorrentSearchOptions{
		IndexerIDs: []int{1},
	}, clients)

	require.NoError(t, err, "torznab failure must not discard gazelle matches")
	require.NotNil(t, resp)
	require.True(t, resp.Partial, "response should be marked partial when the torznab leg failed")
	require.Len(t, resp.Results, 1)
	require.Equal(t, "gazelle:hash", resp.Results[0].MatchReason)
	require.Contains(t, resp.Results[0].DownloadURL, "4242", "download URL should reference the gazelle torrent")
}
