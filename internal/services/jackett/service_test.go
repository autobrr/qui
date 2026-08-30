// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

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
				Season:  new(1),
				Episode: new(1),
			},
			expected: contentTypeTVShow,
		},
		{
			name: "detects TV show by season pack",
			req: &TorznabSearchRequest{
				Query:  "The Wire",
				Season: new(2),
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

func TestComputeSearchTimeoutExcludesQueueWait(t *testing.T) {
	t.Parallel()

	prowlarr := []*models.TorznabIndexer{{Backend: models.TorznabBackendProwlarr}}
	native := []*models.TorznabIndexer{{Backend: models.TorznabBackendNative}}
	mixed := []*models.TorznabIndexer{
		{Backend: models.TorznabBackendProwlarr},
		{Backend: models.TorznabBackendNative},
	}

	if got := computeSearchTimeout(prowlarr); got != timeouts.DefaultSearchTimeout {
		t.Fatalf("Prowlarr timeout = %s, want %s", got, timeouts.DefaultSearchTimeout)
	}
	if got, want := computeSearchTimeout(native), timeouts.DefaultSearchTimeout; got != want {
		t.Fatalf("native timeout = %s, want %s", got, want)
	}
	if got, want := computeSearchTimeout(mixed), timeouts.DefaultSearchTimeout+timeouts.PerIndexerSearchTimeout; got != want {
		t.Fatalf("mixed timeout = %s, want %s", got, want)
	}
}

func TestGetActivityStatusMergesCooldownScopes(t *testing.T) {
	store := &mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{{ID: 1, Name: "IndexerOne"}}}
	service := NewService(store)
	defer service.searchScheduler.Stop()

	queryUntil := time.Now().Add(time.Minute)
	grabUntil := queryUntil.Add(time.Minute)
	service.rateLimiter.SetCooldown(1, rateLimitScopeQuery, queryUntil)
	service.rateLimiter.SetCooldown(1, rateLimitScopeGrab, grabUntil)

	status, err := service.GetActivityStatus(context.Background())
	if err != nil {
		t.Fatalf("GetActivityStatus() error = %v", err)
	}
	if got := len(status.CooldownIndexers); got != 1 {
		t.Fatalf("cooldown rows = %d, want 1", got)
	}
	cooldown := status.CooldownIndexers[0]
	if !cooldown.CooldownEnd.Equal(grabUntil) {
		t.Errorf("cooldown end = %s, want %s", cooldown.CooldownEnd, grabUntil)
	}
	if cooldown.Reason != "query,grab" {
		t.Errorf("reason = %q, want %q", cooldown.Reason, "query,grab")
	}
}

func TestSortSearchResults_SeedersFirstUsesSizeAsTieBreaker(t *testing.T) {
	results := []SearchResult{
		{Title: "small weak", Size: 1000, Seeders: 5},
		{Title: "large popular", Size: 2000, Seeders: 100},
		{Title: "larger popular", Size: 3000, Seeders: 100},
	}

	sortSearchResults(results)

	want := []string{"larger popular", "large popular", "small weak"}
	for i, title := range want {
		if results[i].Title != title {
			t.Fatalf("results[%d].Title = %q, want %q; results=%+v", i, results[i].Title, title, results)
		}
	}
}

func TestBuildSearchCacheSignatureNormalizesLimitToEffectiveIndexerCap(t *testing.T) {
	svc := NewService(nil)
	svc.searchCacheEnabled = true
	svc.searchCacheTTL = time.Hour
	svc.searchCache = &fakeSearchCache{}

	baseReq := &TorznabSearchRequest{
		Query:      "Example Show",
		Categories: []int{CategoryTV},
	}
	const (
		indexerMaxLimit      = 150
		limitAboveIndexerMax = indexerMaxLimit + 1
		limitAboveDefault    = defaultTorznabLimit + 1
	)
	sigForLimit := func(limit int, indexer *models.TorznabIndexer) *searchCacheSignature {
		req := *baseReq
		req.Limit = limit
		sig := svc.buildSearchCacheSignature(searchCacheScopeCrossSeed, &req, contentTypeTVShow, "tvsearch", []*models.TorznabIndexer{indexer})
		if sig == nil {
			t.Fatalf("expected cache signature for limit %d", limit)
		}
		return sig
	}

	highCapIndexer := &models.TorznabIndexer{ID: 1, LimitMax: indexerMaxLimit}
	highCap100 := sigForLimit(defaultTorznabLimit, highCapIndexer)
	highCapMax := sigForLimit(indexerMaxLimit, highCapIndexer)
	highCapAboveMax := sigForLimit(limitAboveIndexerMax, highCapIndexer)

	if highCap100.Key == highCapMax.Key {
		t.Fatal("expected default and high-cap max limits to produce distinct cache keys")
	}
	if highCapMax.Key != highCapAboveMax.Key {
		t.Fatal("expected high-cap max and above-max limits to share cache key")
	}

	defaultCapIndexer := &models.TorznabIndexer{ID: 1}
	defaultCap100 := sigForLimit(defaultTorznabLimit, defaultCapIndexer)
	defaultCapAboveMax := sigForLimit(limitAboveDefault, defaultCapIndexer)
	if defaultCap100.Key != defaultCapAboveMax.Key {
		t.Fatal("expected default and above-default limits to share cache key")
	}
}

func TestLoadCachedSearchPortionReturnsPartialCoverage(t *testing.T) {
	svc := &Service{
		searchCacheEnabled: true,
		searchCacheTTL:     time.Hour,
	}
	req := &TorznabSearchRequest{Query: "My Query"}
	payload := searchCacheKeyPayload{
		SchemaVersion: searchCacheSchemaVersion,
		Scope:         searchCacheScopeCrossSeed,
		Query:         canonicalizeQuery(req.Query),
		IndexerIDs:    []int{1, 2},
		ContentType:   contentTypeTVShow,
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
		SchemaVersion: searchCacheSchemaVersion,
		Scope:         searchCacheScopeCrossSeed,
		Query:         "query",
		ContentType:   contentTypeTVShow,
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

func TestBuildBaseFingerprintFromRaw_LegacyPayloadDoesNotMatchCurrentSchema(t *testing.T) {
	legacyRaw := `{"scope":"dir-scan","query":"query","content_type":1}`

	base, err := buildBaseFingerprintFromRaw(legacyRaw)
	if err != nil {
		t.Fatalf("buildBaseFingerprintFromRaw: %v", err)
	}

	currentPayload := searchCacheKeyPayload{
		SchemaVersion: searchCacheSchemaVersion,
		Scope:         searchCacheScopeDirScan,
		Query:         "query",
		ContentType:   contentTypeMovie,
	}
	_, currentBase, err := buildSearchCacheFingerprints(currentPayload)
	if err != nil {
		t.Fatalf("buildSearchCacheFingerprints: %v", err)
	}

	if base == currentBase {
		t.Fatal("expected legacy cache fingerprint to differ from current schema fingerprint")
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

// TestSearchCachePersistGate locks in that cache persistence is gated by
// SkipCachePersist and is independent of SkipHistory (issue #1997). The
// alternate connector-spelling pass sets SkipHistory but leaves SkipCachePersist
// unset, so it must still populate the cache; only SkipCachePersist suppresses
// the store.
func TestSearchCachePersistGate(t *testing.T) {
	cases := []struct {
		name             string
		skipHistory      bool
		skipCachePersist bool
		wantStored       bool
	}{
		{name: "default persists", wantStored: true},
		{name: "skip history still persists cache", skipHistory: true, wantStored: true},
		{name: "skip cache persist suppresses store", skipCachePersist: true, wantStored: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
			service.searchExecutor = func(_ context.Context, indexers []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
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

			req := &TorznabSearchRequest{
				Query:            "Example",
				Categories:       []int{CategoryTV},
				SkipHistory:      tc.skipHistory,
				SkipCachePersist: tc.skipCachePersist,
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

			if err := service.Search(context.Background(), req); err != nil {
				t.Fatalf("Search returned error: %v", err)
			}

			select {
			case resp := <-respCh:
				if resp == nil {
					t.Fatal("expected response")
				}
			case err := <-errCh:
				t.Fatalf("Search callback returned error: %v", err)
			case <-time.After(1 * time.Second):
				t.Fatal("Search timed out")
			}

			if tc.wantStored && stored == nil {
				t.Fatal("expected cache entry to be stored")
			}
			if !tc.wantStored && stored != nil {
				t.Fatalf("expected no cache entry to be stored, got %+v", stored.IndexerIDs)
			}
		})
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
				Season:  new(1),
				Episode: new(1),
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
				Season:     new(1),
				Episode:    new(1),
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

func TestResponseSearchResultsReturnAllSkipsPagination(t *testing.T) {
	results := []SearchResult{
		{Title: "first"},
		{Title: "second"},
		{Title: "third"},
	}

	tests := []struct {
		name           string
		input          []SearchResult
		page           int
		perPage        int
		returnAll      bool
		wantTotal      int
		wantLen        int
		wantFirstTitle string
	}{
		{
			name:           "paged",
			input:          results,
			page:           1,
			perPage:        1,
			wantTotal:      3,
			wantLen:        1,
			wantFirstTitle: "second",
		},
		{
			name:           "return all",
			input:          results,
			page:           1,
			perPage:        1,
			returnAll:      true,
			wantTotal:      3,
			wantLen:        3,
			wantFirstTitle: "first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotTotal := responseSearchResults(tt.input, tt.page, tt.perPage, tt.returnAll)
			if gotTotal != tt.wantTotal {
				t.Fatalf("total = %d, want %d", gotTotal, tt.wantTotal)
			}
			if len(got) != tt.wantLen {
				t.Fatalf("results length = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantFirstTitle != "" && got[0].Title != tt.wantFirstTitle {
				t.Fatalf("first title = %q, want %q", got[0].Title, tt.wantFirstTitle)
			}
		})
	}
}

func TestClampedTorznabLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "unset", limit: 0, want: 0},
		{name: "negative", limit: -1, want: 0},
		{name: "below fixed max", limit: 50, want: 50},
		{name: "fixed max", limit: 100, want: 100},
		{name: "above fixed max", limit: defaultTorznabLimit + 1, want: defaultTorznabLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampedTorznabLimit(tt.limit)
			if got != tt.want {
				t.Fatalf("clampedTorznabLimit(%d) = %d, want %d", tt.limit, got, tt.want)
			}
		})
	}
}

func TestExecuteIndexerSearchClampsLimitPerIndexer(t *testing.T) {
	tests := []struct {
		name      string
		backend   models.TorznabBackend
		indexerID string
		limitMax  int
		requested string
		wantLimit string
	}{
		{
			name:      "native uses indexer max below fallback cap",
			backend:   models.TorznabBackendNative,
			limitMax:  50,
			requested: "100",
			wantLimit: "50",
		},
		{
			name:      "prowlarr uses indexer max below fallback cap",
			backend:   models.TorznabBackendProwlarr,
			indexerID: "7",
			limitMax:  50,
			requested: "100",
			wantLimit: "50",
		},
		{
			name:      "jackett uses indexer max below fallback cap",
			backend:   models.TorznabBackendJackett,
			indexerID: "test-indexer",
			limitMax:  50,
			requested: "100",
			wantLimit: "50",
		},
		{
			name:      "valid indexer max above fallback cap is honored",
			backend:   models.TorznabBackendNative,
			limitMax:  150,
			requested: "151",
			wantLimit: "150",
		},
		{
			name:      "missing indexer max falls back to hard cap",
			backend:   models.TorznabBackendNative,
			limitMax:  0,
			requested: "101",
			wantLimit: "100",
		},
		{
			name:      "invalid indexer max falls back to hard cap",
			backend:   models.TorznabBackendNative,
			limitMax:  -5,
			requested: "101",
			wantLimit: "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = r.URL.Query()
				w.Header().Set("Content-Type", "application/rss+xml")
				if _, err := w.Write([]byte(`<rss version="2.0"><channel><title>Test</title></channel></rss>`)); err != nil {
					t.Errorf("write RSS response: %v", err)
				}
			}))
			defer server.Close()

			idx := &models.TorznabIndexer{
				ID:             1,
				Name:           "Test Indexer",
				BaseURL:        server.URL,
				Backend:        tt.backend,
				IndexerID:      tt.indexerID,
				LimitMax:       tt.limitMax,
				TimeoutSeconds: 5,
				Enabled:        true,
			}
			service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{idx}})

			result := service.executeIndexerSearch(
				context.Background(),
				idx,
				url.Values{
					"q":     {"Example"},
					"limit": {tt.requested},
				},
				nil,
				indexerExecOptions{},
			)
			if result.err != nil {
				t.Fatalf("executeIndexerSearch() error = %v", result.err)
			}
			if captured == nil {
				t.Fatal("expected outbound request to be captured")
			}
			if got := captured.Get("limit"); got != tt.wantLimit {
				t.Fatalf("outbound limit = %q, want %q", got, tt.wantLimit)
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
		{
			name: "preserves executed search id context",
			input: []Result{
				{
					Tracker: "Tracker1",
					Title:   "Example",
					Seeders: 10,
					Peers:   10,
					Size:    1024,
					Attributes: map[string]string{
						"tmdbid": "42",
					},
					SearchIMDbID: "tt1234567",
					SearchTVDbID: "7654321",
					SearchTMDbID: 42,
				},
			},
			expected: 1,
			checkFn: func(t *testing.T, results []SearchResult) {
				if results[0].SearchIMDbID != "tt1234567" {
					t.Fatalf("SearchIMDbID = %q, want %q", results[0].SearchIMDbID, "tt1234567")
				}
				if results[0].SearchTVDbID != "7654321" {
					t.Fatalf("SearchTVDbID = %q, want %q", results[0].SearchTVDbID, "7654321")
				}
				if results[0].TMDbID != "42" {
					t.Fatalf("TMDbID = %q, want %q", results[0].TMDbID, "42")
				}
				if results[0].SearchTMDbID != 42 {
					t.Fatalf("SearchTMDbID = %d, want %d", results[0].SearchTMDbID, 42)
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

func TestAnnotateResultsWithSearchIDs(t *testing.T) {
	results := []Result{{Title: "Example"}}
	params := map[string]string{
		"imdbid": "1234567",
		"tvdbid": "7654321",
		"tmdbid": "42",
	}

	annotateResultsWithSearchIDs(results, params)

	if results[0].SearchIMDbID != "tt1234567" {
		t.Fatalf("SearchIMDbID = %q, want %q", results[0].SearchIMDbID, "tt1234567")
	}
	if results[0].SearchTVDbID != "7654321" {
		t.Fatalf("SearchTVDbID = %q, want %q", results[0].SearchTVDbID, "7654321")
	}
	if results[0].SearchTMDbID != 42 {
		t.Fatalf("SearchTMDbID = %d, want %d", results[0].SearchTMDbID, 42)
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
				Season:  new(1),
				Episode: new(1),
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

	t.Run("deduplicates repeated categories", func(t *testing.T) {
		tvIndexerCats := []models.TorznabIndexerCategory{
			{CategoryID: CategoryTV},
		}

		filtered, ok := filterCategoriesForIndexer(tvIndexerCats, []int{CategoryTV, CategoryTV, CategoryTV})
		if !ok {
			t.Fatalf("expected parent TV category to be permitted")
		}
		if len(filtered) != 1 || filtered[0] != CategoryTV {
			t.Fatalf("unexpected filtered categories: %+v", filtered)
		}
	})
}

func TestMapCategoriesToIndexerCapabilitiesCompactsParentFallbacks(t *testing.T) {
	service := &Service{}

	tests := []struct {
		name      string
		parent    int
		requested []int
	}{
		{
			name:      "movies",
			parent:    CategoryMovies,
			requested: []int{CategoryMovies, CategoryMoviesSD, CategoryMoviesHD, CategoryMovies4K, CategoryMovies3D},
		},
		{
			name:      "tv and anime",
			parent:    CategoryTV,
			requested: []int{CategoryTV, 5010, 5020, CategoryTVSD, CategoryTVHD, CategoryTV4K, CategoryTVAnime, CategoryTVDocumentary},
		},
		{
			name:      "xxx",
			parent:    CategoryXXX,
			requested: []int{CategoryXXX, CategoryXXXDVD, CategoryXXXWMV, CategoryXXXXviD, CategoryXXXx264, CategoryXXXPack, CategoryXXXImageSet, CategoryXXXOther},
		},
		{
			name:      "books",
			parent:    CategoryBooks,
			requested: []int{CategoryBooks, CategoryBooksEbook, CategoryBooksComics},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexer := &models.TorznabIndexer{
				Categories: []models.TorznabIndexerCategory{
					{CategoryID: tt.parent},
				},
			}

			mapped := service.MapCategoriesToIndexerCapabilities(context.Background(), indexer, tt.requested)
			if !slices.Equal(mapped, []int{tt.parent}) {
				t.Fatalf("expected duplicate subcategory fallbacks to compact to [%d], got %+v", tt.parent, mapped)
			}
		})
	}
}

func TestBuildSearchParamsDeduplicatesCategories(t *testing.T) {
	service := &Service{}
	req := &TorznabSearchRequest{
		Query:      "Example",
		Categories: []int{CategoryTVHD, CategoryTV, CategoryTVHD, CategoryTVAnime, CategoryTV},
	}

	params := service.buildSearchParams(req, "tvsearch")
	if got := params.Get("cat"); got != "5000,5040,5070" {
		t.Fatalf("cat param = %q, want %q", got, "5000,5040,5070")
	}
}

func TestFormatCategoryListPreservesDuplicates(t *testing.T) {
	if got := formatCategoryList([]int{CategoryMoviesHD, CategoryMovies, CategoryMoviesHD}); got != "2040,2000,2040" {
		t.Fatalf("formatCategoryList() = %q, want %q", got, "2040,2000,2040")
	}
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

func TestGetConfiguredTrackerDomains(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "Native A", Backend: models.TorznabBackendNative, BaseURL: "https://aither.cc/torznab", Enabled: true},
			{ID: 2, Name: "Native A dup host", Backend: models.TorznabBackendNative, BaseURL: "https://aither.cc/api/torznab", Enabled: true},
			{ID: 3, Name: "Native B", Backend: models.TorznabBackendNative, BaseURL: "https://blutopia.cc/torznab", Enabled: true},
			// Jackett base_url is the Jackett server, not the tracker — must be skipped.
			{ID: 4, Name: "Jackett", Backend: models.TorznabBackendJackett, BaseURL: "http://jackett:9117/api/v2.0/indexers/aither/results/torznab", Enabled: true},
			// Disabled indexers are filtered out by ListEnabled.
			{ID: 5, Name: "Disabled native", Backend: models.TorznabBackendNative, BaseURL: "https://disabled.cc/torznab", Enabled: false},
			// A native indexer without a base URL yields no domain.
			{ID: 6, Name: "Native no url", Backend: models.TorznabBackendNative, BaseURL: "", Enabled: true},
			// Prowlarr with a non-numeric IndexerID hits getProwlarrTrackerDomains'
			// server-host fallback (no API call). The Prowlarr server host must NOT
			// leak into the result.
			{ID: 7, Name: "Prowlarr bad id", Backend: models.TorznabBackendProwlarr, BaseURL: "http://prowlarr:9696", IndexerID: "not-a-number", Enabled: true},
			// A tracker./www./api. host must NOT be stripped: the domain has to match the
			// full host qBittorrent reports for the active tracker, or the matcher silently
			// fails ("foo.net" != "tracker.foo.net").
			{ID: 8, Name: "Native subdomain", Backend: models.TorznabBackendNative, BaseURL: "https://tracker.foo.net/torznab", Enabled: true},
			// A mixed-case host must be lowercased to match the qBittorrent-keyed active trackers.
			{ID: 9, Name: "Native mixed case", Backend: models.TorznabBackendNative, BaseURL: "https://API.Example.ORG/torznab", Enabled: true},
		},
	}
	s := NewService(store)

	domains, err := s.GetConfiguredTrackerDomains(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"aither.cc", "api.example.org", "blutopia.cc", "tracker.foo.net"}
	if !slices.Equal(domains, want) {
		t.Fatalf("GetConfiguredTrackerDomains() = %v, want %v", domains, want)
	}
	// Guard against the Prowlarr server host leaking via the fallback.
	if slices.Contains(domains, "prowlarr") {
		t.Fatalf("GetConfiguredTrackerDomains() leaked Prowlarr server host: %v", domains)
	}
}

func TestGetConfiguredTrackerDomains_ProwlarrResolvesRealDomain(t *testing.T) {
	// Prowlarr resolves the real tracker domain from its API. Serve an indexer detail whose
	// baseUrl field points at the real tracker and assert that domain is emitted (lowercased)
	// and deduped against a native indexer for the same host. This is the core reason Prowlarr
	// support exists in GetConfiguredTrackerDomains; a regression that dropped real domains
	// instead of only the Prowlarr server-host fallback would fail here.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/indexer/5" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"id":5,"name":"RealTracker","fields":[{"name":"baseUrl","value":"https://RealTracker.org"}]}`)); err != nil {
			t.Errorf("write prowlarr response: %v", err)
		}
	}))
	defer server.Close()

	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "Prowlarr", Backend: models.TorznabBackendProwlarr, BaseURL: server.URL, IndexerID: "5", TimeoutSeconds: 5, Enabled: true},
			// Native indexer for the same tracker: the resolved Prowlarr domain must dedup against it.
			{ID: 2, Name: "Native same host", Backend: models.TorznabBackendNative, BaseURL: "https://realtracker.org/torznab", Enabled: true},
		},
	}
	s := NewService(store)

	domains, err := s.GetConfiguredTrackerDomains(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"realtracker.org"}
	if !slices.Equal(domains, want) {
		t.Fatalf("GetConfiguredTrackerDomains() = %v, want %v", domains, want)
	}
}

