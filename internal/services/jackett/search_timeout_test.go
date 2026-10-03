// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/pkg/timeouts"
)

func TestComputeSearchTimeoutHonorsIndexerTimeouts(t *testing.T) {
	for _, tt := range []struct {
		name    string
		seconds []int
		want    time.Duration
	}{
		{"no indexers", nil, timeouts.DefaultSearchTimeout},
		{"configured timeout", []int{30}, 30 * time.Second},
		{"default timeout", []int{0}, 30 * time.Second},
		{"negative uses default", []int{-1}, 30 * time.Second},
		{"longest timeout", []int{5, 60, 30}, 60 * time.Second},
		{"adaptive floor", []int{5, 5}, 10 * time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			indexers := make([]*models.TorznabIndexer, len(tt.seconds))
			for i, seconds := range tt.seconds {
				indexers[i] = &models.TorznabIndexer{TimeoutSeconds: seconds}
			}
			if got := computeSearchTimeout(indexers); got != tt.want {
				t.Fatalf("timeout = %s, want %s", got, tt.want)
			}
			for _, minimum := range []time.Duration{0, 20 * time.Second, 90 * time.Second} {
				if got := searchExecutionTimeout(indexers, &searchContext{minimumExecutionTimeout: minimum}); got != max(tt.want, minimum) {
					t.Fatalf("execution timeout = %s, want %s", got, max(tt.want, minimum))
				}
			}
		})
	}
}

func TestQueuedSearchAllowsSlowIndexerResponse(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		name := "direct"
		if scheduled {
			name = "scheduled"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(15 * time.Second):
				}
				w.Header().Set("Content-Type", "application/rss+xml")
				_, _ = w.Write([]byte(`<rss version="2.0"><channel><item><title>Example.Search.Result</title><guid>example-result</guid><link>https://example.com/download/1</link></item></channel></rss>`))
			}))
			defer server.Close()
			idx := &models.TorznabIndexer{ID: 1, Name: "Test", BaseURL: server.URL, Backend: models.TorznabBackendProwlarr, IndexerID: "1", TimeoutSeconds: 30, Enabled: true}
			service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{idx}})
			defer service.searchScheduler.Stop()
			if !scheduled {
				service.searchScheduler = nil
			}
			done := make(chan struct{})
			err := service.executeQueuedSearch(t.Context(), []*models.TorznabIndexer{idx}, url.Values{"q": {"Example"}}, nil, nil, func(_ uint64, results []Result, coverage []int, err error) {
				defer close(done)
				if err != nil {
					t.Errorf("search failed: %v", err)
					return
				}
				if len(results) != 1 || results[0].Title != "Example.Search.Result" {
					t.Errorf("unexpected results: %+v", results)
				}
				if !slices.Equal(coverage, []int{1}) {
					t.Errorf("coverage = %v, want [1]", coverage)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(35 * time.Second):
				t.Fatal("search did not complete")
			}
		})
	}
}

func TestQueuedSearchPreservesCallerDeadline(t *testing.T) {
	service := &Service{searchExecutor: func(ctx context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Errorf("caller deadline was extended")
		}
		return nil, nil, nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := service.executeQueuedSearch(ctx, []*models.TorznabIndexer{{TimeoutSeconds: 30}}, nil, nil, nil, func(uint64, []Result, []int, error) {}); err != nil {
		t.Fatal(err)
	}
}
