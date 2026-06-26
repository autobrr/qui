// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/debounce"
)

type contextKey string

const (
	ClientAPIKeyContextKey contextKey = "client_api_key"
	InstanceIDContextKey   contextKey = "instance_id"

	apiKeyDebounceDelay   = 10 * time.Second
	apiKeyDebouncerTTL    = 5 * time.Minute
	apiKeyCleanupInterval = time.Minute
)

type debouncerEntry struct {
	debouncer *debounce.Debouncer
	lastUsed  time.Time
}

var (
	apiKeyDebouncers           = make(map[string]*debouncerEntry)
	apiKeyDebouncersMu         sync.Mutex
	apiKeyDebouncerCleanupOnce sync.Once
)

func userAgentOrUnknown(r *http.Request) string {
	if ua := r.UserAgent(); ua != "" {
		return ua
	}
	return "unknown"
}

// getOrCreateDebouncer returns a debouncer for the given key hash, creating one if it doesn't exist
func getOrCreateDebouncer(keyHash string) *debounce.Debouncer {
	startAPIKeyDebouncerCleanup()

	now := time.Now()
	apiKeyDebouncersMu.Lock()
	if entry, exists := apiKeyDebouncers[keyHash]; exists {
		entry.lastUsed = now
		debouncer := entry.debouncer
		apiKeyDebouncersMu.Unlock()
		return debouncer
	}

	entry := &debouncerEntry{
		debouncer: debounce.New(apiKeyDebounceDelay),
		lastUsed:  now,
	}

	apiKeyDebouncers[keyHash] = entry
	apiKeyDebouncersMu.Unlock()
	return entry.debouncer
}

func startAPIKeyDebouncerCleanup() {
	apiKeyDebouncerCleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(apiKeyCleanupInterval)
			defer ticker.Stop()

			for range ticker.C {
				cleanupStaleDebouncers()
			}
		}()
	})
}

func cleanupStaleDebouncers() {
	now := time.Now()
	var toStop []*debounce.Debouncer

	apiKeyDebouncersMu.Lock()
	for key, entry := range apiKeyDebouncers {
		if now.Sub(entry.lastUsed) > apiKeyDebouncerTTL {
			delete(apiKeyDebouncers, key)
			toStop = append(toStop, entry.debouncer)
		}
	}
	apiKeyDebouncersMu.Unlock()

	for _, debouncer := range toStop {
		debouncer.Stop()
	}
}

// ClientAPIKeyMiddleware validates client API keys and extracts instance information
func ClientAPIKeyMiddleware(store *models.ClientAPIKeyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Debug().
				Str("method", r.Method).
				Msg("ClientAPIKeyMiddleware called")

			// Extract API key from URL path parameter
			apiKey := chi.URLParam(r, "api-key")
			log.Debug().
				Bool("hasKey", apiKey != "").
				Msg("Checking API key from URL parameter")

			if apiKey == "" {
				log.Warn().
					Str("user_agent", userAgentOrUnknown(r)).
					Msg("Missing API key in proxy request")
				http.Error(w, "Missing API key", http.StatusUnauthorized)
				return
			}

			// Validate the API key
			ctx := r.Context()
			clientAPIKey, err := store.ValidateKey(ctx, apiKey)
			if err != nil {
				if err == models.ErrClientAPIKeyNotFound {
					log.Warn().
						Str("user_agent", userAgentOrUnknown(r)).
						Msg("Invalid client API key")
					http.Error(w, "Invalid API key", http.StatusUnauthorized)
					return
				}
				log.Error().Err(err).Msg("Failed to validate client API key")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Update last used timestamp with debouncing per API key
			debouncer := getOrCreateDebouncer(clientAPIKey.KeyHash)

			if !debouncer.Queued() {
				debouncer.Do(func() {
					if err := store.UpdateLastUsed(context.Background(), clientAPIKey.KeyHash); err != nil {
						log.Error().Err(err).Int("keyId", clientAPIKey.ID).Msg("Failed to update API key last used timestamp")
					}
				})
			}

			log.Debug().
				Str("client", clientAPIKey.ClientName).
				Int("instanceId", clientAPIKey.InstanceID).
				Str("method", r.Method).
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
