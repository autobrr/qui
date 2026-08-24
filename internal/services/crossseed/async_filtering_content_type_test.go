// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/autobrr/pkg/ttlcache"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

// The search path must not honor a cached filtering run computed for a
// different content type; category mapping rules can change the type between
// runs (#2313).
func TestSearchIgnoresCachedFilteringStateForDifferentContentType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		cachedContentType string
		wantSearches      bool
	}{
		// Stale audiobook run cached an empty indexer set; a movie search must
		// not reuse it and must query the indexer.
		{name: "mismatch refilters", cachedContentType: "audiobook", wantSearches: true},
		// Matching type with the same empty completed set is honored: no queries.
		{name: "match reuses", cachedContentType: "movie", wantSearches: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const (
				instanceID = 1
				indexerID  = 42
			)
			sourceHash := strings.Repeat("a", 40)
			sourceName := "Example.Movie.2019.1080p.BluRay.x264-GROUP"

			var searchRequests atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("t") == "caps" {
					w.Header().Set("Content-Type", "application/xml")
					fmt.Fprint(w, `<caps><searching>
						<search available="yes" supportedParams="q"/>
						<movie-search available="yes" supportedParams="q"/>
					</searching><categories><category id="2000" name="Movies"/></categories></caps>`)
					return
				}
				searchRequests.Add(1)
				w.Header().Set("Content-Type", "application/rss+xml")
				fmt.Fprintf(w, `<rss version="2.0"><channel><title>Movie Indexer</title><item>
					<title>%s</title><guid>content-type-guard</guid><size>1000</size>
					<enclosure url="%s/candidate.torrent" length="1000" type="application/x-bittorrent" />
				</item></channel></rss>`, sourceName, server.URL)
			}))
			t.Cleanup(server.Close)

			instance := &models.Instance{ID: instanceID, Name: "main"}
			source := qbt.Torrent{
				Hash:      sourceHash,
				Name:      sourceName,
				Size:      1000,
				TotalSize: 1000,
				Progress:  1,
			}
			settings := models.DefaultCrossSeedAutomationSettings()

			filterCache := ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{})
			filterCache.Set(asyncFilteringCacheKey(instanceID, sourceHash), &AsyncIndexerFilteringState{
				CapabilitiesCompleted: true,
				ContentCompleted:      true,
				CapabilityIndexers:    []int{},
				FilteredIndexers:      []int{},
				contentType:           tc.cachedContentType,
			}, ttlcache.DefaultTTL)

			service := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
				jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
					{
						ID:             indexerID,
						Name:           "Movie Indexer",
						BaseURL:        server.URL,
						Backend:        models.TorznabBackendNative,
						TimeoutSeconds: 5,
						Enabled:        true,
					},
				}),
				syncManager: newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
					sourceHash: {{Name: sourceName + ".mkv", Size: 1000}},
				}),
				asyncFilteringCache: filterCache,
				releaseCache:        NewReleaseCache(),
				searchResultCache:   ttlcache.New(ttlcache.Options[string, cachedTorrentSearchResults]{}),
				stringNormalizer:    stringutils.NewDefaultNormalizer(),
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return settings, nil
				},
			}

			_, _, _, err := service.searchTorrentMatches(context.Background(), instanceID, sourceHash, TorrentSearchOptions{SkipGazelle: true}, nil)
			require.NoError(t, err)

			if tc.wantSearches {
				require.Positive(t, searchRequests.Load(), "stale content type must trigger fresh filtering and a real search")
			} else {
				require.Zero(t, searchRequests.Load(), "matching cached run with empty indexer set must skip searching")
			}
		})
	}
}

// A background worker whose cache entry was replaced by a newer run must not
// write itself back into the cache when it finishes (#2313).
func TestFinishedWorkerCannotRestoreReplacedCacheEntry(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	hash := strings.Repeat("b", 40)
	key := asyncFilteringCacheKey(instanceID, hash)

	svc := &Service{
		asyncFilteringCache: ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}),
	}

	stateA := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		contentType:           "audiobook",
	}
	stateB := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		FilteredIndexers:      []int{7},
		contentType:           "book",
	}
	svc.asyncFilteringCache.Set(key, stateB, ttlcache.DefaultTTL)

	// Worker for the replaced run A completes (empty indexer list short-circuits
	// content filtering, no other service dependencies needed).
	svc.performAsyncContentFiltering(context.Background(), instanceID, hash, nil, nil, stateA)

	snapshot := stateA.Clone()
	require.True(t, snapshot.ContentCompleted, "worker must still complete its own state")

	cached, found := svc.asyncFilteringCache.Get(key)
	require.True(t, found)
	require.Same(t, stateB, cached, "finished worker must not clobber the newer cache entry")
}

