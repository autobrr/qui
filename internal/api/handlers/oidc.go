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
	// Stays under the 1s backlog timeout of the /auth throttle. A request that
	// waited for the whole attempt would hold the group's single slot, and
	// password logins would fail with 429 while the provider hangs.
	oidcDiscoveryWait = 500 * time.Millisecond
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
	Email             string   `json:"email"`
	EmailVerified     oidcBool `json:"email_verified"`
	PreferredUsername string   `json:"preferred_username"`
	Name              string   `json:"name"`
	GivenName         string   `json:"given_name"`
	Nickname          string   `json:"nickname"`
	Sub               string   `json:"sub"`
	Picture           string   `json:"picture"`
}

// oidcBool decodes a boolean claim that some providers send as the string "true".
type oidcBool bool

func (b *oidcBool) UnmarshalJSON(data []byte) error {
	*b = string(data) == "true" || string(data) == `"true"`
	return nil
}

// VerifiedEmail returns the email only when the provider marks it as verified.
func (c OIDCClaims) VerifiedEmail() string {
	if !c.EmailVerified {
		return ""
	}
	return c.Email
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

// client waits at most oidcDiscoveryWait. A slower attempt keeps running, and a later request gets its result.
func (h *OIDCHandler) client(ctx context.Context) (*oidcClient, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcDiscoveryWait)
	defer cancel()
	return h.discovery.get(ctx)
}

func (h *OIDCHandler) discoverClient(ctx context.Context) (*oidcClient, error) {
	cfg := h.config.Live()
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
			DisableBuiltInLogin: h.config.Live().OIDCDisableBuiltInLogin,
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

	// Runs before the one-time state and PKCE verifier are consumed, so after a qui
	// restart the user can reload this callback once discovery succeeds.
	client, err := h.client(r.Context())
	if err != nil {
		RespondError(w, http.StatusServiceUnavailable, errOIDCProviderUnavailable.Error())
		return
	}

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
	userInfo, userInfoErr := client.provider.UserInfo(r.Context(), oauth2.StaticTokenSource(oauth2Token))
	if userInfoErr != nil {
		log.Error().Err(userInfoErr).Msg("failed to get userinfo")
	}

	// OIDC Core 5.3.4: user info for another subject must not be used, or its
	// verified email could pass oidcAllowedEmail for this login.
	if userInfo != nil && userInfo.Subject != claims.Sub {
		log.Warn().Str("sub", claims.Sub).Str("userinfo_sub", userInfo.Subject).Msg("userinfo subject does not match ID token subject, ignoring userinfo")
		userInfo = nil
	}

	if userInfo != nil {
		mergeUserInfoClaims(&claims, userInfo)
	}

	username := oidcUsername(claims)

	log.Trace().
		Str("email", claims.Email).
		Str("preferred_username", claims.PreferredUsername).
		Str("nickname", claims.Nickname).
		Str("name", claims.Name).
		Str("sub", claims.Sub).
		Msg("successfully processed OIDC claims")

	cfg := h.config.Live()
	if !cfg.IsOIDCUserAllowed(claims.Sub, claims.VerifiedEmail()) {
		// Only the email check depends on user info. The error is logged above
		// and can hold the provider response body, so it stays out of the reply.
		if userInfoErr != nil && cfg.OIDCAllowedEmail != "" {
			RespondError(w, http.StatusBadGateway, "could not verify user: failed to get userinfo")
			return
		}

		log.Warn().
			Str("sub", claims.Sub).
			Str("email", claims.Email).
			Bool("email_verified", bool(claims.EmailVerified)).
			Str("remote_addr", r.RemoteAddr).
			Msg("OIDC user is not allowed to log in")
		RespondError(w, http.StatusForbidden, "user is not allowed to log in")
		return
	}

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
	h.sessionManager.Put(r.Context(), "oidc_subject", claims.Sub)
	h.sessionManager.Put(r.Context(), "oidc_verified_email", claims.VerifiedEmail())
	h.sessionManager.Put(r.Context(), "profile_picture", claims.Picture)
	h.sessionManager.RememberMe(r.Context(), true)

	// Redirect to the frontend
	// Prefer relative URLs for security, but support absolute URLs for development
	frontendURL := "/dashboard" // Safe default

	// Only construct absolute URLs if explicitly configured
	if cfg.OIDCRedirectURL != "" {
		// Parse the redirect URL to get the frontend base
		if idx := strings.Index(cfg.OIDCRedirectURL, "/api/auth/oidc/callback"); idx > 0 {
			candidateURL := cfg.OIDCRedirectURL[:idx] + "/dashboard"

			// Validate the URL to ensure it's pointing to our application
			if isValidRedirectURL(candidateURL, cfg.OIDCRedirectURL) {
				frontendURL = candidateURL
			}
		}
	} else if cfg.BaseURL != "" && cfg.BaseURL != "/" {
		// Use configured base URL
		base := strings.TrimSuffix(cfg.BaseURL, "/")
		if base == "" {
			base = "/"
		}
		frontendURL = base + "/dashboard"
	}

	log.Trace().
		Str("redirect_url", frontendURL).
		Str("oidc_redirect_url", cfg.OIDCRedirectURL).
		Str("base_url", cfg.BaseURL).
		Str("x_forwarded_proto", r.Header.Get("X-Forwarded-Proto")).
		Str("x_forwarded_host", r.Header.Get("X-Forwarded-Host")).
		Str("host", r.Host).
		Msg("redirecting to frontend after OIDC callback")

	http.Redirect(w, r, frontendURL, http.StatusFound)
}

// mergeUserInfoClaims overrides the ID token claims with the non-empty claims
// from the userinfo endpoint.
func mergeUserInfoClaims(claims *OIDCClaims, userInfo *oidc.UserInfo) {
	// Take the user-info email and its verified flag together, never an
	// unverified user-info email with the verified flag of the ID token.
	if userInfo.Email != "" {
		claims.Email = userInfo.Email
		claims.EmailVerified = oidcBool(userInfo.EmailVerified)
	}

	var userInfoClaims struct {
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
		Nickname          string `json:"nickname"`
		Picture           string `json:"picture"`
	}
	if err := userInfo.Claims(&userInfoClaims); err != nil {
		log.Warn().Err(err).Msg("failed to parse claims from userinfo endpoint, proceeding with ID token claims if available")
		return
	}

	log.Trace().
		Str("userinfo_email", userInfo.Email).
		Str("userinfo_username", userInfoClaims.PreferredUsername).
		Str("userinfo_name", userInfoClaims.Name).
		Str("userinfo_nickname", userInfoClaims.Nickname).
		Msg("successfully parsed claims from userinfo endpoint")

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

// oidcUsername picks the username from the claims: preferred username, then
// nickname, name, email, and subject.
func oidcUsername(claims OIDCClaims) string {
	switch {
	case claims.PreferredUsername != "":
		return claims.PreferredUsername
	case claims.Nickname != "":
		return claims.Nickname
	case claims.Name != "":
		return claims.Name
	case claims.Email != "":
		return claims.Email
	case claims.Sub != "":
		return claims.Sub
	default:
		return "oidc_user"
	}
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
	client, err := h.client(ctx)
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

	cfg := h.config.Live()
	return OIDCConfigResponse{
		Enabled:             cfg.OIDCEnabled,
		AuthorizationURL:    authURL,
		DisableBuiltInLogin: cfg.OIDCDisableBuiltInLogin,
		IssuerURL:           cfg.OIDCIssuer,
	}, state, verifier, nil
}