// Helper functions
//
// Mock store for testing
type mockTorznabIndexerStore struct {
	mu                  sync.Mutex
	indexers            []*models.TorznabIndexer
	capabilities        map[int][]string // indexerID -> capabilities
	panicOnListEnabled  bool
	listEnabledCalls    int
	getCalls            []int
	apiKeyErrors        map[int]error
	apiKeyCalls         []int
	latencyCleanupCalls chan time.Duration
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

func (m *mockTorznabIndexerStore) GetDecryptedBasicPassword(_ *models.TorznabIndexer) (string, error) {
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

func (m *mockTorznabIndexerStore) CleanupOldLatency(ctx context.Context, olderThan time.Duration) (int64, error) {
	if m.latencyCleanupCalls != nil {
		m.latencyCleanupCalls <- olderThan
	}
	return 0, nil
}

func (m *mockTorznabIndexerStore) RecordError(ctx context.Context, indexerID int, errorMessage, errorCode string) error {
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
	service.rateLimiter.SetCooldown(1, rateLimitScopeQuery, time.Now().Add(time.Minute))

	_, _, err := service.searchMultipleIndexers(context.Background(), store.indexers, url.Values{"q": {"test"}}, nil)
	if err == nil {
		t.Fatalf("expected error when all available indexers fail")
	}
	if _, ok := errors.AsType[*RateLimitError](err); !ok {
		t.Fatalf("expected known rate limit to be preferred, got %v", err)
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
	service.rateLimiter.SetCooldown(1, rateLimitScopeQuery, time.Now().Add(time.Minute))
	service.rateLimiter.SetCooldown(2, rateLimitScopeQuery, time.Now().Add(2*time.Minute))

	_, _, err := service.searchMultipleIndexers(context.Background(), store.indexers, url.Values{"q": {"test"}}, nil)
	var rateLimitErr *RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("expected RateLimitError, got %v", err)
	}
	if rateLimitErr.IndexerID != 1 {
		t.Fatalf("rate-limit indexer = %d, want earliest indexer 1", rateLimitErr.IndexerID)
	}

	if len(store.apiKeyCalls) != 0 {
		t.Fatalf("expected no API key calls when all indexers are skipped, apiKeyCalls=%v", store.apiKeyCalls)
	}
}

func TestProwlarrYearParameterWorkaround(t *testing.T) {
	tests := []struct {
		name         string
		backend      models.TorznabBackend
		capabilities []string
		inputParams  map[string]string
		expected     map[string]string
		meta         *searchContext
		description  string
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
		{
			name:    "prowlarr tv season parameter",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t":      "tvsearch",
				"q":      "Some Show",
				"season": "22",
				"cat":    "5000",
			},
			expected: map[string]string{
				"t":   "tvsearch",
				"q":   "Some Show S22",
				"cat": "5000",
			},
			description: "Prowlarr indexer should move TV season parameter to search query",
		},
		{
			name:    "prowlarr tv season episode parameter",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t":      "tvsearch",
				"q":      "Some Show",
				"season": "22",
				"ep":     "32",
			},
			expected: map[string]string{
				"t": "tvsearch",
				"q": "Some Show S22E32",
			},
			description: "Prowlarr indexer should move TV season and episode parameters to search query",
		},
		{
			name:    "prowlarr tv season parameter before trailing resolution",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t":      "tvsearch",
				"q":      "Some Show 720",
				"season": "22",
			},
			expected: map[string]string{
				"t": "tvsearch",
				"q": "Some Show S22 720",
			},
			description: "Prowlarr indexer should place TV season before a trailing resolution token",
		},
		{
			name:    "prowlarr id driven tv restores query with season token",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t":      "tvsearch",
				"season": "17",
				"imdbid": "1785123",
			},
			expected: map[string]string{
				"t":      "tvsearch",
				"q":      "S17",
				"imdbid": "1785123",
			},
			meta: &searchContext{
				originalQuery: "Some.Show.S17.1080p.HULU.WEB-DL.AAC2.0.H.264-RAWR",
				releaseName:   "Some.Show.S17.1080p.HULU.WEB-DL.AAC2.0.H.264-RAWR",
			},
			description: "Prowlarr indexer should keep only the TV token for ID-driven TV searches; resolution must not be added to q (breaks series-name-only indexers)",
		},
		{
			name:    "prowlarr id driven tv drops title from restored query",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t":      "tvsearch",
				"q":      "Greys Anatomy 720",
				"season": "22",
				"imdbid": "0413573",
				"tvdbid": "73762",
				"tmdbid": "1416",
			},
			expected: map[string]string{
				"t":      "tvsearch",
				"q":      "S22",
				"imdbid": "0413573",
				"tvdbid": "73762",
				"tmdbid": "1416",
			},
			description: "Prowlarr indexer should drop title and resolution from q when IDs are present",
		},
		{
			name:         "prowlarr id driven tv keeps structured season when caps support it",
			backend:      models.TorznabBackendProwlarr,
			capabilities: []string{"tv-search", "tv-search-tvdbid", "tv-search-season"},
			inputParams: map[string]string{
				"t":      "tvsearch",
				"season": "5",
				"tvdbid": "153021",
			},
			expected: map[string]string{
				"t":      "tvsearch",
				"season": "5",
				"tvdbid": "153021",
				// q must stay absent: BTN-style free-text search matches release
				// names, so an injected "S05" token returns zero results (#2036)
			},
			description: "Prowlarr indexer with season caps should keep structured season param and not inject a token into q",
		},
		{
			name:         "prowlarr tv keeps structured season and title query when caps support it",
			backend:      models.TorznabBackendProwlarr,
			capabilities: []string{"tv-search", "tv-search-season", "tv-search-ep"},
			inputParams: map[string]string{
				"t":      "tvsearch",
				"q":      "Some Show",
				"season": "22",
				"ep":     "3",
			},
			expected: map[string]string{
				"t":      "tvsearch",
				"q":      "Some Show",
				"season": "22",
				"ep":     "3",
			},
			description: "Prowlarr indexer with season/ep caps should keep structured params alongside the title query",
		},
		{
			name:         "prowlarr tv falls back to token when ep cap missing",
			backend:      models.TorznabBackendProwlarr,
			capabilities: []string{"tv-search", "tv-search-season"},
			inputParams: map[string]string{
				"t":      "tvsearch",
				"q":      "Some Show",
				"season": "22",
				"ep":     "3",
			},
			expected: map[string]string{
				"t": "tvsearch",
				"q": "Some Show S22E03",
			},
			description: "Prowlarr indexer missing the ep cap should fall back to the token workaround for season+episode searches",
		},
		{
			name:         "prowlarr tv restores title query when structured params supported and q empty",
			backend:      models.TorznabBackendProwlarr,
			capabilities: []string{"tv-search", "tv-search-season"},
			inputParams: map[string]string{
				"t":      "tvsearch",
				"season": "17",
			},
			meta: &searchContext{
				originalQuery: "Some Show",
			},
			expected: map[string]string{
				"t":      "tvsearch",
				"q":      "Some Show",
				"season": "17",
			},
			description: "Prowlarr indexer with season caps should restore the title query instead of a season-only search",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test indexer with the specified backend
			indexer := &models.TorznabIndexer{
				ID:           1,
				Name:         "Test Indexer",
				Backend:      tt.backend,
				IndexerID:    "test",
				Capabilities: tt.capabilities,
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
			service.applyProwlarrWorkaround(indexer, inputParams, tt.meta)

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

func TestAppendSearchTokenDetectsExistingSeasonEpisode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		token    string
		expected string
	}{
		{
			name:     "keeps existing season token",
			input:    "Some Show S22E01 720",
			token:    "S22",
			expected: "Some Show S22E01 720",
		},
		{
			name:     "adds missing season token",
			input:    "Some Show S21E01 720",
			token:    "S22",
			expected: "Some Show S21E01 S22 720",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := appendSearchToken(tt.input, tt.token); got != tt.expected {
				t.Fatalf("appendSearchToken() = %q, want %q", got, tt.expected)
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

// TestSearchIndexersWithSchedulerRSSDeduplicatedIndexerStillCompletes reproduces
// the RSS scheduler hang: when an RSS search overlaps an indexer whose previous
// RSS task is still pending, the scheduler used to skip it without a completion,
// leaving searchIndexersWithScheduler's WaitGroup unbalanced so OnJobDone (and
// thus the result callback) blocked forever.
func TestSearchIndexersWithSchedulerRSSDeduplicatedIndexerStillCompletes(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "IndexerOne", Enabled: true},
		},
	}
	service := NewService(store)
	// Swap in a fast rate limiter so the first job's exec starts promptly.
	service.searchScheduler.Stop()
	service.searchScheduler = newSearchScheduler(NewRateLimiter(1*time.Millisecond), 1)
	defer service.searchScheduler.Stop()

	var startOnce sync.Once
	firstExecStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	service.searchExecutor = func(_ context.Context, idxs []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		startOnce.Do(func() { close(firstExecStarted) })
		<-releaseFirst
		coverage := make([]int, 0, len(idxs))
		for _, idx := range idxs {
			coverage = append(coverage, idx.ID)
		}
		return []Result{{Title: "held"}}, coverage, nil
	}

	indexers := []*models.TorznabIndexer{{ID: 1, Name: "IndexerOne", Enabled: true}}
	rssMeta := &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityRSS}}

	// First RSS search holds pendingRSS[1] and the worker until released.
	firstDone := make(chan struct{})
	if err := service.searchIndexersWithScheduler(context.Background(), indexers, url.Values{}, rssMeta, nil, func(uint64, []Result, []int, error) {
		close(firstDone)
	}); err != nil {
		t.Fatalf("first searchIndexersWithScheduler returned error: %v", err)
	}

	select {
	case <-firstExecStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first RSS search never started")
	}

	// Second RSS search to the same indexer is fully deduplicated. Its result
	// callback must still fire instead of hanging.
	secondDone := make(chan error, 1)
	if err := service.searchIndexersWithScheduler(context.Background(), indexers, url.Values{}, rssMeta, nil, func(_ uint64, results []Result, _ []int, err error) {
		if err == nil && len(results) != 0 {
			secondDone <- errors.New("expected empty results for deduplicated search")
			return
		}
		secondDone <- err
	}); err != nil {
		t.Fatalf("second searchIndexersWithScheduler returned error: %v", err)
	}

	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("deduplicated RSS search reported error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deduplicated RSS search never completed (scheduler hang)")
	}

	// Release the first search and let it finish cleanly.
	close(releaseFirst)
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first RSS search never completed after release")
	}
}

