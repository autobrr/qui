// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
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
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

const mediaIDWiringSourceHash = "14a238e56ab06fed600c38d5068998d95e9338c2"

// TestTagIDPrimaryMixedModeWithTitleRescue drives the full search flow against
// two recording Torznab servers: one with an imdbid capability, one without.
// With no arr ID available, the primary search must run ID-first from the
// MKV tag in mixed mode: the ID-capable indexer searches by imdbid with no
// title query, the title-only indexer gets the title query in the same pass.
// When the ID results all fail verification, the title rescue pass must send
// the plain title to the ID-queried indexer only; the title-only indexer
// already searched by title and must not be re-run.
func TestTagIDPrimaryMixedModeWithTitleRescue(t *testing.T) {
	const sourceName = "Signal.Static.1997.1080p.BluRay.DD5.1.x264-FIELDTEST"

	// The ID query returns one (junk) candidate so the flow can observe a
	// nonempty ID-assisted result set; title queries return nothing.
	newRecorder := func(requests *[]url.Values, mu *sync.Mutex) *httptest.Server {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("t") != "caps" {
				mu.Lock()
				*requests = append(*requests, q)
				mu.Unlock()
			}
			w.Header().Set("Content-Type", "application/rss+xml")
			if q.Get("imdbid") != "" {
				_, _ = fmt.Fprintf(w, `<rss version="2.0"><channel><title>Indexer</title><item>
					<title>Contact.1997.1080p.BluRay.x264-OTHER</title><guid>id-hit</guid><size>4000000</size>
					<enclosure url="%s/candidate.torrent" length="4000000" type="application/x-bittorrent" />
				</item></channel></rss>`, server.URL)
				return
			}
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel></channel></rss>`))
		}))
		return server
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
	sourceFiles := qbt.TorrentFiles{{Name: sourceName + "/" + sourceName + ".mkv", Size: 10, Progress: 1}}

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
		// ARR knows the title but has no IDs: the search starts degraded
		// (arr_no_ids) and a successful ID retry must clear that notice.
		arrService: &spyARRLookupService{result: &arr.ExternalIDsResult{}},
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

	resp, _, _, err := svc.searchTorrentMatches(ctx, instance.ID, mediaIDWiringSourceHash, TorrentSearchOptions{IndexerIDs: []int{1, 2}}, nil)
	require.NoError(t, err)
	require.Empty(t, resp.QueryDegraded,
		"a tag-sourced ID primary must clear the searched-by-title-only notice")

	mu.Lock()
	defer mu.Unlock()

	// Primary pass, mixed mode: ID query to the ID-capable indexer, title
	// query to the other one.
	require.NotEmpty(t, idCapableRequests)
	require.NotEmpty(t, idCapableRequests[0].Get("imdbid"),
		"primary must search the ID-capable indexer by ID")
	require.NotEmpty(t, titleOnlyRequests)
	require.NotEmpty(t, titleOnlyRequests[0].Get("q"),
		"primary must search the title-only indexer by title in the same pass")

	var idQueries, titleQueries int
	for _, p := range idCapableRequests {
		if p.Get("imdbid") != "" {
			require.Empty(t, p.Get("q"), "an ID query must not carry a title query")
			idQueries++
		} else {
			require.NotEmpty(t, p.Get("q"))
			titleQueries++
		}
	}
	require.Positive(t, idQueries, "ID-capable indexer must search by ID")
	require.Equal(t, 1, titleQueries,
		"the title rescue must send exactly one title query to the ID-queried indexer")

	for _, p := range titleOnlyRequests {
		require.Empty(t, p.Get("imdbid"), "title-only indexer must never receive an ID param")
	}

	// The rescue adds exactly one extra request, and only to the ID-queried
	// indexer. Equal counts here mean the title-only indexer got a re-run of
	// its already-searched title query: the waste this test pins.
	require.Len(t, idCapableRequests, len(titleOnlyRequests)+1,
		"the title rescue must reach only the ID-queried indexer")
}
