// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupItemRangesMigrationSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	// 010 rebuilds referenced tables and only runs with foreign keys off.
	_, err = conn.ExecContext(t.Context(), "PRAGMA foreign_keys = OFF")
	require.NoError(t, err)
	checkBackupItemRangesMigration(t.Context(), t, conn, migrationsFS, "migrations", "101_backup_item_ranges.sql")
}

func TestBackupItemRangesMigrationPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkBackupItemRangesMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations", "102_backup_item_ranges.sql")
}

// A migrated run must list exactly the items it listed before, and each
// instance's newest snapshot must stay open for the next run to extend.
func checkBackupItemRangesMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadDirFS, dir, target string) {
	t.Helper()

	entries, err := fsys.ReadDir(dir)
	require.NoError(t, err)
	var earlier []string
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".sql") && name < target {
			earlier = append(earlier, name)
		}
	}
	sort.Strings(earlier)
	for _, name := range earlier {
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, string(body))
		require.NoError(t, err, name)
	}

	str := func(value string) int64 {
		var id int64
		require.NoError(t, conn.QueryRowContext(ctx, fmt.Sprintf(
			"INSERT INTO string_pool (value) VALUES ('%s') ON CONFLICT (value) DO UPDATE SET value = excluded.value RETURNING id", value)).Scan(&id))
		return id
	}
	exec := func(query string) {
		_, err := conn.ExecContext(ctx, query)
		require.NoError(t, err, query)
	}
	for id, name := range map[int]string{1: "a", 2: "b"} {
		exec(fmt.Sprintf("INSERT INTO instances (id, name_id, host_id, username_id, password_encrypted) VALUES (%d, %d, %d, %d, 'x')",
			id, str(name), str("http://localhost"), str("user")))
	}
	kind, status, by := str("hourly"), str("success"), str("scheduler")
	// Run 2 is a run whose items never committed.
	for id, instance := range map[int]int{1: 1, 2: 1, 3: 1, 4: 2} {
		exec(fmt.Sprintf("INSERT INTO instance_backup_runs (id, instance_id, kind_id, status_id, requested_by_id) VALUES (%d, %d, %d, %d, %d)",
			id, instance, kind, status, by))
	}
	item := func(id, run int, hash, name string, savePath string) {
		exec(fmt.Sprintf(`INSERT INTO instance_backup_items (id, run_id, torrent_hash_id, name_id, size_bytes, tags_id, save_path)
			VALUES (%d, %d, %d, %d, 100, %d, %s)`, id, run, str(hash), str(name), str("tag-"+hash), savePath))
	}
	item(1, 1, "h1", "One", "NULL")
	item(2, 1, "h2", "Two", "NULL")
	item(3, 3, "h1", "One renamed", "NULL")
	item(4, 3, "h3", "Three", "'/data/cross-seed/three'")
	item(5, 4, "h1", "One", "NULL")

	before := backupRunContents(ctx, t, conn, `
		SELECT r.id, i.torrent_hash, i.name, i.tags, i.save_path
		FROM instance_backup_runs r JOIN instance_backup_items_view i ON i.run_id = r.id`)

	body, err := fs.ReadFile(fsys, path.Join(dir, target))
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(body))
	require.NoError(t, err)

	after := backupRunContents(ctx, t, conn, `
		SELECT r.id, i.torrent_hash, i.name, i.tags, i.save_path
		FROM instance_backup_runs r
		JOIN instance_backup_items_view i
		  ON i.instance_id = r.instance_id AND i.from_seq <= r.items_seq AND (i.to_seq IS NULL OR i.to_seq > r.items_seq)`)
	require.Equal(t, before, after)
	require.Len(t, after, 3)

	seqs := map[int]sql.NullInt64{}
	rows, err := conn.QueryContext(ctx, "SELECT id, items_seq FROM instance_backup_runs")
	require.NoError(t, err)
	for rows.Next() {
		var id int
		var seq sql.NullInt64
		require.NoError(t, rows.Scan(&id, &seq))
		seqs[id] = seq
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Equal(t, map[int]sql.NullInt64{
		1: {Int64: 1, Valid: true},
		2: {},
		3: {Int64: 2, Valid: true},
		4: {Int64: 1, Valid: true},
	}, seqs)

	var open []int
	rows, err = conn.QueryContext(ctx, "SELECT id FROM instance_backup_items WHERE to_seq IS NULL ORDER BY id")
	require.NoError(t, err)
	for rows.Next() {
		var id int
		require.NoError(t, rows.Scan(&id))
		open = append(open, id)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Equal(t, []int{3, 4, 5}, open)
}

func backupRunContents(ctx context.Context, t *testing.T, conn *sql.DB, query string) map[int][]string {
	t.Helper()
	rows, err := conn.QueryContext(ctx, query)
	require.NoError(t, err)
	defer rows.Close()
	contents := map[int][]string{}
	for rows.Next() {
		var run int
		var hash, name string
		var tags, savePath sql.NullString
		require.NoError(t, rows.Scan(&run, &hash, &name, &tags, &savePath))
		contents[run] = append(contents[run], fmt.Sprintf("%s|%s|%v|%v", hash, name, tags, savePath))
	}
	require.NoError(t, rows.Err())
	for _, items := range contents {
		sort.Strings(items)
	}
	return contents
}
