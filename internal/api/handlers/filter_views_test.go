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

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func newFilterViewRouter(t *testing.T) chi.Router {
	t.Helper()

	// No user row is seeded on purpose: OIDC-only and auth-disabled installs
	// never populate the user table, so filter_views must not depend on it.
	db := testdb.NewMigratedSQLite(t, "filter-views-handler")
	h := NewFilterViewHandler(models.NewFilterViewStore(db))

	r := chi.NewRouter()
	r.Get("/filter-views", h.List)
	r.Post("/filter-views", h.Create)
	r.Put("/filter-views/{id}", h.Update)
	r.Delete("/filter-views/{id}", h.Delete)
	return r
}

func doFilterViewRequest(t *testing.T, r chi.Router, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestFilterViewHandlerCRUD(t *testing.T) {
	r := newFilterViewRouter(t)

	rec := doFilterViewRequest(t, r, http.MethodPost, "/filter-views", `{"name":"  Movies  ","filters":{"tags":["hd"]},"sortOrder":2}`)
	require.Equal(t, http.StatusCreated, rec.Code)

	var created models.FilterView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "Movies", created.Name, "name should be trimmed")
	assert.JSONEq(t, `{"tags":["hd"]}`, string(created.Filters))
	assert.Equal(t, 2, created.SortOrder)

	rec = doFilterViewRequest(t, r, http.MethodGet, "/filter-views", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var listed []models.FilterView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, created.ID, listed[0].ID)

	rec = doFilterViewRequest(t, r, http.MethodPut, fmt.Sprintf("/filter-views/%d", created.ID), `{"name":"Shows","filters":{},"sortOrder":0}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var updated models.FilterView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	assert.Equal(t, "Shows", updated.Name)

	rec = doFilterViewRequest(t, r, http.MethodDelete, fmt.Sprintf("/filter-views/%d", created.ID), "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())

	rec = doFilterViewRequest(t, r, http.MethodGet, "/filter-views", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `[]`, rec.Body.String(), "empty list must serialize as [] not null")
}

func TestFilterViewHandlerCreateValidation(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{name: "not json", body: `nope`, wantCode: http.StatusBadRequest},
		{name: "missing name", body: `{"filters":{}}`, wantCode: http.StatusBadRequest},
		{name: "blank name", body: `{"name":"   ","filters":{}}`, wantCode: http.StatusBadRequest},
		{name: "name too long", body: fmt.Sprintf(`{"name":%q,"filters":{}}`, strings.Repeat("a", 101)), wantCode: http.StatusBadRequest},
		{name: "name at limit", body: fmt.Sprintf(`{"name":%q,"filters":{}}`, strings.Repeat("a", 100)), wantCode: http.StatusCreated},
		{name: "missing filters", body: `{"name":"x"}`, wantCode: http.StatusBadRequest},
		{name: "filters not an object", body: `{"name":"y","filters":[1,2]}`, wantCode: http.StatusBadRequest},
		{name: "filters null", body: `{"name":"z","filters":null}`, wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newFilterViewRouter(t)
			rec := doFilterViewRequest(t, r, http.MethodPost, "/filter-views", tt.body)
			assert.Equal(t, tt.wantCode, rec.Code, rec.Body.String())
		})
	}
}

func TestFilterViewHandlerDuplicateNameConflicts(t *testing.T) {
	r := newFilterViewRouter(t)

	rec := doFilterViewRequest(t, r, http.MethodPost, "/filter-views", `{"name":"dupe","filters":{}}`)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = doFilterViewRequest(t, r, http.MethodPost, "/filter-views", `{"name":"dupe","filters":{}}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestFilterViewHandlerUnknownAndBadIDs(t *testing.T) {
	r := newFilterViewRouter(t)

	rec := doFilterViewRequest(t, r, http.MethodPut, "/filter-views/999", `{"name":"x","filters":{}}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = doFilterViewRequest(t, r, http.MethodDelete, "/filter-views/999", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = doFilterViewRequest(t, r, http.MethodDelete, "/filter-views/abc", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = doFilterViewRequest(t, r, http.MethodPut, "/filter-views/0", `{"name":"x","filters":{}}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
