// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestFilterIndexersByExistingContentUsesConfiguredIndexerDomains(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	source := qbt.Torrent{
		Hash:     "sourcehash",
		Name:     "Dummy.Movie.2007.720p.BluRay.DTS.x264-CRiSC",
		Progress: 1,
		Tracker:  "https://source.example/announce",
	}
	sync := &duplicateFilteringSyncManager{
		torrents: map[int][]qbt.Torrent{
			instanceID: {
				source,
				{Hash: "uploadhash", Name: source.Name, Progress: 1, Tracker: "https://upload.cx/announce"},
				{Hash: "mttvhash", Name: source.Name, Progress: 1, Tracker: "https://tracker.morethantv.me/announce"},
				{Hash: "hdbhash", Name: source.Name, Progress: 1, Tracker: "https://hdbits.org/announce"},
			},
		},
		files: map[string]qbt.TorrentFiles{
			"sourcehash": {{Name: "Dummy.Movie.2007.720p.BluRay.DTS.x264-CRiSC.mkv", Size: 1024}},
			"uploadhash": {{Name: "Dummy.Movie.2007.720p.BluRay.DTS.x264-CRiSC.mkv", Size: 1024}},
			"mttvhash":   {{Name: "Dummy.Movie.2007.720p.BluRay.DTS.x264-CRiSC.mkv", Size: 1024}},
			"hdbhash":    {{Name: "Dummy.Movie.2007.720p.BluRay.DTS.x264-CRiSC.mkv", Size: 1024}},
		},
	}
	svc := newDuplicateFilteringContentFilterService(sync)

	indexerIDs := []int{101, 202, 303, 404}
	indexerInfo := map[int]jackett.EnabledIndexerInfo{
		101: {ID: 101, Name: "Upload API", Domain: "upload.cx"},
		202: {ID: 202, Name: "MoreThanTV", Domain: "morethantv.me"},
		303: {ID: 303, Name: "HDBits", Domain: "hdbits.org"},
		404: {ID: 404, Name: "Other", Domain: "other.example"},
	}

	filtered, excluded, contentMatches, _, err := svc.filterIndexersByExistingContent(context.Background(), instanceID, source.Hash, indexerIDs, indexerInfo)
	require.NoError(t, err)
	require.ElementsMatch(t, []int{404}, filtered)
	require.Contains(t, excluded, 101)
	require.Contains(t, excluded, 202)
	require.Contains(t, excluded, 303)
	require.NotContains(t, excluded, 404)
	require.NotEmpty(t, contentMatches)
}

func TestFilterIndexersByExistingContentMatchesFolderSourceToRootlessExisting(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	const sourceHash = "sourcehash"
	const retroHash = "830214dd915a3b57843d0f369d06bc9ad5253c6f"
	const mediaFile = "Dummy.Movie.2007.720p.BluRay.DTS.x264-CRiSC.mkv"
	const mediaSize = int64(4_294_967_296)

	source := qbt.Torrent{
		Hash:     sourceHash,
		Name:     "Dummy Movie 2007 720p BluRay DTS x264-CRiSC",
		Progress: 1,
		Tracker:  "https://source.example/announce",
	}
	existing := qbt.Torrent{
		Hash:     retroHash,
		Name:     "Dummy.Movie.2007.720p.BluRay.DTS.x264-CRiSC",
		Progress: 1,
		Tracker:  "https://retroflix.club/announce",
	}
	sync := &duplicateFilteringSyncManager{
		torrents: map[int][]qbt.Torrent{
			instanceID: {source, existing},
		},
		files: map[string]qbt.TorrentFiles{
			sourceHash: {{Name: "Dummy Movie 2007 720p BluRay DTS x264-CRiSC/" + mediaFile, Size: mediaSize}},
			retroHash:  {{Name: mediaFile, Size: mediaSize}},
		},
	}
	svc := newDuplicateFilteringContentFilterService(sync)

	indexerIDs := []int{10, 20}
	indexerInfo := map[int]jackett.EnabledIndexerInfo{
		10: {ID: 10, Name: "RetroFlix", Domain: "retroflix.club"},
		20: {ID: 20, Name: "Other", Domain: "other.example"},
	}

	filtered, excluded, contentMatches, _, err := svc.filterIndexersByExistingContent(context.Background(), instanceID, source.Hash, indexerIDs, indexerInfo)
	require.NoError(t, err)
	require.ElementsMatch(t, []int{20}, filtered)
	require.Contains(t, excluded, 10)
	require.NotContains(t, excluded, 20)
	require.Equal(t, []string{"Dummy.Movie.2007.720p.BluRay.DTS.x264-CRiSC (Main)"}, contentMatches)
}

func TestFilterIndexersByExistingContentMatchesRootlessSourceToFolderExisting(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	const sourceHash = "sourcehash"
	const existingHash = "folderhash"
	const mediaFile = "Movie.2024.1080p.BluRay.x264-GROUP.mkv"
	const mediaSize = int64(2048)

	source := qbt.Torrent{
		Hash:     sourceHash,
		Name:     "Movie.2024.1080p.BluRay.x264-GROUP",
		Progress: 1,
		Tracker:  "https://source.example/announce",
	}
	existing := qbt.Torrent{
		Hash:     existingHash,
		Name:     "Movie.2024.1080p.BluRay.x264-GROUP",
		Progress: 1,
		Tracker:  "https://tracker.example/announce",
	}
	sync := &duplicateFilteringSyncManager{
		torrents: map[int][]qbt.Torrent{
			instanceID: {source, existing},
		},
		files: map[string]qbt.TorrentFiles{
			sourceHash:   {{Name: mediaFile, Size: mediaSize}},
			existingHash: {{Name: "Movie.2024.1080p.BluRay.x264-GROUP/" + mediaFile, Size: mediaSize}},
		},
	}
	svc := newDuplicateFilteringContentFilterService(sync)

	indexerIDs := []int{10, 20}
	indexerInfo := map[int]jackett.EnabledIndexerInfo{
		10: {ID: 10, Name: "Tracker", Domain: "tracker.example"},
		20: {ID: 20, Name: "Other", Domain: "other.example"},
	}

	filtered, excluded, contentMatches, _, err := svc.filterIndexersByExistingContent(context.Background(), instanceID, source.Hash, indexerIDs, indexerInfo)
	require.NoError(t, err)
	require.ElementsMatch(t, []int{20}, filtered)
	require.Contains(t, excluded, 10)
	require.NotContains(t, excluded, 20)
	require.Equal(t, []string{"Movie.2024.1080p.BluRay.x264-GROUP (Main)"}, contentMatches)
}

