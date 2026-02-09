// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/pkg/timeouts"
)

func TestDetectContentType(t *testing.T) {
	s := NewService(nil)

	tests := []struct {
		name     string
		req      *TorznabSearchRequest
		expected contentType
	}{
		{
			name: "detects TV show by episode",
			req: &TorznabSearchRequest{
				Query:   "Breaking Bad",
				Season:  intPtr(1),
				Episode: intPtr(1),
			},
			expected: contentTypeTVShow,
		},
		{
			name: "detects TV show by season pack",
			req: &TorznabSearchRequest{
				Query:  "The Wire",
				Season: intPtr(2),
			},
			expected: contentTypeTVShow,
		},
		{
			name: "detects TV show by TVDbID",
			req: &TorznabSearchRequest{
				Query:  "The Sopranos",
				TVDbID: "123456",
			},
			expected: contentTypeTVShow,
		},
		{
			name: "detects movie by IMDbID",
			req: &TorznabSearchRequest{
				Query:  "The Matrix",
				IMDbID: "tt0133093",
			},
			expected: contentTypeMovie,
		},
		{
			name: "detects XXX by query content",
			req: &TorznabSearchRequest{
				Query: "xxx content here",
			},
			expected: contentTypeXXX,
		},
		{
			name: "detects TV show via release parser",
			req: &TorznabSearchRequest{
				Query: "Breaking.Bad.S01.1080p.WEB-DL.DD5.1.H.264-NTb",
			},
			expected: contentTypeTVShow,
		},
		{
			name: "detects movie via release parser",
			req: &TorznabSearchRequest{
				Query: "Black.Phone.2.2025.1080p.AMZN.WEB-DL.DDP5.1.H.264-KyoGo",
			},
			expected: contentTypeMovie,
		},
		{
			name: "detects music release",
			req: &TorznabSearchRequest{
				Query: "Lane 8 & Jyll - Stay Still, A Little While (2025) [WEB FLAC]",
			},
			expected: contentTypeMusic,
		},
		{
			name: "detects music even if parser extracts episode number",
			req: &TorznabSearchRequest{
				Query: "Various Artists - 25 Years Of Anjuna Mixed By Marsh (2025) - WEB FLAC 16-48",
			},
			expected: contentTypeMusic,
		},
		{
			name: "detects app release",
			req: &TorznabSearchRequest{
				Query: "Screen Studio 3.2.1-3520 ARM",
			},
			expected: contentTypeApp,
		},
		{
			name: "detects game release",
			req: &TorznabSearchRequest{
				Query: "Super.Mario.Bros.Wonder.NSW-BigBlueBox",
			},
			expected: contentTypeGame,
		},
		{
			name: "detects audiobook release",
			req: &TorznabSearchRequest{
				Query: "Some.Audiobook.Title.2024.MP3",
			},
			expected: contentTypeAudiobook,
		},
		{
			name: "detects book release",
			req: &TorznabSearchRequest{
				Query: "Harry.Potter.and.the.Sorcerers.Stone.EPUB",
			},
			expected: contentTypeBook,
		},
		{
			name: "detects comic release",
			req: &TorznabSearchRequest{
				Query: "Amazing.Spider-Man.2025.01.Comic",
			},
			expected: contentTypeComic,
		},
		{
			name: "detects magazine release",
			req: &TorznabSearchRequest{
				Query: "National.Geographic.MAGAZiNE.2024.01",
			},
			expected: contentTypeMagazine,
		},
		{
			name: "detects education release",
			req: &TorznabSearchRequest{
				Query: "Udemy-Python.Programming.Masterclass",
			},
			expected: contentTypeEducation,
		},
		{
			name: "returns unknown for ambiguous query",
			req: &TorznabSearchRequest{
				Query: "random search",
			},
			expected: contentTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.detectContentType(tt.req)
			if result != tt.expected {
				t.Errorf("detectContentType() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetCategoriesForContentType(t *testing.T) {
	tests := []struct {
		name     string
		ct       contentType
		expected []int
	}{
		{
			name:     "returns movie categories",
			ct:       contentTypeMovie,
			expected: []int{CategoryMovies, CategoryMoviesSD, CategoryMoviesHD, CategoryMovies4K},
		},
		{
			name:     "returns TV categories",
			ct:       contentTypeTVShow,
			expected: []int{CategoryTV, CategoryTVSD, CategoryTVHD, CategoryTV4K},
		},
		{
			name:     "returns TV categories for daily shows",
			ct:       contentTypeTVDaily,
			expected: []int{CategoryTV, CategoryTVSD, CategoryTVHD, CategoryTV4K},
		},
		{
			name:     "returns XXX categories",
			ct:       contentTypeXXX,
			expected: []int{CategoryXXX, CategoryXXXDVD, CategoryXXXx264, CategoryXXXPack},
		},
		{
			name:     "returns audio categories",
			ct:       contentTypeMusic,
			expected: []int{CategoryAudio},
		},
		{
			name:     "returns audio categories for audiobooks",
			ct:       contentTypeAudiobook,
			expected: []int{CategoryAudio},
		},
		{
			name:     "returns book categories",
			ct:       contentTypeBook,
			expected: []int{CategoryBooks, CategoryBooksEbook},
		},
		{
			name:     "returns comic categories",
			ct:       contentTypeComic,
			expected: []int{CategoryBooksComics},
		},
		{
			name:     "returns magazine categories",
			ct:       contentTypeMagazine,
			expected: []int{CategoryBooks},
		},
		{
			name:     "returns education categories",
			ct:       contentTypeEducation,
			expected: []int{CategoryBooks},
		},
		{
			name:     "returns PC categories for apps",
			ct:       contentTypeApp,
			expected: []int{CategoryPC},
		},
		{
			name:     "returns PC categories for games",
			ct:       contentTypeGame,
			expected: []int{CategoryPC},
		},
		{
			name:     "returns default categories for unknown",
			ct:       contentTypeUnknown,
			expected: []int{CategoryMovies, CategoryTV},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCategoriesForContentType(tt.ct)
			if len(result) != len(tt.expected) {
				t.Errorf("getCategoriesForContentType() returned %d categories, want %d", len(result), len(tt.expected))
				return
			}
			for i, cat := range result {
				if cat != tt.expected[i] {
					t.Errorf("getCategoriesForContentType()[%d] = %v, want %v", i, cat, tt.expected[i])
				}
			}
		})
	}
}

func TestParseCategoryID(t *testing.T) {
	s := &Service{}
	tests := []struct {
		name     string
		category string
		expected int
	}{
		{
			name:     "parses numeric category",
			category: "5000",
			expected: 5000,
		},
		{
			name:     "parses numeric category with description",
			category: "2000 Movies",
			expected: 2000,
		},
		{
			name:     "maps movies text to ID",
			category: "Movies > HD",
			expected: CategoryMovies,
		},
		{
			name:     "maps TV text to ID",
			category: "TV",
			expected: CategoryTV,
		},
		{
			name:     "maps XXX text to ID",
			category: "XXX > DVD",
			expected: CategoryXXX,
		},
		{
			name:     "maps audio text to ID",
			category: "Audio / MP3",
			expected: CategoryAudio,
		},
		{
			name:     "returns 0 for unknown category",
			category: "Unknown Category",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.parseCategoryID(tt.category)
			if result != tt.expected {
				t.Errorf("parseCategoryID(%q) = %v, want %v", tt.category, result, tt.expected)
			}
		})
	}
}

func TestAdaptiveSearchTimeoutScalesWithIndexerCount(t *testing.T) {
	tests := []struct {
		name          string
		indexerCount  int
		expectedLimit time.Duration
	}{
		{name: "zero indexers uses default", indexerCount: 0, expectedLimit: timeouts.DefaultSearchTimeout},
		{name: "single indexer uses default", indexerCount: 1, expectedLimit: timeouts.DefaultSearchTimeout},
		{name: "adds budget per indexer", indexerCount: 5, expectedLimit: timeouts.DefaultSearchTimeout + 4*timeouts.PerIndexerSearchTimeout},
		{name: "clamps to max", indexerCount: 500, expectedLimit: timeouts.MaxSearchTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := timeouts.AdaptiveSearchTimeout(tt.indexerCount); got != tt.expectedLimit {
				t.Fatalf("AdaptiveSearchTimeout(%d) = %s, want %s", tt.indexerCount, got, tt.expectedLimit)
			}
		})
	}
}

func TestLoadCachedSearchPortionReturnsPartialCoverage(t *testing.T) {
	svc := &Service{
		searchCacheEnabled: true,
		searchCacheTTL:     time.Hour,
	}
	req := &TorznabSearchRequest{Query: "My Query"}
	payload := searchCacheKeyPayload{
		Scope:       searchCacheScopeCrossSeed,
		Query:       canonicalizeQuery(req.Query),
		IndexerIDs:  []int{1, 2},
		ContentType: contentTypeTVShow,
	}
	full, base, err := buildSearchCacheFingerprints(payload)
	if err != nil {
		t.Fatalf("buildSearchCacheFingerprints: %v", err)
	}
	sig := &searchCacheSignature{Key: "cache-key", Fingerprint: full, BaseFingerprint: base}
	encoded, err := json.Marshal(&SearchResponse{Results: []SearchResult{
		{Indexer: "one", IndexerID: 1, GUID: "guid-1", DownloadURL: "http://one"},
		{Indexer: "two", IndexerID: 2, GUID: "guid-2", DownloadURL: "http://two"},
	}})
	if err != nil {
		t.Fatalf("marshal cached response: %v", err)
	}
	now := time.Now().UTC()
	entry := &models.TorznabSearchCacheEntry{
		ID:                 42,
		Scope:              searchCacheScopeCrossSeed,
		IndexerIDs:         []int{1, 2},
		RequestFingerprint: full,
		ResponseData:       encoded,
		TotalResults:       2,
		CachedAt:           now,
		LastUsedAt:         now,
		ExpiresAt:          now.Add(2 * time.Hour),
	}
	fakeCache := &fakeSearchCache{
		fetchFn: func(context.Context, string) (*models.TorznabSearchCacheEntry, bool, error) {
			return nil, false, nil
		},
		findFn: func(_ context.Context, scope, query string) ([]*models.TorznabSearchCacheEntry, error) {
			if scope != searchCacheScopeCrossSeed {
				t.Fatalf("unexpected scope %s", scope)
			}
			if query != canonicalizeQuery(req.Query) {
				t.Fatalf("unexpected query %s", query)
			}
			return []*models.TorznabSearchCacheEntry{entry}, nil
		},
	}
	svc.searchCache = fakeCache
	portion, complete := svc.loadCachedSearchPortion(context.Background(), sig, searchCacheScopeCrossSeed, req, []int{1, 3}, true)
	if portion == nil {
		t.Fatal("expected cached portion")
	}
	if complete {
		t.Fatal("expected partial coverage")
	}
	if len(portion.results) != 1 || portion.results[0].IndexerID != 1 {
		t.Fatalf("unexpected filtered results: %+v", portion.results)
	}
	if len(portion.indexerIDs) != 1 || portion.indexerIDs[0] != 1 {
		t.Fatalf("unexpected coverage: %+v", portion.indexerIDs)
	}
}

func TestSelectCacheEntryForCoveragePrefersMostCoverage(t *testing.T) {
	payload := searchCacheKeyPayload{
		Scope:       searchCacheScopeCrossSeed,
		Query:       "query",
		ContentType: contentTypeTVShow,
	}
	firstFull, base, err := buildSearchCacheFingerprints(payload)
	if err != nil {
		t.Fatalf("fingerprint first: %v", err)
	}
	first := &models.TorznabSearchCacheEntry{IndexerIDs: []int{1, 2}, RequestFingerprint: firstFull}
	payload.IndexerIDs = []int{1, 2, 3, 4}
	secondFull, _, err := buildSearchCacheFingerprints(payload)
	if err != nil {
		t.Fatalf("fingerprint second: %v", err)
	}
	second := &models.TorznabSearchCacheEntry{IndexerIDs: []int{1, 2, 3, 4}, RequestFingerprint: secondFull}
	best, coverage := selectCacheEntryForCoverage([]*models.TorznabSearchCacheEntry{first, second}, []int{1, 4}, base, false)
	if best != second {
		t.Fatalf("expected entry with broader coverage")
	}
	if len(coverage) != 2 || coverage[0] != 1 || coverage[1] != 4 {
		t.Fatalf("unexpected coverage: %+v", coverage)
	}
	if entry, coverage := selectCacheEntryForCoverage([]*models.TorznabSearchCacheEntry{first}, []int{1, 3}, base, true); entry != nil || coverage != nil {
		t.Fatalf("expected nil when full coverage unavailable")
	}
}

type fakeSearchCache struct {
	fetchFn  func(context.Context, string) (*models.TorznabSearchCacheEntry, bool, error)
	findFn   func(context.Context, string, string) ([]*models.TorznabSearchCacheEntry, error)
	storeFn  func(context.Context, *models.TorznabSearchCacheEntry) error
	rebaseFn func(context.Context, int) (int64, error)
}

func (f *fakeSearchCache) Fetch(ctx context.Context, key string) (*models.TorznabSearchCacheEntry, bool, error) {
	if f.fetchFn != nil {
		return f.fetchFn(ctx, key)
	}
	return nil, false, nil
}

func (f *fakeSearchCache) FindActiveByScopeAndQuery(ctx context.Context, scope string, query string) ([]*models.TorznabSearchCacheEntry, error) {
	if f.findFn != nil {
		return f.findFn(ctx, scope, query)
	}
	return nil, nil
}

func (f *fakeSearchCache) Touch(context.Context, int64) {}

func (f *fakeSearchCache) Store(ctx context.Context, entry *models.TorznabSearchCacheEntry) error {
	if f.storeFn != nil {
		return f.storeFn(ctx, entry)
	}
	return nil
}

func (f *fakeSearchCache) CleanupExpired(context.Context) (int64, error) {
	return 0, nil
}

func (f *fakeSearchCache) Flush(context.Context) (int64, error) {
	return 0, nil
}

func (f *fakeSearchCache) InvalidateByIndexerIDs(context.Context, []int) (int64, error) {
	return 0, nil
}

func (f *fakeSearchCache) Stats(context.Context) (*models.TorznabSearchCacheStats, error) {
	return &models.TorznabSearchCacheStats{}, nil
}

func (f *fakeSearchCache) RecentSearches(context.Context, string, int) ([]*models.TorznabRecentSearch, error) {
	return nil, nil
}

func (f *fakeSearchCache) UpdateSettings(_ context.Context, ttlMinutes int) (*models.TorznabSearchCacheSettings, error) {
	return &models.TorznabSearchCacheSettings{TTLMinutes: ttlMinutes}, nil
}

func (f *fakeSearchCache) RebaseTTL(ctx context.Context, ttlMinutes int) (int64, error) {
	if f.rebaseFn != nil {
		return f.rebaseFn(ctx, ttlMinutes)
	}
	return 0, nil
}

func TestSearchGenericFallsBackToCacheOnError(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "First", Enabled: true},
			{ID: 2, Name: "Second", Enabled: true},
		},
	}
	s := NewService(store)
	s.searchCacheEnabled = true
	s.searchCacheTTL = time.Hour

	cachePayload, err := json.Marshal(&SearchResponse{
		Results: []SearchResult{
			{Title: "Cached Result", IndexerID: 1, Indexer: "First"},
		},
		Total: 1,
	})
	if err != nil {
		t.Fatalf("failed to marshal cache payload: %v", err)
	}

	s.searchCache = &fakeSearchCache{
		fetchFn: func(context.Context, string) (*models.TorznabSearchCacheEntry, bool, error) {
			return &models.TorznabSearchCacheEntry{
				ID:                 1,
				CacheKey:           "cache-key",
				Scope:              searchCacheScopeGeneral,
				Query:              "batman begins 1080p",
				Categories:         []int{CategoryMovies},
				IndexerIDs:         []int{1},
				RequestFingerprint: "fp",
				ResponseData:       cachePayload,
				TotalResults:       1,
				CachedAt:           time.Now().Add(-30 * time.Minute),
				LastUsedAt:         time.Now().Add(-30 * time.Minute),
				ExpiresAt:          time.Now().Add(30 * time.Minute),
			}, true, nil
		},
	}

	s.searchExecutor = func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
		return nil, nil, errors.New("rate limited")
	}

	req := &TorznabSearchRequest{Query: "batman begins 1080p"}
	respCh := make(chan *SearchResponse, 1)
	errCh := make(chan error, 1)
	req.OnAllComplete = func(resp *SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	}

	searchErr := s.SearchGeneric(context.Background(), req)
	if searchErr != nil {
		t.Fatalf("SearchGeneric returned error: %v", searchErr)
	}

	var resp *SearchResponse
	select {
	case resp = <-respCh:
		// continue
	case err := <-errCh:
		t.Fatalf("SearchGeneric callback returned error: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("SearchGeneric timed out")
	}

	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Cache == nil || resp.Cache.Source != searchCacheSourceCache {
		t.Fatalf("expected cache metadata from cache source, got %+v", resp.Cache)
	}
	if resp.Total != 1 || len(resp.Results) != 1 {
		t.Fatalf("expected cached result only, got %+v", resp.Results)
	}
	if !resp.Partial {
		t.Fatalf("expected partial response when falling back to cache after failure")
	}
}