// TestSearchIndexersWithSchedulerMixedFailureAndRateLimitSurfacesRateLimit
// verifies that an unusable response containing a rate limit tells the caller
// when to retry, even when another indexer failed normally.
func TestSearchIndexersWithSchedulerMixedFailureAndRateLimitSurfacesRateLimit(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "IndexerOne", Enabled: true},
			{ID: 2, Name: "IndexerTwo", Enabled: true},
		},
	}
	service := NewService(store)
	service.searchScheduler.Stop()
	service.searchScheduler = newSearchScheduler(NewRateLimiter(1*time.Millisecond), 2)
	defer service.searchScheduler.Stop()

	failErr := errors.New("indexer two unreachable")
	service.searchExecutor = func(_ context.Context, idxs []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		// Indexer 1 is rate-limited, indexer 2 fails outright.
		if idxs[0].ID == 1 {
			return nil, nil, &RateLimitError{IndexerID: 1, IndexerName: "IndexerOne", Scope: rateLimitScopeQuery, RetryAt: time.Now().Add(time.Minute)}
		}
		return nil, nil, failErr
	}

	indexers := []*models.TorznabIndexer{
		{ID: 1, Name: "IndexerOne", Enabled: true},
		{ID: 2, Name: "IndexerTwo", Enabled: true},
	}

	resultCh := make(chan error, 1)
	if err := service.searchIndexersWithScheduler(context.Background(), indexers, url.Values{}, &searchContext{}, nil, func(_ uint64, results []Result, _ []int, err error) {
		if err == nil && len(results) == 0 {
			resultCh <- errors.New("expected an error, got empty success")
			return
		}
		resultCh <- err
	}); err != nil {
		t.Fatalf("searchIndexersWithScheduler returned error: %v", err)
	}

	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("expected a surfaced error for mixed failure/wait-skip, got success")
		}
		if _, ok := errors.AsType[*RateLimitError](err); !ok {
			t.Fatalf("expected the rate limit to be preferred, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("aggregation never invoked the result callback")
	}
}

