// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/domain"
)

func TestOIDCConfigDoesNotExposePKCEVerifier(t *testing.T) {
	var discovery *httptest.Server
	discovery = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"issuer":                           discovery.URL,
			"authorization_endpoint":           discovery.URL + "/authorize",
			"token_endpoint":                   discovery.URL + "/token",
			"userinfo_endpoint":                discovery.URL + "/userinfo",
			"jwks_uri":                         discovery.URL + "/jwks",
			"code_challenge_methods_supported": []string{"S256"},
		}))
	}))
	t.Cleanup(discovery.Close)

	handler, err := NewOIDCHandler(&domain.Config{
		OIDCEnabled:      true,
		OIDCIssuer:       discovery.URL,
		OIDCClientID:     "client-id",
		OIDCClientSecret: "client-secret",
		OIDCRedirectURL:  "http://localhost/callback",
	}, scs.New())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/config", nil)
	ctx, err := handler.sessionManager.Load(req.Context(), "")
	require.NoError(t, err)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.getConfig(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	_, leakedVerifier := body["pkceVerifier"]
	assert.False(t, leakedVerifier)

	storedVerifier := handler.sessionManager.GetString(req.Context(), "oidc_pkce_verifier")
	assert.NotEmpty(t, storedVerifier)

	authURL, ok := body["authorizationUrl"].(string)
	require.True(t, ok)
	assert.Contains(t, authURL, "code_challenge=")
	assert.NotContains(t, authURL, storedVerifier)
	assert.False(t, strings.Contains(rec.Body.String(), `"pkceVerifier"`))
}
