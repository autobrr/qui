// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"

	"github.com/autobrr/qui/internal/domain"
)

const (
	// Bounds each issuer candidate, so a provider host that drops packets cannot
	// hold a login request for the 30s dial timeout.
	oidcDiscoveryTimeout = 5 * time.Second
	// A failed discovery answers every request for this long, so reloading the
	// login page does not send a request to a provider that is down each time.
	oidcDiscoveryCooldown = 10 * time.Second
)

var (
	oidcNewProvider = oidc.NewProvider
	oidcReadRandom  = func(b []byte) error {
		_, err := io.ReadFull(rand.Reader, b)
		return err
	}
)

var errOIDCProviderUnavailable = errors.New("OIDC provider unavailable")

type OIDCHandler struct {
	config         *domain.Config
	sessionManager *scs.SessionManager
	discovery      *oidcDiscovery
}

// oidcClient holds what qui learns from provider discovery.
type oidcClient struct {
	provider    *oidc.Provider
	verifier    *oidc.IDTokenVerifier
	oauthConfig *oauth2.Config
}

// oidcDiscovery runs provider discovery when a request needs it, until one
// attempt succeeds. Concurrent callers share one attempt, and a failed attempt
// answers callers until oidcDiscoveryCooldown passes. qui then starts with the
// provider down, and the first login after the provider comes back succeeds.
type oidcDiscovery struct {
	discover func(context.Context) (*oidcClient, error)

	mu      sync.Mutex
	client  *oidcClient
	attempt *oidcDiscoveryAttempt
}

type oidcDiscoveryAttempt struct {
	done     chan struct{}
	client   *oidcClient
	err      error
	finished time.Time
}

// newOIDCDiscovery starts the first attempt at once, so the first login does not wait for it.
func newOIDCDiscovery(ctx context.Context, discover func(context.Context) (*oidcClient, error)) *oidcDiscovery {
	d := &oidcDiscovery{discover: discover}
	d.attempt = d.startAttempt(ctx)
	return d
}

func (d *oidcDiscovery) get(ctx context.Context) (*oidcClient, error) {
	d.mu.Lock()
	if d.client != nil {
		client := d.client
		d.mu.Unlock()
		return client, nil
	}
	attempt := d.attempt
	if attempt.cooledDown() {
		attempt = d.startAttempt(ctx)
		d.attempt = attempt
	}
	d.mu.Unlock()

	select {
	case <-attempt.done:
		return attempt.client, attempt.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *oidcDiscovery) startAttempt(ctx context.Context) *oidcDiscoveryAttempt {
	attempt := &oidcDiscoveryAttempt{done: make(chan struct{})}
	// Detached: a caller that gives up must not cancel the attempt that other callers wait on.
	ctx = context.WithoutCancel(ctx)
	go func() {
		client, err := d.discover(ctx)
		if client != nil {
			d.mu.Lock()
			d.client = client
			d.mu.Unlock()
		}
		attempt.client, attempt.err, attempt.finished = client, err, time.Now()
		close(attempt.done)
	}()
	return attempt
}

func (a *oidcDiscoveryAttempt) cooledDown() bool {
	select {
	case <-a.done:
		return time.Since(a.finished) >= oidcDiscoveryCooldown
	default:
		return false
	}
}

// OIDCClaims represents the claims returned from the OIDC provider
type OIDCClaims struct {
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	GivenName         string `json:"given_name"`
	Nickname          string `json:"nickname"`
	Sub               string `json:"sub"`
	Picture           string `json:"picture"`
}

// OIDCConfigResponse represents the OIDC configuration response
type OIDCConfigResponse struct {
	Enabled             bool   `json:"enabled"`
	AuthorizationURL    string `json:"authorizationUrl"`
	DisableBuiltInLogin bool   `json:"disableBuiltInLogin"`
	IssuerURL           string `json:"issuerUrl"`
}

func NewOIDCHandler(cfg *domain.Config, sessionManager *scs.SessionManager) (*OIDCHandler, error) {
	log.Debug().
		Bool("oidc_enabled", cfg.OIDCEnabled).
		Str("oidc_issuer", cfg.OIDCIssuer).
		Str("oidc_client_id", cfg.OIDCClientID).
		Str("oidc_redirect_url", cfg.OIDCRedirectURL).
		Msg("initializing OIDC handler with config")

	if cfg.OIDCIssuer == "" {
		log.Error().Msg("OIDC issuer is empty")
		return nil, errors.New("OIDC issuer is required")
	}

	if cfg.OIDCClientID == "" {
		log.Error().Msg("OIDC client ID is empty")
		return nil, errors.New("OIDC client ID is required")
	}

	if cfg.OIDCClientSecret == "" {
		log.Error().Msg("OIDC client secret is empty")
		return nil, errors.New("OIDC client secret is required")
	}

	if cfg.OIDCRedirectURL == "" {
		log.Error().Msg("OIDC redirect URL is empty")
		return nil, errors.New("OIDC redirect URL is required")
	}

	handler := &OIDCHandler{
		config:         cfg,
		sessionManager: sessionManager,
	}
	handler.discovery = newOIDCDiscovery(context.Background(), handler.discoverClient)

	return handler, nil
}

func (h *OIDCHandler) discoverClient(ctx context.Context) (*oidcClient, error) {
	cfg := h.config
	provider, usedIssuer, err := discoverOIDCProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		log.Warn().Err(err).Str("issuer", cfg.OIDCIssuer).Msg("OIDC provider discovery failed; the next OIDC login request retries it")
		return nil, err
	}

	if usedIssuer != cfg.OIDCIssuer {
		log.Trace().
			Str("requested_issuer", cfg.OIDCIssuer).
			Str("resolved_issuer", usedIssuer).
			Msg("initialized OIDC provider using alternate issuer formatting")
	}

	var claims struct {
		AuthURL  string `json:"authorization_endpoint"`
		TokenURL string `json:"token_endpoint"`
		JWKSURL  string `json:"jwks_uri"`
		UserURL  string `json:"userinfo_endpoint"`
	}
	if err := provider.Claims(&claims); err != nil {
		log.Warn().Err(err).Msg("failed to parse provider claims for endpoints")
	} else {
		log.Trace().
			Str("authorization_endpoint", claims.AuthURL).
			Str("token_endpoint", claims.TokenURL).
			Str("jwks_uri", claims.JWKSURL).
			Str("userinfo_endpoint", claims.UserURL).
			Msg("discovered OIDC provider endpoints")
	}

	oidcConfig := &oidc.Config{
		ClientID: cfg.OIDCClientID,
	}

	log.Debug().Str("issuer", usedIssuer).Msg("OIDC provider discovered")
	return &oidcClient{
		provider: provider,
		verifier: provider.Verifier(oidcConfig),
		oauthConfig: &oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			RedirectURL:  cfg.OIDCRedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{"openid", "profile", "email"},
		},
	}, nil
}