// TestRecentSurfacesTotalIndexerFailure verifies Recent propagates an aggregated
// failure to its callback instead of reporting an empty success.
func TestRecentSurfacesTotalIndexerFailure(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "IndexerOne", Enabled: true},
			{ID: 2, Name: "IndexerTwo", Enabled: true},
		},
	}
	service := NewService(store)
	searchErr := errors.New("all indexers unreachable")
	service.searchExecutor = func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
		return nil, nil, searchErr
	}

	respCh := make(chan *SearchResponse, 1)
	errCh := make(chan error, 1)
	if err := service.Recent(context.Background(), 0, 0, nil, func(resp *SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	}); err != nil {
		t.Fatalf("Recent returned error: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, searchErr) {
			t.Fatalf("expected the aggregated search error, got %v", err)
		}
	case resp := <-respCh:
		t.Fatalf("expected Recent to surface the failure, got success response %+v", resp)
	case <-time.After(2 * time.Second):
		t.Fatal("Recent never invoked its callback")
	}
}

// TestRecentPartialCoverageMarksPartial verifies Recent flags incomplete indexer
// coverage as partial even when no error is returned.
func TestRecentPartialCoverageMarksPartial(t *testing.T) {
	originalLogger := log.Logger
	partialLogLevel := zerolog.NoLevel
	log.Logger = zerolog.New(io.Discard).Level(zerolog.TraceLevel).Hook(zerolog.HookFunc(func(_ *zerolog.Event, level zerolog.Level, msg string) {
		if msg == "Recent search returning partial results" {
			partialLogLevel = level
		}
	}))
	t.Cleanup(func() { log.Logger = originalLogger })

	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "IndexerOne", Enabled: true},
			{ID: 2, Name: "IndexerTwo", Enabled: true},
		},
	}
	service := NewService(store)
	service.searchExecutor = func(_ context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		// Only the second indexer returned data; coverage is incomplete.
		return []Result{{
			IndexerID: 2,
			Tracker:   "IndexerTwo",
			Title:     "Example",
			GUID:      "guid-2",
			Link:      "http://example/2",
		}}, []int{2}, nil
	}

	respCh := make(chan *SearchResponse, 1)
	errCh := make(chan error, 1)
	if err := service.Recent(context.Background(), 0, 0, nil, func(resp *SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	}); err != nil {
		t.Fatalf("Recent returned error: %v", err)
	}

	select {
	case resp := <-respCh:
		if !resp.Partial {
			t.Fatal("expected Recent to mark incomplete coverage as partial")
		}
		if resp.Total != 1 {
			t.Fatalf("expected 1 result, got %d", resp.Total)
		}
	case err := <-errCh:
		t.Fatalf("expected partial success, got error %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Recent never invoked its callback")
	}

	if partialLogLevel != zerolog.DebugLevel {
		t.Fatalf("partial search log level = %s, want debug", partialLogLevel)
	}
}

