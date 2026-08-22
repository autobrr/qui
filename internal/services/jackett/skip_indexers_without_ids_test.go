// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/models"
)

// TestSkipIndexersWithoutIDs covers the ID-only pass behavior: an indexer whose
// supported ID params all got pruned is skipped instead of re-running the title
// query. The skip returns rateLimited=false, which executeIndexerSearch maps to
// uncovered=false, so the indexer lands in CoveredIndexerIDs as an intentional
// exclusion.
func TestSkipIndexersWithoutIDs(t *testing.T) {
	tests := []struct {
		name         string
		flag         bool
		capabilities []string
		params       map[string]string
		wantSkip     bool
		wantParams   map[string]string
	}{
		{
			name:         "flagged indexer with matching ID cap is searched with IDs",
			flag:         true,
			capabilities: []string{"movie-search", "movie-search-imdbid"},
			params:       map[string]string{"imdbid": "tt0118884"},
			wantParams:   map[string]string{"imdbid": "tt0118884"},
		},
		{
			name:         "flagged indexer without ID caps is skipped, not title-searched",
			flag:         true,
			capabilities: []string{"movie-search"},
			params:       map[string]string{"imdbid": "tt0118884"},
			wantSkip:     true,
		},
		{
			name:         "unflagged indexer without ID caps keeps the title fallback",
			flag:         false,
			capabilities: []string{"movie-search"},
			params:       map[string]string{"imdbid": "tt0118884"},
			wantParams:   map[string]string{"q": "Original Title 1997"},
		},
		{
			name:         "flagged movie search with TVDb-only IDs is skipped (no movie tvdbid cap exists)",
			flag:         true,
			capabilities: []string{"movie-search", "tv-search-tvdbid"},
			params:       map[string]string{"tvdbid": "123456"},
			wantSkip:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(&mockTorznabIndexerStore{})
			t.Cleanup(service.searchScheduler.Stop)
			indexer := &models.TorznabIndexer{
				ID:           1,
				Name:         "Synthetic Indexer",
				Capabilities: tt.capabilities,
			}
			meta := &searchContext{
				searchMode:             "movie",
				originalQuery:          "Original Title 1997",
				skipIndexersWithoutIDs: tt.flag,
			}
			params := maps.Clone(tt.params)

			skip, rateLimited := service.applyIndexerRestrictions(context.Background(), nil, indexer, "", meta, params)
			if skip != tt.wantSkip {
				t.Fatalf("skip = %v, want %v", skip, tt.wantSkip)
			}
			if rateLimited {
				t.Fatalf("rateLimited = true, want false (skip must count as covered)")
			}
			if tt.wantParams != nil {
				for key, want := range tt.wantParams {
					if got := params[key]; got != want {
						t.Fatalf("params[%q] = %q, want %q", key, got, want)
					}
				}
			}
		})
	}
}

// TestSkipIndexersWithoutIDsEmptyCaps pins the new-indexer path: when the caps
// fetch fails and the indexer has no stored capabilities, ID pruning never runs,
// the IDs survive, and the flagged skip does not trigger (discussion #2198:
// never exclude an indexer whose capabilities are simply unknown).
func TestSkipIndexersWithoutIDsEmptyCaps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	idx := &models.TorznabIndexer{
		ID:      1,
		Name:    "Synthetic Indexer",
		BaseURL: server.URL,
		Backend: models.TorznabBackendProwlarr,
		Enabled: true,
	}
	service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{idx}})
	t.Cleanup(service.searchScheduler.Stop)
	client := NewClient(server.URL, "key", nil, nil, models.TorznabBackendProwlarr, 5)
	meta := &searchContext{searchMode: "movie", originalQuery: "Original Title 1997", skipIndexersWithoutIDs: true}
	params := map[string]string{"imdbid": "tt0118884"}

	skip, rateLimited := service.applyIndexerRestrictions(context.Background(), client, idx, "42", meta, params)
	if skip {
		t.Fatal("skip = true, want false: unknown caps must fall through to the executor")
	}
	if rateLimited {
		t.Fatal("rateLimited = true, want false")
	}
	if params["imdbid"] != "tt0118884" {
		t.Fatalf("imdbid = %q, want retained", params["imdbid"])
	}
}

// TestSearchCacheKeySkipIndexersWithoutIDs: the flag changes which indexers a
// request reaches, so it must change the cache fingerprint. Built through
// buildSearchCacheSignature to pin the request-to-payload wiring, not just the
// payload struct.
func TestSearchCacheKeySkipIndexersWithoutIDs(t *testing.T) {
	svc := NewService(nil)
	svc.searchCacheEnabled = true
	svc.searchCacheTTL = time.Hour
	svc.searchCache = &fakeSearchCache{}

	indexer := &models.TorznabIndexer{ID: 1, Name: "Synthetic Indexer"}
	req := &TorznabSearchRequest{Query: "Original Title 1997", IMDbID: "tt0118884"}

	unflagged := svc.buildSearchCacheSignature(searchCacheScopeCrossSeed, req, contentTypeMovie, "movie", []*models.TorznabIndexer{indexer})
	flaggedReq := *req
	flaggedReq.SkipIndexersWithoutIDs = true
	flagged := svc.buildSearchCacheSignature(searchCacheScopeCrossSeed, &flaggedReq, contentTypeMovie, "movie", []*models.TorznabIndexer{indexer})
	if unflagged == nil || flagged == nil {
		t.Fatal("expected cache signatures")
	}
	if flagged.Fingerprint == unflagged.Fingerprint {
		t.Fatal("fingerprint unchanged by SkipIndexersWithoutIDs")
	}
	if flagged.Key == unflagged.Key {
		t.Fatal("cache key unchanged by SkipIndexersWithoutIDs")
	}
}