func (h *OIDCHandler) Routes(r chi.Router) {
	// Re-apply throttling because chi.Route creates a fresh middleware stack.
	r.Use(middleware.ThrottleBacklog(1, 1, time.Second))
	r.Get("/config", h.getConfig)
	r.Get("/callback", h.handleCallback)
}

func discoverOIDCProvider(ctx context.Context, issuer string) (*oidc.Provider, string, error) {
	candidates := []string{issuer}
	if strings.HasSuffix(issuer, "/") {
		candidates = append(candidates, strings.TrimRight(issuer, "/"))
	} else {
		candidates = append(candidates, issuer+"/")
	}

	var lastErr error
	for _, candidate := range candidates {
		candidateCtx, cancel := context.WithTimeout(ctx, oidcDiscoveryTimeout)
		// go-oidc keeps only the HTTP client from this context, so the provider stays usable after cancel.
		provider, err := oidcNewProvider(candidateCtx, candidate)
		cancel()
		if err == nil {
			return provider, candidate, nil
		}

		lastErr = err
		log.Debug().Err(err).Str("issuer", candidate).Msg("failed to discover OIDC provider candidate")
	}

	return nil, "", lastErr
}

func (h *OIDCHandler) getConfig(w http.ResponseWriter, r *http.Request) {
	// Clear any prior authorization state before generating a new config response.
	h.sessionManager.Remove(r.Context(), "oidc_state")
	h.sessionManager.Remove(r.Context(), "oidc_pkce_verifier")

	// Get the config first
	config, state, pkceVerifier, err := h.GetConfigResponse(r.Context())
	if errors.Is(err, errOIDCProviderUnavailable) {
		// The login page reads disableBuiltInLogin from this body to keep the password form hidden.
		RespondJSON(w, http.StatusServiceUnavailable, oidcUnavailableResponse{
			Error:               errOIDCProviderUnavailable.Error(),
			DisableBuiltInLogin: h.config.OIDCDisableBuiltInLogin,
		})
		return
	}
	if err != nil {
		log.Error().Err(err).Msg("failed to build OIDC config response")
		RespondError(w, http.StatusInternalServerError, "failed to build OIDC config response")
		return
	}

	// Store state and PKCE verifier in session for later validation
	// This is needed even if user is already authenticated, in case they're re-authenticating
	h.sessionManager.Put(r.Context(), "oidc_state", state)
	if pkceVerifier != "" {
		h.sessionManager.Put(r.Context(), "oidc_pkce_verifier", pkceVerifier)
	}

	RespondJSON(w, http.StatusOK, config)
}

