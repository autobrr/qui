// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

func TestRespondRateLimitError(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	err := &jackett.RateLimitError{
		IndexerID: 1,
		Scope:     "query",
		RetryAt:   time.Now().Add(90 * time.Second),
	}

	require.True(t, respondRateLimitError(recorder, err, "Search rate limited"))
	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	assert.Equal(t, "90", recorder.Header().Get("Retry-After"))

	assert.False(t, respondRateLimitError(httptest.NewRecorder(), errors.New("offline"), "Search rate limited"))
}

func TestRespondRateLimitErrorRoundsMaxDurationWithoutOverflow(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	err := &jackett.RateLimitError{
		IndexerID: 1,
		Scope:     "query",
		RetryAt:   time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
	}

	require.True(t, respondRateLimitError(recorder, err, "Search rate limited"))
	assert.Equal(t, "9223372037", recorder.Header().Get("Retry-After"))
}

func TestIndexerTestTimeoutAllowsNativePacing(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 90*time.Second, indexerTestRequestTimeout(&models.TorznabIndexer{
		Backend:        models.TorznabBackendNative,
		TimeoutSeconds: 30,
	}))
	assert.Equal(t, 3*time.Minute, indexerTestRequestTimeout(&models.TorznabIndexer{
		Backend:        models.TorznabBackendNative,
		TimeoutSeconds: 120,
	}))
	assert.Equal(t, 30*time.Second, indexerTestRequestTimeout(&models.TorznabIndexer{
		Backend:        models.TorznabBackendProwlarr,
		TimeoutSeconds: 5,
	}))
}

func TestTestIndexerWaitsForRateLimitedSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><searching><search available="yes" supportedParams="q"/></searching><categories/></caps>`))
			return
		}
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	store := newTorznabStore(t)
	indexer, err := store.CreateWithIndexerID(t.Context(), "Rate Limited", server.URL, "14", "key", nil, nil, true, 0, 5, "prowlarr")
	require.NoError(t, err)
	service := jackett.NewService(store)
	handler := NewJackettHandler(service, store)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/torznab/indexers/1/test", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("indexerID", strconv.Itoa(indexer.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	recorder := httptest.NewRecorder()
	started := time.Now()

	handler.TestIndexer(recorder, req)

	assert.GreaterOrEqual(t, time.Since(started), 50*time.Millisecond)
	assert.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
	assert.Equal(t, "17", recorder.Header().Get("Retry-After"))
	stored, err := store.Get(t.Context(), indexer.ID)
	require.NoError(t, err)
	assert.Equal(t, "error", stored.LastTestStatus)
}

func TestTestIndexerHonorsConfiguredExecutionTimeout(t *testing.T) {
	responseSent := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><searching><search available="yes" supportedParams="q"/></searching><categories/></caps>`))
			return
		}
		time.Sleep(9500 * time.Millisecond)
		_, _ = w.Write([]byte(`<rss version="2.0"><channel><title>Test</title></channel></rss>`))
		close(responseSent)
	}))
	t.Cleanup(server.Close)

	store := newTorznabStore(t)
	indexer, err := store.CreateWithIndexerID(t.Context(), "Slow Indexer", server.URL, "14", "key", nil, nil, true, 0, 15, "prowlarr")
	require.NoError(t, err)
	require.NoError(t, store.SetCapabilities(t.Context(), indexer.ID, []string{"search"}))
	service := jackett.NewService(store)
	handler := NewJackettHandler(service, store)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/torznab/indexers/1/test", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("indexerID", strconv.Itoa(indexer.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	recorder := httptest.NewRecorder()

	handler.TestIndexer(recorder, req)

	select {
	case <-responseSent:
	default:
		t.Fatal("indexer test returned before the valid response completed")
	}
	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	stored, err := store.Get(t.Context(), indexer.ID)
	require.NoError(t, err)
	assert.Equal(t, "ok", stored.LastTestStatus)
}