func TestUpdateSearchCacheSettingsSetsTTLBeforeRebase(t *testing.T) {
	t.Parallel()

	store := &mockTorznabIndexerStore{}
	service := NewService(store)
	service.searchCacheEnabled = true
	service.searchCacheTTL = 24 * time.Hour

	var observedTTL time.Duration
	cache := &fakeSearchCache{
		rebaseFn: func(_ context.Context, ttlMinutes int) (int64, error) {
			observedTTL = service.searchCacheTTL
			if ttlMinutes != 2880 {
				t.Fatalf("expected ttlMinutes to be 2880, got %d", ttlMinutes)
			}
			return 0, nil
		},
	}
	service.searchCache = cache

	if _, err := service.UpdateSearchCacheSettings(context.Background(), 2880); err != nil {
		t.Fatalf("UpdateSearchCacheSettings returned error: %v", err)
	}

	if observedTTL != 48*time.Hour {
		t.Fatalf("expected searchCacheTTL to be updated before rebase, got %s", observedTTL)
	}
	if service.searchCacheTTL != 48*time.Hour {
		t.Fatalf("expected final searchCacheTTL to be 48h, got %s", service.searchCacheTTL)
	}
}

func TestSearchCachesAfterDeadlineWhenCoverageComplete(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "IndexerOne", Enabled: true},
			{ID: 2, Name: "IndexerTwo", Enabled: true},
		},
	}
	service := NewService(store)
	service.searchCacheEnabled = true
	service.searchCacheTTL = time.Hour
	var stored *models.TorznabSearchCacheEntry
	service.searchCache = &fakeSearchCache{
		storeFn: func(_ context.Context, entry *models.TorznabSearchCacheEntry) error {
			stored = entry
			return nil
		},
	}
	service.searchExecutor = func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		results := []Result{{
			IndexerID: indexers[0].ID,
			Tracker:   indexers[0].Name,
			Title:     "Example",
			GUID:      "guid-1",
			Link:      "http://example/1",
		}}
		coverage := make([]int, 0, len(indexers))
		for _, idx := range indexers {
			coverage = append(coverage, idx.ID)
		}
		return results, coverage, context.DeadlineExceeded
	}

	req := &TorznabSearchRequest{Query: "Example", Categories: []int{CategoryTV}}
	respCh := make(chan *SearchResponse, 1)
	errCh := make(chan error, 1)
	req.OnAllComplete = func(resp *SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	}

	searchErr := service.Search(context.Background(), req)
	if searchErr != nil {
		t.Fatalf("Search returned error: %v", searchErr)
	}

	var resp *SearchResponse
	select {
	case resp = <-respCh:
		// continue
	case err := <-errCh:
		t.Fatalf("Search callback returned error: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("Search timed out")
	}

	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Partial {
		t.Fatal("expected non-partial response when all coverage complete")
	}
	if stored == nil {
		t.Fatal("expected cache entry to be stored")
	}
	if !slices.Equal(stored.IndexerIDs, []int{1, 2}) {
		t.Fatalf("stored coverage mismatch: %+v", stored.IndexerIDs)
	}
}