func (h *OIDCHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	log.Debug().Msg("handling OIDC callback")

	// Get and validate state parameter
	expectedState := h.sessionManager.GetString(r.Context(), "oidc_state")
	if expectedState == "" {
		log.Error().Msg("no state found in session")
		RespondError(w, http.StatusBadRequest, "invalid state: no state found in session")
		return
	}

	actualState := r.URL.Query().Get("state")
	if actualState != expectedState {
		log.Error().
			Str("expected", expectedState).
			Str("got", actualState).
			Msg("state did not match")
		RespondError(w, http.StatusBadRequest, "invalid state: state mismatch")
		return
	}

	// Clear the state from session after successful validation
	h.sessionManager.Remove(r.Context(), "oidc_state")

	// Retrieve and clear the PKCE verifier stored during the authorization request
	pkceVerifier := h.sessionManager.GetString(r.Context(), "oidc_pkce_verifier")
	h.sessionManager.Remove(r.Context(), "oidc_pkce_verifier")

	code := r.URL.Query().Get("code")
	if code == "" {
		log.Error().Msg("authorization code is missing from callback request")
		RespondError(w, http.StatusBadRequest, "authorization code is missing from callback request")
		return
	}

	var exchangeOpts []oauth2.AuthCodeOption
	if pkceVerifier != "" {
		exchangeOpts = append(exchangeOpts, oauth2.VerifierOption(pkceVerifier))
	}

	client, err := h.discovery.get(r.Context())
	if err != nil {
		RespondError(w, http.StatusServiceUnavailable, errOIDCProviderUnavailable.Error())
		return
	}

	oauth2Token, err := client.oauthConfig.Exchange(r.Context(), code, exchangeOpts...)
	if err != nil {
		log.Error().Err(err).Msg("failed to exchange token")
		RespondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to exchange token: %v", err))
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		log.Error().Msg("no id_token found in oauth2 token")
		RespondError(w, http.StatusInternalServerError, "no id_token found in oauth2 token")
		return
	}

	idToken, err := client.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		log.Error().Err(err).Msg("failed to verify ID Token")
		RespondError(w, http.StatusUnauthorized, fmt.Sprintf("failed to verify ID Token: %v", err))
		return
	}

	var claims OIDCClaims
	if err := idToken.Claims(&claims); err != nil {
		log.Error().Err(err).Msg("failed to parse claims from ID token")
		RespondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to parse claims from ID token: %v", err))
		return
	}

	// Try to get additional claims from userinfo endpoint
	userInfo, err := client.provider.UserInfo(r.Context(), oauth2.StaticTokenSource(oauth2Token))
	if err != nil {
		log.Error().Err(err).Msg("failed to get userinfo")
	}

	if userInfo != nil {
		var userInfoClaims struct {
			Email             string `json:"email"`
			PreferredUsername string `json:"preferred_username"`
			Name              string `json:"name"`
			Nickname          string `json:"nickname"`
			Picture           string `json:"picture"`
		}
		if err := userInfo.Claims(&userInfoClaims); err != nil {
			log.Warn().Err(err).Msg("failed to parse claims from userinfo endpoint, proceeding with ID token claims if available")
		} else {
			log.Trace().
				Str("userinfo_email", userInfoClaims.Email).
				Str("userinfo_username", userInfoClaims.PreferredUsername).
				Str("userinfo_name", userInfoClaims.Name).
				Str("userinfo_nickname", userInfoClaims.Nickname).
				Msg("successfully parsed claims from userinfo endpoint")

			// Merge userinfo claims with ID token claims
			if userInfoClaims.Email != "" {
				claims.Email = userInfoClaims.Email
			}
			if userInfoClaims.PreferredUsername != "" {
				claims.PreferredUsername = userInfoClaims.PreferredUsername
			}
			if userInfoClaims.Name != "" {
				claims.Name = userInfoClaims.Name
			}
			if userInfoClaims.Nickname != "" {
				claims.Nickname = userInfoClaims.Nickname
			}
			if userInfoClaims.Picture != "" {
				claims.Picture = userInfoClaims.Picture
			}
		}
	}

	// Determine username from claims
	username := claims.PreferredUsername
	if username == "" {
		if claims.Nickname != "" {
			username = claims.Nickname
		} else if claims.Name != "" {
			username = claims.Name
		} else if claims.Email != "" {
			username = claims.Email
		} else if claims.Sub != "" {
			username = claims.Sub
		} else {
			username = "oidc_user"
		}
	}

	log.Trace().
		Str("email", claims.Email).
		Str("preferred_username", claims.PreferredUsername).
		Str("nickname", claims.Nickname).
		Str("name", claims.Name).
		Str("sub", claims.Sub).
		Msg("successfully processed OIDC claims")

	// Create new session
	if err := renewSessionToken(r.Context(), h.sessionManager); err != nil {
		log.Error().Err(err).Msgf("Auth: Failed to renew session token for username: [%s] ip: %s", username, r.RemoteAddr)
		RespondError(w, http.StatusInternalServerError, "could not renew session token")
		return
	}

	// Set session values using sessionManager
	h.sessionManager.Put(r.Context(), "authenticated", true)
	h.sessionManager.Put(r.Context(), "username", username)
	h.sessionManager.Put(r.Context(), "created", time.Now().Unix())
	h.sessionManager.Put(r.Context(), "auth_method", "oidc")
	h.sessionManager.Put(r.Context(), "profile_picture", claims.Picture)
	h.sessionManager.RememberMe(r.Context(), true)

	// Redirect to the frontend
	// Prefer relative URLs for security, but support absolute URLs for development
	frontendURL := "/dashboard" // Safe default

	// Only construct absolute URLs if explicitly configured
	if h.config.OIDCRedirectURL != "" {
		// Parse the redirect URL to get the frontend base
		if idx := strings.Index(h.config.OIDCRedirectURL, "/api/auth/oidc/callback"); idx > 0 {
			candidateURL := h.config.OIDCRedirectURL[:idx] + "/dashboard"

			// Validate the URL to ensure it's pointing to our application
			if isValidRedirectURL(candidateURL, h.config.OIDCRedirectURL) {
				frontendURL = candidateURL
			}
		}
	} else if h.config.BaseURL != "" && h.config.BaseURL != "/" {
		// Use configured base URL
		base := strings.TrimSuffix(h.config.BaseURL, "/")
		if base == "" {
			base = "/"
		}
		frontendURL = base + "/dashboard"
	}

	log.Trace().
		Str("redirect_url", frontendURL).
		Str("oidc_redirect_url", h.config.OIDCRedirectURL).
		Str("base_url", h.config.BaseURL).
		Str("x_forwarded_proto", r.Header.Get("X-Forwarded-Proto")).
		Str("x_forwarded_host", r.Header.Get("X-Forwarded-Host")).
		Str("host", r.Host).
		Msg("redirecting to frontend after OIDC callback")

	http.Redirect(w, r, frontendURL, http.StatusFound)
}

