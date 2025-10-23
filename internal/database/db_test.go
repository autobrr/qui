// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

func TestMigrationNumbering(t *testing.T) {
	files := listMigrationFiles(t)

	seen := make(map[string]struct{})
	prev := -1

	for _, name := range files {
		parts := strings.SplitN(name, "_", 2)
		require.Lenf(t, parts, 2, "migration file %s must follow <number>_<description>.sql", name)

		number := parts[0]
		require.NotContainsf(t, seen, number, "Duplicate migration number found: %s", number)
		seen[number] = struct{}{}

		n, err := strconv.Atoi(number)
		require.NoErrorf(t, err, "migration prefix %s must be numeric", number)
		require.Greaterf(t, n, prev, "migration numbers must be strictly increasing (saw %d then %d)", prev, n)
		prev = n
	}
}

func TestMigrationIdempotency(t *testing.T) {
	log.Logger = log.Output(io.Discard)
	ctx := t.Context()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// First initialization
	db1, err := New(dbPath)
	require.NoError(t, err, "Failed to initialize database first time")
	var count1 int
	require.NoError(t, db1.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations").Scan(&count1))
	require.NoError(t, db1.Close())

	// Second initialization should be a no-op for migrations
	db2, err := New(dbPath)
	require.NoError(t, err, "Failed to initialize database second time")
	t.Cleanup(func() {
		require.NoError(t, db2.Close())
	})

	var count2 int
	require.NoError(t, db2.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations").Scan(&count2))
	require.Equal(t, count1, count2, "Migration count should be the same after re-initialization")
	require.Greater(t, count2, 0, "Should have at least one migration applied")

	files := listMigrationFiles(t)
	require.Equal(t, len(files), count2, "Applied migration count should match number of migration files")

	var duplicates int
	require.NoError(t, db2.Conn().QueryRowContext(ctx, "SELECT COUNT(*) - COUNT(DISTINCT filename) FROM migrations").Scan(&duplicates))
	require.Zero(t, duplicates, "Should not have duplicate migration entries")
}

func TestMigrationsApplyFullSchema(t *testing.T) {
	log.Output(io.Discard)
	ctx := t.Context()
	db := openTestDatabase(t)
	conn := db.Conn()

	files := listMigrationFiles(t)
	var applied int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations").Scan(&applied))
	require.Equal(t, len(files), applied, "All migrations should be recorded as applied")

	t.Run("pragma settings", func(t *testing.T) {
		verifyPragmas(t, t.Context(), conn)
	})

	t.Run("schema", func(t *testing.T) {
		verifySchema(t, t.Context(), conn)
	})

	t.Run("indexes", func(t *testing.T) {
		verifyIndexes(t, t.Context(), conn)
	})

	t.Run("triggers", func(t *testing.T) {
		verifyTriggers(t, t.Context(), conn)
	})
}

func TestConnectionPragmasApplyToEachConnection(t *testing.T) {
	log.Output(io.Discard)
	ctx := t.Context()
	db := openTestDatabase(t)
	sqlDB := db.Conn()

	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(2)

	conn1, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn1.Close())
	})

	conn2, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn2.Close())
	})

	verifyPragmas(t, ctx, conn1)
	verifyPragmas(t, ctx, conn2)
}

func TestCloseDrainsPendingWrites(t *testing.T) {
	log.Output(io.Discard)
	ctx := t.Context()
	dbPath := filepath.Join(t.TempDir(), "close-drain.db")

	db, err := New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	_, err = db.Conn().ExecContext(ctx, "CREATE TABLE IF NOT EXISTS close_drain (id INTEGER PRIMARY KEY AUTOINCREMENT, val TEXT NOT NULL)")
	require.NoError(t, err)

	barrier := make(chan struct{})
	signal := make(chan struct{}, 1)
	db.writeBarrier.Store(barrier)
	db.barrierSignal.Store(signal)
	t.Cleanup(func() {
		db.writeBarrier.Store((chan struct{})(nil))
		db.barrierSignal.Store((chan struct{})(nil))
	})

	firstDone := make(chan error, 1)
	go func() {
		_, execErr := db.ExecContext(ctx, "INSERT INTO close_drain (val) VALUES (?)", "first")
		firstDone <- execErr
	}()

	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("writer did not reach barrier")
	}

	secondDone := make(chan error, 1)
	go func() {
		_, execErr := db.ExecContext(ctx, "INSERT INTO close_drain (val) VALUES (?)", "second")
		secondDone <- execErr
	}()

	require.Eventually(t, func() bool {
		return len(db.writeCh) > 0
	}, time.Second, 10*time.Millisecond)

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- db.Close()
	}()

	require.NoError(t, <-closeDone)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
}

