// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// openWithFreePages returns the path of a closed SQLite database whose file
// holds about 4 MB of free pages.
func openWithFreePages(t *testing.T, markShrink bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "qui.db")
	db, err := New(path)
	require.NoError(t, err)
	ctx := t.Context()
	_, err = db.ExecContext(ctx, "CREATE TABLE shrink_filler (data BLOB)")
	require.NoError(t, err)
	for range 1000 {
		_, err = db.ExecContext(ctx, "INSERT INTO shrink_filler (data) VALUES (randomblob(4000))")
		require.NoError(t, err)
	}
	_, err = db.ExecContext(ctx, "DROP TABLE shrink_filler")
	require.NoError(t, err)
	if markShrink {
		require.NoError(t, db.RequestShrink(ctx))
	}
	require.NoError(t, db.Close())
	return path
}

func reopen(t *testing.T, path string) *DB {
	t.Helper()
	db, err := New(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func fileBytes(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Size()
}

func TestRunPendingShrink(t *testing.T) {
	t.Run("a stored mark shrinks the file and clears the mark", func(t *testing.T) {
		path := openWithFreePages(t, true)
		db := reopen(t, path)
		before := fileBytes(t, path)

		result, ran := db.RunPendingShrink(t.Context())

		require.True(t, ran)
		require.NoError(t, result.Err)
		require.Equal(t, before, result.BeforeBytes)
		require.Less(t, result.AfterBytes, before-3_000_000)
		require.Equal(t, result.AfterBytes, fileBytes(t, path))
		pending, err := db.ShrinkPending(t.Context())
		require.NoError(t, err)
		require.False(t, pending)
	})

	t.Run("a failed shrink clears the mark and leaves the database usable", func(t *testing.T) {
		path := openWithFreePages(t, true)
		db := reopen(t, path)
		vacuumErr := errors.New("disk I/O error")
		original := runVacuum
		// Like a VACUUM that fails late, the fake leaves pages in the WAL.
		runVacuum = func(ctx context.Context, conn *sql.DB) error {
			if _, err := conn.ExecContext(ctx, "CREATE TABLE wal_filler (data BLOB)"); err != nil {
				return err
			}
			for range 200 {
				if _, err := conn.ExecContext(ctx, "INSERT INTO wal_filler (data) VALUES (randomblob(4000))"); err != nil {
					return err
				}
			}
			return vacuumErr
		}
		t.Cleanup(func() { runVacuum = original })

		result, ran := db.RunPendingShrink(t.Context())

		require.True(t, ran)
		require.ErrorIs(t, result.Err, vacuumErr)
		require.Equal(t, ShrinkUnknown, result.Failure)
		require.Zero(t, fileBytes(t, path+"-wal"), "the WAL gives its pages back to the disk")
		pending, err := db.ShrinkPending(t.Context())
		require.NoError(t, err)
		require.False(t, pending)
		var count int
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM instances").Scan(&count))
	})

	t.Run("without a mark no shrink runs", func(t *testing.T) {
		path := openWithFreePages(t, false)
		db := reopen(t, path)
		before := fileBytes(t, path)

		_, ran := db.RunPendingShrink(t.Context())

		require.False(t, ran)
		require.Equal(t, before, fileBytes(t, path))
	})
}
