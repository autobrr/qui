// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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

	// Set max connections to 4: 1 for writeConn + 2 test connections + 1 buffer
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(3)

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

func TestReadOnlyConnectionsDoNotApplyWritePragmas(t *testing.T) {
	log.Output(io.Discard)
	ctx := t.Context()
	statementsRW := make([]string, 0, 8)
	require.NoError(t, applyConnectionPragmas(ctx, func(ctx context.Context, stmt string) error {
		statementsRW = append(statementsRW, stmt)
		return nil
	}, false))
	require.Contains(t, statementsRW, "PRAGMA journal_mode = WAL", "write connections must set journal_mode")

	statementsRO := make([]string, 0, 8)
	require.NoError(t, applyConnectionPragmas(ctx, func(ctx context.Context, stmt string) error {
		statementsRO = append(statementsRO, stmt)
		return nil
	}, true))
	require.NotContains(t, statementsRO, "PRAGMA journal_mode = WAL", "read-only connections must not attempt to set journal_mode")
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
		{Name: "host_id", Type: "INTEGER"},
		{Name: "username_id", Type: "INTEGER"},
		{Name: "password_encrypted", Type: "TEXT"},
		{Name: "basic_username_id", Type: "INTEGER"},
		{Name: "basic_password_encrypted", Type: "TEXT"},
		{Name: "tls_skip_verify", Type: "BOOLEAN"},
		{Name: "sort_order", Type: "INTEGER"},
		{Name: "is_active", Type: "BOOLEAN"},
		{Name: "has_local_filesystem_access", Type: "BOOLEAN"},
		{Name: "use_hardlinks", Type: "BOOLEAN"},
		{Name: "hardlink_base_dir", Type: "TEXT"},
		{Name: "hardlink_dir_preset", Type: "TEXT"},
		{Name: "use_reflinks", Type: "BOOLEAN"},
		{Name: "fallback_to_regular_mode", Type: "BOOLEAN"},
	},
	"licenses": {
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "license_key", Type: "TEXT"},
		{Name: "product_name", Type: "TEXT"},
		{Name: "status", Type: "TEXT"},
		{Name: "activated_at", Type: "DATETIME"},
		{Name: "expires_at", Type: "DATETIME"},
		{Name: "last_validated", Type: "DATETIME"},
		{Name: "provider", Type: "TEXT"},
		{Name: "dodo_instance_id", Type: "TEXT"},
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
	"automations": {
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "instance_id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
		{Name: "tracker_pattern", Type: "TEXT"},
		{Name: "conditions", Type: "TEXT"},
		{Name: "enabled", Type: "INTEGER"},
		{Name: "dry_run", Type: "INTEGER"},
		{Name: "sort_order", Type: "INTEGER"},
		{Name: "interval_seconds", Type: "INTEGER"},
		{Name: "free_space_source", Type: "TEXT"},
		{Name: "created_at", Type: "DATETIME"},
		{Name: "updated_at", Type: "DATETIME"},
		{Name: "sorting_config", Type: "TEXT"},
	},
	"automation_activity": {
		{Name: "id", Type: "INTEGER", PrimaryKey: true},
		{Name: "instance_id", Type: "INTEGER"},
		{Name: "hash", Type: "TEXT"},
		{Name: "torrent_name", Type: "TEXT"},
		{Name: "tracker_domain", Type: "TEXT"},
		{Name: "action", Type: "TEXT"},
		{Name: "rule_id", Type: "INTEGER"},
		{Name: "rule_name", Type: "TEXT"},
		{Name: "outcome", Type: "TEXT"},
		{Name: "reason", Type: "TEXT"},
		{Name: "details", Type: "TEXT"},
		{Name: "created_at", Type: "DATETIME"},
	},
}

var expectedIndexes = map[string][]string{
	"instances":           {"idx_instances_sort_order", "idx_instances_is_active"},
	"licenses":            {"idx_licenses_status", "idx_licenses_theme", "idx_licenses_key"},
	"client_api_keys":     {"idx_client_api_keys_instance_id"},
	"instance_errors":     {"idx_instance_errors_lookup"},
	"sessions":            {"sessions_expiry_idx"},
	"torrent_files_cache": {"idx_torrent_files_cache_lookup", "idx_torrent_files_cache_cached_at"},
	"torrent_files_sync":  {"idx_torrent_files_sync_last_synced"},
	"automations":         {"idx_automations_instance"},
	"automation_activity": {"idx_automation_activity_instance_created"},
}

