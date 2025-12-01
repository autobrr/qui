// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package auth

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
)

// mockQuerier wraps sql.DB to implement dbinterface.Querier for tests
type mockQuerier struct {
	*sql.DB
}

// mockTx wraps sql.Tx to implement dbinterface.TxQuerier for tests
type mockTx struct {
	*sql.Tx
}

func (m *mockTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return m.Tx.ExecContext(ctx, query, args...)
}

func (m *mockTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return m.Tx.QueryContext(ctx, query, args...)
}

func (m *mockTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return m.Tx.QueryRowContext(ctx, query, args...)
}

func (m *mockTx) Commit() error {
	return m.Tx.Commit()
}

func (m *mockTx) Rollback() error {
	return m.Tx.Rollback()
}

func newMockQuerier(db *sql.DB) *mockQuerier {
	return &mockQuerier{DB: db}
}

func (m *mockQuerier) GetOrCreateStringID() string {
	return "(INSERT INTO string_pool (value) VALUES (?) ON CONFLICT (value) DO UPDATE SET value = value RETURNING id)"
}

func (m *mockQuerier) GetStringByID(ctx context.Context, id int64) (string, error) {
	var value string
	err := m.QueryRowContext(ctx, "SELECT value FROM string_pool WHERE id = ?", id).Scan(&value)
	return value, err
}

func (m *mockQuerier) GetStringsByIDs(ctx context.Context, ids []int64) (map[int64]string, error) {
	result := make(map[int64]string)
	for _, id := range ids {
		value, err := m.GetStringByID(ctx, id)
		if err != nil {
			return nil, err
		}
		result[id] = value
	}
	return result, nil
}

func (m *mockQuerier) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := m.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &mockTx{Tx: tx}, nil
}

func (m *mockQuerier) WithTx(ctx context.Context, opts *sql.TxOptions, fn func(tx dbinterface.TxQuerier) error) error {
	tx, err := m.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func setupTestDB(t *testing.T) *mockQuerier {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	db := newMockQuerier(sqlDB)

	// Create required tables
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		);

		CREATE TABLE user (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key_hash TEXT NOT NULL UNIQUE,
			name_id INTEGER NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_used_at TIMESTAMP,
			FOREIGN KEY (name_id) REFERENCES string_pool(id)
		);

		CREATE VIEW api_keys_view AS
		SELECT 
			ak.id,
			ak.key_hash,
			sp.value AS name,
			ak.created_at,
			ak.last_used_at
		FROM api_keys ak
		INNER JOIN string_pool sp ON ak.name_id = sp.id;
	`)
	require.NoError(t, err)

	return db
}

func TestService_SetupUser(t *testing.T) {
	t.Parallel()

	t.Run("successful user creation", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		user, err := svc.SetupUser(ctx, "admin", "password123")
		require.NoError(t, err)
		assert.Equal(t, "admin", user.Username)
		assert.NotEmpty(t, user.PasswordHash)
	})

	t.Run("user already exists", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, err := svc.SetupUser(ctx, "admin", "password123")
		require.NoError(t, err)

		_, err = svc.SetupUser(ctx, "admin", "password123")
		assert.ErrorIs(t, err, models.ErrUserAlreadyExists)
	})

	t.Run("password too short", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, err := svc.SetupUser(ctx, "admin", "short")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least 8 characters")
	})
}

func TestService_Login(t *testing.T) {
	t.Parallel()

	t.Run("successful login", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, err := svc.SetupUser(ctx, "admin", "password123")
		require.NoError(t, err)

		user, err := svc.Login(ctx, "admin", "password123")
		require.NoError(t, err)
		assert.Equal(t, "admin", user.Username)
	})

	t.Run("setup not complete", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, err := svc.Login(ctx, "admin", "password123")
		assert.ErrorIs(t, err, ErrNotSetup)
	})

	t.Run("invalid username", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, err := svc.SetupUser(ctx, "admin", "password123")
		require.NoError(t, err)

		_, err = svc.Login(ctx, "wronguser", "password123")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("invalid password", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, err := svc.SetupUser(ctx, "admin", "password123")
		require.NoError(t, err)

		_, err = svc.Login(ctx, "admin", "wrongpassword")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	})
}

func TestService_ChangePassword(t *testing.T) {
	t.Parallel()

	t.Run("successful password change", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, err := svc.SetupUser(ctx, "admin", "password123")
		require.NoError(t, err)

		err = svc.ChangePassword(ctx, "password123", "newpassword456")
		require.NoError(t, err)

		// Verify new password works
		_, err = svc.Login(ctx, "admin", "newpassword456")
		require.NoError(t, err)

		// Verify old password doesn't work
		_, err = svc.Login(ctx, "admin", "password123")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("wrong old password", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, err := svc.SetupUser(ctx, "admin", "password123")
		require.NoError(t, err)

		err = svc.ChangePassword(ctx, "wrongpassword", "newpassword456")
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("new password too short", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, err := svc.SetupUser(ctx, "admin", "password123")
		require.NoError(t, err)

		err = svc.ChangePassword(ctx, "password123", "short")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least 8 characters")
	})
}

func TestService_IsSetupComplete(t *testing.T) {
	t.Parallel()

	t.Run("returns false when no user", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		complete, err := svc.IsSetupComplete(ctx)
		require.NoError(t, err)
		assert.False(t, complete)
	})

	t.Run("returns true when user exists", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, err := svc.SetupUser(ctx, "admin", "password123")
		require.NoError(t, err)

		complete, err := svc.IsSetupComplete(ctx)
		require.NoError(t, err)
		assert.True(t, complete)
	})
}

func TestService_APIKeys(t *testing.T) {
	t.Parallel()

	t.Run("create and list API keys", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		rawKey, apiKey, err := svc.CreateAPIKey(ctx, "Test Key")
		require.NoError(t, err)
		assert.NotEmpty(t, rawKey)
		assert.Equal(t, "Test Key", apiKey.Name)

		keys, err := svc.ListAPIKeys(ctx)
		require.NoError(t, err)
		assert.Len(t, keys, 1)
	})

	t.Run("validate API key", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		rawKey, _, err := svc.CreateAPIKey(ctx, "Test Key")
		require.NoError(t, err)

		validatedKey, err := svc.ValidateAPIKey(ctx, rawKey)
		require.NoError(t, err)
		assert.Equal(t, "Test Key", validatedKey.Name)
	})

	t.Run("invalid API key", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, err := svc.ValidateAPIKey(ctx, "invalid-key")
		assert.ErrorIs(t, err, models.ErrInvalidAPIKey)
	})

	t.Run("delete API key", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := setupTestDB(t)
		svc := NewService(db)

		_, apiKey, err := svc.CreateAPIKey(ctx, "Test Key")
		require.NoError(t, err)

		err = svc.DeleteAPIKey(ctx, apiKey.ID)
		require.NoError(t, err)

		keys, err := svc.ListAPIKeys(ctx)
		require.NoError(t, err)
		assert.Empty(t, keys)
	})
}
