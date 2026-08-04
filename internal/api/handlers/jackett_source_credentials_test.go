// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func newTorznabStore(t *testing.T) *models.TorznabIndexerStore {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "torznab-source-creds")
	store, err := models.NewTorznabIndexerStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	return store
}

func TestResolveSourceIndexerCredentials(t *testing.T) {
	ctx := t.Context()
	store := newTorznabStore(t)

	user := "proxyuser"
	pass := "proxypass"
	created, err := store.CreateWithIndexerID(ctx, "Aither", "http://localhost:9696", "aither", "prowlarr-key", &user, &pass, true, 0, 30, models.TorznabBackendProwlarr)
	require.NoError(t, err)

	creds, err := resolveSourceIndexerCredentials(ctx, store, created.ID)
	require.NoError(t, err)
	require.Equal(t, "http://localhost:9696", creds.baseURL)
	require.Equal(t, "prowlarr-key", creds.apiKey)
	require.NotNil(t, creds.basicUsername)
	require.Equal(t, "proxyuser", *creds.basicUsername)
	require.NotNil(t, creds.basicPassword)
	require.Equal(t, "proxypass", *creds.basicPassword)
}

func TestDiscoverIndexersRejectsUnknownSource(t *testing.T) {
	ctx := t.Context()
	store := newTorznabStore(t)
	handler := &JackettHandler{indexerStore: store}

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/torznab/indexers/discover", strings.NewReader(`{"source_indexer_id": 12345}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.DiscoverIndexers(resp, req)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "source indexer not found")
}

func TestCreateIndexerCopiesKeyFromSource(t *testing.T) {
	ctx := t.Context()
	store := newTorznabStore(t)

	user := "proxyuser"
	pass := "proxypass"
	source, err := store.CreateWithIndexerID(ctx, "Aither", "http://localhost:9696", "aither", "prowlarr-key", &user, &pass, true, 0, 30, models.TorznabBackendProwlarr)
	require.NoError(t, err)

	handler := &JackettHandler{indexerStore: store} // service nil: caps provided in request, sync skipped

	body := fmt.Sprintf(`{
		"name": "Blutopia",
		"base_url": "http://localhost:9696",
		"indexer_id": "blutopia",
		"backend": "prowlarr",
		"source_indexer_id": %d,
		"capabilities": ["search"]
	}`, source.ID)

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/torznab/indexers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.CreateIndexer(resp, req)
	require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())

	var created models.TorznabIndexer
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &created))

	stored, err := store.Get(ctx, created.ID)
	require.NoError(t, err)

	apiKey, err := store.GetDecryptedAPIKey(stored)
	require.NoError(t, err)
	require.Equal(t, "prowlarr-key", apiKey)

	require.NotNil(t, stored.BasicUsername)
	require.Equal(t, "proxyuser", *stored.BasicUsername)
	password, err := store.GetDecryptedBasicPassword(stored)
	require.NoError(t, err)
	require.Equal(t, "proxypass", password)
}

// Two servers can expose the same indexer name (e.g. Jackett and Prowlarr side
// by side). Importing via a saved connection must move the URL, API key, and
// basic auth together, not keep the old server's credentials.
func TestUpdateIndexerCopiesCredentialsFromSource(t *testing.T) {
	ctx := t.Context()
	store := newTorznabStore(t)

	aUser := "a-user"
	aPass := "a-pass"
	target, err := store.CreateWithIndexerID(ctx, "Aither", "http://server-a:9117", "aither", "a-key", &aUser, &aPass, true, 0, 30, models.TorznabBackendJackett)
	require.NoError(t, err)

	source, err := store.CreateWithIndexerID(ctx, "Blutopia", "http://server-b:9696", "blutopia", "b-key", nil, nil, true, 0, 30, models.TorznabBackendProwlarr)
	require.NoError(t, err)

	handler := &JackettHandler{indexerStore: store}

	body := fmt.Sprintf(`{
		"name": "Aither",
		"base_url": "http://server-b:9696",
		"indexer_id": "aither",
		"backend": "prowlarr",
		"api_key": "",
		"source_indexer_id": %d,
		"capabilities": ["search"]
	}`, source.ID)

	req := httptest.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("/api/torznab/indexers/%d", target.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("indexerID", strconv.Itoa(target.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	resp := httptest.NewRecorder()
	handler.UpdateIndexer(resp, req)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	stored, err := store.Get(ctx, target.ID)
	require.NoError(t, err)
	require.Equal(t, "http://server-b:9696", stored.BaseURL)

	apiKey, err := store.GetDecryptedAPIKey(stored)
	require.NoError(t, err)
	require.Equal(t, "b-key", apiKey)

	// The source has no basic auth, so the copied connection must clear it.
	require.Nil(t, stored.BasicUsername)
	require.Nil(t, stored.BasicPasswordEncrypted)
}

func TestCreateIndexerStillRequiresKeyWithoutSource(t *testing.T) {
	ctx := t.Context()
	store := newTorznabStore(t)
	handler := &JackettHandler{indexerStore: store}

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/torznab/indexers", strings.NewReader(`{
		"name": "Blutopia",
		"base_url": "http://localhost:9696",
		"backend": "prowlarr",
		"indexer_id": "blutopia"
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.CreateIndexer(resp, req)
	require.Equal(t, http.StatusBadRequest, resp.Code)
}
