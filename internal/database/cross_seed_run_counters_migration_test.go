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

func TestCrossSeedRunCountersMigrationBackfillsCrossSeedsAddedSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkCrossSeedRunCountersMigration(t.Context(), t, conn, migrationsFS, "migrations/096_rename_cross_seed_run_counters.sql")
}

func TestCrossSeedRunCountersMigrationBackfillsCrossSeedsAddedPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkCrossSeedRunCountersMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations/097_rename_cross_seed_run_counters.sql")
}

// Old torrents_added values counted cross-seeds, so the rename must preserve them.
func checkCrossSeedRunCountersMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadFileFS, migration string) {
	t.Helper()

	const legacySchema = `
		CREATE TABLE cross_seed_search_runs (
			id INTEGER PRIMARY KEY,
			total_torrents INTEGER NOT NULL DEFAULT 0,
			processed INTEGER NOT NULL DEFAULT 0,
			torrents_added INTEGER NOT NULL DEFAULT 0,
			torrents_failed INTEGER NOT NULL DEFAULT 0,
			torrents_skipped INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE cross_seed_runs (
			id INTEGER PRIMARY KEY,
			torrents_added INTEGER NOT NULL DEFAULT 0,
			torrents_failed INTEGER NOT NULL DEFAULT 0,
			torrents_skipped INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO cross_seed_search_runs (id, total_torrents, processed, torrents_added, torrents_failed, torrents_skipped) VALUES (1, 10, 10, 7, 1, 2);
		INSERT INTO cross_seed_runs (id, torrents_added, torrents_failed, torrents_skipped) VALUES (1, 4, 5, 6);
	`

	_, err := conn.ExecContext(ctx, legacySchema)
	require.NoError(t, err)
	body, err := fsys.ReadFile(migration)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(body))
	require.NoError(t, err)

	var crossSeedsAdded, torrentsWithCrossSeeds int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT cross_seeds_added, torrents_with_cross_seeds FROM cross_seed_search_runs WHERE id = 1").Scan(&crossSeedsAdded, &torrentsWithCrossSeeds))
	require.Equal(t, 7, crossSeedsAdded)
	require.Equal(t, 7, torrentsWithCrossSeeds)

	var rssAdded, rssFailed, rssSkipped int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT cross_seeds_added, candidates_failed, candidates_skipped FROM cross_seed_runs WHERE id = 1").Scan(&rssAdded, &rssFailed, &rssSkipped))
	require.Equal(t, []int{4, 5, 6}, []int{rssAdded, rssFailed, rssSkipped})
}
