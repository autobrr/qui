// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

func TestCreateDisabledIndexerSkipsAutomaticCapsSync(t *testing.T) {
	var capsRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		capsRequests.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	store := newTorznabStore(t)
	handler := NewJackettHandler(jackett.NewService(store), store)
	body := fmt.Sprintf(`{
		"name": "Test Indexer",
		"base_url": %q,
		"indexer_id": "test-indexer",
		"api_key": "test-key",
		"backend": "prowlarr",
		"enabled": false
	}`, server.URL)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/torznab/indexers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.CreateIndexer(resp, req)

	require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())
	require.Zero(t, capsRequests.Load(), "creating a disabled indexer must not fetch caps")
}

func TestUpdateDisabledIndexerSkipsAutomaticCapsSync(t *testing.T) {
	var capsRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		capsRequests.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	store := newTorznabStore(t)
	indexer, err := store.CreateWithIndexerID(t.Context(), "Test Indexer", server.URL, "test-indexer", "test-key", nil, nil, true, 0, 30, models.TorznabBackendProwlarr)
	require.NoError(t, err)

	handler := NewJackettHandler(jackett.NewService(store), store)
	body := fmt.Sprintf(`{
		"name": "Test Indexer",
		"base_url": %q,
		"indexer_id": "test-indexer",
		"backend": "prowlarr",
		"enabled": false
	}`, server.URL)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/torznab/indexers/"+strconv.Itoa(indexer.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("indexerID", strconv.Itoa(indexer.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	resp := httptest.NewRecorder()
	handler.UpdateIndexer(resp, req)

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.Zero(t, capsRequests.Load(), "updating a disabled indexer must not fetch caps")
}
