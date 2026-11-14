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

func TestRemoveTrackerBreakdownMigrationSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkRemoveTrackerBreakdownMigration(t.Context(), t, conn, migrationsFS, "migrations/118_remove_tracker_breakdown.sql")
}

func TestRemoveTrackerBreakdownMigrationPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkRemoveTrackerBreakdownMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations/118_remove_tracker_breakdown.sql")
}

func checkRemoveTrackerBreakdownMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadFileFS, migration string) {
	t.Helper()

	const legacySchema = `
		CREATE TABLE tracker_customizations (
			id INTEGER PRIMARY KEY,
			display_name TEXT NOT NULL,
			domains TEXT NOT NULL,
			included_in_stats TEXT DEFAULT ''
		);
		CREATE TABLE dashboard_settings (
			id INTEGER PRIMARY KEY,
			user_id INTEGER NOT NULL UNIQUE,
			section_visibility TEXT NOT NULL DEFAULT '{}',
			section_order TEXT NOT NULL DEFAULT '[]',
			section_collapsed TEXT NOT NULL DEFAULT '{}',
			tracker_breakdown_sort_column TEXT DEFAULT 'uploaded',
			tracker_breakdown_sort_direction TEXT DEFAULT 'desc',
			tracker_breakdown_items_per_page INTEGER DEFAULT 15
		);
		INSERT INTO tracker_customizations (id, display_name, domains, included_in_stats)
		VALUES (1, 'Tracker', 'a.example.invalid,b.example.invalid', 'b.example.invalid');
		INSERT INTO dashboard_settings (id, user_id, section_visibility, section_order, section_collapsed)
		VALUES (1, 1,
			'{"server-stats":true,"tracker-breakdown":false,"instances":true}',
			'["instances","tracker-breakdown","server-stats","global-stats"]',
			'{"tracker-breakdown":true,"server-stats":true}');
		INSERT INTO dashboard_settings (id, user_id, section_order) VALUES (2, 2, '["global-stats","instances"]');
	`

	_, err := conn.ExecContext(ctx, legacySchema)
	require.NoError(t, err)
	body, err := fsys.ReadFile(migration)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(body))
	require.NoError(t, err)

	var displayName, domains string
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT display_name, domains FROM tracker_customizations WHERE id = 1").Scan(&displayName, &domains))
	require.Equal(t, "Tracker", displayName)
	require.Equal(t, "a.example.invalid,b.example.invalid", domains)

	var visibility, order, collapsed string
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT section_visibility, section_order, section_collapsed FROM dashboard_settings WHERE id = 1").Scan(&visibility, &order, &collapsed))
	require.JSONEq(t, `{"server-stats":true,"instances":true}`, visibility)
	require.JSONEq(t, `["instances","server-stats","global-stats"]`, order)
	require.JSONEq(t, `{"server-stats":true}`, collapsed)

	require.NoError(t, conn.QueryRowContext(ctx, "SELECT section_order FROM dashboard_settings WHERE id = 2").Scan(&order))
	require.Equal(t, `["global-stats","instances"]`, order)

	for _, query := range []string{
		"SELECT included_in_stats FROM tracker_customizations",
		"SELECT tracker_breakdown_sort_column FROM dashboard_settings",
		"SELECT tracker_breakdown_sort_direction FROM dashboard_settings",
		"SELECT tracker_breakdown_items_per_page FROM dashboard_settings",
	} {
		_, err := conn.ExecContext(ctx, query)
		require.Error(t, err, query)
	}
}
