// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newMigrationRenameTestDB(t *testing.T, dialect Dialect) (*DB, *sql.DB) {
	t.Helper()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	conn, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	_, err = conn.ExecContext(ctx, `
		CREATE TABLE migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			filename TEXT NOT NULL UNIQUE,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)

	return &DB{writerConn: conn, dialect: dialect}, conn
}

func assertMigrationRenamed(t *testing.T, conn *sql.DB, from, to string) {
	t.Helper()

	ctx := context.Background()
	var fromCount int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE filename = ?", from).Scan(&fromCount))
	require.Zero(t, fromCount)

	var toCount int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE filename = ?", to).Scan(&toCount))
	require.Equal(t, 1, toCount)
}

func TestNormalizeMigrationFilenames_RenamesLicenseProviderDodo(t *testing.T) {
	ctx := context.Background()
	db, conn := newMigrationRenameTestDB(t, DialectSQLite)

	_, err := conn.ExecContext(ctx, `
		INSERT INTO migrations (filename) VALUES ('055_add_license_provider_dodo.sql');
	`)
	require.NoError(t, err)

	require.NoError(t, db.normalizeMigrationFilenames(ctx))
	assertMigrationRenamed(t, conn, "055_add_license_provider_dodo.sql", "057_add_license_provider_dodo.sql")
}

func TestNormalizeMigrationFilenames_RenamesNotifications061To062(t *testing.T) {
	ctx := context.Background()
	db, conn := newMigrationRenameTestDB(t, DialectSQLite)

	_, err := conn.ExecContext(ctx, `
		INSERT INTO migrations (filename) VALUES ('061_add_notifications.sql');
	`)
	require.NoError(t, err)

	require.NoError(t, db.normalizeMigrationFilenames(ctx))
	assertMigrationRenamed(t, conn, "061_add_notifications.sql", "062_add_notifications.sql")
}

func TestNormalizeMigrationFilenames_RenamesCompletionBypass064To066ForSQLite(t *testing.T) {
	ctx := context.Background()
	db, conn := newMigrationRenameTestDB(t, DialectSQLite)

	_, err := conn.ExecContext(ctx, `
		INSERT INTO migrations (filename) VALUES ('064_add_completion_bypass_torznab_cache.sql');
	`)
	require.NoError(t, err)

	require.NoError(t, db.normalizeMigrationFilenames(ctx))
	assertMigrationRenamed(t, conn, "064_add_completion_bypass_torznab_cache.sql", "066_add_completion_bypass_torznab_cache.sql")
}

func TestNormalizeMigrationFilenames_RenamesCompletionBypass066To067ForPostgres(t *testing.T) {
	ctx := context.Background()
	db, conn := newMigrationRenameTestDB(t, DialectPostgres)

	_, err := conn.ExecContext(ctx, `
		INSERT INTO migrations (filename) VALUES ('066_add_completion_bypass_torznab_cache.sql');
	`)
	require.NoError(t, err)

	require.NoError(t, db.normalizeMigrationFilenamesWithExecer(ctx, conn, sharedMigrationFilenameRenames, postgresMigrationFilenameRenames))
	assertMigrationRenamed(t, conn, "066_add_completion_bypass_torznab_cache.sql", "067_add_completion_bypass_torznab_cache.sql")
}

func TestNormalizeMigrationFilenames_RenamesCompletionBypass065To066ForSQLite(t *testing.T) {
	ctx := context.Background()
	db, conn := newMigrationRenameTestDB(t, DialectSQLite)

	_, err := conn.ExecContext(ctx, `
		INSERT INTO migrations (filename) VALUES ('065_add_completion_bypass_torznab_cache.sql');
	`)
	require.NoError(t, err)

	require.NoError(t, db.normalizeMigrationFilenames(ctx))
	assertMigrationRenamed(t, conn, "065_add_completion_bypass_torznab_cache.sql", "066_add_completion_bypass_torznab_cache.sql")
}