type columnSpec struct {
	Name       string
	Type       string
	PrimaryKey bool
}

var expectedSchema = map[string][]columnSpec{
	"migrations": {
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "filename", Type: "TEXT"},
		{Name: "applied_at", Type: "TIMESTAMP"},
	},
	"user": {
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "username", Type: "TEXT"},
		{Name: "password_hash", Type: "TEXT"},
		{Name: "created_at", Type: "TIMESTAMP"},
		{Name: "updated_at", Type: "TIMESTAMP"},
	},
	"api_keys": {
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "key_hash", Type: "TEXT"},
		{Name: "name_id", Type: "INTEGER"},
		{Name: "created_at", Type: "TIMESTAMP"},
		{Name: "last_used_at", Type: "TIMESTAMP"},
	},
	"instances": {
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "name_id", Type: "INTEGER"},
		{Name: "host", Type: "TEXT"},
		{Name: "username", Type: "TEXT"},
		{Name: "password_encrypted", Type: "TEXT"},
		{Name: "basic_username", Type: "TEXT"},
		{Name: "basic_password_encrypted", Type: "TEXT"},
		{Name: "tls_skip_verify", Type: "BOOLEAN"},
	},
	"licenses": {
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "license_key", Type: "TEXT"},
		{Name: "product_name", Type: "TEXT"},
		{Name: "status", Type: "TEXT"},
		{Name: "activated_at", Type: "DATETIME"},
		{Name: "expires_at", Type: "DATETIME"},
		{Name: "last_validated", Type: "DATETIME"},
		{Name: "polar_customer_id", Type: "TEXT"},
		{Name: "polar_product_id", Type: "TEXT"},
		{Name: "polar_activation_id", Type: "TEXT"},
		{Name: "username", Type: "TEXT"},
		{Name: "created_at", Type: "DATETIME"},
		{Name: "updated_at", Type: "DATETIME"},
	},
	"client_api_keys": {
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "key_hash", Type: "TEXT"},
		{Name: "client_name_id", Type: "INTEGER"},
		{Name: "instance_id", Type: "INTEGER"},
		{Name: "created_at", Type: "TIMESTAMP"},
		{Name: "last_used_at", Type: "TIMESTAMP"},
	},
	"instance_errors": {
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "instance_id", Type: "INTEGER"},
		{Name: "error_type_id", Type: "INTEGER"},
		{Name: "error_message_id", Type: "INTEGER"},
		{Name: "occurred_at", Type: "TIMESTAMP"},
	},
	"sessions": {
		{Name: "token", Type: "TEXT", PrimaryKey: true},
		{Name: "data", Type: "BLOB"},
		{Name: "expiry", Type: "REAL"},
	},
	"torrent_files_cache": {
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "instance_id", Type: "INTEGER"},
		{Name: "torrent_hash_id", Type: "INTEGER"},
		{Name: "file_index", Type: "INTEGER"},
		{Name: "name_id", Type: "INTEGER"},
		{Name: "size", Type: "INTEGER"},
		{Name: "progress", Type: "REAL"},
		{Name: "priority", Type: "INTEGER"},
		{Name: "is_seed", Type: "INTEGER"},
		{Name: "piece_range_start", Type: "INTEGER"},
		{Name: "piece_range_end", Type: "INTEGER"},
		{Name: "availability", Type: "REAL"},
		{Name: "cached_at", Type: "TIMESTAMP"},
	},
	"torrent_files_sync": {
		{Name: "instance_id", Type: "INTEGER", PrimaryKey: true},
		{Name: "torrent_hash_id", Type: "INTEGER", PrimaryKey: true},
		{Name: "last_synced_at", Type: "TIMESTAMP"},
		{Name: "torrent_progress", Type: "REAL"},
		{Name: "file_count", Type: "INTEGER"},
	},
}

