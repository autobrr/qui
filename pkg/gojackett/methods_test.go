// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetTorrentsCtxPreservesRateLimitResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "41")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Host: server.URL})
	_, err := client.GetTorrentsCtx(context.Background(), "tracker", map[string]string{})
	assertStructuredRateLimit(t, err)
}

func TestGetTorrentsCtxPreservesBodyRateLimitResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "41")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><error code="429">Too many requests</error>`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Host: server.URL})
	_, err := client.GetTorrentsCtx(context.Background(), "tracker", map[string]string{})
	assertStructuredRateLimit(t, err)
}

func TestSearchDirectCtxPreservesBodyRateLimitResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "41")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><error code="429">Too many requests</error>`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Host: server.URL, DirectMode: true})
	_, err := client.SearchDirectCtx(context.Background(), "query", map[string]string{})
	assertStructuredRateLimit(t, err)
}

func TestSearchDirectCtxKeepsCallerSearchType(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		query string
		opts  map[string]string
		want  string
	}{
		{name: "movie", query: "The Matrix", opts: map[string]string{"t": "movie"}, want: "movie"},
		{name: "tvsearch", query: "Severance", opts: map[string]string{"t": "tvsearch"}, want: "tvsearch"},
		{name: "empty", query: "ubuntu", opts: map[string]string{"t": ""}, want: "search"},
		{name: "unset", query: "ubuntu", opts: map[string]string{}, want: "search"},
		{name: "nil", query: "ubuntu", opts: nil, want: "search"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotType := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotType <- r.URL.Query().Get("t")
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel></channel></rss>`))
			}))
			t.Cleanup(server.Close)

			client := NewClient(Config{Host: server.URL, DirectMode: true})
			if _, err := client.SearchDirectCtx(context.Background(), tc.query, tc.opts); err != nil {
				t.Fatalf("SearchDirectCtx() error = %v", err)
			}
			if got := <-gotType; got != tc.want {
				t.Errorf("t = %q, want %q", got, tc.want)
			}
		})
	}
}

func assertStructuredRateLimit(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want rate limit error")
	}

	var responseErr interface {
		HTTPStatusCode() int
		RetryAfterHeader() string
	}
	if !errors.As(err, &responseErr) {
		t.Fatalf("error = %T, want structured HTTP response error", err)
	}
	if got := responseErr.HTTPStatusCode(); got != http.StatusTooManyRequests {
		t.Errorf("HTTPStatusCode() = %d, want %d", got, http.StatusTooManyRequests)
	}
	if got := responseErr.RetryAfterHeader(); got != "41" {
		t.Errorf("RetryAfterHeader() = %q, want %q", got, "41")
	}
}