func TestFilterIndexersByExistingContentRejectsDifferentFileSize(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	const sourceHash = "sourcehash"
	const existingHash = "existinghash"
	const mediaFile = "Movie.2024.1080p.BluRay.x264-GROUP.mkv"

	source := qbt.Torrent{
		Hash:     sourceHash,
		Name:     "Movie.2024.1080p.BluRay.x264-GROUP",
		Progress: 1,
		Tracker:  "https://source.example/announce",
	}
	existing := qbt.Torrent{
		Hash:     existingHash,
		Name:     source.Name,
		Progress: 1,
		Tracker:  "https://tracker.example/announce",
	}
	sync := &duplicateFilteringSyncManager{
		torrents: map[int][]qbt.Torrent{
			instanceID: {source, existing},
		},
		files: map[string]qbt.TorrentFiles{
			sourceHash:   {{Name: mediaFile, Size: 1024}},
			existingHash: {{Name: mediaFile, Size: 2048}},
		},
	}
	svc := newDuplicateFilteringContentFilterService(sync)

	indexerIDs := []int{10}
	indexerInfo := map[int]jackett.EnabledIndexerInfo{
		10: {ID: 10, Name: "Tracker", Domain: "tracker.example"},
	}

	filtered, excluded, contentMatches, rejected, err := svc.filterIndexersByExistingContent(context.Background(), instanceID, source.Hash, indexerIDs, indexerInfo)
	require.NoError(t, err)
	require.Equal(t, indexerIDs, filtered)
	require.Empty(t, excluded)
	require.Empty(t, contentMatches)
	rejectionKey := contentPrefilterRejectedContentKey(10, existingHash)
	require.Contains(t, rejected, rejectionKey)
	require.Contains(t, rejected[rejectionKey].Reason, "Size mismatch")
}

func TestFilterIndexersByExistingContentRejectsSidecarOnlyExisting(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	const sourceHash = "sourcehash"
	const existingHash = "existinghash"

	source := qbt.Torrent{
		Hash:     sourceHash,
		Name:     "Movie.2024.1080p.BluRay.x264-GROUP",
		Progress: 1,
		Tracker:  "https://source.example/announce",
	}
	existing := qbt.Torrent{
		Hash:     existingHash,
		Name:     source.Name,
		Progress: 1,
		Tracker:  "https://tracker.example/announce",
	}
	sync := &duplicateFilteringSyncManager{
		torrents: map[int][]qbt.Torrent{
			instanceID: {source, existing},
		},
		files: map[string]qbt.TorrentFiles{
			sourceHash:   {{Name: "Movie.2024.1080p.BluRay.x264-GROUP.mkv", Size: 1024}},
			existingHash: {{Name: "Movie.2024.1080p.BluRay.x264-GROUP.nfo", Size: 1024}},
		},
	}
	svc := newDuplicateFilteringContentFilterService(sync)

	indexerIDs := []int{10}
	indexerInfo := map[int]jackett.EnabledIndexerInfo{
		10: {ID: 10, Name: "Tracker", Domain: "tracker.example"},
	}

	filtered, excluded, contentMatches, _, err := svc.filterIndexersByExistingContent(context.Background(), instanceID, source.Hash, indexerIDs, indexerInfo)
	require.NoError(t, err)
	require.Equal(t, indexerIDs, filtered)
	require.Empty(t, excluded)
	require.Empty(t, contentMatches)
}