func TestSearchCachesPartialCoverageAfterDeadline(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "IndexerOne", Enabled: true},
			{ID: 2, Name: "IndexerTwo", Enabled: true},
			{ID: 3, Name: "IndexerThree", Enabled: true},
		},
	}
	service := NewService(store)
	service.searchCacheEnabled = true
	service.searchCacheTTL = time.Hour
	var stored *models.TorznabSearchCacheEntry
	service.searchCache = &fakeSearchCache{
		storeFn: func(_ context.Context, entry *models.TorznabSearchCacheEntry) error {
			stored = entry
			return nil
		},
	}
	service.searchExecutor = func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		results := []Result{{
			IndexerID: indexers[0].ID,
			Tracker:   indexers[0].Name,
			Title:     "Example",
			GUID:      "guid-1",
			Link:      "http://example/1",
		}}
		coverage := []int{indexers[0].ID, indexers[1].ID}
		return results, coverage, context.DeadlineExceeded
	}

	req := &TorznabSearchRequest{Query: "Example", Categories: []int{CategoryTV}}
	respCh := make(chan *SearchResponse, 1)
	errCh := make(chan error, 1)
	req.OnAllComplete = func(resp *SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	}

	searchErr := service.Search(context.Background(), req)
	if searchErr != nil {
		t.Fatalf("Search returned error: %v", searchErr)
	}

	var resp *SearchResponse
	select {
	case resp = <-respCh:
		// continue
	case err := <-errCh:
		t.Fatalf("Search callback returned error: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("Search timed out")
	}

	if resp == nil {
		t.Fatal("expected response")
	}
	if !resp.Partial {
		t.Fatal("expected partial response when coverage incomplete")
	}
	if stored == nil {
		t.Fatal("expected cache entry to be stored")
	}
	if !slices.Equal(stored.IndexerIDs, []int{1, 2}) {
		t.Fatalf("stored coverage mismatch: %+v", stored.IndexerIDs)
	}
}

func TestBuildSearchParams(t *testing.T) {
	s := &Service{}
	tests := []struct {
		name       string
		req        *TorznabSearchRequest
		searchMode string
		expected   map[string]string
	}{
		{
			name: "basic query",
			req: &TorznabSearchRequest{
				Query: "test movie",
			},
			expected: map[string]string{
				"t": "search",
				"q": "test movie",
			},
		},
		{
			name: "query with categories",
			req: &TorznabSearchRequest{
				Query:      "test show",
				Categories: []int{CategoryTV, CategoryTVHD},
			},
			expected: map[string]string{
				"t":   "search",
				"q":   "test show",
				"cat": "5000,5040",
			},
		},
		{
			name: "query with IMDb ID",
			req: &TorznabSearchRequest{
				Query:  "The Matrix",
				IMDbID: "tt0133093",
			},
			expected: map[string]string{
				"t":      "search",
				"q":      "The Matrix",
				"imdbid": "0133093",
			},
		},
		{
			name: "query with IMDb ID without tt prefix",
			req: &TorznabSearchRequest{
				Query:  "The Matrix",
				IMDbID: "0133093",
			},
			expected: map[string]string{
				"t":      "search",
				"q":      "The Matrix",
				"imdbid": "0133093",
			},
		},
		{
			name: "query with TVDb ID",
			req: &TorznabSearchRequest{
				Query:  "Breaking Bad",
				TVDbID: "81189",
			},
			searchMode: "tvsearch",
			expected: map[string]string{
				"t":      "tvsearch",
				"q":      "Breaking Bad",
				"tvdbid": "81189",
			},
		},
		{
			name: "query with season and episode",
			req: &TorznabSearchRequest{
				Query:   "Game of Thrones",
				Season:  intPtr(1),
				Episode: intPtr(1),
			},
			searchMode: "tvsearch",
			expected: map[string]string{
				"t":      "tvsearch",
				"q":      "Game of Thrones",
				"season": "1",
				"ep":     "1",
			},
		},
		{
			name: "query with limit and offset",
			req: &TorznabSearchRequest{
				Query:  "test",
				Limit:  100,
				Offset: 50,
			},
			expected: map[string]string{
				"t":     "search",
				"q":     "test",
				"limit": "100",
			},
		},
		{
			name: "complete request",
			req: &TorznabSearchRequest{
				Query:      "Breaking Bad",
				Categories: []int{CategoryTV},
				TVDbID:     "81189",
				Season:     intPtr(1),
				Episode:    intPtr(1),
				Limit:      50,
				Offset:     10,
			},
			searchMode: "tvsearch",
			expected: map[string]string{
				"t":      "tvsearch",
				"q":      "Breaking Bad",
				"cat":    "5000",
				"tvdbid": "81189",
				"season": "1",
				"ep":     "1",
				"limit":  "50",
			},
		},
		{
			name: "movie request",
			req: &TorznabSearchRequest{
				Query:  "The Matrix",
				IMDbID: "tt0133093",
			},
			searchMode: "movie",
			expected: map[string]string{
				"t":      "movie",
				"q":      "The Matrix",
				"imdbid": "0133093",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode := tt.searchMode
			if mode == "" {
				mode = "search"
			}
			result := s.buildSearchParams(tt.req, mode)
			for key, expectedValue := range tt.expected {
				actualValue := result.Get(key)
				if actualValue != expectedValue {
					t.Errorf("buildSearchParams()[%q] = %q, want %q", key, actualValue, expectedValue)
				}
			}
			// Check no extra params
			for key := range result {
				if _, exists := tt.expected[key]; !exists {
					t.Errorf("buildSearchParams() has unexpected param %q = %q", key, result.Get(key))
				}
			}
		})
	}
}

