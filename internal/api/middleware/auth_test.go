// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/database"
)

func TestIsAuthenticated_APIKeyHeaderAndUnauthorized(t *testing.T) {
	ctx := t.Context()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	authService := auth.NewService(db)
	sessionManager := scs.New()

	// Create an API key for testing
	apiKeyValue, _, err := authService.CreateAPIKey(ctx, "test-key")
	require.NoError(t, err)

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	authMiddleware := IsAuthenticated(authService, sessionManager)
	// Wrap with session middleware to avoid panic when session is checked
	handler := sessionManager.LoadAndSave(authMiddleware(okHandler))

	tests := []struct {
		name           string
		path           string
		apiKeyQuery    string
		apiKeyHeader   string
		expectedStatus int
	}{
		{
			name:           "endpoint with X-API-Key header",
			path:           "/api/cross-seed/apply",
			apiKeyHeader:   apiKeyValue,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "endpoint without auth",
			path:           "/api/cross-seed/apply",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "endpoint with invalid apikey",
			path:           "/api/cross-seed/apply",
			apiKeyQuery:    "invalid-key",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "query param without middleware is rejected",
			path:           "/api/torrents",
			apiKeyQuery:    apiKeyValue,
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.path
			if tt.apiKeyQuery != "" {
				url += "?apikey=" + tt.apiKeyQuery
			}

			req := httptest.NewRequestWithContext(ctx, http.MethodPost, url, nil)
			if tt.apiKeyHeader != "" {
				req.Header.Set("X-API-Key", tt.apiKeyHeader)
			}

			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)

			assert.Equal(t, tt.expectedStatus, resp.Code, "unexpected status for %s", tt.name)
		})
	}
}
