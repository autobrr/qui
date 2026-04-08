// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/domain"
)

type discoveryDocument struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	UserInfoEndpoint              string   `json:"userinfo_endpoint"`
	JWKSURI                       string   `json:"jwks_uri"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported,omitempty"`
}

func newOIDCDiscoveryServer(t *testing.T, codeChallenges []string) *httptest.Server {
	var discovery *httptest.Server
	discovery = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		doc := discoveryDocument{
			Issuer:                discovery.URL,
			AuthorizationEndpoint: discovery.URL + "/authorize",
			TokenEndpoint:         discovery.URL + "/token",
			UserInfoEndpoint:      discovery.URL + "/userinfo",
			JWKSURI:               discovery.URL + "/jwks",
		}
		if codeChallenges != nil {
			doc.CodeChallengeMethodsSupported = codeChallenges
		}
		if err := json.NewEncoder(w).Encode(doc); err != nil {
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
	rec := httptest.NewRecorder()
	handler.sessionManager.LoadAndSave(http.HandlerFunc(handler.getConfig)).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body OIDCConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotContains(t, rec.Body.String(), `"state"`)

	var sessionCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == handler.sessionManager.Cookie.Name {
			sessionCookie = cookie
			break
		}
	}
	require.NotNil(t, sessionCookie)

	secondReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/oidc/config", nil)
	secondReq.AddCookie(sessionCookie)
	ctx, err := handler.sessionManager.Load(secondReq.Context(), sessionCookie.Value)
	require.NoError(t, err)

	storedVerifier := handler.sessionManager.GetString(ctx, "oidc_pkce_verifier")
	assert.NotEmpty(t, storedVerifier)
	storedState := handler.sessionManager.GetString(ctx, "oidc_state")
	assert.NotEmpty(t, storedState)

	authURL, err := url.Parse(body.AuthorizationURL)
	require.NoError(t, err)
	assert.Equal(t, storedState, authURL.Query().Get("state"))

	assert.Contains(t, body.AuthorizationURL, "code_challenge=")
	assert.NotContains(t, body.AuthorizationURL, storedVerifier)
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
	assert.Contains(t, rec.Body.String(), "failed to build OIDC config response")
	assert.Empty(t, handler.sessionManager.GetString(req.Context(), "oidc_state"))
	assert.Empty(t, handler.sessionManager.GetString(req.Context(), "oidc_pkce_verifier"))
}

func TestGeneratePKCEVerifierReturnsErrorWhenRandomReadFails(t *testing.T) {
	originalReadRandom := oidcReadRandom
	oidcReadRandom = func(_ []byte) error {
		return errors.New("entropy unavailable")
	}
	t.Cleanup(func() {
		oidcReadRandom = originalReadRandom
	})

	verifier, err := generatePKCEVerifier()
	require.Error(t, err)
	assert.Empty(t, verifier)
}

func TestGeneratePKCEVerifierEncodesRandomBytes(t *testing.T) {
	originalReadRandom := oidcReadRandom
	oidcReadRandom = func(b []byte) error {
		for i := range b {
			b[i] = byte(i)
		}
		return nil
	}
	t.Cleanup(func() {
		oidcReadRandom = originalReadRandom
	})

	verifier, err := generatePKCEVerifier()
	require.NoError(t, err)
	assert.Equal(t, base64.RawURLEncoding.EncodeToString(func() []byte {
		b := make([]byte, 32)
		for i := range b {
			b[i] = byte(i)
		}
		return b
	}()), verifier)
}

func TestOIDCConfigReturnsInternalServerErrorWhenPKCEVerifierGenerationFails(t *testing.T) {
	discovery := newOIDCDiscoveryServer(t, []string{"S256"})
	handler := newTestOIDCHandler(t, discovery.URL)

	originalReadRandom := oidcReadRandom
	readCalls := 0
	oidcReadRandom = func(b []byte) error {
		readCalls++
		if readCalls == 1 {
			for i := range b {
				b[i] = byte(i)
			}
			return nil
		}
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
	assert.Contains(t, rec.Body.String(), "failed to build OIDC config response")
	assert.Empty(t, handler.sessionManager.GetString(req.Context(), "oidc_state"))
	assert.Empty(t, handler.sessionManager.GetString(req.Context(), "oidc_pkce_verifier"))
}