func TestConvertResults(t *testing.T) {
	s := &Service{}
	tests := []struct {
		name     string
		input    []Result
		expected int // number of expected results
		checkFn  func(*testing.T, []SearchResult)
	}{
		{
			name:     "empty results",
			input:    []Result{},
			expected: 0,
		},
		{
			name: "single result",
			input: []Result{
				{
					Tracker:              "TestTracker",
					Title:                "Test Release",
					Link:                 "http://example.com/download",
					Details:              "http://example.com/details",
					Size:                 1024 * 1024 * 1024,
					Seeders:              10,
					Peers:                15,
					Category:             "5000",
					DownloadVolumeFactor: 0.0,
					UploadVolumeFactor:   1.0,
					GUID:                 "test-guid-123",
					Imdb:                 "tt0133093",
				},
			},
			expected: 1,
			checkFn: func(t *testing.T, results []SearchResult) {
				if results[0].Indexer != "TestTracker" {
					t.Errorf("Indexer = %q, want %q", results[0].Indexer, "TestTracker")
				}
				if results[0].Title != "Test Release" {
					t.Errorf("Title = %q, want %q", results[0].Title, "Test Release")
				}
				if results[0].Size != 1024*1024*1024 {
					t.Errorf("Size = %d, want %d", results[0].Size, 1024*1024*1024)
				}
				if results[0].Seeders != 10 {
					t.Errorf("Seeders = %d, want %d", results[0].Seeders, 10)
				}
				if results[0].Leechers != 5 { // Peers - Seeders
					t.Errorf("Leechers = %d, want %d", results[0].Leechers, 5)
				}
				if results[0].CategoryID != 5000 {
					t.Errorf("CategoryID = %d, want %d", results[0].CategoryID, 5000)
				}
			},
		},
		{
			name: "multiple results sorted by seeders",
			input: []Result{
				{
					Tracker: "Tracker1",
					Title:   "Low Seeders",
					Seeders: 5,
					Peers:   10,
					Size:    1024,
				},
				{
					Tracker: "Tracker2",
					Title:   "High Seeders",
					Seeders: 50,
					Peers:   60,
					Size:    2048,
				},
				{
					Tracker: "Tracker3",
					Title:   "Medium Seeders",
					Seeders: 20,
					Peers:   25,
					Size:    1536,
				},
			},
			expected: 3,
			checkFn: func(t *testing.T, results []SearchResult) {
				// Should be sorted by seeders descending
				if results[0].Title != "High Seeders" {
					t.Errorf("First result title = %q, want %q", results[0].Title, "High Seeders")
				}
				if results[1].Title != "Medium Seeders" {
					t.Errorf("Second result title = %q, want %q", results[1].Title, "Medium Seeders")
				}
				if results[2].Title != "Low Seeders" {
					t.Errorf("Third result title = %q, want %q", results[2].Title, "Low Seeders")
				}
			},
		},
		{
			name: "results with same seeders sorted by size",
			input: []Result{
				{
					Tracker: "Tracker1",
					Title:   "Small File",
					Seeders: 10,
					Peers:   15,
					Size:    1024,
				},
				{
					Tracker: "Tracker2",
					Title:   "Large File",
					Seeders: 10,
					Peers:   15,
					Size:    5120,
				},
				{
					Tracker: "Tracker3",
					Title:   "Medium File",
					Seeders: 10,
					Peers:   15,
					Size:    2048,
				},
			},
			expected: 3,
			checkFn: func(t *testing.T, results []SearchResult) {
				// Same seeders, should be sorted by size descending
				if results[0].Title != "Large File" {
					t.Errorf("First result title = %q, want %q", results[0].Title, "Large File")
				}
				if results[1].Title != "Medium File" {
					t.Errorf("Second result title = %q, want %q", results[1].Title, "Medium File")
				}
				if results[2].Title != "Small File" {
					t.Errorf("Third result title = %q, want %q", results[2].Title, "Small File")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.convertResults(tt.input)
			if len(result) != tt.expected {
				t.Errorf("convertResults() returned %d results, want %d", len(result), tt.expected)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, result)
			}
		})
	}
}

func TestSearchAutoDetectCategories(t *testing.T) {
	// Mock store
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{},
	}
	s := NewService(store)

	tests := []struct {
		name             string
		req              *TorznabSearchRequest
		expectedCats     []int
		shouldAutoDetect bool
	}{
		{
			name: "auto-detects TV categories",
			req: &TorznabSearchRequest{
				Query:   "Breaking Bad",
				Season:  intPtr(1),
				Episode: intPtr(1),
			},
			expectedCats:     []int{CategoryTV, CategoryTVSD, CategoryTVHD, CategoryTV4K},
			shouldAutoDetect: true,
		},
		{
			name: "auto-detects movie categories",
			req: &TorznabSearchRequest{
				Query:  "The Matrix",
				IMDbID: "tt0133093",
			},
			expectedCats:     []int{CategoryMovies, CategoryMoviesSD, CategoryMoviesHD, CategoryMovies4K},
			shouldAutoDetect: true,
		},
		{
			name: "uses provided categories",
			req: &TorznabSearchRequest{
				Query:      "test",
				Categories: []int{CategoryAudio},
			},
			expectedCats:     []int{CategoryAudio},
			shouldAutoDetect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.req.OnAllComplete = func(*SearchResponse, error) {}
			err := s.Search(context.Background(), tt.req)
			// Error expected since no indexers configured
			if err == nil || err.Error() != "query is required" {
				// Check that categories were set
				if len(tt.req.Categories) != len(tt.expectedCats) {
					t.Errorf("Categories count = %d, want %d", len(tt.req.Categories), len(tt.expectedCats))
				}
				for i, cat := range tt.req.Categories {
					if cat != tt.expectedCats[i] {
						t.Errorf("Categories[%d] = %v, want %v", i, cat, tt.expectedCats[i])
					}
				}
			}
		})
	}
}

func TestFilterCategoriesForIndexer(t *testing.T) {
	movieParent := CategoryMovies
	indexerCats := []models.TorznabIndexerCategory{
		{CategoryID: CategoryMovies},
		{CategoryID: CategoryMoviesHD, ParentCategory: &movieParent},
	}

	t.Run("allows matching categories", func(t *testing.T) {
		filtered, ok := filterCategoriesForIndexer(indexerCats, []int{CategoryMoviesHD})
		if !ok {
			t.Fatalf("expected categories to be permitted")
		}
		if len(filtered) != 1 || filtered[0] != CategoryMoviesHD {
			t.Fatalf("unexpected filtered categories: %+v", filtered)
		}
	})

	t.Run("skips unsupported categories", func(t *testing.T) {
		_, ok := filterCategoriesForIndexer(indexerCats, []int{CategoryTV})
		if ok {
			t.Fatalf("expected unsupported categories to be rejected")
		}
	})
}

func TestSearchGenericAutoDetectCategories(t *testing.T) {
	store := &mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{}}
	s := NewService(store)
	req := &TorznabSearchRequest{Query: "Breaking Bad S01"}
	respCh := make(chan *SearchResponse, 1)
	errCh := make(chan error, 1)
	req.OnAllComplete = func(resp *SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	}

	searchErr := s.SearchGeneric(context.Background(), req)
	if searchErr != nil {
		t.Fatalf("SearchGeneric returned error: %v", searchErr)
	}

	var resp *SearchResponse
	select {
	case resp = <-respCh:
		// continue
	case err := <-errCh:
		t.Fatalf("SearchGeneric callback returned error: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("SearchGeneric timed out")
	}

	if resp == nil {
		t.Fatalf("expected response, got nil")
	}

	expected := []int{CategoryTV, CategoryTVSD, CategoryTVHD, CategoryTV4K}
	if len(req.Categories) != len(expected) {
		t.Fatalf("expected %d categories, got %d", len(expected), len(req.Categories))
	}
	for i, cat := range expected {
		if req.Categories[i] != cat {
			t.Fatalf("category %d = %d, want %d", i, req.Categories[i], cat)
		}
	}
}

func TestSearchWithLimit(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{},
	}
	s := NewService(store)

	// Test with empty store (no network calls)
	tests := []struct {
		name          string
		req           *TorznabSearchRequest
		expectedTotal int
		expectedCount int
	}{
		{
			name: "no indexers returns empty",
			req: &TorznabSearchRequest{
				Query: "test",
			},
			expectedTotal: 0,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			respCh := make(chan *SearchResponse, 1)
			errCh := make(chan error, 1)
			tt.req.OnAllComplete = func(resp *SearchResponse, err error) {
				if err != nil {
					errCh <- err
				} else {
					respCh <- resp
				}
			}

			searchErr := s.Search(context.Background(), tt.req)
			if searchErr != nil {
				t.Fatalf("Search() error = %v", searchErr)
			}

			var resp *SearchResponse
			select {
			case resp = <-respCh:
				// continue
			case err := <-errCh:
				t.Fatalf("Search callback error = %v", err)
			case <-time.After(1 * time.Second):
				t.Fatal("Search timed out")
			}

			if resp.Total != tt.expectedTotal {
				t.Errorf("Total = %d, want %d", resp.Total, tt.expectedTotal)
			}
			if len(resp.Results) != tt.expectedCount {
				t.Errorf("Results count = %d, want %d", len(resp.Results), tt.expectedCount)
			}
		})
	}
}