func TestBuildTorrentSearchResultsFiltersExistingInfohashes(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	duplicateHash := strings.Repeat("a", 40)
	duplicateHashV2 := strings.Repeat("c", 64)
	uniqueHash := strings.Repeat("b", 40)
	svc := &Service{
		syncManager: &duplicateFilteringSyncManager{
			existingByHash: map[string]qbt.Torrent{
				duplicateHash:   {Hash: duplicateHash, Name: "Already.Seeded.2007.720p.BluRay-GROUP", Progress: 1},
				duplicateHashV2: {Hash: duplicateHashV2, Name: "Already.Seeded.V2.2007.720p.BluRay-GROUP", Progress: 1},
			},
		},
	}

	scored := []scoredTorrentSearchResult{
		{
			result: jackett.SearchResult{
				Indexer:     "HDBits",
				IndexerID:   303,
				Title:       "Already.Seeded.2007.720p.BluRay-GROUP",
				DownloadURL: "https://example.invalid/duplicate.torrent",
				GUID:        "duplicate-guid",
				InfoHashV1:  duplicateHash,
				PublishDate: time.Now(),
			},
			score:  1,
			reason: "exact",
		},
		{
			result: jackett.SearchResult{
				Indexer:     "V2Only",
				IndexerID:   304,
				Title:       "Already.Seeded.V2.2007.720p.BluRay-GROUP",
				DownloadURL: "https://example.invalid/duplicate-v2.torrent",
				GUID:        "duplicate-v2-guid",
				InfoHashV2:  duplicateHashV2,
				PublishDate: time.Now(),
			},
			score:  1,
			reason: "exact",
		},
		{
			result: jackett.SearchResult{
				Indexer:     "NoHash",
				IndexerID:   404,
				Title:       "No.Hash.2007.720p.BluRay-GROUP",
				DownloadURL: "https://example.invalid/nohash.torrent",
				GUID:        "nohash-guid",
				PublishDate: time.Now(),
			},
			score:  1,
			reason: "exact",
		},
		{
			result: jackett.SearchResult{
				Indexer:     "Unique",
				IndexerID:   505,
				Title:       "Unique.2007.720p.BluRay-GROUP",
				DownloadURL: "https://example.invalid/unique.torrent",
				GUID:        "unique-guid",
				InfoHashV1:  uniqueHash,
				PublishDate: time.Now(),
			},
			score:  1,
			reason: "exact",
		},
	}

	results, duplicateFiltered, err := svc.buildTorrentSearchResults(context.Background(), instanceID, "", scored, 10)

	require.NoError(t, err)
	require.Equal(t, 2, duplicateFiltered)
	require.Len(t, results, 2)
	require.Equal(t, "No.Hash.2007.720p.BluRay-GROUP", results[0].Title)
	require.Equal(t, "Unique.2007.720p.BluRay-GROUP", results[1].Title)
	require.Equal(t, uniqueHash, results[1].InfoHashV1)
}

func TestBuildTorrentSearchResultsPropagatesContextErrors(t *testing.T) {
	t.Parallel()

	svc := &Service{
		syncManager: &duplicateFilteringSyncManager{
			hashErr: context.Canceled,
		},
	}
	scored := []scoredTorrentSearchResult{
		{
			result: jackett.SearchResult{
				Indexer:    "V2Only",
				IndexerID:  304,
				Title:      "Already.Seeded.V2.2007.720p.BluRay-GROUP",
				InfoHashV2: strings.Repeat("f", 64),
			},
			score:  1,
			reason: "exact",
		},
		{
			result: jackett.SearchResult{
				Indexer:    "HDBits",
				IndexerID:  303,
				Title:      "Already.Seeded.2007.720p.BluRay-GROUP",
				InfoHashV1: strings.Repeat("e", 40),
			},
			score:  1,
			reason: "exact",
		},
	}

	results, duplicateFiltered, err := svc.buildTorrentSearchResults(context.Background(), 1, "", scored, 10)

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, results)
	require.Zero(t, duplicateFiltered)
}

func TestBuildTorrentSearchResultsKeepsDuplicateRejectedByContentPrefilter(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	sourceHash := strings.Repeat("a", 40)
	duplicateHash := strings.Repeat("b", 40)
	existing := qbt.Torrent{
		Hash: duplicateHash,
		Name: "Already.Seeded.2007.720p.BluRay-GROUP",
	}
	svc := &Service{
		syncManager: &duplicateFilteringSyncManager{
			existingByHash: map[string]qbt.Torrent{
				duplicateHash: existing,
			},
		},
		asyncFilteringCache: ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}),
	}
	svc.asyncFilteringCache.Set(asyncFilteringCacheKey(instanceID, sourceHash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		rejectedContentCandidates: map[string]contentPrefilterRejectedTorrent{
			contentPrefilterRejectedContentKey(303, duplicateHash): {
				Hash:   duplicateHash,
				Name:   existing.Name,
				Reason: "Size mismatch: source 1.00 KB vs existing 2.00 KB",
			},
		},
	}, ttlcache.DefaultTTL)
	scored := []scoredTorrentSearchResult{
		{
			result: jackett.SearchResult{
				Indexer:    "HDBits",
				IndexerID:  303,
				Title:      existing.Name,
				InfoHashV1: duplicateHash,
			},
			score:  1,
			reason: "exact",
		},
	}

	results, duplicateFiltered, err := svc.buildTorrentSearchResults(context.Background(), instanceID, sourceHash, scored, 10)

	require.NoError(t, err)
	require.Zero(t, duplicateFiltered)
	require.Len(t, results, 1)
	require.Equal(t, existing.Name, results[0].Title)
	require.Equal(t, duplicateHash, results[0].InfoHashV1)

	scored[0].provenance.Class = searchCandidateClassTitleRescue
	results, duplicateFiltered, err = svc.buildTorrentSearchResults(context.Background(), instanceID, sourceHash, scored, 10)
	require.NoError(t, err)
	require.Equal(t, 1, duplicateFiltered)
	require.Empty(t, results)
}

func TestContentFilteringWaitTimeoutDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, 20*time.Second, contentFilteringWaitTimeout)
}

func TestFilterSearchResultsByLateContentFilterMissingOrIncompleteStateLeavesResults(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	source := &qbt.Torrent{Hash: "sourcehash", Name: "Source.Movie.2015.1080p.BluRay-GROUP"}
	results := []jackett.SearchResult{
		{Indexer: "Indexer One", IndexerID: 1, Title: "Source.Movie.2015.1080p.BluRay-GROUP"},
	}
	svc := &Service{
		asyncFilteringCache: ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}),
	}

	filtered, snapshot, dropped := svc.filterSearchResultsByLateContentFilter(instanceID, source, results)
	require.Equal(t, results, filtered)
	require.Nil(t, snapshot)
	require.Zero(t, dropped)

	svc.asyncFilteringCache.Set(asyncFilteringCacheKey(instanceID, source.Hash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      false,
		CapabilityIndexers:    []int{1},
		FilteredIndexers:      []int{1},
	}, ttlcache.DefaultTTL)

	filtered, snapshot, dropped = svc.filterSearchResultsByLateContentFilter(instanceID, source, results)
	require.Equal(t, results, filtered)
	require.Nil(t, snapshot)
	require.Zero(t, dropped)
}