var expectedTriggers = []string{
	"update_user_updated_at",
	"cleanup_old_instance_errors",
	"trg_automations_updated",
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
	require.NoError(t, rows.Err())

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

func TestCleanupUnusedStrings(t *testing.T) {
	log.Logger = log.Output(io.Discard)
	ctx := t.Context()
	db := openTestDatabase(t)
	conn := db.Conn()

	// Get initial count of strings
	var initialCount int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM string_pool").Scan(&initialCount))

	// Insert some test strings into string_pool
	var id1, id2, id3 int64
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "referenced_string").Scan(&id1))
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "orphaned_string").Scan(&id2))
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "another_orphaned").Scan(&id3))

	// Reference id1 in instances table (create a minimal instance)
	_, err := conn.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (?, ?, ?, ?)", id1, id1, id1, "dummy_password")
	require.NoError(t, err)

	// Verify 3 more strings exist
	var count int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM string_pool").Scan(&count))
	require.Equal(t, initialCount+3, count)

	// Run cleanup
	deleted, err := db.CleanupUnusedStrings(ctx)
	require.NoError(t, err)
	require.Greater(t, deleted, int64(0)) // Should delete some orphaned strings

	// Verify our referenced string still exists
	var exists bool
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM string_pool WHERE id = ?)", id1).Scan(&exists))
	require.True(t, exists)

	// Run cleanup again - should delete nothing since temp table was properly dropped
	deleted2, err := db.CleanupUnusedStrings(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), deleted2)

	// Verify our referenced string still exists
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM string_pool WHERE id = ?)", id1).Scan(&exists))
	require.True(t, exists)
}

func TestCleanupUnusedStrings_BasicAuthStringRefs(t *testing.T) {
	log.Logger = log.Output(io.Discard)
	ctx := t.Context()
	db := openTestDatabase(t)
	conn := db.Conn()

	insertString := func(value string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", value).Scan(&id))
		return id
	}

	arrNameID := insertString("arr_name")
	arrBaseURLID := insertString("http://arr.local")
	arrBasicUserID := insertString("arr_basic_user")
	torNameID := insertString("tor_name")
	torBaseURLID := insertString("http://tor.local")
	torBasicUserID := insertString("tor_basic_user")
	orphanID := insertString("orphan_to_cleanup")

	_, err := conn.ExecContext(ctx, `
		INSERT INTO arr_instances (type, name_id, base_url_id, basic_username_id, basic_password_encrypted, api_key_encrypted, enabled, priority, timeout_seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "sonarr", arrNameID, arrBaseURLID, arrBasicUserID, "enc-basic", "enc-api", true, 0, 15)
	require.NoError(t, err)

	_, err = conn.ExecContext(ctx, `
		INSERT INTO torznab_indexers (name_id, base_url_id, basic_username_id, basic_password_encrypted, backend, api_key_encrypted, enabled, priority, timeout_seconds, limit_default, limit_max)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, torNameID, torBaseURLID, torBasicUserID, "enc-basic", "jackett", "enc-api", true, 0, 30, 100, 100)
	require.NoError(t, err)

	deleted, err := db.CleanupUnusedStrings(ctx)
	require.NoError(t, err)
	require.Positive(t, deleted)

	var exists bool
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM string_pool WHERE id = ?)", arrBasicUserID).Scan(&exists))
	require.True(t, exists, "arr basic username string must remain referenced")
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM string_pool WHERE id = ?)", torBasicUserID).Scan(&exists))
	require.True(t, exists, "torznab basic username string must remain referenced")
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM string_pool WHERE id = ?)", orphanID).Scan(&exists))
	require.False(t, exists, "orphan string should be cleaned up")
}

// TestTransactionCommitSuccessMutexRelease tests that the writer mutex is properly released
// after a successful commit.
func TestTransactionCommitSuccessMutexRelease(t *testing.T) {
	log.Logger = log.Output(io.Discard)
	ctx := t.Context()
	db := openTestDatabase(t)

	// Start a write transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Perform a simple write operation
	_, err = tx.ExecContext(ctx, "INSERT INTO string_pool (value) VALUES (?)", "test_string")
	require.NoError(t, err)

	// Commit should succeed and release the mutex
	err = tx.Commit()
	require.NoError(t, err)

	// Should be able to start another write transaction immediately (mutex was released)
	tx2, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, tx2)

	// Clean up
	require.NoError(t, tx2.Rollback())
}

// TestTransactionRollbackReleasesMutex tests that Rollback() always releases the mutex,
// even after a failed commit attempt.
func TestTransactionRollbackReleasesMutex(t *testing.T) {
	log.Logger = log.Output(io.Discard)
	ctx := t.Context()
	db := openTestDatabase(t)

	// Start a write transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Do some work
	_, err = tx.ExecContext(ctx, "INSERT INTO string_pool (value) VALUES (?)", "test_string")
	require.NoError(t, err)

	// Rollback instead of commit
	err = tx.Rollback()
	require.NoError(t, err)

	// Should be able to start another write transaction immediately (mutex was released)
	tx2, err := db.BeginTx(ctx, nil)
	require.NoError(t, err, "Should be able to start new transaction after rollback")
	require.NotNil(t, tx2)
	require.NoError(t, tx2.Rollback())
}