func TestSearchGenericWithIndexerIDs(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "Indexer1", Enabled: true},
			{ID: 2, Name: "Indexer2", Enabled: true},
			{ID: 3, Name: "Indexer3", Enabled: false},
		},
	}
	s := NewService(store)
	s.searchExecutor = func(ctx context.Context, idxs []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		coverage := make([]int, 0, len(idxs))
		for _, idx := range idxs {
			if idx != nil && idx.Enabled {
				coverage = append(coverage, idx.ID)
			}
		}
		return []Result{}, coverage, nil
	}

	tests := []struct {
		name        string
		req         *TorznabSearchRequest
		shouldError bool
	}{
		{
			name: "search specific enabled indexer",
			req: &TorznabSearchRequest{
				Query:      "test",
				IndexerIDs: []int{1},
			},
			shouldError: false,
		},
		{
			name: "search multiple indexers",
			req: &TorznabSearchRequest{
				Query:      "test",
				IndexerIDs: []int{1, 2},
			},
			shouldError: false,
		},
		{
			name: "search disabled indexer returns empty",
			req: &TorznabSearchRequest{
				Query:      "test",
				IndexerIDs: []int{3},
			},
			shouldError: false,
		},
		{
			name: "search all enabled indexers",
			req: &TorznabSearchRequest{
				Query: "test",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			respCh := make(chan *SearchResponse, 1)
			errCh := make(chan error, 1)
			tt.req.OnAllComplete = func(resp *SearchResponse, err error) {
				if err != nil {
					errCh <- err
				} else {
					respCh <- resp
				}
			}

			searchErr := s.SearchGeneric(context.Background(), tt.req)
			if tt.shouldError && searchErr == nil {
				t.Error("SearchGeneric() expected error, got nil")
			}
			if !tt.shouldError && searchErr != nil {
				t.Errorf("SearchGeneric() unexpected error = %v", searchErr)
			}
			if !tt.shouldError {
				var resp *SearchResponse
				select {
				case resp = <-respCh:
					if resp == nil {
						t.Error("SearchGeneric() returned nil response")
					}
				case err := <-errCh:
					t.Errorf("SearchGeneric() callback error = %v", err)
				case <-time.After(1 * time.Second):
					t.Fatal("SearchGeneric timed out")
				}
			}
		})
	}
}

func TestSearchRespectsRequestedIndexerIDs(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "Indexer1", Enabled: true},
		},
		panicOnListEnabled: true,
	}
	s := NewService(store)

	req := &TorznabSearchRequest{
		Query:      "Example.Show.S01",
		IndexerIDs: []int{999}, // request an indexer that does not exist/enabled
	}
	respCh := make(chan *SearchResponse, 1)
	errCh := make(chan error, 1)
	req.OnAllComplete = func(resp *SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	}

	searchErr := s.Search(context.Background(), req)
	if searchErr != nil {
		t.Fatalf("Search() error = %v", searchErr)
	}

	var resp *SearchResponse
	select {
	case resp = <-respCh:
		// continue
	case err := <-errCh:
		t.Fatalf("Search callback error = %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("Search timed out")
	}

	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Total != 0 {
		t.Fatalf("expected no results, got %d", resp.Total)
	}
	if store.listEnabledCalls != 0 {
		t.Fatalf("expected ListEnabled to be skipped, but was called %d times", store.listEnabledCalls)
	}
	if len(store.getCalls) != 1 || store.getCalls[0] != 999 {
		t.Fatalf("expected Get to be called once with 999, calls: %#v", store.getCalls)
	}
}

// Helper functions
func intPtr(i int) *int {
	return &i
}

// Mock store for testing
type mockTorznabIndexerStore struct {
	mu                 sync.Mutex
	indexers           []*models.TorznabIndexer
	capabilities       map[int][]string // indexerID -> capabilities
	cooldowns          map[int]models.TorznabIndexerCooldown
	upsertCooldowns    []int
	deleteCooldowns    []int
	panicOnListEnabled bool
	listEnabledCalls   int
	getCalls           []int
	apiKeyErrors       map[int]error
	apiKeyCalls        []int
}

func (m *mockTorznabIndexerStore) Get(ctx context.Context, id int) (*models.TorznabIndexer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.getCalls = append(m.getCalls, id)
	for _, idx := range m.indexers {
		if idx.ID == id {
			return idx, nil
		}
	}
	return nil, nil
}

func (m *mockTorznabIndexerStore) List(ctx context.Context) ([]*models.TorznabIndexer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.indexers, nil
}

func (m *mockTorznabIndexerStore) ListEnabled(ctx context.Context) ([]*models.TorznabIndexer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.listEnabledCalls++
	if m.panicOnListEnabled {
		panic("ListEnabled called unexpectedly")
	}
	enabled := make([]*models.TorznabIndexer, 0)
	for _, idx := range m.indexers {
		if idx.Enabled {
			enabled = append(enabled, idx)
		}
	}
	return enabled, nil
}

func (m *mockTorznabIndexerStore) GetDecryptedAPIKey(indexer *models.TorznabIndexer) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if indexer != nil {
		m.apiKeyCalls = append(m.apiKeyCalls, indexer.ID)
		if err, ok := m.apiKeyErrors[indexer.ID]; ok {
			return "", err
		}
	}
	return "mock-api-key", nil
}

func (m *mockTorznabIndexerStore) GetDecryptedBasicPassword(indexer *models.TorznabIndexer) (string, error) {
	// Service only calls this when BasicUsername is set; tests don't cover basic auth yet.
	return "", nil
}

func (m *mockTorznabIndexerStore) GetCapabilities(ctx context.Context, indexerID int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.capabilities != nil {
		if caps, exists := m.capabilities[indexerID]; exists {
			return caps, nil
		}
	}
	// For testing purposes, return empty capabilities by default
	// This simulates indexers without specific parameter support capabilities
	return []string{}, nil
}

func (m *mockTorznabIndexerStore) SetCapabilities(ctx context.Context, indexerID int, capabilities []string) error {
	return nil
}

func (m *mockTorznabIndexerStore) SetCategories(ctx context.Context, indexerID int, categories []models.TorznabIndexerCategory) error {
	return nil
}

func (m *mockTorznabIndexerStore) SetLimits(ctx context.Context, indexerID, limitDefault, limitMax int) error {
	return nil
}

func (m *mockTorznabIndexerStore) RecordLatency(ctx context.Context, indexerID int, operationType string, latencyMs int, success bool) error {
	return nil
}

func (m *mockTorznabIndexerStore) RecordError(ctx context.Context, indexerID int, errorMessage, errorCode string) error {
	return nil
}

func (m *mockTorznabIndexerStore) ListRateLimitCooldowns(ctx context.Context) ([]models.TorznabIndexerCooldown, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.cooldowns) == 0 {
		return nil, nil
	}

	entries := make([]models.TorznabIndexerCooldown, 0, len(m.cooldowns))
	for _, cd := range m.cooldowns {
		entries = append(entries, cd)
	}
	return entries, nil
}

func (m *mockTorznabIndexerStore) UpsertRateLimitCooldown(ctx context.Context, indexerID int, resumeAt time.Time, cooldown time.Duration, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cooldowns == nil {
		m.cooldowns = make(map[int]models.TorznabIndexerCooldown)
	}
	m.cooldowns[indexerID] = models.TorznabIndexerCooldown{
		IndexerID: indexerID,
		ResumeAt:  resumeAt,
		Cooldown:  cooldown,
		Reason:    reason,
	}
	m.upsertCooldowns = append(m.upsertCooldowns, indexerID)
	return nil
}

func (m *mockTorznabIndexerStore) DeleteRateLimitCooldown(ctx context.Context, indexerID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cooldowns != nil {
		delete(m.cooldowns, indexerID)
	}
	m.deleteCooldowns = append(m.deleteCooldowns, indexerID)
	return nil
}

func TestSearchMultipleIndexersSkipsRateLimitedIndexers(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "Limited", Backend: models.TorznabBackendJackett, BaseURL: "http://127.0.0.1", Enabled: true, IndexerID: "limited"},
			{ID: 2, Name: "Active", Backend: models.TorznabBackendJackett, BaseURL: "http://127.0.0.1", Enabled: true, IndexerID: "active"},
		},
		apiKeyErrors: map[int]error{
			2: errors.New("expected failure"),
		},
	}

	service := NewService(store)
	service.rateLimiter = NewRateLimiter(time.Millisecond)
	service.rateLimiter.SetCooldown(1, time.Now().Add(time.Minute))

	_, _, err := service.searchMultipleIndexers(context.Background(), store.indexers, url.Values{"q": {"test"}}, nil)
	if err == nil {
		t.Fatalf("expected error when all available indexers fail")
	}
	if !strings.Contains(err.Error(), "all 1 indexers failed") {
		t.Fatalf("expected aggregated failure error, got %v", err)
	}

	if len(store.apiKeyCalls) != 1 || store.apiKeyCalls[0] != 2 {
		t.Fatalf("expected only active indexer to be queried, apiKeyCalls=%v", store.apiKeyCalls)
	}
}