// isValidRedirectURL validates that the redirect URL is safe
// For a self-hosted application, we ensure the URL points to the same host as configured
func isValidRedirectURL(candidateURL, configuredURL string) bool {
	// Parse both URLs
	candidate, err := url.Parse(candidateURL)
	if err != nil {
		return false
	}

	configured, err := url.Parse(configuredURL)
	if err != nil {
		return false
	}

	// Ensure the host matches our configured redirect URL
	// This prevents open redirects to arbitrary domains
	return candidate.Host == configured.Host
}

func generateRandomState() (string, error) {
	b := make([]byte, 32)
	if err := oidcReadRandom(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generatePKCEVerifier() (string, error) {
	b := make([]byte, 32)
	if err := oidcReadRandom(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (c *oidcClient) supportsPKCE() bool {
	var claims struct {
		CodeChallenges []string `json:"code_challenge_methods_supported"`
	}
	if err := c.provider.Claims(&claims); err != nil {
		return false
	}
	return slices.Contains(claims.CodeChallenges, "S256")
}

type oidcUnavailableResponse struct {
	Error               string `json:"error"`
	DisableBuiltInLogin bool   `json:"disableBuiltInLogin"`
}

// GetConfigResponse returns errOIDCProviderUnavailable while provider discovery has not succeeded.
func (h *OIDCHandler) GetConfigResponse(ctx context.Context) (OIDCConfigResponse, string, string, error) {
	client, err := h.discovery.get(ctx)
	if err != nil {
		return OIDCConfigResponse{}, "", "", fmt.Errorf("%w: %w", errOIDCProviderUnavailable, err)
	}

	state, err := generateRandomState()
	if err != nil {
		return OIDCConfigResponse{}, "", "", err
	}

	var authURL, verifier string
	if client.supportsPKCE() {
		verifier, err = generatePKCEVerifier()
		if err != nil {
			return OIDCConfigResponse{}, "", "", err
		}
		authURL = client.oauthConfig.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	} else {
		authURL = client.oauthConfig.AuthCodeURL(state)
	}

	return OIDCConfigResponse{
		Enabled:             h.config.OIDCEnabled,
		AuthorizationURL:    authURL,
		DisableBuiltInLogin: h.config.OIDCDisableBuiltInLogin,
		IssuerURL:           h.config.OIDCIssuer,
	}, state, verifier, nil
}