// TestSearchIndexersWithSchedulerDedupExcludedFromFailureThreshold pins the
// dedupSkips subtraction in the failure threshold: with one indexer
// RSS-deduplicated and the other failing outright, the failure must surface
// instead of resolving as a silent empty success.
func TestSearchIndexersWithSchedulerDedupExcludedFromFailureThreshold(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "IndexerOne", Enabled: true},
			{ID: 2, Name: "IndexerTwo", Enabled: true},
		},
	}
	service := NewService(store)
	service.searchScheduler.Stop()
	service.searchScheduler = newSearchScheduler(NewRateLimiter(1*time.Millisecond), 2)
	defer service.searchScheduler.Stop()

	failErr := errors.New("indexer two unreachable")
	firstExecStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	service.searchExecutor = func(_ context.Context, idxs []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		if idxs[0].ID == 1 {
			close(firstExecStarted)
			<-releaseFirst
			return []Result{{Title: "held"}}, []int{1}, nil
		}
		return nil, nil, failErr
	}

	rssMeta := &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityRSS}}

	// First RSS search holds pendingRSS[1] until released.
	firstDone := make(chan struct{})
	if err := service.searchIndexersWithScheduler(context.Background(), []*models.TorznabIndexer{
		{ID: 1, Name: "IndexerOne", Enabled: true},
	}, url.Values{}, rssMeta, nil, func(uint64, []Result, []int, error) {
		close(firstDone)
	}); err != nil {
		t.Fatalf("first searchIndexersWithScheduler returned error: %v", err)
	}
	select {
	case <-firstExecStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first RSS search never started")
	}

	// Second RSS search: indexer 1 is deduplicated, indexer 2 fails outright.
	resultCh := make(chan error, 1)
	if err := service.searchIndexersWithScheduler(context.Background(), []*models.TorznabIndexer{
		{ID: 1, Name: "IndexerOne", Enabled: true},
		{ID: 2, Name: "IndexerTwo", Enabled: true},
	}, url.Values{}, rssMeta, nil, func(_ uint64, _ []Result, _ []int, err error) {
		resultCh <- err
	}); err != nil {
		t.Fatalf("second searchIndexersWithScheduler returned error: %v", err)
	}

	select {
	case err := <-resultCh:
		if !errors.Is(err, failErr) {
			t.Fatalf("expected the failure to surface past the dedup skip, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("aggregation never invoked the result callback")
	}

	close(releaseFirst)
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first RSS search never completed after release")
	}
}

