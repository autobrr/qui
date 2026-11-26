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

func IsAuthenticated(authService *auth.Service, sessionManager *scs.SessionManager, cfg *domain.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg != nil && cfg.Live().IsAuthDisabled() {
				ctx := context.WithValue(r.Context(), ctxkeys.Username, "admin")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

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

			if !sessionManager.GetBool(r.Context(), "authenticated") {
				http.Error(w, "Unauthorized", http.StatusForbidden)
				return
			}

			if cfg != nil && !oidcSessionAllowed(r, sessionManager, cfg) {
				http.Error(w, "Unauthorized", http.StatusForbidden)
				return
			}

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

func BrowserOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if PassesBrowserOnly(w, r) {
			next.ServeHTTP(w, r)
		}
	})
}

func PassesBrowserOnly(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey); ok {
		http.Error(w, "API key not allowed; use the web UI", http.StatusForbidden)
		return false
	}
	return true
}

func RequirePermission(area models.PermissionArea, level models.PermissionLevel) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if HasPermission(w, r, area, level) {
				next.ServeHTTP(w, r)
			}
		})
	}
}

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

func CanReadTrackerPasskeys(r *http.Request) bool {
	key, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey)
	return !ok || key.Allows(models.AreaTorrents, models.LevelWrite)
}

func CanReadNotificationURLs(r *http.Request) bool {
	key, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey)
	return !ok || key.Allows(models.AreaNotifications, models.LevelWrite)
}

func HasPermission(w http.ResponseWriter, r *http.Request, area models.PermissionArea, level models.PermissionLevel) bool {
	key, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey)
	if !ok || key.Allows(area, level) {
		return true
	}
	http.Error(w, "API key needs "+models.Permission{Area: area, Level: level}.String(), http.StatusForbidden)
	return false
}

func ReachesInstance(r *http.Request, instanceID int) bool {
	key, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey)
	return !ok || key.ReachesInstance(instanceID)
}

func CanReachInstance(w http.ResponseWriter, r *http.Request, instanceID int) bool {
	if ReachesInstance(r, instanceID) {
		return true
	}
	http.Error(w, CannotReachInstance(instanceID), http.StatusForbidden)
	return false
}

func CannotReachInstance(instanceID int) string {
	return "API key cannot reach instance " + strconv.Itoa(instanceID)
}

func RequireInstance(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		instanceID, _ := strconv.Atoi(chi.URLParam(r, "instanceID"))
		if CanReachInstance(w, r, instanceID) {
			next.ServeHTTP(w, r)
		}
	})
}

func AllInstances(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if key, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey); ok && len(key.InstanceIDs) > 0 {
			http.Error(w, "API key with an instance limit cannot use this route", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

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

func RequireSetup(authService *auth.Service, cfg *domain.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg != nil && cfg.Live().IsAuthDisabled() {
				next.ServeHTTP(w, r)
				return
			}

			if cfg != nil && cfg.Live().OIDCEnabled {
				next.ServeHTTP(w, r)
				return
			}

			if strings.HasSuffix(r.URL.Path, "/auth/setup") || strings.HasSuffix(r.URL.Path, "/auth/check-setup") {
				next.ServeHTTP(w, r)
				return
			}

			if r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/themes") || strings.HasSuffix(r.URL.Path, "/themes/settings")) {
				next.ServeHTTP(w, r)
				return
			}

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
