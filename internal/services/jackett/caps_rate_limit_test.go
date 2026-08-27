// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

const bookOnlyCapsXML = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <searching>
    <search available="yes" supportedParams="q"/>
    <book-search available="yes" supportedParams="q"/>
  </searching>
  <categories>
    <category id="7000" name="Books"/>
  </categories>
</caps>`

const emptyCapsXML = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <searching>
    <search available="no" supportedParams="q"/>
  </searching>
  <categories/>
</caps>`

func TestDetectRateLimitUsesStructuredRetryAfter(t *testing.T) {
	t.Parallel()

	httpDate := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	tests := []struct {
		name string
		err  error
		min  time.Duration
		max  time.Duration
		ok   bool
	}{
		{name: "seconds", err: &responseError{StatusCode: 429, RetryAfter: "37"}, min: 37 * time.Second, max: 37 * time.Second, ok: true},
		{name: "oversized seconds fall back", err: &responseError{StatusCode: 429, RetryAfter: "9223372037"}, min: time.Minute, max: time.Minute, ok: true},
		{name: "http date", err: &responseError{StatusCode: 429, RetryAfter: httpDate}, min: 88 * time.Second, max: 90 * time.Second, ok: true},
		{name: "missing header falls back", err: &responseError{StatusCode: 429}, min: time.Minute, max: time.Minute, ok: true},
		{name: "status in text is ignored", err: errors.New("backend returned 429 too many requests"), ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := detectRateLimit(tt.err)
			if ok != tt.ok {
				t.Fatalf("detectRateLimit() ok = %v, want %v", ok, tt.ok)
			}
			if got < tt.min || got > tt.max {
				t.Fatalf("detectRateLimit() = %v, want between %v and %v", got, tt.min, tt.max)
			}
		})
	}
}

// countCapsWarnings counts the caps-blind warnings emitted while the test runs.
func countCapsWarnings(t *testing.T) *atomic.Int32 {
	t.Helper()

	var count atomic.Int32
	original := log.Logger
	log.Logger = original.Hook(zerolog.HookFunc(func(_ *zerolog.Event, level zerolog.Level, msg string) {
		if level >= zerolog.WarnLevel && strings.Contains(msg, "Searching without indexer capabilities") {
			count.Add(1)
		}
	}))
	t.Cleanup(func() { log.Logger = original })
	return &count
}

func assertSingleCapsFetch(t *testing.T, requests <-chan string) {
	t.Helper()
	if got := len(requests); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	if got, want := <-requests, "/api/v1/indexer/1/newznab?apikey=mock-api-key&t=caps"; got != want {
		t.Fatalf("request = %q, want %q", got, want)
	}
}

