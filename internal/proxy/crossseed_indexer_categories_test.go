// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func setupIndexerCategoryProxyDB(t *testing.T) *database.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "proxy_indexer_categories.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

func insertProxyTestInstance(t *testing.T, db *database.DB, name string) int {
	t.Helper()
	ctx := context.Background()

	for _, v := range []string{name, "http://localhost:8080", "admin"} {
		_, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO string_pool (value) VALUES (?)", v)
		require.NoError(t, err)
	}

	var nameID, hostID, userID int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", name).Scan(&nameID))
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", "http://localhost:8080").Scan(&hostID))
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", "admin").Scan(&userID))

	res, err := db.ExecContext(ctx, `
		INSERT INTO instances (name_id, host_id, username_id, password_encrypted, tls_skip_verify, sort_order, is_active)
		VALUES (?, ?, ?, '', 0, 0, 1)
	`, nameID, hostID, userID)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return int(id)
}

func insertProxyTestIndexer(t *testing.T, db *database.DB, name string) int {
	t.Helper()
	ctx := context.Background()

	for _, v := range []string{name, "https://example.com/torznab"} {
		_, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO string_pool (value) VALUES (?)", v)
		require.NoError(t, err)
	}

	var nameID, urlID int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", name).Scan(&nameID))
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", "https://example.com/torznab").Scan(&urlID))

	res, err := db.ExecContext(ctx, `
		INSERT INTO torznab_indexers (name_id, base_url_id, api_key_encrypted, backend)
		VALUES (?, ?, ?, ?)
	`, nameID, urlID, "encrypted", "jackett")
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return int(id)
}

func TestHandleCrossSeedIndexerCategories_MissingInstanceID(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, nil, "/")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v2/cross-seed/indexer-categories", nil)
	// instanceID = 0 (not set in context)
	ctx := context.WithValue(req.Context(), InstanceIDContextKey, 0)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.handleCrossSeedIndexerCategories(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var errResp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.NotEmpty(t, errResp["error"])
}

func TestHandleCrossSeedIndexerCategories_NilStore(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, nil, "/")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v2/cross-seed/indexer-categories", nil)
	ctx := context.WithValue(req.Context(), InstanceIDContextKey, 1)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.handleCrossSeedIndexerCategories(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var errResp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&errResp))
	assert.NotEmpty(t, errResp["error"])
}

func TestHandleCrossSeedIndexerCategories_ReturnsEmptySlice(t *testing.T) {
	db := setupIndexerCategoryProxyDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	h := NewHandler(nil, nil, nil, nil, nil, nil, store, "/")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v2/cross-seed/indexer-categories", nil)
	ctx := context.WithValue(req.Context(), InstanceIDContextKey, 999)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.handleCrossSeedIndexerCategories(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var result []models.CrossSeedIndexerCategory
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

func TestHandleCrossSeedIndexerCategories_ReturnsMappings(t *testing.T) {
	db := setupIndexerCategoryProxyDB(t)
	store := models.NewCrossSeedIndexerCategoryStore(db)
	ctx := context.Background()

	instanceID := insertProxyTestInstance(t, db, "Test Instance")
	indexerID := insertProxyTestIndexer(t, db, "Aither")
	require.NoError(t, store.Set(ctx, instanceID, indexerID, "Aither"))

	h := NewHandler(nil, nil, nil, nil, nil, nil, store, "/")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v2/cross-seed/indexer-categories", nil)
	reqCtx := context.WithValue(req.Context(), InstanceIDContextKey, instanceID)
	req = req.WithContext(reqCtx)

	rec := httptest.NewRecorder()
	h.handleCrossSeedIndexerCategories(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var result []models.CrossSeedIndexerCategory
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	require.Len(t, result, 1)
	assert.Equal(t, "Aither", result[0].IndexerName)
	assert.Equal(t, "Aither", result[0].Category)
	assert.Equal(t, instanceID, result[0].InstanceID)
}