var expectedIndexes = map[string][]string{
	"api_keys":            {"idx_api_keys_hash"},
	"licenses":            {"idx_licenses_status", "idx_licenses_theme", "idx_licenses_key"},
	"client_api_keys":     {"idx_client_api_keys_key_hash", "idx_client_api_keys_instance_id"},
	"instance_errors":     {"idx_instance_errors_lookup"},
	"sessions":            {"sessions_expiry_idx"},
	"torrent_files_cache": {"idx_torrent_files_cache_lookup", "idx_torrent_files_cache_cached_at"},
	"torrent_files_sync":  {"idx_torrent_files_sync_last_synced"},
}

var expectedTriggers = []string{
	"update_user_updated_at",
	"cleanup_old_instance_errors",
}

func listMigrationFiles(t *testing.T) []string {
	entries, err := migrationsFS.ReadDir("migrations")
	require.NoError(t, err, "Failed to read migrations directory")

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		files = append(files, entry.Name())
	}

	sort.Strings(files)
	return files
}

func openTestDatabase(t *testing.T) *DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	return db
}

type pragmaQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func verifyPragmas(t *testing.T, ctx context.Context, q pragmaQuerier) {
	t.Helper()

	var journalMode string
	require.NoError(t, q.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode))
	require.Equal(t, "wal", strings.ToLower(journalMode))

	var foreignKeys int
	require.NoError(t, q.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys))
	require.Equal(t, 1, foreignKeys)

	var busyTimeout int
	require.NoError(t, q.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout))
	require.Equal(t, defaultBusyTimeoutMillis, busyTimeout)

	rows, err := q.QueryContext(ctx, "PRAGMA foreign_key_check")
	require.NoError(t, err)
	defer rows.Close()
	if rows.Next() {
		t.Fatal("PRAGMA foreign_key_check reported violations")
	}

	var integrity string
	require.NoError(t, q.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity))
	require.Equal(t, "ok", strings.ToLower(integrity))
}

func verifySchema(t *testing.T, ctx context.Context, conn *sql.DB) {
	t.Helper()

	actualTables := make(map[string]struct{})
	rows, err := conn.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	require.NoError(t, err)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		actualTables[name] = struct{}{}
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())

	for table := range expectedSchema {
		require.Containsf(t, actualTables, table, "expected table %s to exist", table)
	}

	for table, expectedCols := range expectedSchema {
		pragma := fmt.Sprintf("PRAGMA table_info(%q)", table)
		colRows, err := conn.QueryContext(ctx, pragma)
		require.NoErrorf(t, err, "failed to inspect columns for table %s", table)

		columns := make(map[string]struct {
			Type       string
			PrimaryKey bool
		})
		for colRows.Next() {
			var (
				cid       int
				name      string
				typ       string
				notNull   int
				dfltValue sql.NullString
				pk        int
			)
			require.NoError(t, colRows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk))
			columns[name] = struct {
				Type       string
				PrimaryKey bool
			}{
				Type:       typ,
				PrimaryKey: pk > 0,
			}
		}
		require.NoError(t, colRows.Err())
		require.NoError(t, colRows.Close())

		require.Lenf(t, columns, len(expectedCols), "table %s column count mismatch", table)
		for _, spec := range expectedCols {
			actual, ok := columns[spec.Name]
			require.Truef(t, ok, "table %s missing column %s", table, spec.Name)
			require.Truef(t, strings.EqualFold(actual.Type, spec.Type), "table %s column %s type mismatch: expected %s got %s", table, spec.Name, spec.Type, actual.Type)
			require.Equalf(t, spec.PrimaryKey, actual.PrimaryKey, "table %s column %s primary key expectation mismatch", table, spec.Name)
		}
	}
}

func verifyIndexes(t *testing.T, ctx context.Context, conn *sql.DB) {
	t.Helper()

	for table, indexes := range expectedIndexes {
		for _, index := range indexes {
			var name string
			err := conn.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='index' AND tbl_name = ? AND name = ?", table, index).Scan(&name)
			require.NoErrorf(t, err, "expected index %s on table %s", index, table)
			require.Equal(t, index, name)
		}
	}
}

func verifyTriggers(t *testing.T, ctx context.Context, conn *sql.DB) {
	t.Helper()

	for _, trigger := range expectedTriggers {
		var name string
		err := conn.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='trigger' AND name = ?", trigger).Scan(&name)
		require.NoErrorf(t, err, "expected trigger %s to exist", trigger)
		require.Equal(t, trigger, name)
	}
}
