// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// rewriteTVDBTransport rewrites requests targeting tvdbBaseURL to
// a local test server.
type rewriteTVDBTransport struct {
	base string
}

func (rt *rewriteTVDBTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Host = strings.TrimPrefix(rt.base, "http://")
	req.URL.Scheme = "http"
	return http.DefaultTransport.RoundTrip(req)
}

func tvdbProviderWithBase(baseURL, apiKey, pin string) *tvdbProvider {
	p := newTVDBProvider(apiKey, pin)
	p.client.Transport = &rewriteTVDBTransport{base: baseURL}
	return p
}

func serveTVDB(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	resp := map[string]any{
		"data": map[string]string{"token": "test-jwt-token"}, //nolint:gosec // test value
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func TestTVDB_AuthFlow(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v4/login", handleLogin)

	ts := serveTVDB(t, mux)
	p := tvdbProviderWithBase(ts.URL, "test-key", "1234")

	err := p.ensureToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.token != "test-jwt-token" {
		t.Errorf("got token %q, want %q", p.token, "test-jwt-token")
	}
}

func TestTVDB_SearchAndEpisodes(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v4/login", handleLogin)

	mux.HandleFunc("/v4/search", func(w http.ResponseWriter, _ *http.Request) {
		resp := tvdbSearchResult{
			Data: []struct {
				TVDBID   string `json:"tvdb_id"`
				Name     string `json:"name"`
				ObjectID string `json:"objectID"`
			}{
				{TVDBID: "81189", Name: "Breaking Bad"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/v4/series/81189/episodes/default", func(w http.ResponseWriter, _ *http.Request) {
		resp := tvdbEpisodesResult{}
		resp.Data.Episodes = []struct {
			SeasonNumber int `json:"seasonNumber"`
			Number       int `json:"number"`
		}{
			{SeasonNumber: 1, Number: 1},
			{SeasonNumber: 1, Number: 2},
			{SeasonNumber: 1, Number: 3},
			{SeasonNumber: 1, Number: 4},
			{SeasonNumber: 1, Number: 5},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	ts := serveTVDB(t, mux)
	p := tvdbProviderWithBase(ts.URL, "test-key", "")

	count, err := p.EpisodesInSeason(context.Background(), "Breaking Bad", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 5 {
		t.Errorf("got count %d, want 5", count)
	}
}

func TestTVDB_TokenRefreshWhenExpired(t *testing.T) {
	t.Parallel()

	var loginCalls int
	mux := http.NewServeMux()

	mux.HandleFunc("/v4/login", func(w http.ResponseWriter, r *http.Request) {
		loginCalls++
		handleLogin(w, r)
	})

	ts := serveTVDB(t, mux)
	p := tvdbProviderWithBase(ts.URL, "test-key", "")

	// Set an expired token so ensureToken must refresh.
	p.token = "old-token"
	p.tokenExpiry = time.Now().Add(-1 * time.Hour)

	err := p.ensureToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loginCalls != 1 {
		t.Errorf("expected 1 login call for refresh, got %d", loginCalls)
	}
	if p.token != "test-jwt-token" {
		t.Errorf("token not refreshed: got %q", p.token)
	}
}

func TestTVDB_SearchFails(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v4/login", handleLogin)

	mux.HandleFunc("/v4/search", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	ts := serveTVDB(t, mux)
	p := tvdbProviderWithBase(ts.URL, "test-key", "")

	_, err := p.EpisodesInSeason(context.Background(), "Breaking Bad", 1)
	if err == nil {
		t.Fatal("expected error when search fails")
	}
	if !strings.Contains(err.Error(), "tvdb") {
		t.Errorf("error should mention tvdb, got: %v", err)
	}
}

func TestTVDB_EpisodesFails(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v4/login", handleLogin)

	mux.HandleFunc("/v4/search", func(w http.ResponseWriter, _ *http.Request) {
		resp := tvdbSearchResult{
			Data: []struct {
				TVDBID   string `json:"tvdb_id"`
				Name     string `json:"name"`
				ObjectID string `json:"objectID"`
			}{
				{TVDBID: "12345", Name: "Some Show"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/v4/series/12345/episodes/default", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	ts := serveTVDB(t, mux)
	p := tvdbProviderWithBase(ts.URL, "test-key", "")

	_, err := p.EpisodesInSeason(context.Background(), "Some Show", 1)
	if err == nil {
		t.Fatal("expected error when episodes endpoint fails")
	}
}
