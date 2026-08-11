// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

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

			skipped, rateLimited := service.applyIndexerRestrictions(context.Background(), client, idx, "42", meta, params)

			if skipped != tt.wantSkip {
				t.Fatalf("applyIndexerRestrictions skip = %v, want %v", skipped, tt.wantSkip)
			}
			if rateLimited != tt.wantRateLimited {
				t.Fatalf("applyIndexerRestrictions rateLimited = %v, want %v", rateLimited, tt.wantRateLimited)
			}
			inCooldown, _ := service.rateLimiter.IsInCooldown(idx.ID)
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
