// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilesSyncPrintMigrationSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkFilesSyncPrintMigration(t.Context(), t, conn, migrationsFS, "migrations/112_add_files_sync_print.sql")
}

func TestFilesSyncPrintMigrationPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkFilesSyncPrintMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations/113_add_files_sync_print.sql")
}

func checkFilesSyncPrintMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadFileFS, migration string) {
	t.Helper()

	const legacySchema = `
		CREATE TABLE torrent_files_sync (
			instance_id INTEGER NOT NULL,
			torrent_hash_id BIGINT NOT NULL,
			last_synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			torrent_progress REAL NOT NULL DEFAULT 0,
			file_count INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (instance_id, torrent_hash_id)
		);
		INSERT INTO torrent_files_sync (instance_id, torrent_hash_id, file_count) VALUES (1, 1, 3);
	`

	_, err := conn.ExecContext(ctx, legacySchema)
	require.NoError(t, err)
	body, err := fsys.ReadFile(migration)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(body))
	require.NoError(t, err)

	var stored sql.NullInt64
	var printSyncedAt sql.NullTime
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT maindata_print, print_synced_at FROM torrent_files_sync").Scan(&stored, &printSyncedAt))
	require.False(t, stored.Valid)
	require.False(t, printSyncedAt.Valid)
}
