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

func TestDirScanFileIDMigrationClearsLegacyIDsSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkDirScanFileIDMigration(t.Context(), t, conn, migrationsFS, "migrations/104_reset_dir_scan_file_ids.sql", "BLOB")
}

func TestDirScanFileIDMigrationClearsLegacyIDsPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkDirScanFileIDMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations/105_reset_dir_scan_file_ids.sql", "BYTEA")
}

// Rows written in the old platform widths cannot be decoded as the tagged
// form, so the migration clears every stored ID and the next scan refills it.
func checkDirScanFileIDMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadFileFS, migration, blobType string) {
	t.Helper()

	_, err := conn.ExecContext(ctx, `CREATE TABLE dir_scan_files (id INTEGER PRIMARY KEY, file_path TEXT NOT NULL, file_id `+blobType+`)`)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO dir_scan_files (id, file_path, file_id) VALUES (1, '/data/a.mkv', $1), (2, '/data/b.mkv', NULL)`, make([]byte, 16))
	require.NoError(t, err)

	body, err := fsys.ReadFile(migration)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(body))
	require.NoError(t, err)

	var withID, total int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(file_id), COUNT(*) FROM dir_scan_files").Scan(&withID, &total))
	require.Equal(t, 0, withID)
	require.Equal(t, 2, total)
}