func TestFilterSearchResultsByLateContentFilterCompletedStateNoExcludedResultIndexers(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	source := &qbt.Torrent{Hash: "sourcehash", Name: "Source.Movie.2015.1080p.BluRay-GROUP"}
	results := []jackett.SearchResult{
		{Indexer: "Indexer One", IndexerID: 1, Title: "Source.Movie.2015.1080p.BluRay-GROUP"},
		{Indexer: "Indexer Two", IndexerID: 2, Title: "Source.Movie.2015.1080p.BluRay-GROUP"},
	}
	svc := &Service{
		asyncFilteringCache: ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}),
	}
	svc.asyncFilteringCache.Set(asyncFilteringCacheKey(instanceID, source.Hash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		CapabilityIndexers:    []int{1, 2, 3},
		FilteredIndexers:      []int{1, 2},
		ExcludedIndexers:      map[int]string{3: "already seeded from Tracker Three"},
	}, ttlcache.DefaultTTL)

	filtered, snapshot, dropped := svc.filterSearchResultsByLateContentFilter(instanceID, source, results)
	require.Equal(t, results, filtered)
	require.NotNil(t, snapshot)
	require.Zero(t, dropped)
}

func TestFilterSearchResultsByLateContentFilterCompletedStateWithNoResultsReturnsSnapshot(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	source := &qbt.Torrent{Hash: "sourcehash", Name: "Source.Movie.2015.1080p.BluRay-GROUP"}
	svc := &Service{
		asyncFilteringCache: ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}),
	}
	svc.asyncFilteringCache.Set(asyncFilteringCacheKey(instanceID, source.Hash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		CapabilityIndexers:    []int{1, 2},
		FilteredIndexers:      []int{2},
		ExcludedIndexers:      map[int]string{1: "already seeded from Tracker One"},
		ContentMatches:        []string{"Existing.Movie.2015.1080p.BluRay-GROUP"},
	}, ttlcache.DefaultTTL)

	filtered, snapshot, dropped := svc.filterSearchResultsByLateContentFilter(instanceID, source, nil)
	require.Empty(t, filtered)
	require.NotNil(t, snapshot)
	require.True(t, snapshot.ContentCompleted)
	require.Equal(t, []int{2}, snapshot.FilteredIndexers)
	require.Equal(t, map[int]string{1: "already seeded from Tracker One"}, snapshot.ExcludedIndexers)
	require.Equal(t, []string{"Existing.Movie.2015.1080p.BluRay-GROUP"}, snapshot.ContentMatches)
	require.Zero(t, dropped)
}

func TestSearchTorrentMatchesRefreshesLateFilterStatus(t *testing.T) {
	const (
		instanceID = 1
		sourceHash = "0123456789abcdef0123456789abcdef01234567"
		sourceName = "Example.Show.S01E01.1080p.WEB-DL.DDP5.1.H.264-GROUP"
	)

	tests := []struct {
		name        string
		resultItems string
		wantResults int
	}{
		{name: "zero results"},
		{
			name: "populated results",
			resultItems: `<item>
				<title>` + sourceName + `</title>
				<guid>candidate-guid</guid>
				<size>123</size>
				<enclosure url="https://example.invalid/candidate.torrent" length="123" type="application/x-bittorrent" />
			</item>`,
			wantResults: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filterCache := ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{})
			cacheKey := asyncFilteringCacheKey(instanceID, sourceHash)
			filterCache.Set(cacheKey, &AsyncIndexerFilteringState{
				CapabilitiesCompleted: true,
				ContentCompleted:      false,
				CapabilityIndexers:    []int{1, 2},
				FilteredIndexers:      []int{1, 2},
			}, ttlcache.DefaultTTL)

			lateState := &AsyncIndexerFilteringState{
				CapabilitiesCompleted: true,
				ContentCompleted:      true,
				CapabilityIndexers:    []int{1, 2},
				FilteredIndexers:      []int{1},
				ExcludedIndexers:      map[int]string{2: "already seeded from Tracker Two"},
				ContentMatches:        []string{"Existing.Show.S01E01.1080p.WEB-DL-GROUP"},
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				filterCache.Set(cacheKey, lateState, ttlcache.DefaultTTL)
				w.Header().Set("Content-Type", "application/rss+xml")
				if _, writeErr := fmt.Fprintf(w, `<rss version="2.0"><channel><title>Tracker One</title>%s</channel></rss>`, test.resultItems); writeErr != nil {
					t.Errorf("write Torznab response: %v", writeErr)
				}
			}))
			t.Cleanup(server.Close)

			instance := &models.Instance{ID: instanceID, Name: "main"}
			source := qbt.Torrent{
				Hash:     sourceHash,
				Name:     sourceName,
				Size:     123,
				Progress: 1,
			}
			service := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
				jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
					{
						ID:             1,
						Name:           "Tracker One",
						BaseURL:        server.URL,
						Backend:        models.TorznabBackendNative,
						TimeoutSeconds: 5,
						Enabled:        true,
					},
				}),
				syncManager: newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
					sourceHash: {{Name: sourceName + ".mkv", Size: source.Size}},
				}),
				asyncFilteringCache: filterCache,
				releaseCache:        NewReleaseCache(),
				stringNormalizer:    stringutils.NewDefaultNormalizer(),
			}

			response, _, _, err := service.searchTorrentMatches(
				context.Background(),
				instanceID,
				sourceHash,
				TorrentSearchOptions{
					IndexerIDs:  []int{1},
					SkipGazelle: true,
				},
				nil,
			)
			require.NoError(t, err)
			require.Len(t, response.Results, test.wantResults)
			require.Equal(t, lateState.CapabilityIndexers, response.SourceTorrent.AvailableIndexers)
			require.Equal(t, lateState.FilteredIndexers, response.SourceTorrent.FilteredIndexers)
			require.Equal(t, lateState.ExcludedIndexers, response.SourceTorrent.ExcludedIndexers)
			require.Equal(t, lateState.ContentMatches, response.SourceTorrent.ContentMatches)
			require.True(t, response.SourceTorrent.ContentFilteringCompleted)
		})
	}
}

