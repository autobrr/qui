// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type apiKeyResponse struct {
	ID          int                 `json:"id"`
	Name        string              `json:"name"`
	Permissions []models.Permission `json:"permissions"`
	InstanceIDs []int               `json:"instanceIds"`
}

func TestAPIKeyCreateUpdateList(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "api-key-routes")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	inst, err := instances.Create(t.Context(), "first", "http://127.0.0.1:1", "admin", "secret", nil, nil, false, nil)
	require.NoError(t, err)
	h := &AuthHandler{authService: auth.NewService(db), instanceStore: instances}
	router := chi.NewRouter()
	router.Get("/api-keys", h.ListAPIKeys)
	router.Post("/api-keys", h.CreateAPIKey)
	router.Put("/api-keys/{id}", h.UpdateAPIKey)

	send := func(t *testing.T, method, path, body string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}
	decode := func(t *testing.T, body string, v any) {
		t.Helper()
		require.NoError(t, json.Unmarshal([]byte(body), v))
	}

	status, body := send(t, http.MethodPost, "/api-keys", `{"name":"no list"}`)
	require.Equal(t, http.StatusBadRequest, status, "a create with no Permission list is refused: %s", body)

	status, body = send(t, http.MethodPost, "/api-keys", `{"name":"script","permissions":[{"area":"torrents","level":"read"}]}`)
	require.Equal(t, http.StatusCreated, status, body)
	var created apiKeyResponse
	decode(t, body, &created)
	require.Equal(t, []int{}, created.InstanceIDs)

	invalid := map[string]string{
		"empty Permission list": `{"name":"k","permissions":[]}`,
		"invalid instance id":   `{"name":"k","permissions":[{"area":"torrents","level":"read"}],"instanceIds":[1,0]}`,
		"unknown instance id":   `{"name":"k","permissions":[{"area":"torrents","level":"read"}],"instanceIds":[1,2]}`,
		"missing name":          `{"permissions":[{"area":"torrents","level":"read"}]}`,
	}
	for name, req := range invalid {
		status, body := send(t, http.MethodPost, "/api-keys", req)
		require.Equal(t, http.StatusBadRequest, status, "create, %s: %s", name, body)
		status, body = send(t, http.MethodPut, "/api-keys/1", req)
		require.Equal(t, http.StatusBadRequest, status, "update, %s: %s", name, body)
	}

	status, body = send(t, http.MethodPut, "/api-keys/1",
		`{"name":"dashboard","permissions":[{"area":"torrents","level":"read"}],"instanceIds":[1]}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = send(t, http.MethodGet, "/api-keys", "")
	require.Equal(t, http.StatusOK, status, body)
	var listed []apiKeyResponse
	decode(t, body, &listed)
	require.Equal(t, []apiKeyResponse{{
		ID:          created.ID,
		Name:        "dashboard",
		Permissions: []models.Permission{{Area: models.AreaTorrents, Level: models.LevelRead}},
		InstanceIDs: []int{inst.ID},
	}}, listed)

	require.NoError(t, instances.Delete(t.Context(), inst.ID))
	status, body = send(t, http.MethodPut, "/api-keys/1",
		`{"name":"dashboard","permissions":[{"area":"torrents","level":"read"}],"instanceIds":[1]}`)
	require.Equal(t, http.StatusOK, status, "an edit keeps a deleted instance in the limit: %s", body)
	status, body = send(t, http.MethodPost, "/api-keys", `{"name":"k","permissions":[{"area":"torrents","level":"read"}],"instanceIds":[1]}`)
	require.Equal(t, http.StatusBadRequest, status, "a new key cannot name a deleted instance: %s", body)

	status, _ = send(t, http.MethodPut, "/api-keys/99", `{"name":"k","permissions":[{"area":"torrents","level":"read"}]}`)
	require.Equal(t, http.StatusNotFound, status)
}
