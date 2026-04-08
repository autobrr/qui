// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/domain"
)

func newOIDCDiscoveryServer(t *testing.T, codeChallenges []string) *httptest.Server {
	var discovery *httptest.Server
	discovery = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		payload := map[string]any{
			"issuer":                 discovery.URL,
			"authorization_endpoint": discovery.URL + "/authorize",
			"token_endpoint":         discovery.URL + "/token",
			"userinfo_endpoint":      discovery.URL + "/userinfo",
			"jwks_uri":               discovery.URL + "/jwks",
		}
		if codeChallenges != nil {
			payload["code_challenge_methods_supported"] = codeChallenges
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encode OIDC discovery response: %v", err)
			http.Error(w, "failed to encode discovery response", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(discovery.Close)
	return discovery
}

func newTestOIDCHandler(t *testing.T, issuer string) *OIDCHandler {
	t.Helper()

	// #nosec G101 -- test-only OIDC client config values.
	cfg := &domain.Config{
		OIDCEnabled:     true,
		OIDCIssuer:      issuer,
		OIDCClientID:    "client-id",
		OIDCRedirectURL: "http://localhost/callback",
	}
	// #nosec G101 -- test-only placeholder used to satisfy OIDC config validation.
	cfg.OIDCClientSecret = "placeholder"

	handler, err := NewOIDCHandler(cfg, scs.New())
	require.NoError(t, err)

	return handler
}

func TestOIDCConfigDoesNotExposePKCEVerifier(t *testing.T) {
	discovery := newOIDCDiscoveryServer(t, []string{"S256"})
	handler := newTestOIDCHandler(t, discovery.URL)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/oidc/config", nil)
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
	assert.NotContains(t, rec.Body.String(), `"pkceVerifier"`)
}

func TestOIDCConfigReturnsInternalServerErrorWhenStateGenerationFails(t *testing.T) {
	discovery := newOIDCDiscoveryServer(t, nil)
	handler := newTestOIDCHandler(t, discovery.URL)

	originalReadRandom := oidcReadRandom
	oidcReadRandom = func(_ []byte) error {
		return errors.New("entropy unavailable")
	}
	t.Cleanup(func() {
		oidcReadRandom = originalReadRandom
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/oidc/config", nil)
	ctx, err := handler.sessionManager.Load(req.Context(), "")
	require.NoError(t, err)
	req = req.WithContext(ctx)
	handler.sessionManager.Put(req.Context(), "oidc_state", "stale-state")
	handler.sessionManager.Put(req.Context(), "oidc_pkce_verifier", "stale-verifier")

	rec := httptest.NewRecorder()
	handler.getConfig(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to generate OIDC state")
	assert.Empty(t, handler.sessionManager.GetString(req.Context(), "oidc_state"))
	assert.Empty(t, handler.sessionManager.GetString(req.Context(), "oidc_pkce_verifier"))
}
