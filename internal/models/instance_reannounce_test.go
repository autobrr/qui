// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupInstanceReannounceStore(t *testing.T) (*InstanceReannounceStore, func()) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	db := newMockQuerier(sqlDB)

	ctx := t.Context()

	// Create table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE instance_reannounce_settings (
			instance_id INTEGER PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 0,
			initial_wait_seconds INTEGER NOT NULL DEFAULT 15,
			reannounce_interval_seconds INTEGER NOT NULL DEFAULT 7,
			max_age_seconds INTEGER NOT NULL DEFAULT 600,
			aggressive INTEGER NOT NULL DEFAULT 0,
			monitor_all INTEGER NOT NULL DEFAULT 0,
			categories_json TEXT NOT NULL DEFAULT '[]',
			tags_json TEXT NOT NULL DEFAULT '[]',
			trackers_json TEXT NOT NULL DEFAULT '[]',
			exclude_categories INTEGER NOT NULL DEFAULT 0,
			exclude_tags INTEGER NOT NULL DEFAULT 0,
			exclude_trackers INTEGER NOT NULL DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	require.NoError(t, err)

	store := NewInstanceReannounceStore(db)
	cleanup := func() { _ = sqlDB.Close() }

	return store, cleanup
}

func TestNewInstanceReannounceStore(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer sqlDB.Close()

	db := newMockQuerier(sqlDB)
	store := NewInstanceReannounceStore(db)

	require.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestDefaultInstanceReannounceSettings(t *testing.T) {
	t.Parallel()

	settings := DefaultInstanceReannounceSettings(1)

	assert.Equal(t, 1, settings.InstanceID)
	assert.False(t, settings.Enabled)
	assert.Equal(t, 15, settings.InitialWaitSeconds)
	assert.Equal(t, 7, settings.ReannounceIntervalSeconds)
	assert.Equal(t, 600, settings.MaxAgeSeconds)
	assert.False(t, settings.Aggressive)
	assert.False(t, settings.MonitorAll)
	assert.False(t, settings.ExcludeCategories)
	assert.Empty(t, settings.Categories)
	assert.False(t, settings.ExcludeTags)
	assert.Empty(t, settings.Tags)
	assert.False(t, settings.ExcludeTrackers)
	assert.Empty(t, settings.Trackers)
}

func TestInstanceReannounceStore_Get(t *testing.T) {
	t.Parallel()

	t.Run("returns defaults when no settings exist", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupInstanceReannounceStore(t)
		defer cleanup()

		ctx := t.Context()

		settings, err := store.Get(ctx, 1)
		require.NoError(t, err)
		require.NotNil(t, settings)

		// Should return default values
		assert.Equal(t, 1, settings.InstanceID)
		assert.False(t, settings.Enabled)
		assert.Equal(t, 15, settings.InitialWaitSeconds)
		assert.Equal(t, 7, settings.ReannounceIntervalSeconds)
		assert.Equal(t, 600, settings.MaxAgeSeconds)
	})

	t.Run("returns saved settings", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupInstanceReannounceStore(t)
		defer cleanup()

		ctx := t.Context()

		// Save custom settings
		custom := &InstanceReannounceSettings{
			InstanceID:                1,
			Enabled:                   true,
			InitialWaitSeconds:        30,
			ReannounceIntervalSeconds: 10,
			MaxAgeSeconds:             1200,
			Aggressive:                true,
			MonitorAll:                true,
			Categories:                []string{"Movies", "TV"},
			Tags:                      []string{"private"},
			Trackers:                  []string{"tracker.example.com"},
		}
		_, err := store.Upsert(ctx, custom)
		require.NoError(t, err)

		// Get should return custom settings
		settings, err := store.Get(ctx, 1)
		require.NoError(t, err)
		assert.True(t, settings.Enabled)
		assert.Equal(t, 30, settings.InitialWaitSeconds)
		assert.Equal(t, 10, settings.ReannounceIntervalSeconds)
		assert.Equal(t, 1200, settings.MaxAgeSeconds)
		assert.True(t, settings.Aggressive)
		assert.True(t, settings.MonitorAll)
		assert.Equal(t, []string{"Movies", "TV"}, settings.Categories)
		assert.Equal(t, []string{"private"}, settings.Tags)
		assert.Equal(t, []string{"tracker.example.com"}, settings.Trackers)
	})
}

