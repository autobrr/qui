// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// serveTVMaze creates an httptest server that routes TVMaze-style paths.
func serveTVMaze(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// tvmazeProviderWithBase returns a tvmazeProvider whose doGet
// hits the test server instead of the real TVMaze API. We achieve
// this by replacing the hardcoded base URL in the request URL via
// a custom RoundTripper.
func tvmazeProviderWithBase(baseURL string) *tvmazeProvider {
	p := newTVMazeProvider()
	p.client.Transport = &rewriteTransport{base: baseURL}
	return p
}

// rewriteTransport rewrites requests targeting tvmazeBaseURL to a
// local test server URL.
type rewriteTransport struct {
	base string
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace the real base URL with the test server URL.
	req.URL.Host = strings.TrimPrefix(rt.base, "http://")
	req.URL.Scheme = "http"
	return http.DefaultTransport.RoundTrip(req)
}

func TestTVMaze_SuccessfulLookup(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()

	mux.HandleFunc("/singlesearch/shows", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(tvmazeShow{ID: 169, Name: "Breaking Bad"})
	})

	mux.HandleFunc("/shows/169/episodes", func(w http.ResponseWriter, _ *http.Request) {
		eps := []tvmazeEpisode{
			{Season: 1, Number: 1},
			{Season: 1, Number: 2},
			{Season: 1, Number: 3},
			{Season: 2, Number: 1},
		}
		_ = json.NewEncoder(w).Encode(eps)
	})

	ts := serveTVMaze(t, mux)
	p := tvmazeProviderWithBase(ts.URL)

	count, err := p.EpisodesInSeason(context.Background(), "Breaking Bad", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("got count %d, want 3", count)
	}
}

func TestTVMaze_RetryWithNormalizedTitle(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	mux := http.NewServeMux()

	mux.HandleFunc("/singlesearch/shows", func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			// First call with original title returns 404.
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Second call with normalized title succeeds.
		_ = json.NewEncoder(w).Encode(tvmazeShow{ID: 42, Name: "The Show"})
	})

	mux.HandleFunc("/shows/42/episodes", func(w http.ResponseWriter, _ *http.Request) {
		eps := []tvmazeEpisode{
			{Season: 1, Number: 1},
			{Season: 1, Number: 2},
		}
		_ = json.NewEncoder(w).Encode(eps)
	})

	ts := serveTVMaze(t, mux)
	p := tvmazeProviderWithBase(ts.URL)

	// Title with year suffix triggers normalization on retry.
	count, err := p.EpisodesInSeason(context.Background(), "The Show (2024)", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("got count %d, want 2", count)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 search calls, got %d", got)
	}
}

func TestTVMaze_RateLimited(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/singlesearch/shows", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	ts := serveTVMaze(t, mux)
	p := tvmazeProviderWithBase(ts.URL)

	_, err := p.EpisodesInSeason(context.Background(), "Some Show", 1)
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error should mention rate limiting, got: %v", err)
	}
	if !strings.Contains(err.Error(), "30") {
		t.Errorf("error should include retry-after value, got: %v", err)
	}
}

// TestTVMaze_TransientErrorSkipsNormalizationRetry verifies that non-404
// errors (rate limits, 5xx) do not trigger a normalized-title retry.
func TestTVMaze_TransientErrorSkipsNormalizationRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/singlesearch/shows", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})

	ts := serveTVMaze(t, mux)
	p := tvmazeProviderWithBase(ts.URL)

	// Title with year suffix would normally trigger normalization on a 404.
	_, err := p.EpisodesInSeason(context.Background(), "Some Show (2024)", 1)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected exactly 1 search call (no retry), got %d", got)
	}
}

func TestTVMaze_NoEpisodesInSeason(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()

	mux.HandleFunc("/singlesearch/shows", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(tvmazeShow{ID: 99, Name: "Short Show"})
	})

	mux.HandleFunc("/shows/99/episodes", func(w http.ResponseWriter, _ *http.Request) {
		// Only season 1 episodes, none for season 5.
		eps := []tvmazeEpisode{
			{Season: 1, Number: 1},
		}
		_ = json.NewEncoder(w).Encode(eps)
	})

	ts := serveTVMaze(t, mux)
	p := tvmazeProviderWithBase(ts.URL)

	_, err := p.EpisodesInSeason(context.Background(), "Short Show", 5)
	if err == nil {
		t.Fatal("expected error for season with no episodes")
	}
	if !strings.Contains(err.Error(), "no episodes found") {
		t.Errorf("error should mention no episodes, got: %v", err)
	}
}