func TestFilterSearchResultsByLateContentFilterDropsExcludedIndexers(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	source := &qbt.Torrent{Hash: "sourcehash", Name: "Source.Movie.2015.1080p.BluRay-GROUP"}
	results := []jackett.SearchResult{
		{Indexer: "Indexer One", IndexerID: 1, Title: "Source.Movie.2015.1080p.BluRay-GROUP"},
		{Indexer: "Indexer Two", IndexerID: 2, Title: "Source.Movie.2015.1080p.BluRay-GROUP"},
		{Indexer: "Indexer Three", IndexerID: 3, Title: "Source.Movie.2015.1080p.BluRay-GROUP"},
	}
	svc := &Service{
		asyncFilteringCache: ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}),
	}
	svc.asyncFilteringCache.Set(asyncFilteringCacheKey(instanceID, source.Hash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		CapabilityIndexers:    []int{1, 2, 3},
		FilteredIndexers:      []int{2},
		ExcludedIndexers: map[int]string{
			1: "already seeded from Tracker One",
			3: "already seeded from Tracker Three",
		},
	}, ttlcache.DefaultTTL)

	filtered, snapshot, dropped := svc.filterSearchResultsByLateContentFilter(instanceID, source, results)
	require.NotNil(t, snapshot)
	require.Equal(t, 2, dropped)
	require.Len(t, filtered, 1)
	require.Equal(t, 2, filtered[0].IndexerID)
}

func TestFilterSearchResultsByLateContentFilterNearMissRegression(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	const releaseTitle = "Example.Release.2015.1080p.BluRay.Remux-GROUP"
	source := &qbt.Torrent{
		Hash: "0123456789abcdef0123456789abcdef01234567",
		Name: releaseTitle + ".mkv",
	}
	results := []jackett.SearchResult{
		{Indexer: "ExcludedIndexerOne", IndexerID: 1, Title: releaseTitle},
		{Indexer: "ExcludedIndexerTwo", IndexerID: 8, Title: releaseTitle},
		{Indexer: "AllowedIndexer", IndexerID: 34, Title: releaseTitle},
	}
	svc := &Service{
		asyncFilteringCache: ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}),
	}
	svc.asyncFilteringCache.Set(asyncFilteringCacheKey(instanceID, source.Hash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		CapabilityIndexers:    []int{1, 8, 34},
		FilteredIndexers:      []int{34},
		ExcludedIndexers: map[int]string{
			1: "already seeded from ExcludedIndexerOne",
			8: "already seeded from ExcludedIndexerTwo",
		},
	}, ttlcache.DefaultTTL)

	filtered, snapshot, dropped := svc.filterSearchResultsByLateContentFilter(instanceID, source, results)
	require.NotNil(t, snapshot)
	require.Equal(t, 2, dropped)
	require.Len(t, filtered, 1)
	require.Equal(t, "AllowedIndexer", filtered[0].Indexer)
	require.Equal(t, 34, filtered[0].IndexerID)
}

