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

func setupAPIKeyStore(t *testing.T) (*APIKeyStore, func()) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	db := newMockQuerier(sqlDB)

	ctx := t.Context()

	// Create tables
	_, err = db.ExecContext(ctx, `
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		CREATE TABLE api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key_hash TEXT NOT NULL UNIQUE,
			name_id INTEGER NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_used_at TIMESTAMP,
			FOREIGN KEY (name_id) REFERENCES string_pool(id)
		)
	`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		CREATE VIEW api_keys_view AS
		SELECT 
			ak.id,
			ak.key_hash,
			sp_name.value AS name,
			ak.created_at,
			ak.last_used_at
		FROM api_keys ak
		INNER JOIN string_pool sp_name ON ak.name_id = sp_name.id
	`)
	require.NoError(t, err)

	store := NewAPIKeyStore(db)
	cleanup := func() { _ = sqlDB.Close() }

	return store, cleanup
}

func TestNewAPIKeyStore(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer sqlDB.Close()

	db := newMockQuerier(sqlDB)
	store := NewAPIKeyStore(db)

	require.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestGenerateAPIKey(t *testing.T) {
	t.Parallel()

	t.Run("returns 64-char hex string", func(t *testing.T) {
		t.Parallel()

		key, err := GenerateAPIKey()
		require.NoError(t, err)
		assert.Len(t, key, 64)
		// Verify it's valid hex
		for _, c := range key {
			assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
				"character '%c' is not valid hex", c)
		}
	})

	t.Run("generates different keys each time", func(t *testing.T) {
		t.Parallel()

		key1, err := GenerateAPIKey()
		require.NoError(t, err)

		key2, err := GenerateAPIKey()
		require.NoError(t, err)

		assert.NotEqual(t, key1, key2)
	})
}

func TestHashAPIKey(t *testing.T) {
	t.Parallel()

	t.Run("consistent hashing", func(t *testing.T) {
		t.Parallel()

		key := "test-api-key"
		hash1 := HashAPIKey(key)
		hash2 := HashAPIKey(key)

		assert.Equal(t, hash1, hash2)
		assert.Len(t, hash1, 64) // SHA256 produces 64-char hex
	})

	t.Run("different keys produce different hashes", func(t *testing.T) {
		t.Parallel()

		hash1 := HashAPIKey("key1")
		hash2 := HashAPIKey("key2")

		assert.NotEqual(t, hash1, hash2)
	})
}

func TestAPIKeyStore_Create(t *testing.T) {
	t.Parallel()

	t.Run("successful creation", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()
		rawKey, apiKey, err := store.Create(ctx, "My API Key")

		require.NoError(t, err)
		require.NotNil(t, apiKey)
		assert.NotEmpty(t, rawKey)
		assert.Len(t, rawKey, 64)
		assert.Equal(t, 1, apiKey.ID)
		assert.Equal(t, "My API Key", apiKey.Name)
		assert.NotEmpty(t, apiKey.KeyHash)
		assert.False(t, apiKey.CreatedAt.IsZero())
		assert.Nil(t, apiKey.LastUsedAt)
	})

	t.Run("creates multiple keys", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		key1, apiKey1, err := store.Create(ctx, "Key 1")
		require.NoError(t, err)

		key2, apiKey2, err := store.Create(ctx, "Key 2")
		require.NoError(t, err)

		assert.NotEqual(t, key1, key2)
		assert.NotEqual(t, apiKey1.ID, apiKey2.ID)
	})
}

func TestAPIKeyStore_GetByHash(t *testing.T) {
	t.Parallel()

	t.Run("existing key", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create key
		rawKey, _, err := store.Create(ctx, "Test Key")
		require.NoError(t, err)

		// Get by hash
		keyHash := HashAPIKey(rawKey)
		apiKey, err := store.GetByHash(ctx, keyHash)

		require.NoError(t, err)
		require.NotNil(t, apiKey)
		assert.Equal(t, "Test Key", apiKey.Name)
		assert.Equal(t, keyHash, apiKey.KeyHash)
	})

	t.Run("key not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		apiKey, err := store.GetByHash(ctx, "nonexistenthash")
		assert.ErrorIs(t, err, ErrAPIKeyNotFound)
		assert.Nil(t, apiKey)
	})
}

func TestAPIKeyStore_List(t *testing.T) {
	t.Parallel()

	t.Run("returns all keys", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create multiple keys
		_, _, err := store.Create(ctx, "Key 1")
		require.NoError(t, err)
		_, _, err = store.Create(ctx, "Key 2")
		require.NoError(t, err)
		_, _, err = store.Create(ctx, "Key 3")
		require.NoError(t, err)

		// List
		keys, err := store.List(ctx)
		require.NoError(t, err)
		assert.Len(t, keys, 3)
	})

	t.Run("empty list when no keys", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		keys, err := store.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, keys)
	})
}

func TestAPIKeyStore_UpdateLastUsed(t *testing.T) {
	t.Parallel()

	t.Run("successful update", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create key
		rawKey, _, err := store.Create(ctx, "Test Key")
		require.NoError(t, err)

		// Update last used
		err = store.UpdateLastUsed(ctx, 1)
		require.NoError(t, err)

		// Verify update
		keyHash := HashAPIKey(rawKey)
		apiKey, err := store.GetByHash(ctx, keyHash)
		require.NoError(t, err)
		assert.NotNil(t, apiKey.LastUsedAt)
	})

	t.Run("key not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		err := store.UpdateLastUsed(ctx, 999)
		assert.ErrorIs(t, err, ErrAPIKeyNotFound)
	})
}

func TestAPIKeyStore_Delete(t *testing.T) {
	t.Parallel()

	t.Run("successful deletion", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create key
		_, apiKey, err := store.Create(ctx, "Test Key")
		require.NoError(t, err)

		// Delete
		err = store.Delete(ctx, apiKey.ID)
		require.NoError(t, err)

		// Verify deleted
		keys, err := store.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, keys)
	})

	t.Run("key not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		err := store.Delete(ctx, 999)
		assert.ErrorIs(t, err, ErrAPIKeyNotFound)
	})
}

func TestAPIKeyStore_ValidateAPIKey(t *testing.T) {
	t.Parallel()

	t.Run("valid key", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create key
		rawKey, _, err := store.Create(ctx, "Test Key")
		require.NoError(t, err)

		// Validate
		apiKey, err := store.ValidateAPIKey(ctx, rawKey)
		require.NoError(t, err)
		require.NotNil(t, apiKey)
		assert.Equal(t, "Test Key", apiKey.Name)
	})

	t.Run("invalid key", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		apiKey, err := store.ValidateAPIKey(ctx, "invalid-key")
		assert.ErrorIs(t, err, ErrInvalidAPIKey)
		assert.Nil(t, apiKey)
	})
}
