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

func TestGetIndexersCtxPreservesRateLimitResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "41")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Host: server.URL})
	_, err := client.GetIndexersCtx(context.Background())
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

func TestSearchDirectCtxPreservesRateLimitResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "41")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Host: server.URL, DirectMode: true})
	_, err := client.SearchDirectCtx(context.Background(), "query", nil)
	assertStructuredRateLimit(t, err)
}

func TestGetTorrentsCtxDecodesSuccessfulResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<rss version="2.0"><channel><title>Synthetic Tracker</title><item><title>Synthetic Release</title></item></channel></rss>`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Host: server.URL})
	rss, err := client.GetTorrentsCtx(context.Background(), "tracker", map[string]string{})
	if err != nil {
		t.Fatalf("GetTorrentsCtx() error = %v", err)
	}
	if got := rss.Channel.Title; got != "Synthetic Tracker" {
		t.Errorf("Channel.Title = %q, want %q", got, "Synthetic Tracker")
	}
	if got := len(rss.Channel.Item); got != 1 {
		t.Fatalf("len(Channel.Item) = %d, want 1", got)
	}
	if got := rss.Channel.Item[0].Title; got != "Synthetic Release" {
		t.Errorf("Channel.Item[0].Title = %q, want %q", got, "Synthetic Release")
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
