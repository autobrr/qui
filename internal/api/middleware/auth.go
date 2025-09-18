// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/autobrr/qui/internal/auth"
	"github.com/rs/zerolog/log"
)

// IsAuthenticated middleware checks if the user is authenticated
func IsAuthenticated(authService *auth.Service, sessionManager *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for API key first
			if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
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
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireSetup middleware ensures initial setup is complete
func RequireSetup(authService *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
