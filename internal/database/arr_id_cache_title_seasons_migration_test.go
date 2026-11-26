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

func TestArrIDCacheTitleSeasonsMigrationSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkArrIDCacheTitleSeasonsMigration(t.Context(), t, conn, migrationsFS, "migrations/129_add_arr_id_cache_title_seasons.sql")
}

func TestArrIDCacheTitleSeasonsMigrationPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkArrIDCacheTitleSeasonsMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations/129_add_arr_id_cache_title_seasons.sql")
}

func checkArrIDCacheTitleSeasonsMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadFileFS, migration string) {
	t.Helper()

	const legacySchema = `
		CREATE TABLE arr_id_cache (
			id INTEGER PRIMARY KEY,
			title_hash TEXT NOT NULL,
			content_type TEXT NOT NULL,
			tvdb_id INTEGER,
			titles_json TEXT,
			cached_at TIMESTAMP NOT NULL,
			expires_at TIMESTAMP NOT NULL
		);
		INSERT INTO arr_id_cache (id, title_hash, content_type, tvdb_id, titles_json, cached_at, expires_at) VALUES
			(1, 'show', 'tv', 70, '["Lantern Ward","Laternenwache"]', '2026-11-20 10:00:00', '2026-12-20 10:00:00'),
			(2, 'anime', 'anime', 71, '["Azure Compass","Aoi Rashinban"]', '2026-11-20 10:00:00', '2026-11-20 10:10:00'),
			(3, 'movie', 'movie', NULL, '["Copper Fjord","Kupferfjord"]', '2026-11-20 10:00:00', '2026-12-20 10:00:00');
	`

	_, err := conn.ExecContext(ctx, legacySchema)
	require.NoError(t, err)
	body, err := fsys.ReadFile(migration)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(body))
	require.NoError(t, err)

	rows, err := conn.QueryContext(ctx, `SELECT id, titles_json, title_seasons_json, CAST(cached_at AS TEXT), CAST(expires_at AS TEXT) FROM arr_id_cache ORDER BY id`)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rows.Close()) })
	type row struct {
		titles, seasons     sql.NullString
		cachedAt, expiresAt string
	}
	got := map[int]row{}
	for rows.Next() {
		var id int
		var r row
		require.NoError(t, rows.Scan(&id, &r.titles, &r.seasons, &r.cachedAt, &r.expiresAt))
		got[id] = r
	}
	require.NoError(t, rows.Err())

	require.Len(t, got, 3)
	require.False(t, got[1].titles.Valid, "a show is asked again for its titles with their seasons")
	require.False(t, got[2].titles.Valid, "an anime is asked again for its titles with their seasons")
	require.Equal(t, sql.NullString{String: `["Copper Fjord","Kupferfjord"]`, Valid: true}, got[3].titles, "a movie keeps its titles")
	for id, r := range got {
		require.False(t, r.seasons.Valid, id)
	}
	require.Contains(t, got[2].expiresAt, "10:10:00", "a partial answer keeps the lifetime that marks it")
	require.Contains(t, got[2].cachedAt, "10:00:00")
}