func TestInstanceReannounceStore_List(t *testing.T) {
	t.Parallel()

	t.Run("returns all overrides", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupInstanceReannounceStore(t)
		defer cleanup()

		ctx := t.Context()

		// Save settings for multiple instances
		_, err := store.Upsert(ctx, &InstanceReannounceSettings{
			InstanceID: 1,
			Enabled:    true,
		})
		require.NoError(t, err)

		_, err = store.Upsert(ctx, &InstanceReannounceSettings{
			InstanceID: 2,
			Enabled:    false,
		})
		require.NoError(t, err)

		// List
		settings, err := store.List(ctx)
		require.NoError(t, err)
		assert.Len(t, settings, 2)
	})

	t.Run("empty when no overrides", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupInstanceReannounceStore(t)
		defer cleanup()

		ctx := t.Context()

		settings, err := store.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, settings)
	})
}

func TestInstanceReannounceStore_Upsert(t *testing.T) {
	t.Parallel()

	t.Run("creates new settings", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupInstanceReannounceStore(t)
		defer cleanup()

		ctx := t.Context()

		settings := &InstanceReannounceSettings{
			InstanceID:                1,
			Enabled:                   true,
			InitialWaitSeconds:        20,
			ReannounceIntervalSeconds: 5,
			MaxAgeSeconds:             900,
			Categories:                []string{"Movies"},
		}

		result, err := store.Upsert(ctx, settings)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, 1, result.InstanceID)
		assert.True(t, result.Enabled)
		assert.Equal(t, 20, result.InitialWaitSeconds)
		assert.Equal(t, 5, result.ReannounceIntervalSeconds)
		assert.Equal(t, 900, result.MaxAgeSeconds)
	})

	t.Run("updates existing settings", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupInstanceReannounceStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create initial
		_, err := store.Upsert(ctx, &InstanceReannounceSettings{
			InstanceID: 1,
			Enabled:    false,
		})
		require.NoError(t, err)

		// Update
		updated, err := store.Upsert(ctx, &InstanceReannounceSettings{
			InstanceID:         1,
			Enabled:            true,
			InitialWaitSeconds: 30,
		})
		require.NoError(t, err)
		assert.True(t, updated.Enabled)
		assert.Equal(t, 30, updated.InitialWaitSeconds)

		// Verify only one entry
		all, err := store.List(ctx)
		require.NoError(t, err)
		assert.Len(t, all, 1)
	})

	t.Run("nil settings error", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupInstanceReannounceStore(t)
		defer cleanup()

		ctx := t.Context()

		result, err := store.Upsert(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "settings cannot be nil")
		assert.Nil(t, result)
	})

	t.Run("sanitizes settings with zero values", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupInstanceReannounceStore(t)
		defer cleanup()

		ctx := t.Context()

		// Settings with zero/negative values should use defaults
		settings := &InstanceReannounceSettings{
			InstanceID:                1,
			InitialWaitSeconds:        0,
			ReannounceIntervalSeconds: -1,
			MaxAgeSeconds:             0,
		}

		result, err := store.Upsert(ctx, settings)
		require.NoError(t, err)

		assert.Equal(t, 15, result.InitialWaitSeconds)
		assert.Equal(t, 7, result.ReannounceIntervalSeconds)
		assert.Equal(t, 600, result.MaxAgeSeconds)
	})

	t.Run("with exclusion flags", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupInstanceReannounceStore(t)
		defer cleanup()

		ctx := t.Context()

		settings := &InstanceReannounceSettings{
			InstanceID:        1,
			ExcludeCategories: true,
			Categories:        []string{"Movies", "TV"},
			ExcludeTags:       true,
			Tags:              []string{"private"},
			ExcludeTrackers:   true,
			Trackers:          []string{"tracker.com"},
		}

		result, err := store.Upsert(ctx, settings)
		require.NoError(t, err)

		assert.True(t, result.ExcludeCategories)
		assert.True(t, result.ExcludeTags)
		assert.True(t, result.ExcludeTrackers)
	})
}

func TestSanitizeStringSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "nil slice",
			input:    nil,
			expected: []string{},
		},
		{
			name:     "removes duplicates case insensitive",
			input:    []string{"Movies", "movies", "MOVIES"},
			expected: []string{"Movies"},
		},
		{
			name:     "trims whitespace",
			input:    []string{"  Movies  ", "  TV  "},
			expected: []string{"Movies", "TV"},
		},
		{
			name:     "removes empty strings",
			input:    []string{"Movies", "", "TV", "   "},
			expected: []string{"Movies", "TV"},
		},
		{
			name:     "preserves order",
			input:    []string{"C", "A", "B"},
			expected: []string{"C", "A", "B"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := sanitizeStringSlice(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEncodeDecodeStringSliceJSON(t *testing.T) {
	t.Parallel()

	t.Run("encode empty slice", func(t *testing.T) {
		t.Parallel()

		result, err := encodeStringSliceJSON([]string{})
		require.NoError(t, err)
		assert.Equal(t, "[]", result)
	})

	t.Run("encode nil slice", func(t *testing.T) {
		t.Parallel()

		result, err := encodeStringSliceJSON(nil)
		require.NoError(t, err)
		assert.Equal(t, "[]", result)
	})

	t.Run("encode with values", func(t *testing.T) {
		t.Parallel()

		result, err := encodeStringSliceJSON([]string{"a", "b", "c"})
		require.NoError(t, err)
		assert.Equal(t, `["a","b","c"]`, result)
	})

	t.Run("decode empty string", func(t *testing.T) {
		t.Parallel()

		result, err := decodeStringSliceJSON(sql.NullString{Valid: false})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("decode empty JSON array", func(t *testing.T) {
		t.Parallel()

		result, err := decodeStringSliceJSON(sql.NullString{Valid: true, String: "[]"})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("decode with values", func(t *testing.T) {
		t.Parallel()

		result, err := decodeStringSliceJSON(sql.NullString{Valid: true, String: `["a","b","c"]`})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("decode invalid JSON returns error", func(t *testing.T) {
		t.Parallel()

		_, err := decodeStringSliceJSON(sql.NullString{Valid: true, String: "invalid"})
		assert.Error(t, err)
	})
}

func TestBoolToSQLite(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, boolToSQLite(true))
	assert.Equal(t, 0, boolToSQLite(false))
}

func TestInstanceReannounceSettings_Struct(t *testing.T) {
	t.Parallel()

	t.Run("all fields", func(t *testing.T) {
		t.Parallel()

		settings := InstanceReannounceSettings{
			InstanceID:                5,
			Enabled:                   true,
			InitialWaitSeconds:        20,
			ReannounceIntervalSeconds: 10,
			MaxAgeSeconds:             1200,
			Aggressive:                true,
			MonitorAll:                true,
			ExcludeCategories:         true,
			Categories:                []string{"Movies", "TV"},
			ExcludeTags:               true,
			Tags:                      []string{"private", "freeleech"},
			ExcludeTrackers:           true,
			Trackers:                  []string{"tracker1.com", "tracker2.com"},
		}

		assert.Equal(t, 5, settings.InstanceID)
		assert.True(t, settings.Enabled)
		assert.Equal(t, 20, settings.InitialWaitSeconds)
		assert.Equal(t, 10, settings.ReannounceIntervalSeconds)
		assert.Equal(t, 1200, settings.MaxAgeSeconds)
		assert.True(t, settings.Aggressive)
		assert.True(t, settings.MonitorAll)
		assert.True(t, settings.ExcludeCategories)
		assert.Len(t, settings.Categories, 2)
		assert.True(t, settings.ExcludeTags)
		assert.Len(t, settings.Tags, 2)
		assert.True(t, settings.ExcludeTrackers)
		assert.Len(t, settings.Trackers, 2)
	})
}