// TestSearchIDDrivenMovieCategoryBehavior pins the outbound Torznab request for an
// ID-driven movie search. An indexer that can search by IMDb ID must receive the ID
// alone, without a query or a category filter that could hide a correctly matching
// release stored outside the mapped category. An indexer without that capability
// falls back to the title search and keeps its mapped category.
func TestSearchIDDrivenMovieCategoryBehavior(t *testing.T) {
	tests := []struct {
		name         string
		capabilities []string
		noStoredCats bool
		wantQuery    string
		wantCategory string
		wantIMDbID   string
	}{
		{
			name:         "supported ID omits query and category",
			capabilities: []string{"movie-search", "movie-search-imdbid"},
			wantIMDbID:   "1234567",
		},
		{
			name:         "unsupported ID restores title and category",
			capabilities: []string{"movie-search"},
			wantQuery:    "Synthetic Documentary",
			wantCategory: "2000",
		},
		{
			name:         "unsupported ID falls back without stored categories",
			capabilities: []string{"movie-search"},
			noStoredCats: true,
			wantQuery:    "Synthetic Documentary",
			wantCategory: "2000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outboundCh := make(chan url.Values, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				if query.Get("t") == "caps" {
					// An indexer with no stored categories triggers a caps fetch first.
					// Answer without categories so it stays category-blind.
					w.Header().Set("Content-Type", "application/xml")
					if _, writeErr := w.Write([]byte(`<caps><categories/></caps>`)); writeErr != nil {
						t.Errorf("write caps response: %v", writeErr)
					}
					return
				}
				select {
				case outboundCh <- query:
				default:
				}
				w.Header().Set("Content-Type", "application/rss+xml")
				if _, writeErr := w.Write([]byte(`<rss version="2.0"><channel><title>Test</title></channel></rss>`)); writeErr != nil {
					t.Errorf("write RSS response: %v", writeErr)
				}
			}))
			defer server.Close()

			indexer := &models.TorznabIndexer{
				ID:             1,
				Name:           "Synthetic Prowlarr Indexer",
				BaseURL:        server.URL,
				Backend:        models.TorznabBackendProwlarr,
				IndexerID:      "7",
				Enabled:        true,
				TimeoutSeconds: 5,
				Capabilities:   tt.capabilities,
				Categories: []models.TorznabIndexerCategory{
					{CategoryID: CategoryMovies},
				},
			}
			if tt.noStoredCats {
				indexer.Categories = nil
			}
			service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{indexer}})

			done := make(chan error, 1)
			req := &TorznabSearchRequest{
				Query:           "Synthetic Documentary",
				Categories:      []int{CategoryMovies},
				IMDbID:          "tt1234567",
				OmitQueryForIDs: true,
				IndexerIDs:      []int{1},
				OnAllComplete: func(_ *SearchResponse, err error) {
					done <- err
				},
			}

			if err := service.Search(context.Background(), req); err != nil {
				t.Fatalf("Search() error = %v", err)
			}

			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("search completed with error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for search to complete")
			}

			var outbound url.Values
			select {
			case outbound = <-outboundCh:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for the outbound request")
			}

			if got := outbound.Get("t"); got != "movie" {
				t.Fatalf("t = %q, want movie", got)
			}
			if got := outbound.Get("q"); got != tt.wantQuery {
				t.Fatalf("q = %q, want %q", got, tt.wantQuery)
			}
			if got := outbound.Get("cat"); got != tt.wantCategory {
				t.Fatalf("cat = %q, want %q", got, tt.wantCategory)
			}
			if got := outbound.Get("imdbid"); got != tt.wantIMDbID {
				t.Fatalf("imdbid = %q, want %q", got, tt.wantIMDbID)
			}
		})
	}
}

