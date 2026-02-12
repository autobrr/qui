// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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
	oidcInitMaxAttempts    = 5
	oidcInitInitialBackoff = time.Second
)

var (
	oidcNewProvider = oidc.NewProvider
	oidcSleep       = time.Sleep
)

type OIDCHandler struct {
	config         *domain.Config
	provider       *oidc.Provider
	verifier       *oidc.IDTokenVerifier
	oauthConfig    *oauth2.Config
	sessionManager *scs.SessionManager
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
	State               string `json:"state"`
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
		return nil, fmt.Errorf("OIDC issuer is required")
	}

	if cfg.OIDCClientID == "" {
		log.Error().Msg("OIDC client ID is empty")
		return nil, fmt.Errorf("OIDC client ID is required")
	}

	if cfg.OIDCClientSecret == "" {
		log.Error().Msg("OIDC client secret is empty")
		return nil, fmt.Errorf("OIDC client secret is required")
	}

	if cfg.OIDCRedirectURL == "" {
		log.Error().Msg("OIDC redirect URL is empty")
		return nil, fmt.Errorf("OIDC redirect URL is required")
	}

	scopes := []string{"openid", "profile", "email"}

	ctx := context.Background()

	provider, usedIssuer, err := discoverOIDCProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		log.Error().Err(err).Msg("failed to initialize OIDC provider")
		return nil, fmt.Errorf("failed to initialize OIDC provider: %w", err)
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

	handler := &OIDCHandler{
		config:         cfg,
		provider:       provider,
		verifier:       provider.Verifier(oidcConfig),
		sessionManager: sessionManager,
		oauthConfig: &oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			RedirectURL:  cfg.OIDCRedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
	}

	log.Debug().Msg("OIDC handler initialized successfully")
	return handler, nil
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
	for attempt := 1; attempt <= oidcInitMaxAttempts; attempt++ {
		for idx, candidate := range candidates {
			provider, err := oidcNewProvider(ctx, candidate)
			if err == nil {
				return provider, candidate, nil
			}

			lastErr = err
			log.Warn().
				Err(err).
				Int("attempt", attempt).
				Int("candidate_index", idx).
				Str("issuer", candidate).
				Msg("failed to initialize OIDC provider candidate")
		}

		if attempt < oidcInitMaxAttempts {
			backoff := oidcInitInitialBackoff << (attempt - 1)
			log.Trace().
				Int("next_attempt", attempt+1).
				Dur("sleep", backoff).
				Msg("retrying OIDC provider initialization after backoff")
			oidcSleep(backoff)
		}
	}

	return nil, "", fmt.Errorf("attempted %d times without success: %w", oidcInitMaxAttempts, lastErr)
}

func (h *OIDCHandler) getConfig(w http.ResponseWriter, r *http.Request) {
	// Get the config first
	config := h.GetConfigResponse()

	// Store state in session for later validation
	// This is needed even if user is already authenticated, in case they're re-authenticating
	h.sessionManager.Put(r.Context(), "oidc_state", config.State)

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

	code := r.URL.Query().Get("code")
	if code == "" {
		log.Error().Msg("authorization code is missing from callback request")
		RespondError(w, http.StatusBadRequest, "authorization code is missing from callback request")
		return
	}

	oauth2Token, err := h.oauthConfig.Exchange(r.Context(), code)
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

	idToken, err := h.verifier.Verify(r.Context(), rawIDToken)
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
	userInfo, err := h.provider.UserInfo(r.Context(), oauth2.StaticTokenSource(oauth2Token))
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
	if err := h.sessionManager.RenewToken(r.Context()); err != nil {
		log.Error().Err(err).Msgf("Auth: Failed to renew session token for username: [%s] ip: %s", username, r.RemoteAddr)
		RespondError(w, http.StatusInternalServerError, "could not renew session token")
		return
	}

	// Set cookie options
	h.sessionManager.Cookie.HttpOnly = true
	h.sessionManager.Cookie.SameSite = http.SameSiteLaxMode
	h.sessionManager.Cookie.Path = h.config.BaseURL
	if h.sessionManager.Cookie.Path == "" {
		h.sessionManager.Cookie.Path = "/"
	}

	// If forwarded protocol is https then set cookie secure.
	// Keep SameSite=Lax so the session survives the IdP -> callback cross-site redirect.
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		h.sessionManager.Cookie.Secure = true
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

func generateRandomState() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to a less secure random state
		b = make([]byte, 32)
		_, _ = rand.Read(b)
	}
	return hex.EncodeToString(b)
}

func (h *OIDCHandler) GetConfigResponse() OIDCConfigResponse {
	state := generateRandomState()
	authURL := h.oauthConfig.AuthCodeURL(state)

	return OIDCConfigResponse{
		Enabled:             h.config.OIDCEnabled,
		AuthorizationURL:    authURL,
		State:               state,
		DisableBuiltInLogin: h.config.OIDCDisableBuiltInLogin,
		IssuerURL:           h.config.OIDCIssuer,
	}
}