// A rate-limited caps fetch must skip the indexer and engage the backoff
// ladder instead of failing open (rationale at the skip site in service.go).
func TestApplyIndexerRestrictionsCapsFetchRateLimit(t *testing.T) {
	tests := []struct {
		name             string
		capsStatus       int
		capsBody         string
		storedCaps       []string
		storedCategories []models.TorznabIndexerCategory
		wantSkip         bool
		wantRateLimited  bool
		wantCooldown     bool
		wantCapsCalls    int32
		wantCapsWarnings int32
	}{
		{
			name:            "caps 429 skips indexer and applies cooldown",
			capsStatus:      http.StatusTooManyRequests,
			wantSkip:        true,
			wantRateLimited: true,
			wantCooldown:    true,
			wantCapsCalls:   1,
		},
		{
			name:             "caps 500 keeps fail-open and warns",
			capsStatus:       http.StatusInternalServerError,
			wantSkip:         false,
			wantCooldown:     false,
			wantCapsCalls:    1,
			wantCapsWarnings: 1,
		},
		{
			// A 200 that advertises no search modes stores nothing and returns no
			// error, so the search degrades exactly as a failed fetch does.
			name:             "caps 200 without search modes warns",
			capsStatus:       http.StatusOK,
			capsBody:         emptyCapsXML,
			wantSkip:         false,
			wantCooldown:     false,
			wantCapsCalls:    1,
			wantCapsWarnings: 1,
		},
		{
			name:          "healed caps without tv-search skip via caps gate without cooldown",
			capsStatus:    http.StatusOK,
			capsBody:      bookOnlyCapsXML,
			wantSkip:      true,
			wantCooldown:  false,
			wantCapsCalls: 1,
		},
		{
			name:             "stored caps skip the heal entirely",
			storedCaps:       []string{"search", "tv-search", "tv-search-season"},
			storedCategories: []models.TorznabIndexerCategory{{CategoryID: 5000, CategoryName: "TV"}},
			wantSkip:         false,
			wantCooldown:     false,
			wantCapsCalls:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capsWarnings := countCapsWarnings(t)

			var capsCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				capsCalls.Add(1)
				w.WriteHeader(tt.capsStatus)
				if tt.capsBody != "" {
					_, _ = w.Write([]byte(tt.capsBody))
				}
			}))
			defer server.Close()

			idx := &models.TorznabIndexer{
				ID:           92,
				Name:         "MyAnonamouse",
				BaseURL:      server.URL,
				Backend:      models.TorznabBackendProwlarr,
				Enabled:      true,
				Capabilities: tt.storedCaps,
				Categories:   tt.storedCategories,
			}
			store := &mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{idx}}
			service := NewService(store)
			t.Cleanup(service.searchScheduler.Stop)

			client := NewClient(server.URL, "key", nil, nil, models.TorznabBackendProwlarr, 5)
			meta := &searchContext{searchMode: "tvsearch", categories: []int{5000}}
			params := map[string]string{"q": "some show", "season": "1"}

			skipped, rateLimitErr := service.applyIndexerRestrictions(context.Background(), client, idx, "42", meta, params)

			if skipped != tt.wantSkip {
				t.Fatalf("applyIndexerRestrictions skip = %v, want %v", skipped, tt.wantSkip)
			}
			if got := rateLimitErr != nil; got != tt.wantRateLimited {
				t.Fatalf("applyIndexerRestrictions rateLimited = %v, want %v", got, tt.wantRateLimited)
			}
			inCooldown, _ := service.rateLimiter.IsInCooldown(idx.ID, rateLimitScopeQuery)
			if inCooldown != tt.wantCooldown {
				t.Fatalf("IsInCooldown = %v, want %v", inCooldown, tt.wantCooldown)
			}
			if got := capsCalls.Load(); got != tt.wantCapsCalls {
				t.Fatalf("caps fetches = %d, want %d", got, tt.wantCapsCalls)
			}
			if got := capsWarnings.Load(); got != tt.wantCapsWarnings {
				t.Fatalf("caps warnings = %d, want %d", got, tt.wantCapsWarnings)
			}
		})
	}
}

func TestQueryCooldownDoesNotBlockTorrentDownload(t *testing.T) {
	t.Parallel()

	var downloadCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		downloadCalls.Add(1)
		_, _ = w.Write([]byte("torrent data"))
	}))
	t.Cleanup(server.Close)

	idx := &models.TorznabIndexer{
		ID:        92,
		Name:      "ScopedCooldown",
		BaseURL:   server.URL,
		Backend:   models.TorznabBackendProwlarr,
		IndexerID: "42",
		Enabled:   true,
	}
	service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{idx}})
	t.Cleanup(service.searchScheduler.Stop)
	client := NewClient(server.URL, "", nil, nil, models.TorznabBackendProwlarr, 5)

	skipped, rateLimitErr := service.applyIndexerRestrictions(t.Context(), client, idx, "42", &searchContext{searchMode: "tvsearch"}, map[string]string{"season": "1"})
	require.True(t, skipped)
	require.NotNil(t, rateLimitErr)

	data, err := service.DownloadTorrent(t.Context(), TorrentDownloadRequest{
		IndexerID:   idx.ID,
		DownloadURL: server.URL + "/download",
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("torrent data"), data)
	assert.Equal(t, int32(1), downloadCalls.Load())
}

// One indexer that cannot serve caps must not fill the log.
func TestCapsUnavailableWarningIsSuppressedWithinCooldown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	capsWarnings := countCapsWarnings(t)

	idx := &models.TorznabIndexer{
		ID:      92,
		Name:    "EmptyCaps",
		BaseURL: server.URL,
		Backend: models.TorznabBackendProwlarr,
		Enabled: true,
	}
	service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{idx}})
	t.Cleanup(service.searchScheduler.Stop)

	client := NewClient(server.URL, "key", nil, nil, models.TorznabBackendProwlarr, 5)
	meta := &searchContext{searchMode: "tvsearch"}

	for range 3 {
		if skipped, _ := service.applyIndexerRestrictions(t.Context(), client, idx, "42", meta, map[string]string{"q": "some show", "season": "1"}); skipped {
			t.Fatal("applyIndexerRestrictions skipped the indexer, want fail-open")
		}
	}

	if got := capsWarnings.Load(); got != 1 {
		t.Fatalf("caps warnings = %d, want 1", got)
	}
}

