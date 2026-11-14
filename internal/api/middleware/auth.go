// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/api/ctxkeys"
	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
)

// IsAuthenticated middleware checks if the user is authenticated
func IsAuthenticated(authService *auth.Service, sessionManager *scs.SessionManager, cfg *domain.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// When authentication is disabled, set a synthetic user and pass through
			if cfg != nil && cfg.Live().IsAuthDisabled() {
				ctx := context.WithValue(r.Context(), ctxkeys.Username, "admin")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Check for API key first
			apiKey := r.Header.Get("X-API-Key")
			if apiKey != "" {
				key, err := authService.ValidateAPIKey(r.Context(), apiKey)
				if err != nil {
					log.Warn().Err(err).Msg("Invalid API key")
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}

				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxkeys.APIKey, key)))
				return
			}

			// Check session using SCS
			if !sessionManager.GetBool(r.Context(), "authenticated") {
				// Use 403 to avoid Chromium resetting upstream Basic Auth creds when
				// qui is behind a reverse proxy (e.g. Swizzin nginx auth_basic).
				// The frontend logs the user out on a 403 whose message is "unauthorized"
				// in any letter case, as plain text or in a JSON "error" or "message"
				// field (web/src/lib/api.ts). Give any other 403 another message.
				http.Error(w, "Unauthorized", http.StatusForbidden)
				return
			}

			if cfg != nil && !oidcSessionAllowed(r, sessionManager, cfg) {
				http.Error(w, "Unauthorized", http.StatusForbidden)
				return
			}

			// A cookie rides along on cross-site form posts; a custom header
			// does not. Reserve cookie writes for the qui web UI.
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				if r.Header.Get("X-Requested-With") == "" {
					http.Error(w, "Missing X-Requested-With header", http.StatusForbidden)
					return
				}
			}

			username := sessionManager.GetString(r.Context(), "username")
			ctx := context.WithValue(r.Context(), ctxkeys.Username, username)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// BrowserOnly refuses every request that an API key authenticated. Register it
// after IsAuthenticated. The body must not say "unauthorized": the frontend
// signs the user out on a 403 with that word.
func BrowserOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey); ok {
			http.Error(w, "API key not allowed; use the web UI", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequirePermission refuses an API key that lacks the Permission. Register it
// after IsAuthenticated. A session or auth-disabled request has no key and passes.
func RequirePermission(area models.PermissionArea, level models.PermissionLevel) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if HasPermission(w, r, area, level) {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// AreaPermissions holds the RequirePermission checks of one area.
type AreaPermissions struct {
	Read, Run, Write func(http.Handler) http.Handler
}

func Permissions(area models.PermissionArea) AreaPermissions {
	return AreaPermissions{
		Read:  RequirePermission(area, models.LevelRead),
		Run:   RequirePermission(area, models.LevelRun),
		Write: RequirePermission(area, models.LevelWrite),
	}
}

// CanReadTrackerPasskeys matches the Permission of a .torrent export, which holds the same passkey (ADR 0040).
func CanReadTrackerPasskeys(r *http.Request) bool {
	key, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey)
	return !ok || key.Allows(models.AreaTorrents, models.LevelWrite)
}

// HasPermission reports whether the request may use the Permission, and writes
// the 403 when it may not. The body must not say "unauthorized": the frontend
// signs the user out on a 403 with that word.
func HasPermission(w http.ResponseWriter, r *http.Request, area models.PermissionArea, level models.PermissionLevel) bool {
	key, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey)
	if !ok || key.Allows(area, level) {
		return true
	}
	http.Error(w, "API key needs "+models.Permission{Area: area, Level: level}.String(), http.StatusForbidden)
	return false
}

// ReachesInstance reports whether the request may reach the instance. Only an
// API key with an Instance limit can be refused.
func ReachesInstance(r *http.Request, instanceID int) bool {
	key, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey)
	return !ok || key.ReachesInstance(instanceID)
}

// CanReachInstance reports whether the request may reach the instance, and
// writes the 403 when it may not.
func CanReachInstance(w http.ResponseWriter, r *http.Request, instanceID int) bool {
	if ReachesInstance(r, instanceID) {
		return true
	}
	http.Error(w, CannotReachInstance(instanceID), http.StatusForbidden)
	return false
}

// CannotReachInstance is the refusal for an instance outside the Instance limit.
func CannotReachInstance(instanceID int) string {
	return "API key cannot reach instance " + strconv.Itoa(instanceID)
}

// RequireInstance refuses an API key whose Instance limit leaves out the
// {instanceID} of the route. Register it after IsAuthenticated.
func RequireInstance(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		instanceID, _ := strconv.Atoi(chi.URLParam(r, "instanceID"))
		if CanReachInstance(w, r, instanceID) {
			next.ServeHTTP(w, r)
		}
	})
}

// AllInstances refuses an API key with an Instance limit. Register it after
// IsAuthenticated on a route that spans instances (ADR 0040).
func AllInstances(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if key, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey); ok && len(key.InstanceIDs) > 0 {
			http.Error(w, "API key with an instance limit cannot use this route", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// oidcSessionAllowed destroys an OIDC session whose user is no longer allowed.
// Sessions outlive a config change, so the check runs on each request. The
// caller's 403 "Unauthorized" logs the web UI out.
func oidcSessionAllowed(r *http.Request, sessionManager *scs.SessionManager, cfg *domain.Config) bool {
	if sessionManager.GetString(r.Context(), "auth_method") != "oidc" {
		return true
	}
	subject := sessionManager.GetString(r.Context(), "oidc_subject")
	if cfg.Live().IsOIDCUserAllowed(subject, sessionManager.GetString(r.Context(), "oidc_verified_email")) {
		return true
	}
	log.Warn().Str("sub", subject).Str("remote_addr", r.RemoteAddr).Msg("OIDC session user is no longer allowed, destroying session")
	if err := sessionManager.Destroy(r.Context()); err != nil {
		log.Error().Err(err).Msg("failed to destroy session")
	}
	return false
}

// RequireSetup middleware ensures initial setup is complete
func RequireSetup(authService *auth.Service, cfg *domain.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// When authentication is disabled or OIDC is enabled we don't require
			// a local user to exist, so skip the setup precondition entirely.
			if cfg != nil && cfg.Live().IsAuthDisabled() {
				next.ServeHTTP(w, r)
				return
			}

			if cfg != nil && cfg.Live().OIDCEnabled {
				next.ServeHTTP(w, r)
				return
			}

			// Allow setup-related endpoints
			if strings.HasSuffix(r.URL.Path, "/auth/setup") || strings.HasSuffix(r.URL.Path, "/auth/check-setup") {
				next.ServeHTTP(w, r)
				return
			}

			// The built-in theme catalog and stored selection are public so
			// the setup and login pages paint the selected theme too.
			if r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/themes") || strings.HasSuffix(r.URL.Path, "/themes/settings")) {
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
				_, _ = w.Write([]byte(`{"error":"Initial setup required","setup_required":true}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