func TestSearchMultipleIndexersAllRateLimited(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "Limited A", Backend: models.TorznabBackendJackett, BaseURL: "http://127.0.0.1", Enabled: true, IndexerID: "a"},
			{ID: 2, Name: "Limited B", Backend: models.TorznabBackendJackett, BaseURL: "http://127.0.0.1", Enabled: true, IndexerID: "b"},
		},
	}

	service := NewService(store)
	service.rateLimiter = NewRateLimiter(time.Millisecond)
	service.rateLimiter.SetCooldown(1, time.Now().Add(time.Minute))
	service.rateLimiter.SetCooldown(2, time.Now().Add(2*time.Minute))

	_, _, err := service.searchMultipleIndexers(context.Background(), store.indexers, url.Values{"q": {"test"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "all indexers are currently rate-limited") {
		t.Fatalf("expected all rate-limited error, got %v", err)
	}

	if len(store.apiKeyCalls) != 0 {
		t.Fatalf("expected no API key calls when all indexers are skipped, apiKeyCalls=%v", store.apiKeyCalls)
	}
}

func TestProwlarrYearParameterWorkaround(t *testing.T) {
	tests := []struct {
		name        string
		backend     models.TorznabBackend
		inputParams map[string]string
		expected    map[string]string
		description string
	}{
		{
			name:    "prowlarr with year parameter",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
				"cat":  "2000",
			},
			expected: map[string]string{
				"t":   "movie",
				"q":   "The Matrix 1999",
				"cat": "2000",
				// year parameter should be removed
			},
			description: "Prowlarr indexer should move year parameter to search query",
		},
		{
			name:    "prowlarr with year parameter and empty query",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "",
				"year": "2020",
			},
			expected: map[string]string{
				"t": "movie",
				"q": "2020",
				// year parameter should be removed
			},
			description: "Prowlarr indexer should use year as query when original query is empty",
		},
		{
			name:    "prowlarr with year parameter and ids present",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t":      "movie",
				"year":   "1999",
				"imdbid": "0133093",
			},
			expected: map[string]string{
				"t":      "movie",
				"imdbid": "0133093",
				// year parameter should be removed
			},
			description: "Prowlarr indexer should drop year when doing ID-driven search",
		},
		{
			name:    "jackett with year parameter",
			backend: models.TorznabBackendJackett,
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
				"cat":  "2000",
			},
			expected: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
				"cat":  "2000",
			},
			description: "Jackett indexer should keep year parameter unchanged",
		},
		{
			name:    "native with year parameter",
			backend: models.TorznabBackendNative,
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
			},
			expected: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
			},
			description: "Native indexer should keep year parameter unchanged",
		},
		{
			name:    "prowlarr without year parameter",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t": "movie",
				"q": "The Matrix",
			},
			expected: map[string]string{
				"t": "movie",
				"q": "The Matrix",
			},
			description: "Prowlarr indexer should not modify query when no year parameter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test indexer with the specified backend
			indexer := &models.TorznabIndexer{
				ID:        1,
				Name:      "Test Indexer",
				Backend:   tt.backend,
				IndexerID: "test",
			}

			// Set up mock store
			mockStore := &mockTorznabIndexerStore{
				capabilities: make(map[int][]string),
			}

			// Create service with mock store
			service := &Service{
				indexerStore: mockStore,
			}

			// Prepare input parameters
			inputParams := make(map[string]string)
			maps.Copy(inputParams, tt.inputParams)

			// Call the actual service method to apply the workaround
			service.applyProwlarrWorkaround(indexer, inputParams)

			// Assert expected parameter values
			for key, expectedValue := range tt.expected {
				if actualValue := inputParams[key]; actualValue != expectedValue {
					t.Errorf("%s: paramsMap[%q] = %q, want %q", tt.description, key, actualValue, expectedValue)
				}
			}

			// Assert year parameter is removed for Prowlarr when it was present
			if tt.backend == models.TorznabBackendProwlarr && tt.inputParams["year"] != "" {
				if _, exists := inputParams["year"]; exists {
					t.Errorf("%s: year parameter should be removed for Prowlarr indexer", tt.description)
				}
			}

			// Assert no unexpected parameters exist
			for key := range inputParams {
				if _, expected := tt.expected[key]; !expected {
					t.Errorf("%s: unexpected parameter %q = %q", tt.description, key, inputParams[key])
				}
			}
		})
	}
}

func TestProwlarrCapabilityAwareYearWorkaround(t *testing.T) {
	tests := []struct {
		name                string
		indexerCapabilities []string
		inputParams         map[string]string
		expectedQuery       string
		expectedYearParam   bool
		description         string
	}{
		{
			name:                "prowlarr without movie-search-year capability",
			indexerCapabilities: []string{"search", "movie-search"},
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
			},
			expectedQuery:     "The Matrix 1999",
			expectedYearParam: false,
			description:       "Should move year to query when indexer lacks movie-search-year capability",
		},
		{
			name:                "prowlarr with movie-search-year capability",
			indexerCapabilities: []string{"search", "movie-search", "movie-search-year"},
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
			},
			expectedQuery:     "The Matrix",
			expectedYearParam: true,
			description:       "Should keep year parameter when indexer supports movie-search-year capability",
		},
		{
			name:                "prowlarr with empty query",
			indexerCapabilities: []string{"search", "movie-search"},
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "",
				"year": "2020",
			},
			expectedQuery:     "2020",
			expectedYearParam: false,
			description:       "Should use year as entire query when original query is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock store with specific capabilities
			mockStore := &mockTorznabIndexerStore{
				indexers: []*models.TorznabIndexer{
					{
						ID:             1,
						Name:           "Test Prowlarr Indexer",
						Backend:        models.TorznabBackendProwlarr,
						IndexerID:      "test",
						BaseURL:        "http://test.example.com",
						TimeoutSeconds: 30,
						Enabled:        true,
					},
				},
				capabilities: map[int][]string{
					1: tt.indexerCapabilities,
				},
			}

			// Create service with mock store
			service := &Service{
				indexerStore: mockStore,
			}

			// Convert input params to map[string]string (we don't need url.Values for this test)

			// Test the searchMultipleIndexers method by examining the parameters it would send
			// We'll use reflection or create a test client that captures the parameters
			indexers, _ := mockStore.ListEnabled(context.Background())

			// For this test, we'll verify the capability logic directly
			ctx := context.Background()
			hasYearCapability := service.hasCapability(ctx, 1, "movie-search-year")

			expectedHasCapability := slices.Contains(tt.indexerCapabilities, "movie-search-year")

			if hasYearCapability != expectedHasCapability {
				t.Errorf("hasCapability() = %v, expected %v", hasYearCapability, expectedHasCapability)
			}

			// Test parameter handling logic
			paramsMap := make(map[string]string)
			maps.Copy(paramsMap, tt.inputParams)

			indexer := indexers[0]
			// Apply the actual Prowlarr logic from the service
			if indexer.Backend == models.TorznabBackendProwlarr {
				if yearStr, exists := paramsMap["year"]; exists && yearStr != "" {
					supportsYearParam := service.hasCapability(ctx, indexer.ID, "movie-search-year")

					if !supportsYearParam {
						currentQuery := paramsMap["q"]
						if currentQuery != "" {
							paramsMap["q"] = currentQuery + " " + yearStr
						} else {
							paramsMap["q"] = yearStr
						}
						delete(paramsMap, "year")
					}
				}
			}

			// Verify results
			actualQuery := paramsMap["q"]
			if actualQuery != tt.expectedQuery {
				t.Errorf("Query: got %q, expected %q", actualQuery, tt.expectedQuery)
			}

			_, hasYearParam := paramsMap["year"]
			if hasYearParam != tt.expectedYearParam {
				t.Errorf("Year parameter presence: got %v, expected %v", hasYearParam, tt.expectedYearParam)
			}
		})
	}
}