// TestTransactionDoubleRollbackSafe tests that calling Rollback() twice doesn't panic
// or cause mutex issues (sync.Once protects the unlock).
func TestTransactionDoubleRollbackSafe(t *testing.T) {
	log.Logger = log.Output(io.Discard)
	ctx := t.Context()
	db := openTestDatabase(t)

	// Start a write transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// Rollback twice - second call should be safe (returns ErrTxDone, but no panic)
	err = tx.Rollback()
	require.NoError(t, err)

	err = tx.Rollback()
	require.Error(t, err) // Expected: sql.ErrTxDone

	// Mutex should still be released, can start new transaction
	tx2, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, tx2.Rollback())
}

// TestTransactionCommitThenRollbackSafe tests that calling Rollback() after successful
// Commit() doesn't cause mutex issues (sync.Once protects the unlock).
func TestTransactionCommitThenRollbackSafe(t *testing.T) {
	log.Logger = log.Output(io.Discard)
	ctx := t.Context()
	db := openTestDatabase(t)

	// Start a write transaction
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	// Insert something
	_, err = tx.ExecContext(ctx, "INSERT INTO string_pool (value) VALUES (?)", "test_string")
	require.NoError(t, err)

	// Commit successfully
	err = tx.Commit()
	require.NoError(t, err)

	// Call Rollback after commit (like a deferred rollback pattern) - should be safe
	err = tx.Rollback()
	require.Error(t, err) // Expected: sql.ErrTxDone

	// Mutex should still be released, can start new transaction
	tx2, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, tx2.Rollback())
}

// TestTransactionSerialization tests that write transactions are properly serialized
// and that the mutex prevents concurrent write transactions.
func TestTransactionSerialization(t *testing.T) {
	log.Logger = log.Output(io.Discard)
	ctx := t.Context()
	db := openTestDatabase(t)

	// Channel to coordinate the test
	started := make(chan bool, 1)
	committed := make(chan bool, 1)

	// Start first transaction in a goroutine
	go func() {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Errorf("Failed to begin first transaction: %v", err)
			return
		}
		defer tx.Rollback()

		// Signal that we started
		started <- true

		// Hold the transaction for a bit
		time.Sleep(200 * time.Millisecond)

		// Insert something
		_, err = tx.ExecContext(ctx, "INSERT INTO string_pool (value) VALUES (?)", "serialization_test")
		if err != nil {
			t.Errorf("Failed to insert in first transaction: %v", err)
			return
		}

		// Commit
		err = tx.Commit()
		if err != nil {
			t.Errorf("Failed to commit first transaction: %v", err)
			return
		}

		committed <- true
	}()

	// Wait for first transaction to start
	select {
	case <-started:
		// Good, first transaction started
	case <-time.After(1 * time.Second):
		t.Fatal("First transaction didn't start")
	}

	// Now try to start a second transaction - it should block until the first commits
	start := time.Now()
	tx2, err := db.BeginTx(ctx, nil)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, tx2)

	// Should have taken at least 100ms (the sleep time in the first transaction)
	require.GreaterOrEqual(t, elapsed, 100*time.Millisecond, "Second transaction should have been blocked by first")

	// Clean up
	require.NoError(t, tx2.Rollback())

	// Wait for first transaction to complete
	select {
	case <-committed:
		// Good
	case <-time.After(1 * time.Second):
		t.Fatal("First transaction didn't commit")
	}
}

// TestReadOnlyTransactionConcurrency tests that read-only transactions can run concurrently
// with write transactions (due to WAL mode).
func TestReadOnlyTransactionConcurrency(t *testing.T) {
	log.Logger = log.Output(io.Discard)
	ctx := t.Context()
	db := openTestDatabase(t)

	// Start a write transaction
	txWrite, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, txWrite)

	// Insert something in the write transaction
	_, err = txWrite.ExecContext(ctx, "INSERT INTO string_pool (value) VALUES (?)", "concurrency_test")
	require.NoError(t, err)

	// Start a read-only transaction while write transaction is active
	txRead, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	require.NotNil(t, txRead)

	// Read transaction should be able to query (may or may not see the uncommitted write)
	var count int
	err = txRead.QueryRowContext(ctx, "SELECT COUNT(*) FROM string_pool").Scan(&count)
	require.NoError(t, err)
	// Count could be anything depending on isolation level

	// Commit the read transaction
	require.NoError(t, txRead.Rollback())

	// Commit the write transaction
	require.NoError(t, txWrite.Commit())
}
