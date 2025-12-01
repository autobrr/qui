// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func setupClientAPIKeysHandler(t *testing.T) (*ClientAPIKeysHandler, *models.ClientAPIKeyStore, *models.InstanceStore, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)

	clientAPIKeyStore := models.NewClientAPIKeyStore(db)

	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}
	instanceStore, err := models.NewInstanceStore(db, encryptionKey)
	require.NoError(t, err)

	handler := NewClientAPIKeysHandler(clientAPIKeyStore, instanceStore, "/")

	cleanup := func() {
		require.NoError(t, db.Close())
	}

	return handler, clientAPIKeyStore, instanceStore, cleanup
}

func TestNewClientAPIKeysHandler(t *testing.T) {
	t.Parallel()

	handler, _, _, cleanup := setupClientAPIKeysHandler(t)
	defer cleanup()

	require.NotNil(t, handler)
	assert.NotNil(t, handler.clientAPIKeyStore)
	assert.NotNil(t, handler.instanceStore)
}

func TestClientAPIKeysHandler_CreateClientAPIKey(t *testing.T) {
	t.Parallel()

	t.Run("successful creation", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupClientAPIKeysHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test Instance", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		// Create API key request
		body := strings.NewReader(fmt.Sprintf(`{"clientName": "Test Client", "instanceId": %d}`, instance.ID))
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/client-api-keys", body)
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()

		handler.CreateClientAPIKey(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Contains(t, resp.Body.String(), "key")
		assert.Contains(t, resp.Body.String(), "clientApiKey")
	})

	t.Run("missing client name", func(t *testing.T) {
		t.Parallel()

		handler, _, instanceStore, cleanup := setupClientAPIKeysHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance first
		instance, err := instanceStore.Create(ctx, "Test Instance", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		body := strings.NewReader(fmt.Sprintf(`{"clientName": "", "instanceId": %d}`, instance.ID))
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/client-api-keys", body)
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()

		handler.CreateClientAPIKey(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "Client name is required")
	})

	t.Run("missing instance id", func(t *testing.T) {
		t.Parallel()

		handler, _, _, cleanup := setupClientAPIKeysHandler(t)
		defer cleanup()

		ctx := t.Context()

		body := strings.NewReader(`{"clientName": "Test Client", "instanceId": 0}`)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/client-api-keys", body)
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()

		handler.CreateClientAPIKey(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "Instance ID is required")
	})

	t.Run("instance not found", func(t *testing.T) {
		t.Parallel()

		handler, _, _, cleanup := setupClientAPIKeysHandler(t)
		defer cleanup()

		ctx := t.Context()

		body := strings.NewReader(`{"clientName": "Test Client", "instanceId": 999}`)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/client-api-keys", body)
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()

		handler.CreateClientAPIKey(resp, req)

		assert.Equal(t, http.StatusNotFound, resp.Code)
		assert.Contains(t, resp.Body.String(), "Instance not found")
	})
}

func TestClientAPIKeysHandler_ListClientAPIKeys(t *testing.T) {
	t.Parallel()

	t.Run("returns all keys", func(t *testing.T) {
		t.Parallel()

		handler, clientAPIKeyStore, instanceStore, cleanup := setupClientAPIKeysHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance and keys
		instance, err := instanceStore.Create(ctx, "Test Instance", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		_, _, err = clientAPIKeyStore.Create(ctx, "Client 1", instance.ID)
		require.NoError(t, err)
		_, _, err = clientAPIKeyStore.Create(ctx, "Client 2", instance.ID)
		require.NoError(t, err)

		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/client-api-keys", nil)
		resp := httptest.NewRecorder()

		handler.ListClientAPIKeys(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Contains(t, resp.Body.String(), "Client 1")
		assert.Contains(t, resp.Body.String(), "Client 2")
	})

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()

		handler, _, _, cleanup := setupClientAPIKeysHandler(t)
		defer cleanup()

		ctx := t.Context()

		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/client-api-keys", nil)
		resp := httptest.NewRecorder()

		handler.ListClientAPIKeys(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Equal(t, "null\n", resp.Body.String()) // Empty slice becomes null
	})
}

func TestClientAPIKeysHandler_DeleteClientAPIKey(t *testing.T) {
	t.Parallel()

	t.Run("successful deletion", func(t *testing.T) {
		t.Parallel()

		handler, clientAPIKeyStore, instanceStore, cleanup := setupClientAPIKeysHandler(t)
		defer cleanup()

		ctx := t.Context()

		// Create instance and key
		instance, err := instanceStore.Create(ctx, "Test Instance", "http://localhost:8080", "user", "pass", nil, nil, false)
		require.NoError(t, err)

		_, key, err := clientAPIKeyStore.Create(ctx, "To Delete", instance.ID)
		require.NoError(t, err)

		// Create chi context with URL param
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", strconv.Itoa(key.ID))

		req := httptest.NewRequestWithContext(ctx, http.MethodDelete, "/api/client-api-keys/"+strconv.Itoa(key.ID), nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.DeleteClientAPIKey(resp, req)

		assert.Equal(t, http.StatusNoContent, resp.Code)
	})

	t.Run("key not found", func(t *testing.T) {
		t.Parallel()

		handler, _, _, cleanup := setupClientAPIKeysHandler(t)
		defer cleanup()

		ctx := t.Context()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "999")

		req := httptest.NewRequestWithContext(ctx, http.MethodDelete, "/api/client-api-keys/999", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		resp := httptest.NewRecorder()

		handler.DeleteClientAPIKey(resp, req)

		assert.Equal(t, http.StatusNotFound, resp.Code)
	})
}