func TestApplyTorrentSearchResultsSkipsCachedSelectionWhenInfohashExists(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	sourceHash := strings.Repeat("c", 40)
	duplicateHash := strings.Repeat("d", 40)
	source := qbt.Torrent{Hash: sourceHash, Name: "Source.2007.720p.BluRay-GROUP", Progress: 1}
	existing := qbt.Torrent{Hash: duplicateHash, Name: "Already.Seeded.2007.720p.BluRay-GROUP", Progress: 1, Size: 1024}

	sync := &duplicateFilteringSyncManager{
		torrents: map[int][]qbt.Torrent{
			instanceID: {source, existing},
		},
		existingByHash: map[string]qbt.Torrent{
			duplicateHash: existing,
		},
	}
	var downloadCalled atomic.Bool
	var invokerCalled atomic.Bool
	svc := &Service{
		instanceStore: &duplicateFilteringInstanceStore{
			instances: map[int]*models.Instance{
				instanceID: {ID: instanceID, Name: "Main"},
			},
		},
		syncManager:       sync,
		searchResultCache: ttlcache.New(ttlcache.Options[string, cachedTorrentSearchResults]{}),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled.Store(true)
			return []byte("torrent"), nil
		},
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			invokerCalled.Store(true)
			return &CrossSeedResponse{Success: true}, nil
		},
	}

	cached := TorrentSearchResult{
		Indexer:     "HDBits",
		IndexerID:   303,
		Title:       "Already.Seeded.2007.720p.BluRay-GROUP",
		DownloadURL: "https://example.invalid/duplicate.torrent",
		GUID:        "duplicate-guid",
		InfoHashV1:  duplicateHash,
		Size:        existing.Size,
	}
	svc.cacheSearchResults(instanceID, sourceHash, []TorrentSearchResult{cached})

	resp, err := svc.ApplyTorrentSearchResults(context.Background(), instanceID, sourceHash, &ApplyTorrentSearchRequest{
		Selections: []TorrentSearchSelection{
			{IndexerID: cached.IndexerID, DownloadURL: cached.DownloadURL, GUID: cached.GUID},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.False(t, resp.Results[0].Success)
	require.Equal(t, "Torrent already exists in this instance", resp.Results[0].Error)
	require.Equal(t, existing.Name, resp.Results[0].TorrentName)
	require.Len(t, resp.Results[0].InstanceResults, 1)
	require.Equal(t, "exists", resp.Results[0].InstanceResults[0].Status)
	require.NotNil(t, resp.Results[0].InstanceResults[0].MatchedTorrent)
	require.False(t, downloadCalled.Load())
	require.False(t, invokerCalled.Load())
}

func TestApplyTorrentSearchResultsFailsCachedSelectionWhenRejectedInfohashExists(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	sourceHash := strings.Repeat("c", 40)
	duplicateHash := strings.Repeat("d", 40)
	source := qbt.Torrent{Hash: sourceHash, Name: "Source.2007.720p.BluRay-GROUP", Progress: 1}
	existing := qbt.Torrent{Hash: duplicateHash, Name: "Already.Seeded.2007.720p.BluRay-GROUP", Progress: 1, Size: 2048}

	sync := &duplicateFilteringSyncManager{
		torrents: map[int][]qbt.Torrent{
			instanceID: {source, existing},
		},
		existingByHash: map[string]qbt.Torrent{
			duplicateHash: existing,
		},
	}
	var downloadCalled atomic.Bool
	var invokerCalled atomic.Bool
	svc := &Service{
		instanceStore: &duplicateFilteringInstanceStore{
			instances: map[int]*models.Instance{
				instanceID: {ID: instanceID, Name: "Main"},
			},
		},
		syncManager:         sync,
		asyncFilteringCache: ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}),
		searchResultCache:   ttlcache.New(ttlcache.Options[string, cachedTorrentSearchResults]{}),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled.Store(true)
			return []byte("torrent"), nil
		},
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			invokerCalled.Store(true)
			return &CrossSeedResponse{Success: true}, nil
		},
	}
	svc.asyncFilteringCache.Set(asyncFilteringCacheKey(instanceID, sourceHash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		rejectedContentCandidates: map[string]contentPrefilterRejectedTorrent{
			contentPrefilterRejectedContentKey(303, duplicateHash): {
				Hash:   duplicateHash,
				Name:   existing.Name,
				Reason: "Size mismatch: source 1.00 KB vs existing 2.00 KB",
			},
		},
	}, ttlcache.DefaultTTL)

	cached := TorrentSearchResult{
		Indexer:     "HDBits",
		IndexerID:   303,
		Title:       existing.Name,
		DownloadURL: "https://example.invalid/duplicate.torrent",
		GUID:        "duplicate-guid",
		InfoHashV1:  duplicateHash,
		Size:        existing.Size,
	}
	svc.cacheSearchResults(instanceID, sourceHash, []TorrentSearchResult{cached})

	resp, err := svc.ApplyTorrentSearchResults(context.Background(), instanceID, sourceHash, &ApplyTorrentSearchRequest{
		Selections: []TorrentSearchSelection{
			{IndexerID: cached.IndexerID, DownloadURL: cached.DownloadURL, GUID: cached.GUID},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.False(t, resp.Results[0].Success)
	require.Contains(t, resp.Results[0].Error, "rejected by content prefilter")
	require.Contains(t, resp.Results[0].Error, "Size mismatch")
	require.Equal(t, existing.Name, resp.Results[0].TorrentName)
	require.Len(t, resp.Results[0].InstanceResults, 1)
	require.Equal(t, "size_mismatch", resp.Results[0].InstanceResults[0].Status)
	require.NotNil(t, resp.Results[0].InstanceResults[0].MatchedTorrent)
	require.False(t, downloadCalled.Load())
	require.False(t, invokerCalled.Load())
}

func TestApplyTorrentSearchResultsSkipsCachedSelectionWhenRejectedInfohashExistsForDifferentIndexer(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	sourceHash := strings.Repeat("c", 40)
	duplicateHash := strings.Repeat("d", 40)
	source := qbt.Torrent{Hash: sourceHash, Name: "Source.2007.720p.BluRay-GROUP", Progress: 1}
	existing := qbt.Torrent{Hash: duplicateHash, Name: "Already.Seeded.2007.720p.BluRay-GROUP", Progress: 1, Size: 2048}

	sync := &duplicateFilteringSyncManager{
		torrents: map[int][]qbt.Torrent{
			instanceID: {source, existing},
		},
		existingByHash: map[string]qbt.Torrent{
			duplicateHash: existing,
		},
	}
	var downloadCalled atomic.Bool
	var invokerCalled atomic.Bool
	svc := &Service{
		instanceStore: &duplicateFilteringInstanceStore{
			instances: map[int]*models.Instance{
				instanceID: {ID: instanceID, Name: "Main"},
			},
		},
		syncManager:         sync,
		asyncFilteringCache: ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}),
		searchResultCache:   ttlcache.New(ttlcache.Options[string, cachedTorrentSearchResults]{}),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled.Store(true)
			return []byte("torrent"), nil
		},
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			invokerCalled.Store(true)
			return &CrossSeedResponse{Success: true}, nil
		},
	}
	svc.asyncFilteringCache.Set(asyncFilteringCacheKey(instanceID, sourceHash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		rejectedContentCandidates: map[string]contentPrefilterRejectedTorrent{
			contentPrefilterRejectedContentKey(404, duplicateHash): {
				Hash:   duplicateHash,
				Name:   existing.Name,
				Reason: "Size mismatch: source 1.00 KB vs existing 2.00 KB",
			},
		},
	}, ttlcache.DefaultTTL)

	cached := TorrentSearchResult{
		Indexer:     "HDBits",
		IndexerID:   303,
		Title:       existing.Name,
		DownloadURL: "https://example.invalid/duplicate.torrent",
		GUID:        "duplicate-guid",
		InfoHashV1:  duplicateHash,
		Size:        existing.Size,
	}
	svc.cacheSearchResults(instanceID, sourceHash, []TorrentSearchResult{cached})

	resp, err := svc.ApplyTorrentSearchResults(context.Background(), instanceID, sourceHash, &ApplyTorrentSearchRequest{
		Selections: []TorrentSearchSelection{
			{IndexerID: cached.IndexerID, DownloadURL: cached.DownloadURL, GUID: cached.GUID},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.False(t, resp.Results[0].Success)
	require.Equal(t, "Torrent already exists in this instance", resp.Results[0].Error)
	require.Len(t, resp.Results[0].InstanceResults, 1)
	require.Equal(t, "exists", resp.Results[0].InstanceResults[0].Status)
	require.False(t, downloadCalled.Load())
	require.False(t, invokerCalled.Load())
}

func TestExecuteCrossSeedSearchAttemptFailsExistingRejectedByPrefilterWithoutPersistedRun(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	sourceHash := strings.Repeat("a", 40)
	duplicateHash := strings.Repeat("b", 40)
	source := qbt.Torrent{Hash: sourceHash, Name: "Source.2007.720p.BluRay-GROUP", Progress: 1}
	existingName := "Already.Seeded.2007.720p.BluRay-GROUP"

	var downloadCalled atomic.Bool
	var invokerCalled atomic.Bool
	svc := &Service{
		asyncFilteringCache: ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled.Store(true)
			return []byte("torrent"), nil
		},
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			invokerCalled.Store(true)
			return &CrossSeedResponse{
				Success: false,
				TorrentInfo: &TorrentInfo{
					Hash: duplicateHash,
					Name: existingName,
				},
				Results: []InstanceCrossSeedResult{{
					InstanceID: instanceID,
					Success:    false,
					Status:     "exists",
					Message:    "Torrent already exists in this instance",
					MatchedTorrent: &MatchedTorrent{
						Hash: duplicateHash,
						Name: existingName,
					},
				}},
			}, nil
		},
	}
	svc.asyncFilteringCache.Set(asyncFilteringCacheKey(instanceID, sourceHash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		rejectedContentCandidates: map[string]contentPrefilterRejectedTorrent{
			contentPrefilterRejectedContentKey(303, duplicateHash): {
				Hash:   duplicateHash,
				Name:   existingName,
				Reason: "Size mismatch: source 1.00 KB vs existing 2.00 KB",
			},
		},
	}, ttlcache.DefaultTTL)

	state := &searchRunState{
		opts: SearchRunOptions{
			InstanceID: instanceID,
		},
	}
	result, err := svc.executeCrossSeedSearchAttempt(context.Background(), state, &source, TorrentSearchResult{
		Indexer:     "HDBits",
		IndexerID:   303,
		Title:       existingName,
		DownloadURL: "https://example.invalid/duplicate.torrent",
	}, time.Now().UTC())

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, models.CrossSeedSearchResultStatusFailed, result.Status)
	require.Contains(t, result.Message, "rejected by content prefilter")
	require.Contains(t, result.Message, "Size mismatch")
	require.True(t, downloadCalled.Load())
	require.True(t, invokerCalled.Load())
}