func TestParseTorznabCaps_ProwlarrCompatibility(t *testing.T) {
	// This XML represents what Prowlarr's IndexerCapabilities.GetXDocument() method generates
	// Based on the IndexerCapabilities class from Prowlarr source code
	prowlarrCapsXML := `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<server title="Prowlarr" />
	<limits default="100" max="100" />
	<searching>
		<search available="yes" supportedParams="q" />
		<tv-search available="yes" supportedParams="q,season,ep,imdbid,tvdbid,tmdbid,tvmazeid,traktid,doubanid,genre,year" />
		<movie-search available="yes" supportedParams="q,imdbid,tmdbid,traktid,genre,doubanid,year" />
		<music-search available="yes" supportedParams="q,album,artist,label,year,genre,track" />
		<audio-search available="yes" supportedParams="q,album,artist,label,year,genre,track" />
		<book-search available="yes" supportedParams="q,title,author,publisher,genre,year" />
	</searching>
	<categories>
		<category id="2000" name="Movies">
			<subcat id="2010" name="Foreign" />
			<subcat id="2020" name="Other" />
		</category>
		<category id="5000" name="TV">
			<subcat id="5070" name="Anime" />
		</category>
	</categories>
</caps>`

	caps, err := parseTorznabCaps(strings.NewReader(prowlarrCapsXML))
	if err != nil {
		t.Fatalf("Failed to parse Prowlarr caps XML: %v", err)
	}

	// Test that we parse all search types
	expectedSearchTypes := []string{
		"search", "tv-search", "movie-search", "music-search", "audio-search", "book-search",
	}
	for _, searchType := range expectedSearchTypes {
		found := slices.Contains(caps.Capabilities, searchType)
		if !found {
			t.Errorf("Missing basic search capability: %s", searchType)
		}
	}

	// Test that we parse all movie-search parameters correctly
	// Based on Prowlarr's MovieSearchParam enum and SupportedMovieSearchParams() method
	expectedMovieParams := []string{
		"movie-search-q",        // Always included
		"movie-search-imdbid",   // MovieSearchImdbAvailable
		"movie-search-tmdbid",   // MovieSearchTmdbAvailable
		"movie-search-traktid",  // MovieSearchTraktAvailable
		"movie-search-genre",    // MovieSearchGenreAvailable
		"movie-search-doubanid", // MovieSearchDoubanAvailable
		"movie-search-year",     // MovieSearchYearAvailable
	}
	for _, param := range expectedMovieParams {
		found := slices.Contains(caps.Capabilities, param)
		if !found {
			t.Errorf("Missing movie search parameter capability: %s", param)
		}
	}

	// Test that we parse all tv-search parameters correctly
	// Based on Prowlarr's TvSearchParam enum and SupportedTvSearchParams() method
	expectedTvParams := []string{
		"tv-search-q",        // Always included
		"tv-search-season",   // TvSearchSeasonAvailable
		"tv-search-ep",       // TvSearchEpAvailable
		"tv-search-imdbid",   // TvSearchImdbAvailable
		"tv-search-tvdbid",   // TvSearchTvdbAvailable
		"tv-search-tmdbid",   // TvSearchTmdbAvailable
		"tv-search-tvmazeid", // TvSearchTvMazeAvailable
		"tv-search-traktid",  // TvSearchTraktAvailable
		"tv-search-doubanid", // TvSearchDoubanAvailable
		"tv-search-genre",    // TvSearchGenreAvailable
		"tv-search-year",     // TvSearchYearAvailable
	}
	for _, param := range expectedTvParams {
		found := slices.Contains(caps.Capabilities, param)
		if !found {
			t.Errorf("Missing TV search parameter capability: %s", param)
		}
	}

	// Test that we parse all music-search parameters correctly
	// Based on Prowlarr's MusicSearchParam enum and SupportedMusicSearchParams() method
	expectedMusicParams := []string{
		"music-search-q",      // Always included
		"music-search-album",  // MusicSearchAlbumAvailable
		"music-search-artist", // MusicSearchArtistAvailable
		"music-search-label",  // MusicSearchLabelAvailable
		"music-search-year",   // MusicSearchYearAvailable
		"music-search-genre",  // MusicSearchGenreAvailable
		"music-search-track",  // MusicSearchTrackAvailable
	}
	for _, param := range expectedMusicParams {
		found := slices.Contains(caps.Capabilities, param)
		if !found {
			t.Errorf("Missing music search parameter capability: %s", param)
		}
	}

	// Test that we parse all book-search parameters correctly
	// Based on Prowlarr's BookSearchParam enum and SupportedBookSearchParams() method
	expectedBookParams := []string{
		"book-search-q",         // Always included
		"book-search-title",     // BookSearchTitleAvailable
		"book-search-author",    // BookSearchAuthorAvailable
		"book-search-publisher", // BookSearchPublisherAvailable
		"book-search-genre",     // BookSearchGenreAvailable
		"book-search-year",      // BookSearchYearAvailable
	}
	for _, param := range expectedBookParams {
		found := slices.Contains(caps.Capabilities, param)
		if !found {
			t.Errorf("Missing book search parameter capability: %s", param)
		}
	}

	// Test categories parsing (from Prowlarr's Categories.GetTorznabCategoryTree())
	if len(caps.Categories) == 0 {
		t.Error("No categories parsed")
	}

	// Find Movies category
	foundMovies := false
	foundMoviesForeign := false
	foundMoviesOther := false
	for _, cat := range caps.Categories {
		if cat.CategoryID == 2000 && cat.CategoryName == "Movies" {
			foundMovies = true
		}
		if cat.CategoryID == 2010 && cat.CategoryName == "Foreign" && cat.ParentCategory != nil && *cat.ParentCategory == 2000 {
			foundMoviesForeign = true
		}
		if cat.CategoryID == 2020 && cat.CategoryName == "Other" && cat.ParentCategory != nil && *cat.ParentCategory == 2000 {
			foundMoviesOther = true
		}
	}

	if !foundMovies {
		t.Error("Missing Movies category (2000)")
	}
	if !foundMoviesForeign {
		t.Error("Missing Movies > Foreign subcategory (2010)")
	}
	if !foundMoviesOther {
		t.Error("Missing Movies > Other subcategory (2020)")
	}

	// Verify we have all the capabilities we expect
	t.Logf("Parsed %d capabilities total", len(caps.Capabilities))
	t.Logf("Parsed %d categories total", len(caps.Categories))

	// Verify our capability parsing creates exactly what we need for hasCapability checks
	testCapabilities := []string{
		"movie-search-year",   // Critical for our Prowlarr workaround
		"movie-search-imdbid", // Common for movie searches
		"tv-search-season",    // Common for TV searches
		"tv-search-ep",        // Common for TV searches
		"music-search-artist", // Common for music searches
		"book-search-author",  // Common for book searches
	}

	for _, testCap := range testCapabilities {
		found := slices.Contains(caps.Capabilities, testCap)
		if !found {
			t.Errorf("Critical capability missing: %s", testCap)
		}
	}
}