func TestSearchMultipleIndexersCapsRateLimitIsNotCovered(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.RequestURI()
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	indexers := []*models.TorznabIndexer{
		{
			ID:        1,
			Name:      "Rate limited",
			BaseURL:   server.URL,
			Backend:   models.TorznabBackendProwlarr,
			IndexerID: "1",
			Enabled:   true,
		},
		{
			ID:           2,
			Name:         "Books only",
			BaseURL:      server.URL,
			Backend:      models.TorznabBackendProwlarr,
			IndexerID:    "2",
			Enabled:      true,
			Capabilities: []string{"book-search"},
			Categories:   []models.TorznabIndexerCategory{{CategoryID: 7000, CategoryName: "Books"}},
		},
	}
	service := NewService(&mockTorznabIndexerStore{indexers: indexers})
	t.Cleanup(service.searchScheduler.Stop)

	params := url.Values{"q": {"some show"}, "season": {"1"}, "cat": {"5000"}}
	meta := &searchContext{searchMode: "tvsearch", categories: []int{5000}}
	_, covered, err := service.searchMultipleIndexers(context.Background(), indexers, params, meta)

	if err != nil {
		t.Fatalf("searchMultipleIndexers error = %v", err)
	}
	if len(covered) != 1 || covered[0] != 2 {
		t.Fatalf("covered indexers = %v, want [2]", covered)
	}
	assertSingleCapsFetch(t, requests)
}

func TestSearchMultipleIndexersCapsRateLimitReturnsTypedError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "73")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	indexer := &models.TorznabIndexer{
		ID: 1, Name: "Rate limited", BaseURL: server.URL,
		Backend: models.TorznabBackendProwlarr, IndexerID: "1", Enabled: true,
	}
	service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{indexer}})
	t.Cleanup(service.searchScheduler.Stop)
	before := time.Now().Add(72 * time.Second)

	_, _, err := service.searchMultipleIndexers(t.Context(), []*models.TorznabIndexer{indexer}, url.Values{"season": {"1"}}, &searchContext{searchMode: "tvsearch"})
	var rateLimitErr *RateLimitError
	require.ErrorAs(t, err, &rateLimitErr)
	assert.Equal(t, rateLimitScopeQuery, rateLimitErr.Scope)
	assert.WithinRange(t, rateLimitErr.RetryAt, before, time.Now().Add(74*time.Second))
}

func TestScheduledSearchSkipsKnownQueryCooldown(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	indexer := &models.TorznabIndexer{
		ID: 1, Name: "Rate limited", BaseURL: server.URL,
		Backend: models.TorznabBackendProwlarr, IndexerID: "1", Enabled: true,
		Capabilities: []string{"movie-search"},
		Categories:   []models.TorznabIndexerCategory{{CategoryID: 2000, CategoryName: "Movies"}},
	}
	service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{indexer}})
	t.Cleanup(service.searchScheduler.Stop)

	search := func() *RateLimitError {
		t.Helper()
		done := make(chan error, 1)
		req := &TorznabSearchRequest{
			Query:      "synthetic movie",
			Categories: []int{2000},
			IndexerIDs: []int{indexer.ID},
			CacheMode:  CacheModeBypass,
			OnAllComplete: func(_ *SearchResponse, err error) {
				done <- err
			},
		}
		require.NoError(t, service.SearchGeneric(t.Context(), req))
		select {
		case err := <-done:
			var rateLimitErr *RateLimitError
			require.ErrorAs(t, err, &rateLimitErr)
			return rateLimitErr
		case <-time.After(3 * time.Second):
			t.Fatal("scheduled search did not complete")
			return nil
		}
	}

	first := search()
	second := search()
	assert.Equal(t, int32(1), requests.Load())
	assert.WithinDuration(t, first.RetryAt, second.RetryAt, time.Microsecond)
}

func TestSyncIndexerCapsRateLimitSetsQueryCooldown(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "45")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	indexer := &models.TorznabIndexer{
		ID: 1, Name: "Rate limited", BaseURL: server.URL,
		Backend: models.TorznabBackendProwlarr, IndexerID: "1", Enabled: true,
	}
	service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{indexer}})
	t.Cleanup(service.searchScheduler.Stop)

	_, err := service.SyncIndexerCaps(t.Context(), indexer.ID)
	var rateLimitErr *RateLimitError
	require.ErrorAs(t, err, &rateLimitErr)
	assert.Equal(t, rateLimitScopeQuery, rateLimitErr.Scope)
	inCooldown, retryAt := service.rateLimiter.IsInCooldown(indexer.ID, rateLimitScopeQuery)
	assert.True(t, inCooldown)
	assert.WithinDuration(t, rateLimitErr.RetryAt, retryAt, time.Microsecond)

	_, err = service.SyncIndexerCaps(t.Context(), indexer.ID)
	var secondRateLimitErr *RateLimitError
	require.ErrorAs(t, err, &secondRateLimitErr)
	assert.Equal(t, int32(1), requests.Load())
	assert.WithinDuration(t, rateLimitErr.RetryAt, secondRateLimitErr.RetryAt, time.Microsecond)
}

