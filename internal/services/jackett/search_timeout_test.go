// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"net/url"
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

func TestQueuedSearchAllowsIndexerTimeout(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		name := "direct"
		if scheduled {
			name = "scheduled"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			idx := &models.TorznabIndexer{ID: 1, Name: "Test", TimeoutSeconds: 30, Enabled: true}
			service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{idx}})
			defer service.searchScheduler.Stop()
			service.searchExecutor = func(ctx context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) < 29*time.Second {
					t.Errorf("execution deadline = %v (set %v), want at least the 30s indexer timeout", time.Until(deadline), ok)
				}
				return nil, []int{idx.ID}, nil
			}
			done := make(chan struct{})
			resultCallback := func(uint64, []Result, []int, error) { close(done) }
			var err error
			// executeQueuedSearch skips the scheduler when searchExecutor is set.
			if scheduled {
				err = service.searchIndexersWithScheduler(t.Context(), []*models.TorznabIndexer{idx}, nil, nil, nil, resultCallback)
			} else {
				err = service.executeQueuedSearch(t.Context(), []*models.TorznabIndexer{idx}, nil, nil, nil, resultCallback)
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
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
