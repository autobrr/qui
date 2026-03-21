// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/filesmanager"
)

func TestOpenPostgres(t *testing.T) {
	t.Parallel()

	db, ctx := openPostgresTestDB(t)

	if got := db.Dialect(); got != string(DialectPostgres) {
		t.Fatalf("unexpected dialect: %s", got)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations").Scan(&count); err != nil {
		t.Fatalf("query migrations table: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected at least one postgres migration row, got %d", count)
	}

	var columnCount int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'cross_seed_settings'
		  AND column_name IN (
			  'enable_pooled_partial_completion',
			  'allow_reflink_single_file_size_mismatch',
			  'max_missing_bytes_after_recheck'
		  )
	`).Scan(&columnCount)
	require.NoError(t, err)
	require.Equal(t, 3, columnCount)
}

func TestCleanupUnusedStringsPostgres(t *testing.T) {
	t.Parallel()

	db, ctx := openPostgresTestDB(t)
	conn := db.Conn()

	var referencedID, orphanID int64
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "pg_referenced").Scan(&referencedID))
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "pg_orphan").Scan(&orphanID))

	_, err := conn.ExecContext(ctx, `
		INSERT INTO instances (name_id, host_id, username_id, password_encrypted)
		VALUES (?, ?, ?, ?)
	`, referencedID, referencedID, referencedID, "dummy_password")
	require.NoError(t, err)

	deleted, err := db.CleanupUnusedStrings(ctx)
	require.NoError(t, err)
	require.Positive(t, deleted)

	var exists bool
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM string_pool WHERE id = ?)", referencedID).Scan(&exists))
	require.True(t, exists)
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM string_pool WHERE id = ?)", orphanID).Scan(&exists))
	require.False(t, exists)

	deletedAgain, err := db.CleanupUnusedStrings(ctx)
	require.NoError(t, err)
	require.Zero(t, deletedAgain)
}

func TestMigratedSQLiteFilesmanagerCleanupPostgres(t *testing.T) {
	t.Parallel()

	ctx, testDSN := openPostgresTestSchema(t)
	sqlitePath := filepath.Join(t.TempDir(), "fixture.db")
	sqliteDB, err := New(sqlitePath)
	require.NoError(t, err)

	var (
		instanceNameID int64
		hostID         int64
		usernameID     int64
		keepHashID     int64
		dropHashID     int64
		fileNameID     int64
	)

	conn := sqliteDB.Conn()
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-name").Scan(&instanceNameID))
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-host").Scan(&hostID))
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-user").Scan(&usernameID))
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "keep-hash").Scan(&keepHashID))
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "drop-hash").Scan(&dropHashID))
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "file.mkv").Scan(&fileNameID))

	_, err = conn.ExecContext(ctx, `
		INSERT INTO instances (id, name_id, host_id, username_id, password_encrypted)
		VALUES (?, ?, ?, ?, ?)
	`, 1, instanceNameID, hostID, usernameID, "enc")
	require.NoError(t, err)

	now := time.Now().UTC()
	_, err = conn.ExecContext(ctx, `
		INSERT INTO torrent_files_cache
			(instance_id, torrent_hash_id, file_index, name_id, size, progress, priority, is_seed, piece_range_start, piece_range_end, availability, cached_at)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?),
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		1, keepHashID, 0, fileNameID, 100, 1.0, 1, 1, 0, 1, 1.0, now,
		1, dropHashID, 0, fileNameID, 200, 0.5, 1, 0, 0, 1, 0.5, now,
	)
	require.NoError(t, err)

	_, err = conn.ExecContext(ctx, `
		INSERT INTO torrent_files_sync
			(instance_id, torrent_hash_id, last_synced_at, torrent_progress, file_count)
		VALUES
			(?, ?, ?, ?, ?),
			(?, ?, ?, ?, ?)
	`,
		1, keepHashID, now, 1.0, 1,
		1, dropHashID, now, 0.5, 1,
	)
	require.NoError(t, err)
	require.NoError(t, sqliteDB.Close())

	report, err := MigrateSQLiteToPostgres(ctx, SQLiteToPostgresMigrationOptions{
		SQLitePath:  sqlitePath,
		PostgresDSN: testDSN,
		Apply:       true,
	})
	require.NoError(t, err)
	require.True(t, report.Applied)

	pgDB, err := Open(OpenOptions{
		Engine:      string(DialectPostgres),
		PostgresDSN: testDSN,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pgDB.Close())
	})

	repo := filesmanager.NewRepository(pgDB)
	deleted, err := repo.DeleteCacheForRemovedTorrents(ctx, 1, []string{"keep-hash"})
	require.NoError(t, err)
	require.Equal(t, 1, deleted)

	var count int
	require.NoError(t, pgDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM torrent_files_cache_view WHERE instance_id = ? AND torrent_hash = ?", 1, "keep-hash").Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, pgDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM torrent_files_cache_view WHERE instance_id = ? AND torrent_hash = ?", 1, "drop-hash").Scan(&count))
	require.Zero(t, count)
	require.NoError(t, pgDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM torrent_files_sync_view WHERE instance_id = ? AND torrent_hash = ?", 1, "drop-hash").Scan(&count))
	require.Zero(t, count)
}

func openPostgresTestDB(t *testing.T) (*DB, context.Context) {
	t.Helper()

	ctx, testDSN := openPostgresTestSchema(t)
	db, err := Open(OpenOptions{
		Engine:      string(DialectPostgres),
		PostgresDSN: testDSN,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db, ctx
}

func openPostgresTestSchema(t *testing.T) (context.Context, string) {
	t.Helper()

	baseDSN := strings.TrimSpace(os.Getenv("QUI_TEST_POSTGRES_DSN"))
	if baseDSN == "" {
		t.Skip("QUI_TEST_POSTGRES_DSN not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminPool, err := pgxpool.New(ctx, baseDSN)
	require.NoError(t, err)
	t.Cleanup(adminPool.Close)

	schemaName := fmt.Sprintf("qui_test_%d", time.Now().UnixNano())
	_, err = adminPool.Exec(ctx, "CREATE SCHEMA "+quoteIdent(schemaName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA %s CASCADE", quoteIdent(schemaName)))
	})

	return ctx, dsnWithSearchPath(t, baseDSN, schemaName)
}

func dsnWithSearchPath(t *testing.T, dsn string, schema string) string {
	t.Helper()

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse postgres dsn: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
