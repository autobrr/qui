// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

const musicQuerySourceHash = "5f2b1c4d3e6a7089bacd1122334455667788990a"

// Regression: two signals the name parse cannot see force the music content type.
// The file-extension signal corrects a numeric album title that reads as a season
// or an episode, and a category mapping rule corrects what the extensions cannot,
// such as a disc image that carries no audio extension. Either way the
// Torznab request must come out music-shaped: the album title has to survive into
// the query, and the season or episode the name parse invented must not reach the
// indexer.
func TestSearchTorrentMatches_ForcedMusicBuildsMusicQuery(t *testing.T) {
	tests := []struct {
		name                string
		sourceName          string
		fileName            string
		category            string
		wantFileSignalMusic bool
		wantInQuery         string
	}{
		{
			name:                "numeric album title parses as an episode",
			sourceName:          "Sable Wren - 9 (2025) [WEB FLAC]",
			fileName:            "01. Opening Track.flac",
			wantFileSignalMusic: true,
			wantInQuery:         "9",
		},
		{
			name:                "album title parses as a series",
			sourceName:          "Sable Wren - Season 2 (2024) WEB FLAC",
			fileName:            "01. Opening Track.flac",
			wantFileSignalMusic: true,
			wantInQuery:         "Season 2",
		},
		{
			name:        "category rule forces music the extensions cannot",
			sourceName:  "Sable Wren - 9 (2025) WEB",
			fileName:    "album.iso",
			category:    "audio",
			wantInQuery: "9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var params []url.Values

			tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("t") != "caps" {
					mu.Lock()
					params = append(params, r.URL.Query())
					mu.Unlock()
				}
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel></channel></rss>`))
			}))
			t.Cleanup(tracker.Close)

			sourceTorrent := qbt.Torrent{
				Hash:     musicQuerySourceHash,
				Name:     tt.sourceName,
				Category: tt.category,
				Progress: 1.0,
				Size:     100,
				Tracker:  "https://example.invalid/announce",
			}
			sourceFiles := qbt.TorrentFiles{
				{Name: tt.sourceName + "/" + tt.fileName, Size: 100},
			}

			// A rule with an empty category never matches, so the rows that rely on the
			// file signal keep the settings a nil loader would have produced.
			settings := models.DefaultCrossSeedAutomationSettings()
			settings.CategoryMappingRules = []models.CategoryMappingRule{{Categories: []string{tt.category}, ContentType: "music"}}

			ctx := context.Background()
			db := testdb.NewMigratedSQLite(t, "crossseed-forced-music-query")
			instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
			require.NoError(t, err)

			svc := &Service{
				instanceStore: instanceStore,
				jackettService: jackett.NewService(&gettableIndexerStore{failingEnabledIndexerStore{indexers: []*models.TorznabIndexer{
					{
						ID:           1,
						Name:         "Generic Indexer",
						Enabled:      true,
						BaseURL:      tracker.URL,
						Backend:      models.TorznabBackendNative,
						Capabilities: []string{"search", "music-search", "audio-search"},
						Categories:   []models.TorznabIndexerCategory{{IndexerID: 1, CategoryID: 3000, CategoryName: "Audio"}},
					},
				}}}, jackett.WithMinRequestInterval(time.Millisecond)),
				syncManager: &gazelleSkipHashSyncManager{
					torrents: []qbt.Torrent{sourceTorrent},
					filesByHash: map[string]qbt.TorrentFiles{
						strings.ToLower(musicQuerySourceHash): sourceFiles,
					},
				},
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return settings, nil
				},
			}

			sourceRelease := svc.releaseCache.Parse(tt.sourceName)
			require.True(t, sourceRelease.Series > 0 || sourceRelease.Episode > 0,
				"fixture only exercises the fix while the name parses with TV structure")
			contentDetectionRelease, _ := svc.selectContentDetectionRelease(tt.sourceName, sourceRelease, sourceFiles)
			require.NotEqual(t, rls.Music, contentDetectionRelease.Type,
				"fixture only exercises the fix while the detection release is not already music")
			require.Equal(t, tt.wantFileSignalMusic,
				DetermineContentTypeWithFiles(contentDetectionRelease, sourceFiles).ContentType == "music",
				"file-extension signal must match the row's intent")

			// nil gazelle clients: this source is not a gazelle tracker, and a client set here
			// would send the fixture out to the real RED/OPS hosts.
			_, _, _, err = svc.searchTorrentMatches(ctx, instance.ID, musicQuerySourceHash, TorrentSearchOptions{IndexerIDs: []int{1}}, nil)
			require.NoError(t, err)

			mu.Lock()
			defer mu.Unlock()
			require.NotEmpty(t, params, "indexer was never queried")
			for _, p := range params {
				require.Contains(t, p.Get("q"), tt.wantInQuery,
					"album title must survive into the query, not just the artist")
				require.Empty(t, p.Get("season"), "music search must not carry a season")
				require.Empty(t, p.Get("ep"), "music search must not carry an episode")
			}
		})
	}
}