// TestApplyIndexerRestrictionsKeepsContentTypeRouting locks the capability gate that
// keeps a search on indexers of the correct content type. Omitting categories from an
// ID-driven request must not let a TV or music search reach a movie-only indexer.
func TestApplyIndexerRestrictionsKeepsContentTypeRouting(t *testing.T) {
	tests := []struct {
		name         string
		searchMode   string
		capabilities []string
		params       map[string]string
		wantSkip     bool
	}{
		{
			name:         "TV rejects movie-only indexer",
			searchMode:   "tvsearch",
			capabilities: []string{"movie-search", "movie-search-imdbid"},
			params:       map[string]string{"tvdbid": "123456"},
			wantSkip:     true,
		},
		{
			name:         "TV accepts TV indexer",
			searchMode:   "tvsearch",
			capabilities: []string{"tv-search", "tv-search-tvdbid"},
			params:       map[string]string{"tvdbid": "123456"},
		},
		{
			name:         "music rejects movie-only indexer",
			searchMode:   "music",
			capabilities: []string{"movie-search"},
			params:       map[string]string{"q": "Synthetic Artist Synthetic Album", "artist": "Synthetic Artist", "album": "Synthetic Album"},
			wantSkip:     true,
		},
		{
			name:         "music accepts music indexer",
			searchMode:   "music",
			capabilities: []string{"music-search"},
			params:       map[string]string{"q": "Synthetic Artist Synthetic Album", "artist": "Synthetic Artist", "album": "Synthetic Album"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(&mockTorznabIndexerStore{})
			indexer := &models.TorznabIndexer{
				ID:           1,
				Name:         "Synthetic Indexer",
				Capabilities: tt.capabilities,
			}
			meta := &searchContext{searchMode: tt.searchMode}

			skip, rateLimitErr := service.applyIndexerRestrictions(context.Background(), nil, indexer, "", meta, maps.Clone(tt.params))
			if skip != tt.wantSkip {
				t.Fatalf("skip = %v, want %v", skip, tt.wantSkip)
			}
			if rateLimitErr != nil {
				t.Fatalf("rateLimited = true, want false")
			}
		})
	}
}