func TestExecuteCrossSeedSearchAttemptFailsKnownRejectedInfohashWithoutDownloading(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	sourceHash := strings.Repeat("a", 40)
	duplicateHash := strings.Repeat("b", 40)
	source := qbt.Torrent{Hash: sourceHash, Name: "Source.2007.720p.BluRay-GROUP", Progress: 1}
	existingName := "Already.Seeded.2007.720p.BluRay-GROUP"

	var downloadCalled atomic.Bool
	var invokerCalled atomic.Bool
	svc := &Service{
		asyncFilteringCache: ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled.Store(true)
			return []byte("torrent"), nil
		},
		crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
			invokerCalled.Store(true)
			return &CrossSeedResponse{Success: true}, nil
		},
	}
	svc.asyncFilteringCache.Set(asyncFilteringCacheKey(instanceID, sourceHash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		rejectedContentCandidates: map[string]contentPrefilterRejectedTorrent{
			contentPrefilterRejectedContentKey(303, duplicateHash): {
				Hash:   duplicateHash,
				Name:   existingName,
				Reason: "Size mismatch: source 1.00 KB vs existing 2.00 KB",
			},
		},
	}, ttlcache.DefaultTTL)

	state := &searchRunState{
		opts: SearchRunOptions{
			InstanceID: instanceID,
		},
	}
	result, err := svc.executeCrossSeedSearchAttempt(context.Background(), state, &source, TorrentSearchResult{
		Indexer:     "HDBits",
		IndexerID:   303,
		Title:       existingName,
		DownloadURL: "https://example.invalid/duplicate.torrent",
		InfoHashV1:  duplicateHash,
	}, time.Now().UTC())

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, models.CrossSeedSearchResultStatusFailed, result.Status)
	require.Contains(t, result.Message, "rejected by content prefilter")
	require.Contains(t, result.Message, "Size mismatch")
	require.False(t, downloadCalled.Load())
	require.False(t, invokerCalled.Load())
}

