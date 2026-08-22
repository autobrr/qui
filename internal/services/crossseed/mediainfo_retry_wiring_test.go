// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	mediainfo "github.com/autobrr/go-mediainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

const mediaIDWiringSourceHash = "14a238e56ab06fed600c38d5068998d95e9338c2"

// TestMediaIDRetrySkipsNonIDIndexers drives the full search flow against two
// recording Torznab servers: one with an imdbid capability, one without. The
// MediaInfo ID retry must query the capable indexer by ID and send NOTHING to
// the other one; before SkipIndexersWithoutIDs the executor restored the title
// query for it, re-running a search that had just returned zero results.
func TestMediaIDRetrySkipsNonIDIndexers(t *testing.T) {
	const sourceName = "Signal.Static.1997.1080p.BluRay.DD5.1.x264-FIELDTEST"

	newRecorder := func(requests *[]url.Values, mu *sync.Mutex) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("t") != "caps" {
				mu.Lock()
				*requests = append(*requests, r.URL.Query())
				mu.Unlock()
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel></channel></rss>`))
		}))
	}

	var mu sync.Mutex
	var idCapableRequests, titleOnlyRequests []url.Values
	idCapable := newRecorder(&idCapableRequests, &mu)
	t.Cleanup(idCapable.Close)
	titleOnly := newRecorder(&titleOnlyRequests, &mu)
	t.Cleanup(titleOnly.Close)

	saveDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(saveDir, sourceName), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(saveDir, sourceName, sourceName+".mkv"), make([]byte, 10), 0o600))

	sourceTorrent := qbt.Torrent{
		Hash:     mediaIDWiringSourceHash,
		Name:     sourceName,
		Progress: 1.0,
		Size:     10,
		SavePath: saveDir,
		Tracker:  "https://example.invalid/announce",
	}
	sourceFiles := qbt.TorrentFiles{{Name: sourceName + "/" + sourceName + ".mkv", Size: 10}}

	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "crossseed-mediainfo-retry-wiring")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	localFS := true
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, &localFS)
	require.NoError(t, err)

	movieCategories := []models.TorznabIndexerCategory{{IndexerID: 1, CategoryID: 2000, CategoryName: "Movies"}}
	svc := &Service{
		instanceStore: instanceStore,
		jackettService: jackett.NewService(&gettableIndexerStore{failingEnabledIndexerStore{indexers: []*models.TorznabIndexer{
			{
				ID:           1,
				Name:         "ID Capable Indexer",
				Enabled:      true,
				BaseURL:      idCapable.URL,
				Backend:      models.TorznabBackendNative,
				Capabilities: []string{"search", "movie-search", "movie-search-imdbid"},
				Categories:   movieCategories,
			},
			{
				ID:           2,
				Name:         "Title Only Indexer",
				Enabled:      true,
				BaseURL:      titleOnly.URL,
				Backend:      models.TorznabBackendNative,
				Capabilities: []string{"search", "movie-search"},
				Categories:   movieCategories,
			},
		}}}),
		syncManager: &gazelleSkipHashSyncManager{
			torrents: []qbt.Torrent{sourceTorrent},
			filesByHash: map[string]qbt.TorrentFiles{
				mediaIDWiringSourceHash: sourceFiles,
			},
		},
		releaseCache:      NewReleaseCache(),
		stringNormalizer:  stringutils.NewDefaultNormalizer(),
		mediaIDCacheStore: newFakeMediaIDCache(),
		analyzeMediaFile: func(context.Context, string) (mediainfo.Report, error) {
			return generalReport(mediainfo.Field{Name: "IMDB", Value: "tt0118884"}), nil
		},
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	_, _, _, err = svc.searchTorrentMatches(ctx, instance.ID, mediaIDWiringSourceHash, TorrentSearchOptions{IndexerIDs: []int{1, 2}}, nil)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	var idPassRequests int
	for _, p := range idCapableRequests {
		if p.Get("imdbid") != "" {
			idPassRequests++
			require.Empty(t, p.Get("q"), "ID pass must not carry a title query")
		}
	}
	require.Equal(t, 1, idPassRequests, "ID-capable indexer must get exactly one ID query")

	// The retry adds exactly one extra request, and only to the ID-capable
	// indexer. Equal counts here mean the title-only indexer got a re-run of
	// its failed title search: the regression this test pins.
	require.Len(t, idCapableRequests, len(titleOnlyRequests)+1,
		"the ID retry must reach only the ID-capable indexer")
}
