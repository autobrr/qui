// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
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
)

func TestFilterIndexersByExistingContentUsesConfiguredIndexerDomains(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	source := qbt.Torrent{
		Hash:     "sourcehash",
		Name:     "Mr.Beans.Holiday.2007.720p.BluRay.DTS.x264-CRiSC",
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
	}
	svc := &Service{
		syncManager:    sync,
		releaseCache:   NewReleaseCache(),
		domainMappings: initializeDomainMappings(),
	}

	indexerIDs := []int{101, 202, 303, 404}
	indexerInfo := map[int]jackett.EnabledIndexerInfo{
		101: {ID: 101, Name: "Upload API", Domain: "upload.cx"},
		202: {ID: 202, Name: "MoreThanTV", Domain: "morethantv.me"},
		303: {ID: 303, Name: "HDBits", Domain: "hdbits.org"},
		404: {ID: 404, Name: "Other", Domain: "other.example"},
	}

	filtered, excluded, contentMatches, err := svc.filterIndexersByExistingContent(context.Background(), instanceID, source.Hash, indexerIDs, indexerInfo)
	require.NoError(t, err)
	require.ElementsMatch(t, []int{404}, filtered)
	require.Contains(t, excluded, 101)
	require.Contains(t, excluded, 202)
	require.Contains(t, excluded, 303)
	require.NotContains(t, excluded, 404)
	require.NotEmpty(t, contentMatches)
}

func TestBuildTorrentSearchResultsFiltersExistingInfohashes(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	duplicateHash := strings.Repeat("a", 40)
	uniqueHash := strings.Repeat("b", 40)
	svc := &Service{
		syncManager: &duplicateFilteringSyncManager{
			existingByHash: map[string]qbt.Torrent{
				duplicateHash: {Hash: duplicateHash, Name: "Already.Seeded.2007.720p.BluRay-GROUP", Progress: 1},
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

	results, duplicateFiltered, err := svc.buildTorrentSearchResults(context.Background(), instanceID, scored, 10)

	require.NoError(t, err)
	require.Equal(t, 1, duplicateFiltered)
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
				Indexer:    "HDBits",
				IndexerID:  303,
				Title:      "Already.Seeded.2007.720p.BluRay-GROUP",
				InfoHashV1: strings.Repeat("e", 40),
			},
			score:  1,
			reason: "exact",
		},
	}

	results, duplicateFiltered, err := svc.buildTorrentSearchResults(context.Background(), 1, scored, 10)

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, results)
	require.Zero(t, duplicateFiltered)
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
	svc.cacheSearchResults(instanceID, sourceHash, []TorrentSearchResult{cached}, 5)

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
	svc.cacheSearchResults(instanceID, sourceHash, []TorrentSearchResult{cached}, 5)

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
	existingByHash map[string]qbt.Torrent
	hashErr        error
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

func (*duplicateFilteringSyncManager) GetTorrentFilesBatch(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
	return map[string]qbt.TorrentFiles{}, nil
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
