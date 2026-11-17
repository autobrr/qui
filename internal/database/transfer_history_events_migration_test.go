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

func TestTransferHistoryEventsMigrationSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkTransferHistoryEventsMigration(t.Context(), t, conn, migrationsFS, "migrations")
}

func TestTransferHistoryEventsMigrationPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkTransferHistoryEventsMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations")
}

// A record that completed before the upgrade must not get a completion event
// at the first reading after it.
func checkTransferHistoryEventsMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadFileFS, dir string) {
	t.Helper()

	run := func(migration string) {
		body, err := fsys.ReadFile(dir + "/" + migration)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, string(body))
		require.NoError(t, err)
	}
	run("114_add_transfer_history.sql")
	_, err := conn.ExecContext(ctx, `
		INSERT INTO transfer_history_torrents (instance_id, hash, name, tracker_domain, category, size,
			release_group, resolution, source, content_type, statistics_cross_seed,
			baseline_uploaded, baseline_downloaded, baseline_tracker_domain, baseline_category)
		VALUES
			(1, 'complete', '', '', '', 1, '', '', '', '', FALSE, 0, 0, '', ''),
			(1, 'incomplete', '', '', '', 1, '', '', '', '', NULL, 0, 0, '', '')`)
	require.NoError(t, err)
	run("115_add_transfer_history_events.sql")

	rows, err := conn.QueryContext(ctx, "SELECT hash, completed, deleted FROM transfer_history_torrents")
	require.NoError(t, err)
	defer rows.Close()
	completed := make(map[string]bool)
	for rows.Next() {
		var hash string
		var isCompleted, isDeleted bool
		require.NoError(t, rows.Scan(&hash, &isCompleted, &isDeleted))
		require.False(t, isDeleted, hash)
		completed[hash] = isCompleted
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[string]bool{"complete": true, "incomplete": false}, completed)
}
