// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

type contextKey string

const (
	ClientAPIKeyContextKey contextKey = "client_api_key"
	InstanceIDContextKey   contextKey = "instance_id"
)

// ClientAPIKeyMiddleware validates client API keys and extracts instance information
func ClientAPIKeyMiddleware(store *models.ClientAPIKeyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract API key from URL path parameter
			apiKey := chi.URLParam(r, "api-key")
			if apiKey == "" {
				log.Warn().Msg("Missing API key in proxy request")
				http.Error(w, "Missing API key", http.StatusUnauthorized)
				return
			}

			// Validate the API key
			ctx := r.Context()
			clientAPIKey, err := store.ValidateKey(ctx, apiKey)
			if err != nil {
				if err == models.ErrClientAPIKeyNotFound {
					log.Warn().
						Str("key_prefix", apiKey[:min(8, len(apiKey))]).
						Msg("Invalid client API key")
					http.Error(w, "Invalid API key", http.StatusUnauthorized)
					return
				}
				log.Error().Err(err).Msg("Failed to validate client API key")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Update last used timestamp asynchronously to avoid slowing down the request
			go func() {
				if err := store.UpdateLastUsed(context.Background(), clientAPIKey.KeyHash); err != nil {
					log.Error().Err(err).Int("keyId", clientAPIKey.ID).Msg("Failed to update API key last used timestamp")
				}
			}()

			log.Debug().
				Str("client", clientAPIKey.ClientName).
				Int("instanceId", clientAPIKey.InstanceID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Msg("Client API key validated successfully")

			// Add client API key and instance ID to request context
			ctx = context.WithValue(ctx, ClientAPIKeyContextKey, clientAPIKey)
			ctx = context.WithValue(ctx, InstanceIDContextKey, clientAPIKey.InstanceID)

			// Continue with the request
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetClientAPIKeyFromContext retrieves the client API key from the request context
func GetClientAPIKeyFromContext(ctx context.Context) *models.ClientAPIKey {
	if key, ok := ctx.Value(ClientAPIKeyContextKey).(*models.ClientAPIKey); ok {
		return key
	}
	return nil
}

// GetInstanceIDFromContext retrieves the instance ID from the request context
func GetInstanceIDFromContext(ctx context.Context) int {
	if id, ok := ctx.Value(InstanceIDContextKey).(int); ok {
		return id
	}
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
