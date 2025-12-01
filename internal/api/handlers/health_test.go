// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHealthHandler(t *testing.T) {
	t.Parallel()

	h := NewHealthHandler()
	require.NotNil(t, h)
}

func TestHealthHandler_Routes(t *testing.T) {
	t.Parallel()

	h := NewHealthHandler()
	r := chi.NewRouter()
	h.Routes(r)

	// Verify routes are registered
	routes := r.Routes()
	assert.NotEmpty(t, routes)
}

func TestHealthHandler_HandleHealth(t *testing.T) {
	t.Parallel()

	h := NewHealthHandler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.HandleHealth(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp["status"])
}

func TestHealthHandler_HandleReady(t *testing.T) {
	t.Parallel()

	h := NewHealthHandler()

	req := httptest.NewRequest(http.MethodGet, "/readiness", nil)
	rec := httptest.NewRecorder()

	h.HandleReady(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "ready", resp["status"])
}

func TestHealthHandler_HandleLiveness(t *testing.T) {
	t.Parallel()

	h := NewHealthHandler()

	req := httptest.NewRequest(http.MethodGet, "/liveness", nil)
	rec := httptest.NewRecorder()

	h.HandleLiveness(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "alive", resp["status"])
}

func TestHealthHandler_Integration(t *testing.T) {
	t.Parallel()

	h := NewHealthHandler()
	r := chi.NewRouter()
	r.Route("/health", h.Routes)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   map[string]string
	}{
		{
			name:           "readiness endpoint",
			path:           "/health/readiness",
			expectedStatus: http.StatusOK,
			expectedBody:   map[string]string{"status": "ready"},
		},
		{
			name:           "liveness endpoint",
			path:           "/health/liveness",
			expectedStatus: http.StatusOK,
			expectedBody:   map[string]string{"status": "alive"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)

			var resp map[string]string
			err := json.NewDecoder(rec.Body).Decode(&resp)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedBody, resp)
		})
	}
}
