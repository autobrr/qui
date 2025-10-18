// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupInstanceErrorTestDB(t *testing.T) (*sql.DB, *InstanceErrorStore) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	_, err = db.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE instances (
			id INTEGER PRIMARY KEY,
			name TEXT
		);
		CREATE TABLE instance_errors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id INTEGER NOT NULL,
			error_type TEXT NOT NULL,
			error_message TEXT NOT NULL,
			occurred_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(instance_id) REFERENCES instances(id) ON DELETE CASCADE
		);
	`)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db, NewInstanceErrorStore(db)
}

func countInstanceErrors(t *testing.T, db dbinterface.Querier, instanceID int) int {
	t.Helper()

	var count int
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM instance_errors WHERE instance_id = ?", instanceID).Scan(&count))
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
