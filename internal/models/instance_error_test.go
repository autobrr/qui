// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// mockDBWithStringInterning wraps sql.DB to implement DBWithStringInterning for tests
type mockDBWithStringInterning struct {
	*sql.DB
	stringCache map[string]int64
	nextID      int64
	mu          sync.Mutex
}

func newMockDBWithStringInterning(db *sql.DB) *mockDBWithStringInterning {
	return &mockDBWithStringInterning{
		DB:          db,
		stringCache: make(map[string]int64),
		nextID:      1,
	}
}

func (m *mockDBWithStringInterning) GetOrCreateStringID(ctx context.Context, value string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check cache first
	if id, ok := m.stringCache[value]; ok {
		return id, nil
	}

	// Check if it exists in the database
	var id int64
	err := m.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", value).Scan(&id)
	if err == nil {
		m.stringCache[value] = id
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	// Insert new string
	result, err := m.ExecContext(ctx, "INSERT INTO string_pool (value) VALUES (?)", value)
	if err != nil {
		return 0, err
	}

	id, err = result.LastInsertId()
	if err != nil {
		return 0, err
	}

	m.stringCache[value] = id
	return id, nil
}

func (m *mockDBWithStringInterning) GetStringByID(ctx context.Context, id int64) (string, error) {
	var value string
	err := m.QueryRowContext(ctx, "SELECT value FROM string_pool WHERE id = ?", id).Scan(&value)
	return value, err
}

func (m *mockDBWithStringInterning) GetStringsByIDs(ctx context.Context, ids []int64) (map[int64]string, error) {
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

func (m *mockDBWithStringInterning) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return m.DB.BeginTx(ctx, opts)
}

func setupInstanceErrorTestDB(t *testing.T) (*mockDBWithStringInterning, *InstanceErrorStore) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	_, err = sqlDB.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		);
		CREATE INDEX idx_string_pool_value ON string_pool(value);
		CREATE TABLE instances (
			id INTEGER PRIMARY KEY,
			name TEXT
		);
		CREATE TABLE instance_errors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id INTEGER NOT NULL,
			error_type_id INTEGER NOT NULL,
			error_message_id INTEGER NOT NULL,
			occurred_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(instance_id) REFERENCES instances(id) ON DELETE CASCADE,
			FOREIGN KEY(error_type_id) REFERENCES string_pool(id),
			FOREIGN KEY(error_message_id) REFERENCES string_pool(id)
		);
		CREATE VIEW instance_errors_view AS
		SELECT 
		    ie.id,
		    ie.instance_id,
		    sp_type.value AS error_type,
		    sp_msg.value AS error_message,
		    ie.occurred_at
		FROM instance_errors ie
		JOIN string_pool sp_type ON ie.error_type_id = sp_type.id
		JOIN string_pool sp_msg ON ie.error_message_id = sp_msg.id;
	`)
	require.NoError(t, err)

	// Wrap with mock that implements DBWithStringInterning
	db := newMockDBWithStringInterning(sqlDB)

	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})

	return db, NewInstanceErrorStore(db)
}

func countInstanceErrors(t *testing.T, db dbinterface.Querier, instanceID int) int {
	t.Helper()

	var count int
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM instance_errors_view WHERE instance_id = ?", instanceID).Scan(&count))
	return count
}

func TestInstanceErrorStore_RecordError_SkipsMissingInstance(t *testing.T) {
	ctx := context.Background()
	db, store := setupInstanceErrorTestDB(t)

	// Ensure no rows exist before recording
	require.Equal(t, 0, countInstanceErrors(t, db, 99))

	err := store.RecordError(ctx, 99, errors.New("connection refused"))
	require.NoError(t, err)

	require.Equal(t, 0, countInstanceErrors(t, db, 99), "errors should not be recorded for missing instances")
}

func TestInstanceErrorStore_RecordError_DeduplicatesWithinOneMinute(t *testing.T) {
	ctx := context.Background()
	db, store := setupInstanceErrorTestDB(t)

	_, err := db.Exec("INSERT INTO instances (id, name) VALUES (?, ?)", 1, "test")
	require.NoError(t, err)

	firstErr := errors.New("connection refused")
	require.NoError(t, store.RecordError(ctx, 1, firstErr))
	require.Equal(t, 1, countInstanceErrors(t, db, 1))

	// Duplicate error within a minute should be ignored
	require.NoError(t, store.RecordError(ctx, 1, firstErr))
	require.Equal(t, 1, countInstanceErrors(t, db, 1))

	// Different error should be recorded
	require.NoError(t, store.RecordError(ctx, 1, errors.New("authentication failed")))
	require.Equal(t, 2, countInstanceErrors(t, db, 1))
}
