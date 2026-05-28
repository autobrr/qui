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

func TestNormalizeMigrationFilenames_RenamesSeasonPackMigrations(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		dialect         Dialect
		initialFilename string
		expectedName    string
		useExecer       bool
	}{
		{
			name:            "sqlite",
			dialect:         DialectSQLite,
			initialFilename: "070_add_season_pack_settings_and_runs.sql",
			expectedName:    "075_add_season_pack_settings_and_runs.sql",
		},
		{
			name:            "postgres",
			dialect:         DialectPostgres,
			initialFilename: "071_add_season_pack_settings_and_runs.sql",
			expectedName:    "076_add_season_pack_settings_and_runs.sql",
			useExecer:       true,
		},
		{
			name:            "sqlite previous current",
			dialect:         DialectSQLite,
			initialFilename: "073_add_season_pack_settings_and_runs.sql",
			expectedName:    "075_add_season_pack_settings_and_runs.sql",
		},
		{
			name:            "postgres previous current",
			dialect:         DialectPostgres,
			initialFilename: "074_add_season_pack_settings_and_runs.sql",
			expectedName:    "076_add_season_pack_settings_and_runs.sql",
			useExecer:       true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			db, conn := newMigrationRenameTestDB(t, tc.dialect)

			_, err := conn.ExecContext(ctx, `
				INSERT INTO migrations (filename) VALUES (?);
			`, tc.initialFilename)
			require.NoError(t, err)

			if tc.useExecer {
				require.NoError(t, db.normalizeMigrationFilenamesWithExecer(ctx, conn, sharedMigrationFilenameRenames, postgresMigrationFilenameRenames))
			} else {
				require.NoError(t, db.normalizeMigrationFilenames(ctx))
			}

			assertMigrationRenamed(t, conn, tc.initialFilename, tc.expectedName)
		})
	}
}
