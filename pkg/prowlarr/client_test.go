// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prowlarr

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchIndexerPreservesRateLimitResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "37")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Host: server.URL})
	_, err := client.SearchIndexer(context.Background(), "14", nil)
	if err == nil {
		t.Fatal("SearchIndexer() error = nil, want rate limit error")
	}

	var responseErr interface {
		HTTPStatusCode() int
		RetryAfterHeader() string
	}
	if !errors.As(err, &responseErr) {
		t.Fatalf("SearchIndexer() error = %T, want structured HTTP response error", err)
	}
	if got := responseErr.HTTPStatusCode(); got != http.StatusTooManyRequests {
		t.Errorf("HTTPStatusCode() = %d, want %d", got, http.StatusTooManyRequests)
	}
	if got := responseErr.RetryAfterHeader(); got != "37" {
		t.Errorf("RetryAfterHeader() = %q, want %q", got, "37")
	}
}
