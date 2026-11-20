// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

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
		"data": map[string]string{"token": "test-jwt-token"},
	}
	_ = json.NewEncoder(w).Encode(resp)
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

	mux := http.NewServeMux()
	mux.HandleFunc("/v4/login", handleLogin)
	handleShowWithTwoEpisodes(mux, func(auth string) bool { return auth == "Bearer test-jwt-token" })

	ts := serveTVDB(t, mux)
	p := tvdbProviderWithBase(ts.URL, "test-key", "")

	p.token = "old-token"
	p.tokenExpiry = time.Now().Add(-1 * time.Hour)

	count, err := p.EpisodesInSeason(t.Context(), "Test Show", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("got count %d, want 2", count)
	}
}

func handleShowWithTwoEpisodes(mux *http.ServeMux, accepts func(auth string) bool) {
	mux.HandleFunc("/v4/search", func(w http.ResponseWriter, r *http.Request) {
		if !accepts(r.Header.Get("Authorization")) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp := tvdbSearchResult{
			Data: []struct {
				TVDBID   string `json:"tvdb_id"`
				Name     string `json:"name"`
				ObjectID string `json:"objectID"`
			}{
				{TVDBID: "999", Name: "Test Show"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/v4/series/999/episodes/default", func(w http.ResponseWriter, r *http.Request) {
		if !accepts(r.Header.Get("Authorization")) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		result := tvdbEpisodesResult{}
		result.Data.Episodes = []struct {
			SeasonNumber int `json:"seasonNumber"`
			Number       int `json:"number"`
		}{
			{SeasonNumber: 1, Number: 1},
			{SeasonNumber: 1, Number: 2},
		}
		_ = json.NewEncoder(w).Encode(result)
	})
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

func TestTVDB_UnauthorizedClearsToken(t *testing.T) {
	t.Parallel()

	var logins atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/login", func(w http.ResponseWriter, _ *http.Request) {
		token := fmt.Sprintf("token-%d", logins.Add(1))
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"token": token}})
	})
	handleShowWithTwoEpisodes(mux, func(auth string) bool { return auth != "Bearer token-1" })

	ts := serveTVDB(t, mux)
	p := tvdbProviderWithBase(ts.URL, "test-key", "")

	_, err := p.EpisodesInSeason(t.Context(), "Test Show", 1)
	if err == nil {
		t.Fatal("expected error on 401")
	}

	count, err := p.EpisodesInSeason(t.Context(), "Test Show", 1)
	if err != nil {
		t.Fatalf("second call should succeed after re-auth: %v", err)
	}
	if count != 2 {
		t.Errorf("got count %d, want 2", count)
	}
}
