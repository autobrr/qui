// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeProvider is a test double implementing the Provider interface.
type fakeProvider struct {
	count int
	err   error
}

func (f *fakeProvider) EpisodesInSeason(_ context.Context, _ string, _ int) (int, error) {
	return f.count, f.err
}

func TestLookupEpisodeTotal_CacheHit(t *testing.T) {
	t.Parallel()

	// Provider that always errors -- if called, the test fails.
	failing := &fakeProvider{err: errors.New("should not be called")}

	svc := &Service{
		tvmaze: failing,
		tvdb:   failing,
		cache:  newResultCache(),
	}

	// Pre-populate cache.
	svc.cache.Set("Cached Show", 1, 10)

	count, err := svc.LookupEpisodeTotal(context.Background(), "Cached Show", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 10 {
		t.Errorf("got count %d, want 10", count)
	}
}

func TestLookupEpisodeTotal_TVMazeOnly(t *testing.T) {
	t.Parallel()

	svc := &Service{
		tvmaze: &fakeProvider{count: 8},
		tvdb:   nil, // not configured
		cache:  newResultCache(),
	}

	count, err := svc.LookupEpisodeTotal(context.Background(), "TVMaze Show", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 8 {
		t.Errorf("got count %d, want 8", count)
	}
}

func TestLookupEpisodeTotal_BothProviders_TVDBPreferred(t *testing.T) {
	t.Parallel()

	svc := &Service{
		tvmaze: &fakeProvider{count: 10},
		tvdb:   &fakeProvider{count: 12},
		cache:  newResultCache(),
	}

	count, err := svc.LookupEpisodeTotal(context.Background(), "Dual Show", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// TVDB is preferred when both succeed.
	if count != 12 {
		t.Errorf("got count %d, want 12 (TVDB preferred)", count)
	}
}

func TestLookupEpisodeTotal_TVDBFailsTVMazeSucceeds(t *testing.T) {
	t.Parallel()

	svc := &Service{
		tvmaze: &fakeProvider{count: 6},
		tvdb:   &fakeProvider{err: errors.New("tvdb down")},
		cache:  newResultCache(),
	}

	count, err := svc.LookupEpisodeTotal(context.Background(), "Fallback Show", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 6 {
		t.Errorf("got count %d, want 6 (TVMaze fallback)", count)
	}
}

func TestLookupEpisodeTotal_BothFail(t *testing.T) {
	t.Parallel()

	svc := &Service{
		tvmaze: &fakeProvider{err: errors.New("tvmaze down")},
		tvdb:   &fakeProvider{err: errors.New("tvdb down")},
		cache:  newResultCache(),
	}

	_, err := svc.LookupEpisodeTotal(context.Background(), "Doomed Show", 1)
	if err == nil {
		t.Fatal("expected error when both providers fail")
	}
	if !strings.Contains(err.Error(), "all providers failed") {
		t.Errorf("error should mention all providers failed, got: %v", err)
	}
}