func TestExecuteIndexerSearchSchedulesLatencyCleanup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		if _, err := w.Write([]byte(`<rss version="2.0"><channel><title>Test</title></channel></rss>`)); err != nil {
			t.Errorf("write RSS response: %v", err)
		}
	}))
	defer server.Close()

	cleanupCalls := make(chan time.Duration, 1)
	store := &mockTorznabIndexerStore{latencyCleanupCalls: cleanupCalls}
	service := NewService(store)
	indexer := &models.TorznabIndexer{
		ID:      1,
		BaseURL: server.URL,
		Backend: models.TorznabBackendNative,
	}

	result := service.executeIndexerSearch(context.Background(), indexer, nil, nil, indexerExecOptions{})
	if result.err != nil {
		t.Fatalf("executeIndexerSearch() error = %v", result.err)
	}

	select {
	case olderThan := <-cleanupCalls:
		if olderThan != 14*24*time.Hour {
			t.Fatalf("CleanupOldLatency() olderThan = %v, want 14 days", olderThan)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected search to schedule latency cleanup")
	}

	service.maybeScheduleLatencyCleanup()
	select {
	case <-cleanupCalls:
		t.Fatal("expected latency cleanup to be interval-gated")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestGetActivityStatusOrdersCooldownsByEnd(t *testing.T) {
	store := &mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{
		{ID: 1, Name: "Alpha"},
		{ID: 2, Name: "Charlie"},
		{ID: 3, Name: "Bravo"},
		{ID: 4, Name: "Delta"},
	}}
	service := NewService(store)
	defer service.searchScheduler.Stop()

	base := time.Now().Add(time.Minute)
	// Indexers 2 and 3 share a cooldown end, and their names sort the opposite
	// way from their IDs, so the name tiebreak has to decide.
	service.rateLimiter.SetCooldown(1, rateLimitScopeQuery, base.Add(3*time.Minute))
	service.rateLimiter.SetCooldown(2, rateLimitScopeQuery, base.Add(time.Minute))
	service.rateLimiter.SetCooldown(3, rateLimitScopeGrab, base.Add(time.Minute))
	service.rateLimiter.SetCooldown(4, rateLimitScopeQuery, base)

	want := []int{4, 3, 2, 1}
	for range 20 {
		status, err := service.GetActivityStatus(context.Background())
		if err != nil {
			t.Fatalf("GetActivityStatus() error = %v", err)
		}
		got := make([]int, 0, len(status.CooldownIndexers))
		for _, cooldown := range status.CooldownIndexers {
			got = append(got, cooldown.IndexerID)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("cooldown order = %v, want %v", got, want)
		}
	}
}
