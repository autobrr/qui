// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

// TestEpisodeMapSearchSendsSeasonedPairOnIDPath drives the search for an
// absolute-numbered source through two recording Torznab servers. With a map,
// the ID-capable indexer receives the mapped season and episode next to the
// TVDb ID and no title query; the text indexer receives the title alone with
// no season or episode, and the title retry passes stay unchanged. Without a
// map the ID-capable indexer receives the ID alone.
func TestEpisodeMapSearchSendsSeasonedPairOnIDPath(t *testing.T) {
	const (
		sourceHash = "9b3f0d8e5a1c4b7e2f6d0a9c8b7e6f5d4c3b2a10"
		sourceName = "[KiraSubs] Azure Compass - 81 (1080p) [ABCD1234].mkv"
	)

	for _, tt := range []struct {
		name       string
		episodeMap *models.EpisodeMap
	}{
		{name: "with map", episodeMap: episodeMapS04E15},
		{name: "without map"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var idCapableRequests, textRequests []url.Values
			newRecorder := func(requests *[]url.Values) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					q := r.URL.Query()
					if q.Get("t") != "caps" {
						mu.Lock()
						*requests = append(*requests, q)
						mu.Unlock()
					}
					w.Header().Set("Content-Type", "application/rss+xml")
					_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel></channel></rss>`))
				}))
			}
			idCapable := newRecorder(&idCapableRequests)
			t.Cleanup(idCapable.Close)
			text := newRecorder(&textRequests)
			t.Cleanup(text.Close)

			ctx := context.Background()
			instance := &models.Instance{ID: 1, Name: "Test"}

			sourceTorrent := qbt.Torrent{Hash: sourceHash, Name: sourceName, Progress: 1, Size: episodeMapSize, TotalSize: episodeMapSize, Tracker: "https://example.invalid/announce"}
			tvCategories := []models.TorznabIndexerCategory{{IndexerID: 1, CategoryID: 5000, CategoryName: "TV"}, {IndexerID: 1, CategoryID: 5070, CategoryName: "TV/Anime"}}
			svc := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
				arrService: &spyARRLookupService{result: &arr.ExternalIDsResult{
					IDs:        &models.ExternalIDs{TVDbID: 424242},
					EpisodeMap: tt.episodeMap,
				}},
				jackettService: jackett.NewService(&gettableIndexerStore{failingEnabledIndexerStore{indexers: []*models.TorznabIndexer{
					{ID: 1, Name: "ID Capable Indexer", Enabled: true, BaseURL: idCapable.URL, Backend: models.TorznabBackendNative, Capabilities: []string{"search", "tv-search", "tv-search-tvdbid", "tv-search-season", "tv-search-ep"}, Categories: tvCategories},
					{ID: 2, Name: "Text Indexer", Enabled: true, BaseURL: text.URL, Backend: models.TorznabBackendNative, Capabilities: []string{"search", "tv-search"}, Categories: tvCategories},
				}}}, jackett.WithMinRequestInterval(time.Millisecond)),
				syncManager: &gazelleSkipHashSyncManager{
					torrents:    []qbt.Torrent{sourceTorrent},
					filesByHash: map[string]qbt.TorrentFiles{sourceHash: {{Name: sourceName, Size: episodeMapSize, Progress: 1}}},
				},
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return models.DefaultCrossSeedAutomationSettings(), nil
				},
			}

			_, _, _, err := svc.searchTorrentMatches(ctx, instance.ID, sourceHash, TorrentSearchOptions{IndexerIDs: []int{1, 2}}, nil)
			require.NoError(t, err)

			mu.Lock()
			defer mu.Unlock()

			var idQueries int
			for _, p := range idCapableRequests {
				if p.Get("tvdbid") == "" {
					// Title retry pass: text mode, no mapped pair.
					require.NotEmpty(t, p.Get("q"))
					require.Empty(t, p.Get("season"), "a title retry must not carry the mapped season")
					require.Empty(t, p.Get("ep"), "a title retry must not carry the mapped episode")
					continue
				}
				idQueries++
				require.Empty(t, p.Get("q"), "an ID query must not carry a title query")
				if tt.episodeMap == nil {
					require.Empty(t, p.Get("season"), "without a map the ID path sends the ID alone")
					require.Empty(t, p.Get("ep"), "without a map the ID path sends the ID alone")
					continue
				}
				require.Equal(t, "4", p.Get("season"), "the ID path must carry the mapped season")
				require.Equal(t, "15", p.Get("ep"), "the ID path must carry the mapped episode")
			}
			require.Positive(t, idQueries, "ID-capable indexer must search by ID")

			require.NotEmpty(t, textRequests)
			for _, p := range textRequests {
				require.NotEmpty(t, p.Get("q"), "text indexer must search by title")
				require.Empty(t, p.Get("tvdbid"), "text indexer must never receive an ID param")
				require.Empty(t, p.Get("season"), "text indexer must keep the title-only query")
				require.Empty(t, p.Get("ep"), "text indexer must keep the title-only query")
			}
		})
	}
}