func TestHandleRateLimitReturnsEffectiveCooldown(t *testing.T) {
	t.Parallel()

	indexer := &models.TorznabIndexer{ID: 1, Name: "Rate limited", Enabled: true}
	service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{indexer}})
	t.Cleanup(service.searchScheduler.Stop)

	longerRetryAt := time.Now().Add(2 * time.Minute)
	service.rateLimiter.SetCooldown(indexer.ID, rateLimitScopeQuery, longerRetryAt)

	rateLimitErr := service.handleRateLimit(t.Context(), indexer, rateLimitScopeQuery, 10*time.Second, errors.New("rate limited"))
	inCooldown, storedRetryAt := service.rateLimiter.IsInCooldown(indexer.ID, rateLimitScopeQuery)

	require.True(t, inCooldown)
	assert.WithinDuration(t, longerRetryAt, storedRetryAt, time.Microsecond)
	assert.WithinDuration(t, storedRetryAt, rateLimitErr.RetryAt, time.Microsecond)
}

func TestSearchMultipleIndexersReturnsEarliestCooldown(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	indexers := []*models.TorznabIndexer{
		{ID: 1, Name: "Already limited", BaseURL: server.URL, Backend: models.TorznabBackendProwlarr, IndexerID: "1", Enabled: true, Capabilities: []string{"search"}},
		{ID: 2, Name: "Newly limited", BaseURL: server.URL, Backend: models.TorznabBackendProwlarr, IndexerID: "2", Enabled: true, Capabilities: []string{"search"}},
	}
	service := NewService(&mockTorznabIndexerStore{indexers: indexers})
	t.Cleanup(service.searchScheduler.Stop)
	earlyRetryAt := time.Now().Add(10 * time.Second)
	service.rateLimiter.SetCooldown(indexers[0].ID, rateLimitScopeQuery, earlyRetryAt)

	_, _, err := service.searchMultipleIndexers(t.Context(), indexers, url.Values{"q": {"synthetic movie"}}, &searchContext{searchMode: "search"})
	var rateLimitErr *RateLimitError
	require.ErrorAs(t, err, &rateLimitErr)
	assert.Equal(t, indexers[0].ID, rateLimitErr.IndexerID)
	assert.WithinDuration(t, earlyRetryAt, rateLimitErr.RetryAt, time.Microsecond)
}

func TestScheduledSearchCapsRateLimitIsNotCovered(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.RequestURI()
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	indexers := []*models.TorznabIndexer{
		{
			ID:        1,
			Name:      "Rate limited",
			BaseURL:   server.URL,
			Backend:   models.TorznabBackendProwlarr,
			IndexerID: "1",
			Enabled:   true,
		},
		{
			ID:           2,
			Name:         "Books only",
			BaseURL:      server.URL,
			Backend:      models.TorznabBackendProwlarr,
			IndexerID:    "2",
			Enabled:      true,
			Capabilities: []string{"book-search"},
			Categories:   []models.TorznabIndexerCategory{{CategoryID: 7000, CategoryName: "Books"}},
		},
	}
	service := NewService(&mockTorznabIndexerStore{indexers: indexers})
	t.Cleanup(service.searchScheduler.Stop)

	type searchResult struct {
		covered []int
		err     error
	}
	done := make(chan searchResult, 1)
	params := url.Values{"q": {"some show"}, "season": {"1"}, "cat": {"5000"}}
	meta := &searchContext{searchMode: "tvsearch", categories: []int{5000}}
	if err := service.searchIndexersWithScheduler(context.Background(), indexers, params, meta, nil, func(_ uint64, _ []Result, covered []int, err error) {
		done <- searchResult{covered: covered, err: err}
	}); err != nil {
		t.Fatalf("searchIndexersWithScheduler error = %v", err)
	}

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("scheduled search error = %v", result.err)
		}
		if len(result.covered) != 1 || result.covered[0] != 2 {
			t.Fatalf("covered indexers = %v, want [2]", result.covered)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled search timed out")
	}
	assertSingleCapsFetch(t, requests)
}
