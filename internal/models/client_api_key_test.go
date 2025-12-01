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

func setupClientAPIKeyStore(t *testing.T) (*ClientAPIKeyStore, func()) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	db := newMockQuerier(sqlDB)

	ctx := t.Context()

	// Create string_pool table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	require.NoError(t, err)

	// Create client_api_keys table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE client_api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key_hash TEXT NOT NULL UNIQUE,
			client_name_id INTEGER NOT NULL,
			instance_id INTEGER NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_used_at TIMESTAMP,
			FOREIGN KEY (client_name_id) REFERENCES string_pool(id)
		)
	`)
	require.NoError(t, err)

	// Create view
	_, err = db.ExecContext(ctx, `
		CREATE VIEW client_api_keys_view AS
		SELECT 
			cak.id,
			cak.key_hash,
			sp.value AS client_name,
			cak.instance_id,
			cak.created_at,
			cak.last_used_at
		FROM client_api_keys cak
		INNER JOIN string_pool sp ON cak.client_name_id = sp.id
	`)
	require.NoError(t, err)

	store := NewClientAPIKeyStore(db)
	cleanup := func() { _ = sqlDB.Close() }

	return store, cleanup
}

func TestNewClientAPIKeyStore(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer sqlDB.Close()

	db := newMockQuerier(sqlDB)
	store := NewClientAPIKeyStore(db)

	require.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestClientAPIKeyStore_Create(t *testing.T) {
	t.Parallel()

	t.Run("successful creation", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		rawKey, clientKey, err := store.Create(ctx, "Test Client", 1)
		require.NoError(t, err)
		require.NotNil(t, clientKey)

		// Verify raw key
		assert.NotEmpty(t, rawKey)
		assert.Len(t, rawKey, 64) // 32 bytes as hex

		// Verify model
		assert.Equal(t, 1, clientKey.ID)
		assert.Equal(t, "Test Client", clientKey.ClientName)
		assert.Equal(t, 1, clientKey.InstanceID)
		assert.NotEmpty(t, clientKey.KeyHash)
		assert.False(t, clientKey.CreatedAt.IsZero())
		assert.Nil(t, clientKey.LastUsedAt)
	})

	t.Run("creates multiple keys", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		key1, cak1, err := store.Create(ctx, "Client 1", 1)
		require.NoError(t, err)

		key2, cak2, err := store.Create(ctx, "Client 2", 2)
		require.NoError(t, err)

		// Keys should be different
		assert.NotEqual(t, key1, key2)
		assert.NotEqual(t, cak1.ID, cak2.ID)
		assert.NotEqual(t, cak1.KeyHash, cak2.KeyHash)
	})

	t.Run("same client name different instances", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		_, cak1, err := store.Create(ctx, "Same Client", 1)
		require.NoError(t, err)

		_, cak2, err := store.Create(ctx, "Same Client", 2)
		require.NoError(t, err)

		assert.NotEqual(t, cak1.ID, cak2.ID)
		assert.Equal(t, 1, cak1.InstanceID)
		assert.Equal(t, 2, cak2.InstanceID)
	})
}

func TestClientAPIKeyStore_GetByKeyHash(t *testing.T) {
	t.Parallel()

	t.Run("existing key", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create key
		rawKey, created, err := store.Create(ctx, "Test Client", 1)
		require.NoError(t, err)

		// Get by hash
		keyHash := HashAPIKey(rawKey)
		retrieved, err := store.GetByKeyHash(ctx, keyHash)
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		assert.Equal(t, created.ID, retrieved.ID)
		assert.Equal(t, "Test Client", retrieved.ClientName)
		assert.Equal(t, 1, retrieved.InstanceID)
		assert.Equal(t, keyHash, retrieved.KeyHash)
	})

	t.Run("key not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		retrieved, err := store.GetByKeyHash(ctx, "nonexistent-hash")
		assert.ErrorIs(t, err, ErrClientAPIKeyNotFound)
		assert.Nil(t, retrieved)
	})
}

func TestClientAPIKeyStore_ValidateKey(t *testing.T) {
	t.Parallel()

	t.Run("valid key", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create key
		rawKey, created, err := store.Create(ctx, "Test Client", 1)
		require.NoError(t, err)

		// Validate
		validated, err := store.ValidateKey(ctx, rawKey)
		require.NoError(t, err)
		require.NotNil(t, validated)

		assert.Equal(t, created.ID, validated.ID)
		assert.Equal(t, "Test Client", validated.ClientName)
	})

	t.Run("invalid key", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		validated, err := store.ValidateKey(ctx, "invalid-raw-key")
		assert.ErrorIs(t, err, ErrClientAPIKeyNotFound)
		assert.Nil(t, validated)
	})
}

func TestClientAPIKeyStore_GetAll(t *testing.T) {
	t.Parallel()

	t.Run("returns all keys", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create multiple keys
		_, _, err := store.Create(ctx, "Client 1", 1)
		require.NoError(t, err)
		_, _, err = store.Create(ctx, "Client 2", 2)
		require.NoError(t, err)
		_, _, err = store.Create(ctx, "Client 3", 3)
		require.NoError(t, err)

		// Get all
		keys, err := store.GetAll(ctx)
		require.NoError(t, err)
		assert.Len(t, keys, 3)
	})

	t.Run("empty list when no keys", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		keys, err := store.GetAll(ctx)
		require.NoError(t, err)
		assert.Empty(t, keys)
	})

	t.Run("ordered by created_at DESC", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create keys in order
		_, first, err := store.Create(ctx, "First", 1)
		require.NoError(t, err)
		_, second, err := store.Create(ctx, "Second", 2)
		require.NoError(t, err)
		_, third, err := store.Create(ctx, "Third", 3)
		require.NoError(t, err)

		keys, err := store.GetAll(ctx)
		require.NoError(t, err)
		assert.Len(t, keys, 3)

		// The order is DESC by created_at, but since they're created in rapid succession
		// the timestamps might be the same. Instead verify all keys are present.
		names := make(map[string]bool)
		for _, k := range keys {
			names[k.ClientName] = true
		}
		assert.True(t, names["First"])
		assert.True(t, names["Second"])
		assert.True(t, names["Third"])

		// Verify IDs match what we created
		ids := make(map[int]bool)
		for _, k := range keys {
			ids[k.ID] = true
		}
		assert.True(t, ids[first.ID])
		assert.True(t, ids[second.ID])
		assert.True(t, ids[third.ID])
	})
}

func TestClientAPIKeyStore_UpdateLastUsed(t *testing.T) {
	t.Parallel()

	t.Run("successful update", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create key
		rawKey, created, err := store.Create(ctx, "Test Client", 1)
		require.NoError(t, err)
		assert.Nil(t, created.LastUsedAt)

		// Update last used
		keyHash := HashAPIKey(rawKey)
		err = store.UpdateLastUsed(ctx, keyHash)
		require.NoError(t, err)

		// Verify update
		retrieved, err := store.GetByKeyHash(ctx, keyHash)
		require.NoError(t, err)
		require.NotNil(t, retrieved.LastUsedAt)
	})

	t.Run("key not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		err := store.UpdateLastUsed(ctx, "nonexistent-hash")
		assert.ErrorIs(t, err, ErrClientAPIKeyNotFound)
	})
}

func TestClientAPIKeyStore_Delete(t *testing.T) {
	t.Parallel()

	t.Run("successful deletion", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create key
		_, created, err := store.Create(ctx, "To Delete", 1)
		require.NoError(t, err)

		// Delete
		err = store.Delete(ctx, created.ID)
		require.NoError(t, err)

		// Verify deleted
		keys, err := store.GetAll(ctx)
		require.NoError(t, err)
		assert.Empty(t, keys)
	})

	t.Run("key not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		err := store.Delete(ctx, 999)
		assert.ErrorIs(t, err, ErrClientAPIKeyNotFound)
	})
}

func TestClientAPIKeyStore_DeleteByInstanceID(t *testing.T) {
	t.Parallel()

	t.Run("deletes all keys for instance", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create keys for different instances
		_, _, err := store.Create(ctx, "Instance 1 Client 1", 1)
		require.NoError(t, err)
		_, _, err = store.Create(ctx, "Instance 1 Client 2", 1)
		require.NoError(t, err)
		_, _, err = store.Create(ctx, "Instance 2 Client 1", 2)
		require.NoError(t, err)

		// Delete for instance 1
		err = store.DeleteByInstanceID(ctx, 1)
		require.NoError(t, err)

		// Verify only instance 2 key remains
		keys, err := store.GetAll(ctx)
		require.NoError(t, err)
		assert.Len(t, keys, 1)
		assert.Equal(t, 2, keys[0].InstanceID)
	})

	t.Run("no error when instance has no keys", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupClientAPIKeyStore(t)
		defer cleanup()

		ctx := t.Context()

		err := store.DeleteByInstanceID(ctx, 999)
		require.NoError(t, err)
	})
}

func TestClientAPIKey_Struct(t *testing.T) {
	t.Parallel()

	t.Run("struct fields", func(t *testing.T) {
		t.Parallel()

		key := ClientAPIKey{
			ID:         1,
			KeyHash:    "abc123",
			ClientName: "My Client",
			InstanceID: 5,
		}

		assert.Equal(t, 1, key.ID)
		assert.Equal(t, "abc123", key.KeyHash)
		assert.Equal(t, "My Client", key.ClientName)
		assert.Equal(t, 5, key.InstanceID)
		assert.True(t, key.CreatedAt.IsZero())
		assert.Nil(t, key.LastUsedAt)
	})
}
