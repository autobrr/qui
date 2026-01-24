// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCORSPreflightBypassesAuth(t *testing.T) {
	deps := newTestDependencies(t)

	server := NewServer(deps)
	router, err := server.Handler()
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodOptions, "/api/auth/me", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSAllowsXRequestedWithHeader(t *testing.T) {
	deps := newTestDependencies(t)

	server := NewServer(deps)
	router, err := server.Handler()
	require.NoError(t, err)

	// Preflight request asking if X-Requested-With is allowed
	// (browsers send this header in lowercase)
	req := httptest.NewRequest(http.MethodOptions, "/api/auth/me", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "x-requested-with")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Basic CORS preflight should work
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))

	// rs/cors echoes back allowed headers (normalized to lowercase)
	allowedHeaders := strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers"))
	require.Contains(t, allowedHeaders, "x-requested-with",
		"CORS should allow X-Requested-With header for SSO proxy compatibility")
}
