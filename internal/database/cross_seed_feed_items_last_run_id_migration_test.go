// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDropFeedItemsLastRunIDMigrationSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	// PRAGMA foreign_keys is per connection.
	conn.SetMaxOpenConns(1)
	checkDropFeedItemsLastRunIDMigration(t.Context(), t, conn, migrationsFS, "migrations/103_drop_cross_seed_feed_items_last_run_id.sql", "DATETIME", true)
}

func TestDropFeedItemsLastRunIDMigrationPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkDropFeedItemsLastRunIDMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations/104_drop_cross_seed_feed_items_last_run_id.sql", "TIMESTAMP", false)
}

func checkDropFeedItemsLastRunIDMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadFileFS, migration, timestampType string, sqlite bool) {
	t.Helper()

	for _, stmt := range []string{
		"CREATE TABLE torznab_indexers (id INTEGER PRIMARY KEY)",
		"CREATE TABLE cross_seed_runs (id INTEGER PRIMARY KEY)",
		`CREATE TABLE cross_seed_feed_items (
			guid TEXT NOT NULL,
			indexer_id INTEGER NOT NULL,
			title TEXT,
			first_seen_at ` + timestampType + ` NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_seen_at ` + timestampType + ` NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_status TEXT NOT NULL DEFAULT 'pending',
			last_run_id INTEGER,
			info_hash TEXT,
			PRIMARY KEY (guid, indexer_id),
			FOREIGN KEY (indexer_id) REFERENCES torznab_indexers(id) ON DELETE CASCADE,
			FOREIGN KEY (last_run_id) REFERENCES cross_seed_runs(id) ON DELETE SET NULL
		)`,
		"CREATE INDEX idx_cross_seed_feed_items_indexer ON cross_seed_feed_items(indexer_id)",
		"CREATE INDEX idx_cross_seed_feed_items_last_seen ON cross_seed_feed_items(last_seen_at DESC)",
		"INSERT INTO torznab_indexers (id) VALUES (1)",
		"INSERT INTO cross_seed_runs (id) VALUES (7)",
		`INSERT INTO cross_seed_feed_items (guid, indexer_id, title, first_seen_at, last_seen_at, last_status, last_run_id, info_hash) VALUES
			('with-run', 1, 'Synthetic.Show.S01E01.1080p.WEB-DL.H.264-GRP', '2026-09-01 10:00:00', '2026-09-29 00:00:00', 'processed', 7, '0123456789abcdef0123456789abcdef01234567'),
			('without-run', 1, 'Synthetic.Show.S01E02.1080p.WEB-DL.H.264-GRP', '2026-09-02 10:00:00', '2026-09-30 00:00:00', 'skipped', NULL, NULL)`,
	} {
		_, err := conn.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	if sqlite {
		// A row left behind while foreign keys were off. qui migrates with them on.
		for _, stmt := range []string{
			"PRAGMA foreign_keys = OFF",
			"INSERT INTO cross_seed_feed_items (guid, indexer_id, last_status) VALUES ('orphan', 99, 'skipped')",
			"PRAGMA foreign_keys = ON",
		} {
			_, err := conn.ExecContext(ctx, stmt)
			require.NoError(t, err)
		}
	}

	body, err := fsys.ReadFile(migration)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(body))
	require.NoError(t, err)

	_, err = conn.ExecContext(ctx, "SELECT last_run_id FROM cross_seed_feed_items")
	require.Error(t, err, "last_run_id should be gone")

	rows, err := conn.QueryContext(ctx, "SELECT guid, indexer_id, title, last_status, info_hash FROM cross_seed_feed_items ORDER BY first_seen_at")
	require.NoError(t, err)
	defer rows.Close()
	var got [][]any
	for rows.Next() {
		var guid, title, status string
		var indexerID int
		var infoHash sql.NullString
		require.NoError(t, rows.Scan(&guid, &indexerID, &title, &status, &infoHash))
		got = append(got, []any{guid, indexerID, title, status, infoHash.String})
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, [][]any{
		{"with-run", 1, "Synthetic.Show.S01E01.1080p.WEB-DL.H.264-GRP", "processed", "0123456789abcdef0123456789abcdef01234567"},
		{"without-run", 1, "Synthetic.Show.S01E02.1080p.WEB-DL.H.264-GRP", "skipped", ""},
	}, got)

	if sqlite {
		var indexes int
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name IN ('idx_cross_seed_feed_items_indexer', 'idx_cross_seed_feed_items_last_seen')").Scan(&indexes))
		assert.Equal(t, 2, indexes)
		// Migration 092 dropped the touch trigger on purpose; the rebuild must not bring it back.
		var triggers int
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND tbl_name = 'cross_seed_feed_items'").Scan(&triggers))
		assert.Zero(t, triggers)
		var violations int
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_foreign_key_check('cross_seed_feed_items')").Scan(&violations))
		assert.Zero(t, violations)
	}
}
