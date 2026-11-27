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

func TestCrossSeedGazelleSitePicksMigrationSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkCrossSeedGazelleSitePicksMigration(t.Context(), t, conn, migrationsFS, "migrations/130_add_cross_seed_gazelle_site_picks.sql")
}

func TestCrossSeedGazelleSitePicksMigrationPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkCrossSeedGazelleSitePicksMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations/130_add_cross_seed_gazelle_site_picks.sql")
}

func checkCrossSeedGazelleSitePicksMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadFileFS, migration string) {
	t.Helper()

	const legacySchema = `
		CREATE TABLE instance_crossseed_completion_settings (
			instance_id INTEGER PRIMARY KEY,
			indexer_ids_json TEXT NOT NULL DEFAULT '[]'
		);
		INSERT INTO instance_crossseed_completion_settings (instance_id, indexer_ids_json) VALUES
			(1, '[4,9]'),
			(2, '[]');
		CREATE TABLE cross_seed_search_settings (
			id INTEGER PRIMARY KEY,
			indexer_ids TEXT NOT NULL DEFAULT '[]'
		);
		INSERT INTO cross_seed_search_settings (id, indexer_ids) VALUES (1, '[7]');
	`
	_, err := conn.ExecContext(ctx, legacySchema)
	require.NoError(t, err)
	body, err := fsys.ReadFile(migration)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(body))
	require.NoError(t, err)

	completion := map[int]string{}
	rows, err := conn.QueryContext(ctx, `SELECT instance_id, gazelle_sites_json FROM instance_crossseed_completion_settings`)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rows.Close()) })
	for rows.Next() {
		var id int
		var sites string
		require.NoError(t, rows.Scan(&id, &sites))
		completion[id] = sites
	}
	require.NoError(t, rows.Err())
	require.Equal(t, `["orpheus.network","redacted.sh"]`, completion[1], "a saved Torznab pick keeps asking every RED/OPS site")
	require.Equal(t, `[]`, completion[2], "an empty pick still asks every site")

	var library string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT gazelle_sites FROM cross_seed_search_settings WHERE id = 1`).Scan(&library))
	require.Equal(t, `["orpheus.network","redacted.sh"]`, library, "a saved Library Scan pick keeps asking every RED/OPS site")
}
