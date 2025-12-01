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

func setupUserStore(t *testing.T) (*UserStore, func()) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	db := newMockQuerier(sqlDB)

	ctx := t.Context()

	// Create user table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE user (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL
		)
	`)
	require.NoError(t, err)

	store := NewUserStore(db)
	cleanup := func() { _ = sqlDB.Close() }

	return store, cleanup
}

func TestNewUserStore(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer sqlDB.Close()

	db := newMockQuerier(sqlDB)
	store := NewUserStore(db)

	require.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestUserStore_Create(t *testing.T) {
	t.Parallel()

	t.Run("successful user creation", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupUserStore(t)
		defer cleanup()

		ctx := t.Context()
		user, err := store.Create(ctx, "testuser", "hashedpassword123")

		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, 1, user.ID)
		assert.Equal(t, "testuser", user.Username)
		assert.Equal(t, "hashedpassword123", user.PasswordHash)
	})

	t.Run("duplicate user error", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupUserStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create first user
		_, err := store.Create(ctx, "testuser", "hashedpassword123")
		require.NoError(t, err)

		// Try to create another user - should fail with constraint error
		// The system only allows one user with id=1
		user, err := store.Create(ctx, "anotheruser", "anotherpassword")
		require.Error(t, err, "creating second user should fail")
		assert.Nil(t, user)
		// The error message should indicate a constraint violation
		assert.Contains(t, err.Error(), "constraint", "error should mention constraint violation")
	})
}

func TestUserStore_Get(t *testing.T) {
	t.Parallel()

	t.Run("existing user", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupUserStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create user
		_, err := store.Create(ctx, "testuser", "hashedpassword")
		require.NoError(t, err)

		// Get user
		user, err := store.Get(ctx)
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, 1, user.ID)
		assert.Equal(t, "testuser", user.Username)
		assert.Equal(t, "hashedpassword", user.PasswordHash)
	})

	t.Run("user not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupUserStore(t)
		defer cleanup()

		ctx := t.Context()

		user, err := store.Get(ctx)
		assert.ErrorIs(t, err, ErrUserNotFound)
		assert.Nil(t, user)
	})
}

func TestUserStore_GetByUsername(t *testing.T) {
	t.Parallel()

	t.Run("existing user", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupUserStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create user
		_, err := store.Create(ctx, "testuser", "hashedpassword")
		require.NoError(t, err)

		// Get by username
		user, err := store.GetByUsername(ctx, "testuser")
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, "testuser", user.Username)
	})

	t.Run("user not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupUserStore(t)
		defer cleanup()

		ctx := t.Context()

		user, err := store.GetByUsername(ctx, "nonexistent")
		assert.ErrorIs(t, err, ErrUserNotFound)
		assert.Nil(t, user)
	})
}

func TestUserStore_UpdatePassword(t *testing.T) {
	t.Parallel()

	t.Run("successful update", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupUserStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create user
		_, err := store.Create(ctx, "testuser", "oldhash")
		require.NoError(t, err)

		// Update password
		err = store.UpdatePassword(ctx, "newhash")
		require.NoError(t, err)

		// Verify update
		user, err := store.Get(ctx)
		require.NoError(t, err)
		assert.Equal(t, "newhash", user.PasswordHash)
	})

	t.Run("user not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupUserStore(t)
		defer cleanup()

		ctx := t.Context()

		err := store.UpdatePassword(ctx, "newhash")
		assert.ErrorIs(t, err, ErrUserNotFound)
	})
}

func TestUserStore_Exists(t *testing.T) {
	t.Parallel()

	t.Run("returns true when user exists", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupUserStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create user
		_, err := store.Create(ctx, "testuser", "hashedpassword")
		require.NoError(t, err)

		exists, err := store.Exists(ctx)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("returns false when no user", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupUserStore(t)
		defer cleanup()

		ctx := t.Context()

		exists, err := store.Exists(ctx)
		require.NoError(t, err)
		assert.False(t, exists)
	})
}
