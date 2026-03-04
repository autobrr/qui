// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/api/ctxkeys"
	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/domain"
)

// IsAuthenticated middleware checks if the user is authenticated
func IsAuthenticated(authService *auth.Service, sessionManager *scs.SessionManager, cfg *domain.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// When authentication is disabled, set a synthetic user and pass through
			if cfg != nil && cfg.IsAuthDisabled() {
				ctx := context.WithValue(r.Context(), ctxkeys.Username, "admin")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Check for API key first
			apiKey := r.Header.Get("X-API-Key")
			if apiKey != "" {
				// Validate API key
				apiKeyModel, err := authService.ValidateAPIKey(r.Context(), apiKey)
				if err != nil {
					log.Warn().Err(err).Msg("Invalid API key")
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}

				// Set API key info in context (optional, for logging)
				log.Debug().Int("apiKeyID", apiKeyModel.ID).Str("name", apiKeyModel.Name).Msg("API key authenticated")
				next.ServeHTTP(w, r)
				return
			}

			// Check session using SCS
			if !sessionManager.GetBool(r.Context(), "authenticated") {
				// Use 403 to avoid Chromium resetting upstream Basic Auth creds when
				// qui is behind a reverse proxy (e.g. Swizzin nginx auth_basic).
				http.Error(w, "Unauthorized", http.StatusForbidden)
				return
			}

			username := sessionManager.GetString(r.Context(), "username")
			ctx := context.WithValue(r.Context(), ctxkeys.Username, username)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// RequireSetup middleware ensures initial setup is complete
func RequireSetup(authService *auth.Service, cfg *domain.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// When authentication is disabled or OIDC is enabled we don't require
			// a local user to exist, so skip the setup precondition entirely.
			if cfg != nil && cfg.IsAuthDisabled() {
				next.ServeHTTP(w, r)
				return
			}

			if cfg != nil && cfg.OIDCEnabled {
				next.ServeHTTP(w, r)
				return
			}

			// Allow setup-related endpoints
			if strings.HasSuffix(r.URL.Path, "/auth/setup") || strings.HasSuffix(r.URL.Path, "/auth/check-setup") {
				next.ServeHTTP(w, r)
				return
			}

			// Check if setup is complete
			complete, err := authService.IsSetupComplete(r.Context())
			if err != nil {
				log.Error().Err(err).Msg("Failed to check setup status")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			if !complete {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPreconditionRequired)
				w.Write([]byte(`{"error":"Initial setup required","setup_required":true}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
