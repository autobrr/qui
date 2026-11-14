// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/domain"
)

func stubOIDCNewProvider(t *testing.T, fn func(context.Context, string) (*oidc.Provider, error)) {
	t.Helper()
	original := oidcNewProvider
	oidcNewProvider = fn
	t.Cleanup(func() { oidcNewProvider = original })
}

func newOIDCOnlyConfig() *domain.Config {
	// Test-only OIDC client config values.
	cfg := &domain.Config{
		OIDCEnabled:             true,
		OIDCIssuer:              "https://issuer.example.com",
		OIDCClientID:            "client-id",
		OIDCRedirectURL:         "http://localhost/callback",
		OIDCDisableBuiltInLogin: true,
	}
	cfg.OIDCClientSecret = "placeholder"
	return cfg
}

// regression: #3404
func TestNewOIDCHandlerStartsWhileProviderIsDown(t *testing.T) {
	stubOIDCNewProvider(t, func(context.Context, string) (*oidc.Provider, error) {
		return nil, errors.New("connection refused")
	})

	handler, err := NewOIDCHandler(newOIDCOnlyConfig(), scs.New())
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/oidc/config", nil)
	rec := httptest.NewRecorder()
	handler.sessionManager.LoadAndSave(http.HandlerFunc(handler.getConfig)).ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var body oidcUnavailableResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.True(t, body.DisableBuiltInLogin)
	assert.NotContains(t, rec.Body.String(), "connection refused")
}

// regression: #3860
func TestOIDCDiscoveryReadsPublishedConfig(t *testing.T) {
	var requestedIssuer string
	stubOIDCNewProvider(t, func(ctx context.Context, issuer string) (*oidc.Provider, error) {
		requestedIssuer = issuer
		return (&oidc.ProviderConfig{IssuerURL: issuer}).NewProvider(ctx), nil
	})

	cfg := newOIDCOnlyConfig()
	cfg.Publish()
	// A reload writes these fields before it publishes them.
	cfg.OIDCIssuer = "https://reload.example.invalid"
	cfg.OIDCClientID = "client-id-from-reload"

	client, err := (&OIDCHandler{config: cfg}).discoverClient(t.Context())
	require.NoError(t, err)

	assert.Equal(t, "https://issuer.example.com", requestedIssuer)
	assert.Equal(t, "client-id", client.oauthConfig.ClientID)
}

// regression: #3404
func TestOIDCConfigAnswersWithinWaitWhileDiscoveryHangs(t *testing.T) {
	// Outside the bubble: the memstore cleanup goroutine ticks forever.
	sessionManager := scs.New()
	synctest.Test(t, func(t *testing.T) {
		stubOIDCNewProvider(t, func(ctx context.Context, _ string) (*oidc.Provider, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
		handler, err := NewOIDCHandler(newOIDCOnlyConfig(), sessionManager)
		require.NoError(t, err)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/oidc/config", nil)
		ctx, err := sessionManager.Load(req.Context(), "")
		require.NoError(t, err)
		rec := httptest.NewRecorder()

		start := time.Now()
		handler.getConfig(rec, req.WithContext(ctx))
		assert.Equal(t, oidcDiscoveryWait, time.Since(start))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

		// Let the attempt finish before the stub cleanup restores oidcNewProvider.
		_, err = handler.discovery.get(context.Background())
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

// regression: #3404
func TestOIDCCallbackKeepsStateWhileProviderIsUnavailable(t *testing.T) {
	stubOIDCNewProvider(t, func(context.Context, string) (*oidc.Provider, error) {
		return nil, errors.New("connection refused")
	})
	handler, err := NewOIDCHandler(newOIDCOnlyConfig(), scs.New())
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/oidc/callback?state=synthetic-state&code=synthetic-code", nil)
	ctx, err := handler.sessionManager.Load(req.Context(), "")
	require.NoError(t, err)
	handler.sessionManager.Put(ctx, "oidc_state", "synthetic-state")
	handler.sessionManager.Put(ctx, "oidc_pkce_verifier", "synthetic-verifier")

	rec := httptest.NewRecorder()
	handler.handleCallback(rec, req.WithContext(ctx))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "synthetic-state", handler.sessionManager.GetString(ctx, "oidc_state"))
	assert.Equal(t, "synthetic-verifier", handler.sessionManager.GetString(ctx, "oidc_pkce_verifier"))
}

// discoverOIDCProvider cancels its context, and go-oidc fetches signing keys on the first Verify.
func TestDiscoveredVerifierFetchesKeysAfterDiscoveryContextEnds(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	var jwksFetches atomic.Int32
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			body = map[string]string{
				"issuer":                 provider.URL,
				"authorization_endpoint": provider.URL + "/authorize",
				"token_endpoint":         provider.URL + "/token",
				"jwks_uri":               provider.URL + "/jwks",
			}
		case "/jwks":
			jwksFetches.Add(1)
			body = map[string]any{"keys": []map[string]string{{
				"kty": "RSA",
				"kid": "test-key",
				"alg": "RS256",
				"use": "sig",
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}}}
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(body))
	}))
	t.Cleanup(provider.Close)

	handler := newTestOIDCHandler(t, provider.URL)
	client, err := handler.discovery.get(t.Context())
	require.NoError(t, err)
	require.Equal(t, int32(0), jwksFetches.Load())

	now := time.Now()
	idToken := signTestIDToken(t, key, map[string]any{
		"iss": provider.URL,
		"aud": "client-id",
		"sub": "synthetic-user",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})
	_, err = client.verifier.Verify(t.Context(), idToken)
	require.NoError(t, err)
	assert.Equal(t, int32(1), jwksFetches.Load())
}

func signTestIDToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	encode := func(v any) string {
		raw, err := json.Marshal(v)
		require.NoError(t, err)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	signingInput := encode(map[string]string{"alg": "RS256", "kid": "test-key", "typ": "JWT"}) + "." + encode(claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	require.NoError(t, err)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func TestOIDCDiscoveryRetriesOnDemandAfterCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		var providerUp atomic.Bool
		want := &oidcClient{}
		discovery := newOIDCDiscovery(t.Context(), func(context.Context) (*oidcClient, error) {
			calls.Add(1)
			if !providerUp.Load() {
				return nil, errors.New("provider down")
			}
			return want, nil
		})

		_, err := discovery.get(t.Context())
		require.Error(t, err)

		providerUp.Store(true)
		_, err = discovery.get(t.Context())
		require.Error(t, err, "a request inside the cooldown reuses the failed attempt")
		assert.Equal(t, int32(1), calls.Load())

		time.Sleep(oidcDiscoveryCooldown)
		got, err := discovery.get(t.Context())
		require.NoError(t, err)
		assert.Same(t, want, got)

		got, err = discovery.get(t.Context())
		require.NoError(t, err)
		assert.Same(t, want, got)
		assert.Equal(t, int32(2), calls.Load(), "discovery stops after the first success")
	})
}

func TestOIDCDiscoveryConcurrentCallersShareOneAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		release := make(chan struct{})
		discovery := newOIDCDiscovery(t.Context(), func(context.Context) (*oidcClient, error) {
			calls.Add(1)
			<-release
			return &oidcClient{}, nil
		})

		results := make(chan *oidcClient, 3)
		for range 3 {
			go func() {
				client, err := discovery.get(t.Context())
				assert.NoError(t, err)
				results <- client
			}()
		}
		synctest.Wait()
		close(release)

		first := <-results
		require.NotNil(t, first)
		assert.Same(t, first, <-results)
		assert.Same(t, first, <-results)
		assert.Equal(t, int32(1), calls.Load())
	})
}

func TestDiscoverOIDCProviderBoundsEachCandidate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stubOIDCNewProvider(t, func(ctx context.Context, _ string) (*oidc.Provider, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})

		start := time.Now()
		provider, _, err := discoverOIDCProvider(t.Context(), "https://issuer.example.com")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Nil(t, provider)
		assert.Equal(t, 2*oidcDiscoveryTimeout, time.Since(start))
	})
}

func TestDiscoverOIDCProviderTrimsTrailingSlash(t *testing.T) {
	issuer := "https://issuer.example.com/"
	trimmed := strings.TrimRight(issuer, "/")

	var issuersTried []string
	stubOIDCNewProvider(t, func(_ context.Context, candidate string) (*oidc.Provider, error) {
		issuersTried = append(issuersTried, candidate)
		if candidate == trimmed {
			return &oidc.Provider{}, nil
		}
		return nil, fmt.Errorf("issuer %s failed", candidate)
	})

	provider, usedIssuer, err := discoverOIDCProvider(context.Background(), issuer)
	require.NoErrorf(t, err, "issuers tried: %v", issuersTried)
	require.NotNil(t, provider)
	assert.Equal(t, trimmed, usedIssuer)
	assert.Equal(t, []string{issuer, trimmed}, issuersTried)
}
