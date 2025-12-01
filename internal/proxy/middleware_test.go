// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestGetClientAPIKeyFromContext(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() context.Context
		expected *models.ClientAPIKey
	}{
		{
			name: "with valid client API key",
			setup: func() context.Context {
				key := &models.ClientAPIKey{
					ID:         1,
					ClientName: "TestClient",
					InstanceID: 42,
				}
				return context.WithValue(context.Background(), ClientAPIKeyContextKey, key)
			},
			expected: &models.ClientAPIKey{
				ID:         1,
				ClientName: "TestClient",
				InstanceID: 42,
			},
		},
		{
			name: "with nil context value",
			setup: func() context.Context {
				return context.WithValue(context.Background(), ClientAPIKeyContextKey, nil)
			},
			expected: nil,
		},
		{
			name: "with no context value",
			setup: func() context.Context {
				return context.Background()
			},
			expected: nil,
		},
		{
			name: "with wrong type in context",
			setup: func() context.Context {
				return context.WithValue(context.Background(), ClientAPIKeyContextKey, "wrong type")
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setup()
			result := GetClientAPIKeyFromContext(ctx)

			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expected.ID, result.ID)
				assert.Equal(t, tt.expected.ClientName, result.ClientName)
				assert.Equal(t, tt.expected.InstanceID, result.InstanceID)
			}
		})
	}
}

func TestGetInstanceIDFromContext(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() context.Context
		expected int
	}{
		{
			name: "with valid instance ID",
			setup: func() context.Context {
				return context.WithValue(context.Background(), InstanceIDContextKey, 42)
			},
			expected: 42,
		},
		{
			name: "with zero instance ID",
			setup: func() context.Context {
				return context.WithValue(context.Background(), InstanceIDContextKey, 0)
			},
			expected: 0,
		},
		{
			name: "with no context value",
			setup: func() context.Context {
				return context.Background()
			},
			expected: 0,
		},
		{
			name: "with wrong type in context",
			setup: func() context.Context {
				return context.WithValue(context.Background(), InstanceIDContextKey, "42")
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setup()
			result := GetInstanceIDFromContext(ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		name     string
		a, b     int
		expected int
	}{
		{"a less than b", 1, 5, 1},
		{"a greater than b", 10, 3, 3},
		{"a equals b", 7, 7, 7},
		{"negative numbers", -5, -2, -5},
		{"zero and positive", 0, 10, 0},
		{"zero and negative", 0, -10, -10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := min(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUserAgentOrUnknown(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		expected  string
	}{
		{"with user agent", "Mozilla/5.0", "Mozilla/5.0"},
		{"empty user agent", "", "unknown"},
		{"custom user agent", "CustomClient/1.0", "CustomClient/1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.userAgent != "" {
				r.Header.Set("User-Agent", tt.userAgent)
			}
			result := userAgentOrUnknown(r)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetOrCreateDebouncer(t *testing.T) {
	// Reset the global state for testing
	apiKeyDebouncersMu.Lock()
	oldDebouncers := apiKeyDebouncers
	apiKeyDebouncers = make(map[string]*debouncerEntry)
	apiKeyDebouncersMu.Unlock()
	defer func() {
		apiKeyDebouncersMu.Lock()
		apiKeyDebouncers = oldDebouncers
		apiKeyDebouncersMu.Unlock()
	}()

	t.Run("creates new debouncer for unknown key", func(t *testing.T) {
		d1 := getOrCreateDebouncer("key1")
		require.NotNil(t, d1)

		apiKeyDebouncersMu.Lock()
		entry, exists := apiKeyDebouncers["key1"]
		apiKeyDebouncersMu.Unlock()

		assert.True(t, exists)
		assert.NotNil(t, entry)
	})

	t.Run("returns existing debouncer for known key", func(t *testing.T) {
		d1 := getOrCreateDebouncer("key2")
		d2 := getOrCreateDebouncer("key2")

		// Should return the same debouncer instance
		assert.Equal(t, d1, d2)
	})

	t.Run("updates lastUsed when getting existing debouncer", func(t *testing.T) {
		_ = getOrCreateDebouncer("key3")

		apiKeyDebouncersMu.Lock()
		firstLastUsed := apiKeyDebouncers["key3"].lastUsed
		apiKeyDebouncersMu.Unlock()

		time.Sleep(1 * time.Millisecond)

		_ = getOrCreateDebouncer("key3")

		apiKeyDebouncersMu.Lock()
		secondLastUsed := apiKeyDebouncers["key3"].lastUsed
		apiKeyDebouncersMu.Unlock()

		assert.True(t, secondLastUsed.After(firstLastUsed) || secondLastUsed.Equal(firstLastUsed))
	})
}

func TestCleanupStaleDebouncers(t *testing.T) {
	// Reset the global state for testing
	apiKeyDebouncersMu.Lock()
	oldDebouncers := apiKeyDebouncers
	apiKeyDebouncers = make(map[string]*debouncerEntry)
	apiKeyDebouncersMu.Unlock()
	defer func() {
		apiKeyDebouncersMu.Lock()
		apiKeyDebouncers = oldDebouncers
		apiKeyDebouncersMu.Unlock()
	}()

	// First create debouncers via the normal path
	freshDebouncer := getOrCreateDebouncer("fresh")
	staleDebouncer := getOrCreateDebouncer("stale")

	// Ensure they exist
	require.NotNil(t, freshDebouncer)
	require.NotNil(t, staleDebouncer)

	// Now manipulate the lastUsed time
	apiKeyDebouncersMu.Lock()
	// Keep fresh entry as-is (recently used)
	// Make stale entry old (older than apiKeyDebouncerTTL)
	if entry, exists := apiKeyDebouncers["stale"]; exists {
		entry.lastUsed = time.Now().Add(-apiKeyDebouncerTTL - time.Minute)
	}
	apiKeyDebouncersMu.Unlock()

	cleanupStaleDebouncers()

	apiKeyDebouncersMu.Lock()
	_, freshExists := apiKeyDebouncers["fresh"]
	_, staleExists := apiKeyDebouncers["stale"]
	apiKeyDebouncersMu.Unlock()

	assert.True(t, freshExists, "Fresh entry should still exist")
	assert.False(t, staleExists, "Stale entry should be cleaned up")
}

func TestContextKeys(t *testing.T) {
	// Test that context keys are distinct
	assert.NotEqual(t, ClientAPIKeyContextKey, InstanceIDContextKey)

	// Test type of context keys
	assert.IsType(t, contextKey(""), ClientAPIKeyContextKey)
	assert.IsType(t, contextKey(""), InstanceIDContextKey)
}
