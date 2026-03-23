// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCORSDisabledByDefault(t *testing.T) {
	deps := newTestDependencies(t)

	server := NewServer(deps)
	router, err := server.Handler()
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/auth/me", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSPreflightAllowsConfiguredOrigin(t *testing.T) {
	deps := newTestDependencies(t)
	deps.Config.Config.CORSAllowedOrigins = []string{"https://example.com"}

	server := NewServer(deps)
	router, err := server.Handler()
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/auth/me", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSPreflightDeniesUnconfiguredOrigin(t *testing.T) {
	deps := newTestDependencies(t)
	deps.Config.Config.CORSAllowedOrigins = []string{"https://allowed.example"}

	server := NewServer(deps)
	router, err := server.Handler()
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/auth/me", nil)
	req.Header.Set("Origin", "https://blocked.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSAllowsXRequestedWithHeaderForConfiguredOrigin(t *testing.T) {
	deps := newTestDependencies(t)
	deps.Config.Config.CORSAllowedOrigins = []string{"https://example.com"}

	server := NewServer(deps)
	router, err := server.Handler()
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/auth/me", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "x-requested-with")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))

	allowedHeaders := strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers"))
	require.Contains(t, allowedHeaders, "x-requested-with",
		"CORS should allow X-Requested-With header for SSO proxy compatibility")
}

func TestCORSPreflightDeniesUnconfiguredRequestHeader(t *testing.T) {
	deps := newTestDependencies(t)
	deps.Config.Config.CORSAllowedOrigins = []string{"https://example.com"}

	server := NewServer(deps)
	router, err := server.Handler()
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/auth/me", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "x-not-allowed")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSPreflightWithCustomBaseURLAndConfiguredOrigin(t *testing.T) {
	deps := newTestDependencies(t)
	deps.Config.Config.BaseURL = "/qui"
	deps.Config.Config.CORSAllowedOrigins = []string{"https://example.com"}

	server := NewServer(deps)
	router, err := server.Handler()
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/qui/api/auth/me", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSGetIncludesHeadersForConfiguredOrigin(t *testing.T) {
	deps := newTestDependencies(t)
	deps.Config.Config.CORSAllowedOrigins = []string{"https://example.com"}

	server := NewServer(deps)
	router, err := server.Handler()
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Origin", "https://example.com")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSGetOmitsHeadersForUnconfiguredOrigin(t *testing.T) {
	deps := newTestDependencies(t)
	deps.Config.Config.CORSAllowedOrigins = []string{"https://allowed.example"}

	server := NewServer(deps)
	router, err := server.Handler()
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Origin", "https://blocked.example")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
}
