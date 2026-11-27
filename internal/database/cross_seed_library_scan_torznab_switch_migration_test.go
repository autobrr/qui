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

func TestCrossSeedLibraryScanTorznabSwitchMigrationSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkCrossSeedLibraryScanTorznabSwitchMigration(t.Context(), t, conn, migrationsFS, "migrations/131_drop_cross_seed_library_scan_torznab_switch.sql")
}

func TestCrossSeedLibraryScanTorznabSwitchMigrationPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkCrossSeedLibraryScanTorznabSwitchMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations/131_drop_cross_seed_library_scan_torznab_switch.sql")
}

type librarySitePickRow struct {
	indexerIDs   string
	gazelleSites string
}

func checkCrossSeedLibraryScanTorznabSwitchMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadFileFS, migration string) {
	t.Helper()

	const legacySchema = `
		CREATE TABLE cross_seed_search_settings (
			id INTEGER PRIMARY KEY,
			indexer_ids TEXT NOT NULL DEFAULT '[]',
			gazelle_sites TEXT NOT NULL DEFAULT '[]',
			torznab_enabled INTEGER NOT NULL DEFAULT 1
		);
		INSERT INTO cross_seed_search_settings (id, indexer_ids, gazelle_sites, torznab_enabled) VALUES
			(1, '[]', '[]', 0),
			(2, '[]', '["redacted.sh"]', 0),
			(3, '[4,9]', '["orpheus.network","redacted.sh"]', 0),
			(4, '[7]', '["orpheus.network"]', 1);
	`
	_, err := conn.ExecContext(ctx, legacySchema)
	require.NoError(t, err)
	body, err := fsys.ReadFile(migration)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(body))
	require.NoError(t, err)

	got := map[int]librarySitePickRow{}
	rows, err := conn.QueryContext(ctx, `SELECT id, indexer_ids, gazelle_sites FROM cross_seed_search_settings`)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rows.Close()) })
	for rows.Next() {
		var id int
		var row librarySitePickRow
		require.NoError(t, rows.Scan(&id, &row.indexerIDs, &row.gazelleSites))
		got[id] = row
	}
	require.NoError(t, rows.Err())

	require.Equal(t, librarySitePickRow{`[]`, `["orpheus.network","redacted.sh"]`}, got[1], "Torznab off with an empty pick keeps asking every RED/OPS site and no Torznab indexer")
	require.Equal(t, librarySitePickRow{`[]`, `["redacted.sh"]`}, got[2], "Torznab off with RED picked keeps asking only RED")
	require.Equal(t, librarySitePickRow{`[]`, `["orpheus.network","redacted.sh"]`}, got[3], "Torznab off drops the saved Torznab indexers")
	require.Equal(t, librarySitePickRow{`[7]`, `["orpheus.network"]`}, got[4], "Torznab on keeps the pick")

	_, err = conn.ExecContext(ctx, `SELECT torznab_enabled FROM cross_seed_search_settings`)
	require.Error(t, err, "the switch column is gone")
}