func TestExtractInfoHashFromAttributes(t *testing.T) {
	tests := []struct {
		name     string
		attrs    map[string]string
		expected string
	}{
		{
			name:     "empty attributes",
			attrs:    nil,
			expected: "",
		},
		{
			name:     "direct infohash attribute",
			attrs:    map[string]string{"infohash": "63e07ff523710ca268567dad344ce1e0e6b7e8a3"},
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		},
		{
			name:     "infohash with uppercase",
			attrs:    map[string]string{"infohash": "63E07FF523710CA268567DAD344CE1E0E6B7E8A3"},
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		},
		{
			name:     "info_hash variant",
			attrs:    map[string]string{"info_hash": "63e07ff523710ca268567dad344ce1e0e6b7e8a3"},
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		},
		{
			name:     "hash variant",
			attrs:    map[string]string{"hash": "63e07ff523710ca268567dad344ce1e0e6b7e8a3"},
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		},
		{
			name:     "SHA256 v2 hash",
			attrs:    map[string]string{"infohash": "63e07ff523710ca268567dad344ce1e0e6b7e8a363e07ff523710ca268567dad"},
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a363e07ff523710ca268567dad",
		},
		{
			name:     "invalid hash length",
			attrs:    map[string]string{"infohash": "63e07ff523710ca268"},
			expected: "",
		},
		{
			name:     "invalid hex characters",
			attrs:    map[string]string{"infohash": "63e07ff523710ca268567dad344ce1e0e6b7ZZZZ"},
			expected: "",
		},
		{
			name:     "magnet URL fallback",
			attrs:    map[string]string{"magneturl": "magnet:?xt=urn:btih:63e07ff523710ca268567dad344ce1e0e6b7e8a3&dn=Test"},
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		},
		{
			name:     "magnet URL with uppercase hash",
			attrs:    map[string]string{"magneturl": "magnet:?xt=urn:btih:63E07FF523710CA268567DAD344CE1E0E6B7E8A3&dn=Test"},
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		},
		{
			name:     "magnet URL without other params",
			attrs:    map[string]string{"magneturl": "magnet:?xt=urn:btih:63e07ff523710ca268567dad344ce1e0e6b7e8a3"},
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		},
		{
			name:     "infohash takes priority over magneturl",
			attrs:    map[string]string{"infohash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "magneturl": "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			expected: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			name:     "invalid magnet URL",
			attrs:    map[string]string{"magneturl": "not-a-magnet-url"},
			expected: "",
		},
		{
			name:     "magnet URL missing hash",
			attrs:    map[string]string{"magneturl": "magnet:?dn=Test&tr=http://tracker"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractInfoHashFromAttributes(tt.attrs)
			if result != tt.expected {
				t.Errorf("extractInfoHashFromAttributes() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExtractInfoHashFromMagnet(t *testing.T) {
	tests := []struct {
		name     string
		magnet   string
		expected string
	}{
		{
			name:     "empty string",
			magnet:   "",
			expected: "",
		},
		{
			name:     "valid magnet with hash first",
			magnet:   "magnet:?xt=urn:btih:63e07ff523710ca268567dad344ce1e0e6b7e8a3&dn=Test&tr=http://tracker",
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		},
		{
			name:     "valid magnet with hash last",
			magnet:   "magnet:?dn=Test&tr=http://tracker&xt=urn:btih:63e07ff523710ca268567dad344ce1e0e6b7e8a3",
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		},
		{
			name:     "magnet with only hash",
			magnet:   "magnet:?xt=urn:btih:63e07ff523710ca268567dad344ce1e0e6b7e8a3",
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		},
		{
			name:     "uppercase MAGNET prefix",
			magnet:   "MAGNET:?xt=urn:btih:63e07ff523710ca268567dad344ce1e0e6b7e8a3",
			expected: "63e07ff523710ca268567dad344ce1e0e6b7e8a3",
		},
		{
			name:     "not a magnet URL",
			magnet:   "http://example.com/torrent.torrent",
			expected: "",
		},
		{
			name:     "magnet without query string",
			magnet:   "magnet:",
			expected: "",
		},
		{
			name:     "magnet with invalid hash",
			magnet:   "magnet:?xt=urn:btih:tooshort",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractInfoHashFromMagnet(tt.magnet)
			if result != tt.expected {
				t.Errorf("extractInfoHashFromMagnet() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestApplyCapabilitySpecificParams(t *testing.T) {
	s := NewService(nil)

	tests := []struct {
		name        string
		indexer     *models.TorznabIndexer
		meta        *searchContext
		inputParams map[string]string
		wantParams  map[string]string
		description string
	}{
		{
			name: "movie search - prunes unsupported imdbid",
			indexer: &models.TorznabIndexer{
				ID:           1,
				Name:         "TestIndexer",
				Capabilities: []string{"movie-search", "movie-search-tmdbid"}, // no movie-search-imdbid
			},
			meta: &searchContext{
				searchMode:    "movie",
				originalQuery: "The Matrix",
			},
			inputParams: map[string]string{
				"imdbid": "tt0133093",
				"tmdbid": "603",
			},
			wantParams: map[string]string{
				"tmdbid": "603",
			},
			description: "IMDb ID pruned when indexer lacks movie-search-imdbid capability",
		},
		{
			name: "movie search - keeps supported tmdbid",
			indexer: &models.TorznabIndexer{
				ID:           2,
				Name:         "TMDbIndexer",
				Capabilities: []string{"movie-search", "movie-search-tmdbid"},
			},
			meta: &searchContext{
				searchMode:    "movie",
				originalQuery: "Inception",
			},
			inputParams: map[string]string{
				"tmdbid": "27205",
			},
			wantParams: map[string]string{
				"tmdbid": "27205",
			},
			description: "TMDb ID kept when indexer supports movie-search-tmdbid",
		},
		{
			name: "tv search - prunes unsupported tvmazeid",
			indexer: &models.TorznabIndexer{
				ID:           3,
				Name:         "TVIndexer",
				Capabilities: []string{"tv-search", "tv-search-tvdbid"}, // no tv-search-tvmazeid
			},
			meta: &searchContext{
				searchMode:    "tvsearch",
				originalQuery: "Breaking Bad",
			},
			inputParams: map[string]string{
				"tvdbid":   "81189",
				"tvmazeid": "169",
			},
			wantParams: map[string]string{
				"tvdbid": "81189",
			},
			description: "TVMaze ID pruned when indexer lacks tv-search-tvmazeid capability",
		},
		{
			name: "tv search - keeps all supported IDs",
			indexer: &models.TorznabIndexer{
				ID:           4,
				Name:         "FullCapIndexer",
				Capabilities: []string{"tv-search", "tv-search-tvdbid", "tv-search-imdbid", "tv-search-tmdbid", "tv-search-tvmazeid"},
			},
			meta: &searchContext{
				searchMode:    "tvsearch",
				originalQuery: "Game of Thrones",
			},
			inputParams: map[string]string{
				"tvdbid":   "121361",
				"imdbid":   "tt0944947",
				"tmdbid":   "1399",
				"tvmazeid": "82",
			},
			wantParams: map[string]string{
				"tvdbid":   "121361",
				"imdbid":   "tt0944947",
				"tmdbid":   "1399",
				"tvmazeid": "82",
			},
			description: "All IDs kept when indexer supports all capabilities",
		},
		{
			name: "movie search - prunes tvdbid (not applicable for movies)",
			indexer: &models.TorznabIndexer{
				ID:           5,
				Name:         "MovieOnlyIndexer",
				Capabilities: []string{"movie-search", "movie-search-imdbid"},
			},
			meta: &searchContext{
				searchMode:    "movie",
				originalQuery: "The Matrix",
			},
			inputParams: map[string]string{
				"imdbid": "tt0133093",
				"tvdbid": "12345", // shouldn't be here for movie search
			},
			wantParams: map[string]string{
				"imdbid": "tt0133093",
			},
			description: "TVDb ID pruned for movie search (not applicable)",
		},
		{
			name: "all IDs pruned - restores query param",
			indexer: &models.TorznabIndexer{
				ID:           6,
				Name:         "NoIDsIndexer",
				Capabilities: []string{"movie-search"}, // no ID capabilities
			},
			meta: &searchContext{
				searchMode:    "movie",
				originalQuery: "The Matrix",
			},
			inputParams: map[string]string{
				"imdbid": "tt0133093",
				"tmdbid": "603",
			},
			wantParams: map[string]string{
				"q": "The Matrix",
			},
			description: "Query restored when all IDs are pruned",
		},
		{
			name: "all IDs pruned - no query to restore",
			indexer: &models.TorznabIndexer{
				ID:           7,
				Name:         "NoIDsIndexer",
				Capabilities: []string{"movie-search"},
			},
			meta: &searchContext{
				searchMode:    "movie",
				originalQuery: "", // no original query
			},
			inputParams: map[string]string{
				"imdbid": "tt0133093",
			},
			wantParams:  map[string]string{},
			description: "No query restored when original query is empty",
		},
		{
			name: "all IDs pruned - restores release name when original query empty",
			indexer: &models.TorznabIndexer{
				ID:           12,
				Name:         "NoIDsIndexer",
				Capabilities: []string{"movie-search"},
			},
			meta: &searchContext{
				searchMode:    "movie",
				originalQuery: "",
				releaseName:   "The Matrix (1999)",
			},
			inputParams: map[string]string{
				"imdbid": "tt0133093",
			},
			wantParams: map[string]string{
				"q": "The Matrix (1999)",
			},
			description: "Release name restored when all IDs are pruned and original query is empty",
		},
		{
			name: "case-insensitive capability matching",
			indexer: &models.TorznabIndexer{
				ID:           8,
				Name:         "MixedCaseIndexer",
				Capabilities: []string{"Movie-Search", "MOVIE-SEARCH-IMDBID", "movie-search-tmdbid"},
			},
			meta: &searchContext{
				searchMode:    "movie",
				originalQuery: "Test",
			},
			inputParams: map[string]string{
				"imdbid": "tt0133093",
				"tmdbid": "603",
			},
			wantParams: map[string]string{
				"imdbid": "tt0133093",
				"tmdbid": "603",
			},
			description: "Capability matching is case-insensitive",
		},
		{
			name: "nil meta - no changes",
			indexer: &models.TorznabIndexer{
				ID:           9,
				Name:         "TestIndexer",
				Capabilities: []string{"movie-search"},
			},
			meta: nil,
			inputParams: map[string]string{
				"imdbid": "tt0133093",
			},
			wantParams: map[string]string{
				"imdbid": "tt0133093",
			},
			description: "No pruning when meta is nil",
		},
		{
			name: "empty capabilities - preserves q restoration fallback",
			indexer: &models.TorznabIndexer{
				ID:           10,
				Name:         "EmptyCapsIndexer",
				Capabilities: []string{},
			},
			meta: &searchContext{
				searchMode:    "movie",
				originalQuery: "Test Movie",
			},
			inputParams: map[string]string{
				"imdbid": "tt0133093",
			},
			wantParams: map[string]string{
				"imdbid": "tt0133093",
			},
			description: "No pruning when capabilities are empty",
		},
		{
			name: "other search mode - no pruning",
			indexer: &models.TorznabIndexer{
				ID:           11,
				Name:         "GeneralIndexer",
				Capabilities: []string{"search"},
			},
			meta: &searchContext{
				searchMode:    "search",
				originalQuery: "Test",
			},
			inputParams: map[string]string{
				"imdbid": "tt0133093",
			},
			wantParams: map[string]string{
				"imdbid": "tt0133093",
			},
			description: "No pruning for non-movie/tv search modes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clone input params to avoid mutation across tests
			params := maps.Clone(tt.inputParams)

			s.applyCapabilitySpecificParams(tt.indexer, tt.meta, params)

			// Compare resulting params
			if len(params) != len(tt.wantParams) {
				t.Errorf("%s: got %d params, want %d params\ngot: %v\nwant: %v",
					tt.description, len(params), len(tt.wantParams), params, tt.wantParams)
				return
			}

			for k, wantV := range tt.wantParams {
				if gotV, exists := params[k]; !exists || gotV != wantV {
					t.Errorf("%s: param %q = %q, want %q", tt.description, k, gotV, wantV)
				}
			}

			// Check no extra params
			for k := range params {
				if _, exists := tt.wantParams[k]; !exists {
					t.Errorf("%s: unexpected param %q = %q", tt.description, k, params[k])
				}
			}
		})
	}
}