// failingListIndexerStore errors on List, the call AnalyzeTorrentForSearchAsync
// uses for indexer discovery.
type failingListIndexerStore struct {
	failingEnabledIndexerStore
}

func (s *failingListIndexerStore) List(context.Context) ([]*models.TorznabIndexer, error) {
	return nil, s.err
}

// A failed indexer lookup must not be cached: it would complete as an empty
// run and suppress searches for the whole TTL.
func TestAnalyzeAsyncDoesNotCacheFailedIndexerDiscovery(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	hash := "deadbeefcafe"
	movieName := "Example.Movie.2019.1080p.BluRay.x264-GROUP"
	instance := &models.Instance{ID: instanceID, Name: "main"}

	staleState := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		FilteredIndexers:      []int{7},
		contentType:           "audiobook",
	}
	cache := ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{})
	cache.Set(asyncFilteringCacheKey(instanceID, hash), staleState, ttlcache.DefaultTTL)

	svc := &Service{
		instanceStore:  &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		jackettService: jackett.NewService(&failingListIndexerStore{failingEnabledIndexerStore{err: errors.New("temporarily unreachable")}}),
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{{
			Hash: hash, Name: movieName, Progress: 1, Size: 1000,
		}}, map[string]qbt.TorrentFiles{
			hash: {{Name: movieName + ".mkv", Size: 1000}},
		}),
		asyncFilteringCache: cache,
		releaseCache:        NewReleaseCache(),
		stringNormalizer:    stringutils.NewDefaultNormalizer(),
	}

	result, err := svc.AnalyzeTorrentForSearchAsync(context.Background(), instanceID, hash, true)
	require.NoError(t, err)

	cached, found := svc.asyncFilteringCache.Get(asyncFilteringCacheKey(instanceID, hash))
	require.True(t, found)
	require.NotSame(t, result.FilteringState, cached, "failed discovery run must not enter the cache")
}

// AnalyzeTorrentForSearchAsync only reuses a completed cached run for the same
// content type (#2313).
func TestAnalyzeAsyncReusesOnlyMatchingContentType(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	hash := "deadbeefcafe"
	movieName := "Example.Movie.2019.1080p.BluRay.x264-GROUP"

	newService := func(cachedType string) (*Service, *AsyncIndexerFilteringState) {
		instance := &models.Instance{ID: instanceID, Name: "main"}
		cachedState := &AsyncIndexerFilteringState{
			CapabilitiesCompleted: true,
			ContentCompleted:      true,
			CapabilityIndexers:    []int{7},
			FilteredIndexers:      []int{7},
			contentType:           cachedType,
		}
		cache := ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{})
		cache.Set(asyncFilteringCacheKey(instanceID, hash), cachedState, ttlcache.DefaultTTL)
		return &Service{
			instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
			syncManager: newFakeSyncManager(instance, []qbt.Torrent{{
				Hash: hash, Name: movieName, Progress: 1, Size: 1000,
			}}, map[string]qbt.TorrentFiles{
				hash: {{Name: movieName + ".mkv", Size: 1000}},
			}),
			asyncFilteringCache: cache,
			releaseCache:        NewReleaseCache(),
			stringNormalizer:    stringutils.NewDefaultNormalizer(),
		}, cachedState
	}

	svc, cachedState := newService("movie")
	result, err := svc.AnalyzeTorrentForSearchAsync(context.Background(), instanceID, hash, true)
	require.NoError(t, err)
	require.Same(t, cachedState, result.FilteringState, "matching content type must reuse the cached run")
	require.Equal(t, []int{7}, result.TorrentInfo.FilteredIndexers)
	require.True(t, result.TorrentInfo.ContentFilteringCompleted)

	svc, cachedState = newService("audiobook")
	result, err = svc.AnalyzeTorrentForSearchAsync(context.Background(), instanceID, hash, true)
	require.NoError(t, err)
	require.NotSame(t, cachedState, result.FilteringState, "different content type must not reuse the cached run")
	require.NotEqual(t, []int{7}, result.TorrentInfo.FilteredIndexers)

	// no indexers available here (nil jackett)
	cached, found := svc.asyncFilteringCache.Get(asyncFilteringCacheKey(instanceID, hash))
	require.True(t, found)
	require.Same(t, result.FilteringState, cached, "fresh analysis must replace the stale differently typed cache entry")
}
