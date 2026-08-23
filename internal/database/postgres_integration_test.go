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
	"sync/atomic"
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
}

func TestCleanupUnusedStringsPostgres(t *testing.T) {
	t.Parallel()

	db, ctx := openPostgresTestDB(t)

	// Through the DB wrapper, not db.Conn(): the raw handle skips the ?-to-$n
	// rebinding, so every placeholder below would reach Postgres verbatim.
	var referencedID, orphanID int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "pg_referenced").Scan(&referencedID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "pg_orphan").Scan(&orphanID))

	_, err := db.ExecContext(ctx, `
		INSERT INTO instances (name_id, host_id, username_id, password_encrypted)
		VALUES (?, ?, ?, ?)
	`, referencedID, referencedID, referencedID, "dummy_password")
	require.NoError(t, err)

	deleted, err := db.CleanupUnusedStrings(ctx)
	require.NoError(t, err)
	require.Positive(t, deleted)

	var exists bool
	require.NoError(t, db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM string_pool WHERE id = ?)", referencedID).Scan(&exists))
	require.True(t, exists)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM string_pool WHERE id = ?)", orphanID).Scan(&exists))
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

// testSchemaSeq keeps parallel tests from claiming the same schema name.
var testSchemaSeq atomic.Int64

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

	// UnixNano alone collides: the clock is coarse enough that two tests
	// starting together get the same value, and these all run in parallel.
	schemaName := fmt.Sprintf("qui_test_%d_%d", time.Now().UnixNano(), testSchemaSeq.Add(1))
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

// TestPostgresImportForeignKeysIgnoreOtherSchemas pins both catalog queries to
// the active schema. A foreign key referencing another schema's same-named
// table used to survive: regclass text output qualifies only what search_path
// cannot reach, and stripping that qualifier turned the reference into a local
// one. That invents an import dependency (here a cycle, which fails the order)
// and hands the row filter a parent table that is not the one being referenced.
func TestPostgresImportForeignKeysIgnoreOtherSchemas(t *testing.T) {
	t.Parallel()

	ctx, testDSN := openPostgresTestSchema(t)
	pool, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	var activeSchema string
	require.NoError(t, pool.QueryRow(ctx, "SELECT current_schema()").Scan(&activeSchema))
	decoySchema := activeSchema + "_decoy"
	_, err = pool.Exec(ctx, "CREATE SCHEMA "+quoteIdent(decoySchema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA %s CASCADE", quoteIdent(decoySchema)))
	})

	// The decoy holds a table whose name collides with one in the active schema,
	// so a stripped qualifier lands on the local table of the same name.
	_, err = pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE %s.orphan_target (id integer PRIMARY KEY);
		CREATE TABLE orphan_target (id integer PRIMARY KEY, linker_id integer);
		CREATE TABLE linker (id integer PRIMARY KEY, decoy_id integer REFERENCES %s.orphan_target(id));
		ALTER TABLE orphan_target ADD CONSTRAINT orphan_target_linker_fk FOREIGN KEY (linker_id) REFERENCES linker(id);
	`, quoteIdent(decoySchema), quoteIdent(decoySchema)))
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	// linker -> decoy.orphan_target is the cross-schema edge. Read as local it
	// pairs with orphan_target -> linker into a cycle and the sort fails.
	ordered, err := orderPostgresImportTables(ctx, tx, []string{"linker", "orphan_target"})
	require.NoError(t, err)
	require.Equal(t, []string{"linker", "orphan_target"}, ordered)

	fks, err := postgresForeignKeysForTable(ctx, tx, "linker")
	require.NoError(t, err)
	require.Empty(t, fks, "a foreign key into another schema must not filter the copy against a local table")
}
