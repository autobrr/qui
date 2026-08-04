// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
