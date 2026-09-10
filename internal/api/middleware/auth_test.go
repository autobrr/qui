// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/api/ctxkeys"
	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestIsAuthenticated_APIKeyHeaderAndSessionForbidden(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "middleware-auth")

	authService := auth.NewService(db)
	sessionManager := scs.New()

	// Create an API key for testing
	apiKeyValue, _, err := authService.CreateAPIKey(ctx, "test-key")
	require.NoError(t, err)

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	authMiddleware := IsAuthenticated(authService, sessionManager, nil)
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
			name:           "endpoint with invalid X-API-Key header",
			path:           "/api/cross-seed/apply",
			apiKeyHeader:   "invalid-key",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "endpoint without auth",
			path:           "/api/cross-seed/apply",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "endpoint with invalid apikey",
			path:           "/api/cross-seed/apply",
			apiKeyQuery:    "invalid-key",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "query param without middleware is rejected",
			path:           "/api/torrents",
			apiKeyQuery:    apiKeyValue,
			expectedStatus: http.StatusForbidden,
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

func TestIsAuthenticated_AuthDisabled(t *testing.T) {
	cfg := &domain.Config{AuthDisabled: true, IAcknowledgeThisIsABadIdea: true}

	var capturedUsername string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUsername, _ = r.Context().Value(ctxkeys.Username).(string)
		w.WriteHeader(http.StatusOK)
	})

	handler := IsAuthenticated(nil, nil, cfg)(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "admin", capturedUsername)
}

func TestRequireSetup_AuthDisabled(t *testing.T) {
	cfg := &domain.Config{AuthDisabled: true, IAcknowledgeThisIsABadIdea: true}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	handler := RequireSetup(nil, cfg)(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "OK", resp.Body.String())
}

func TestRequireSetup_ThemeCatalogAllowedBeforeSetup(t *testing.T) {
	// A non-default status proves the inner handler ran: a bare recorder
	// already reports 200.
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	// A nil auth service proves the allow-list short-circuits before the
	// setup check: any fall-through would panic.
	handler := RequireSetup(nil, &domain.Config{})(inner)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/themes", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusTeapot, resp.Code)
}

func TestIsAuthenticated_AuthDisabledWithoutConfirmation(t *testing.T) {
	// AuthDisabled alone without IAcknowledgeThisIsABadIdea should NOT bypass auth
	cfg := &domain.Config{AuthDisabled: true, IAcknowledgeThisIsABadIdea: false}

	db := testdb.NewMigratedSQLite(t, "middleware-auth")

	authService := auth.NewService(db)
	sessionManager := scs.New()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := sessionManager.LoadAndSave(IsAuthenticated(authService, sessionManager, cfg)(inner))

	req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusForbidden, resp.Code)
}

func TestIsAuthenticatedRequiresRequestHeaderForCookieWrites(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "auth-middleware")
	authService := auth.NewService(db)
	rawKey, _, err := authService.CreateAPIKey(t.Context(), "test")
	require.NoError(t, err)

	sessionManager := scs.New()
	handler := sessionManager.LoadAndSave(
		IsAuthenticated(authService, sessionManager, &domain.Config{})(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		),
	)

	// Mint a session cookie the way login does.
	loginRec := httptest.NewRecorder()
	sessionManager.LoadAndSave(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sessionManager.Put(r.Context(), "authenticated", true)
		sessionManager.Put(r.Context(), "username", "alice")
	})).ServeHTTP(loginRec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login", nil))
	cookies := loginRec.Result().Cookies()
	require.Len(t, cookies, 1)
	cookie := cookies[0]

	tests := []struct {
		name          string
		method        string
		cookie        bool
		apiKey        bool
		requestedWith bool
		want          int
	}{
		{name: "cookie GET without header", method: http.MethodGet, cookie: true, want: http.StatusOK},
		{name: "cookie POST without header", method: http.MethodPost, cookie: true, want: http.StatusForbidden},
		{name: "cookie PUT without header", method: http.MethodPut, cookie: true, want: http.StatusForbidden},
		{name: "cookie PATCH without header", method: http.MethodPatch, cookie: true, want: http.StatusForbidden},
		{name: "cookie DELETE without header", method: http.MethodDelete, cookie: true, want: http.StatusForbidden},
		{name: "cookie POST with header", method: http.MethodPost, cookie: true, requestedWith: true, want: http.StatusOK},
		{name: "API key POST without header", method: http.MethodPost, apiKey: true, want: http.StatusOK},
		{name: "API key and cookie POST without header", method: http.MethodPost, apiKey: true, cookie: true, want: http.StatusOK},
		{name: "no credentials POST", method: http.MethodPost, want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/api/instances", nil)
			if tt.cookie {
				req.AddCookie(cookie)
			}
			if tt.apiKey {
				req.Header.Set("X-API-Key", rawKey)
			}
			if tt.requestedWith {
				req.Header.Set("X-Requested-With", "XMLHttpRequest")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.want, rec.Code)
			if tt.cookie && tt.want == http.StatusForbidden {
				// The frontend treats a bare 403 "Unauthorized" as an expired
				// session and bounces to login; a missing header must not.
				assert.Contains(t, rec.Body.String(), "X-Requested-With")
			}
		})
	}
}