func TestApplyTorrentSearchResultsPropagatesCachedDuplicateContextError(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	sourceHash := strings.Repeat("f", 40)
	source := qbt.Torrent{Hash: sourceHash, Name: "Source.2007.720p.BluRay-GROUP", Progress: 1}

	sync := &duplicateFilteringSyncManager{
		torrents: map[int][]qbt.Torrent{
			instanceID: {source},
		},
		hashErr: context.DeadlineExceeded,
	}
	var downloadCalled atomic.Bool
	svc := &Service{
		syncManager:       sync,
		searchResultCache: ttlcache.New(ttlcache.Options[string, cachedTorrentSearchResults]{}),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled.Store(true)
			return []byte("torrent"), nil
		},
	}

	cached := TorrentSearchResult{
		Indexer:     "HDBits",
		IndexerID:   303,
		Title:       "Already.Seeded.2007.720p.BluRay-GROUP",
		DownloadURL: "https://example.invalid/duplicate.torrent",
		GUID:        "duplicate-guid",
		InfoHashV1:  strings.Repeat("0", 40),
	}
	svc.cacheSearchResults(instanceID, sourceHash, []TorrentSearchResult{cached})

	resp, err := svc.ApplyTorrentSearchResults(context.Background(), instanceID, sourceHash, &ApplyTorrentSearchRequest{
		Selections: []TorrentSearchSelection{
			{IndexerID: cached.IndexerID, DownloadURL: cached.DownloadURL, GUID: cached.GUID},
		},
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, resp)
	require.False(t, downloadCalled.Load())
}

type duplicateFilteringInstanceStore struct {
	instances map[int]*models.Instance
}

func (s *duplicateFilteringInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	instance, ok := s.instances[id]
	if !ok {
		return nil, models.ErrInstanceNotFound
	}
	copied := *instance
	return &copied, nil
}

func (s *duplicateFilteringInstanceStore) List(context.Context) ([]*models.Instance, error) {
	instances := make([]*models.Instance, 0, len(s.instances))
	for _, instance := range s.instances {
		copied := *instance
		instances = append(instances, &copied)
	}
	return instances, nil
}

type duplicateFilteringSyncManager struct {
	torrents       map[int][]qbt.Torrent
	files          map[string]qbt.TorrentFiles
	existingByHash map[string]qbt.Torrent
	hashErr        error
}

func newDuplicateFilteringContentFilterService(sync *duplicateFilteringSyncManager) *Service {
	return &Service{
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		domainMappings:   initializeDomainMappings(),
		stringNormalizer: stringutils.DefaultNormalizer,
	}
}

func (m *duplicateFilteringSyncManager) GetTorrents(_ context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	torrents := m.torrents[instanceID]
	if torrents == nil {
		return nil, fmt.Errorf("instance %d not found", instanceID)
	}
	if len(filter.Hashes) == 0 {
		return append([]qbt.Torrent(nil), torrents...), nil
	}

	targets := make(map[string]struct{}, len(filter.Hashes))
	for _, hash := range filter.Hashes {
		if normalized := normalizeHash(hash); normalized != "" {
			targets[normalized] = struct{}{}
		}
	}

	filtered := make([]qbt.Torrent, 0, len(torrents))
	for _, torrent := range torrents {
		if _, ok := targets[normalizeHash(torrent.Hash)]; ok {
			filtered = append(filtered, torrent)
			continue
		}
		if _, ok := targets[normalizeHash(torrent.InfohashV1)]; ok {
			filtered = append(filtered, torrent)
			continue
		}
		if _, ok := targets[normalizeHash(torrent.InfohashV2)]; ok {
			filtered = append(filtered, torrent)
		}
	}
	return filtered, nil
}

func (m *duplicateFilteringSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		normalized := normalizeHash(hash)
		files, ok := m.files[normalized]
		if !ok {
			continue
		}
		copied := make(qbt.TorrentFiles, len(files))
		copy(copied, files)
		result[normalized] = copied
	}
	return result, nil
}

func (*duplicateFilteringSyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", errors.New("not implemented")
}

func (m *duplicateFilteringSyncManager) HasTorrentByAnyHash(_ context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
	if m.hashErr != nil {
		return nil, false, m.hashErr
	}

	for _, hash := range hashes {
		normalized := normalizeHash(hash)
		if torrent, ok := m.existingByHash[normalized]; ok {
			copied := torrent
			return &copied, true, nil
		}
	}

	for i := range m.torrents[instanceID] {
		torrent := m.torrents[instanceID][i]
		for _, hash := range hashes {
			normalized := normalizeHash(hash)
			if normalized == "" {
				continue
			}
			if normalized == normalizeHash(torrent.Hash) ||
				normalized == normalizeHash(torrent.InfohashV1) ||
				normalized == normalizeHash(torrent.InfohashV2) {
				copied := torrent
				return &copied, true, nil
			}
		}
	}
	return nil, false, nil
}

func (*duplicateFilteringSyncManager) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
}

func (*duplicateFilteringSyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
}

func (*duplicateFilteringSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) (*qbt.TorrentAddResponse, error) {
	return nil, errors.New("not implemented")
}

func (*duplicateFilteringSyncManager) BulkAction(context.Context, int, []string, string) error {
	return errors.New("not implemented")
}

func (m *duplicateFilteringSyncManager) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]internalqb.CrossInstanceTorrentView, error) {
	torrents := m.torrents[instanceID]
	if torrents == nil {
		return nil, fmt.Errorf("instance %d not found", instanceID)
	}
	views := make([]internalqb.CrossInstanceTorrentView, len(torrents))
	for i := range torrents {
		torrent := &torrents[i]
		views[i] = internalqb.CrossInstanceTorrentView{
			TorrentView:  &internalqb.TorrentView{Torrent: torrent},
			InstanceID:   instanceID,
			InstanceName: "Main",
		}
	}
	return views, nil
}

func (*duplicateFilteringSyncManager) ExtractDomainFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parsed.Hostname()))
}

func (*duplicateFilteringSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, errors.New("not implemented")
}

func (*duplicateFilteringSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (*duplicateFilteringSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (*duplicateFilteringSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (*duplicateFilteringSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (*duplicateFilteringSyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (*duplicateFilteringSyncManager) CreateCategory(context.Context, int, string, string) error {
	return nil
}
