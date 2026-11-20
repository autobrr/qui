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

func serveTVMaze(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func tvmazeProviderWithBase(baseURL string) *tvmazeProvider {
	p := newTVMazeProvider()
	p.client.Transport = &rewriteTransport{base: baseURL}
	return p
}

type rewriteTransport struct {
	base string
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

	mux.HandleFunc("/singlesearch/shows", func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		q := r.URL.Query().Get("q")
		if n == 1 {
			if q != "The Show (2024)" {
				t.Errorf("first search: got q=%q, want %q", q, "The Show (2024)")
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if q != "The Show" {
			t.Errorf("second search: got q=%q, want %q", q, "The Show")
		}
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

func TestNormalizeTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"The Show (2024)", "The Show"},
		{"The Show 2024", "The Show 2024"},
		{"The Show 1080p WEB-DL", "The Show"},
		{"1923", "1923"},
		{"Yellowstone 1923", "Yellowstone 1923"},
		{"Show Name HDTV x264", "Show Name"},
		{"  Spacey Title  ", "Spacey Title"},
		{"인간실격 (2021)", "인간실격"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := normalizeTitle(tt.input)
			if got != tt.want {
				t.Errorf("normalizeTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
