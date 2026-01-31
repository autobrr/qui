// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package database provides a SQLite database layer with string interning.
//
// WRITE CONCURRENCY MODEL:
//
// Single writer connection with read-only reader pool architecture:
//   - writerConn: Single connection (SetMaxOpenConns=1) for all write operations
//   - readerPool: Read-only connection pool for concurrent reads
//   - ExecContext: Routes writes to writerConn, reads to readerPool
//   - QueryContext: Routes writes to writerConn, reads to readerPool
//   - QueryRowContext: Routes writes to writerConn, reads to readerPool
//   - BeginTx (write): Uses writerConn, fully serialized by writerMu mutex
//   - BeginTx (read-only): Uses readerPool (concurrent)
//   - WAL mode allows concurrent readers during writes
//
// The single writer connection + writerMu mutex eliminates both SQLITE_BUSY errors
// and "cannot start a transaction within a transaction" errors by fully serializing
// all write transactions. Only one write transaction can be active at a time.
//
// STRING POOL SYSTEM:
//
// String interning uses the database string_pool table as the source of truth.
// String operations are handled through dbinterface package functions that work
// within transactions using INSERT ... ON CONFLICT for deduplication.
//
// CLEANUP CONCURRENCY PROTECTION:
//
// Periodic cleanup removes unused strings from string_pool:
//   - Runs automatically every 24 hours
//   - atomic.Bool prevents overlapping cleanup operations
//   - Uses write transaction for atomicity
//
// FAILURE MODES & RECOVERY:
//
// 1. Cleanup fails:
//   - Strings remain in database (orphaned but harmless)
//   - Next cleanup (24hrs) will retry
//   - Disk space gradually increases until next successful cleanup
//
// MONITORING:
//
// Use GetStringPoolMetrics() to monitor:
//   - cleanupDeleted = total strings removed (growing = healthy cleanup)
package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	"github.com/rs/zerolog/log"
	"modernc.org/sqlite"

	"github.com/autobrr/qui/internal/dbinterface"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// reader/writer fields on DB
type DB struct {
	writerConn  *sql.DB                            // Single connection for all writes (SetMaxOpenConns=1)
	readerPool  *sql.DB                            // Read-only connection pool for concurrent reads
	writerStmts *ttlcache.Cache[string, *sql.Stmt] // Prepared statements for writer connection
	readerStmts *ttlcache.Cache[string, *sql.Stmt] // Prepared statements for reader pool
	stmtMu      sync.RWMutex                       // Protects stmt caches during Close and cache ops

	// Write transaction serialization
	// Even though writerConn has SetMaxOpenConns=1, BeginTx doesn't queue properly
	// and fails immediately with "cannot start a transaction within a transaction"
	// This mutex ensures write transactions are properly serialized
	writerMu sync.Mutex

	// Metrics for string pool cache performance
	cleanupDeleted atomic.Uint64 // Total strings deleted by cleanup

	// Cleanup coordination
	cleanupRunning atomic.Bool // Prevents concurrent cleanup operations

	// Cleanup cancellation
	cleanupCancel context.CancelFunc

	closing atomic.Bool

	closeOnce sync.Once
	closeErr  error
}

// Tx wraps sql.Tx to provide prepared statement caching for transaction queries
type Tx struct {
	tx         *sql.Tx
	db         *DB
	ctx        context.Context // context from BeginTx, used for commit/rollback
	isWriteTx  bool            // true if this is a write transaction that needs serialized commit
	unlockFn   func()          // function to unlock writerMu when transaction completes (write tx only)
	unlockOnce sync.Once       // ensures unlock happens only once

	// Track statements prepared during this transaction for promotion to DB cache after commit
	txStmts map[string]struct{} // query -> struct{} (used as set to track which queries to cache)
	txMu    sync.Mutex          // protects txStmts map
}

// markQueryForCaching marks a query for promotion to the DB cache
func (t *Tx) markQueryForCaching(query string) {
	t.txMu.Lock()
	if t.txStmts == nil {
		t.txStmts = make(map[string]struct{})
	}
	t.txStmts[query] = struct{}{}
	t.txMu.Unlock()
}

type txExecResult struct{ tx *Tx }

func (e txExecResult) execStmt(stmt *sql.Stmt, ctx context.Context, args []any) (sql.Result, error) {
	return stmt.ExecContext(ctx, args...)
}

func (e txExecResult) execDirect(_ *sql.DB, ctx context.Context, query string, args []any) (sql.Result, error) {
	e.tx.markQueryForCaching(query)
	return e.tx.tx.ExecContext(ctx, query, args...)
}

func (txExecResult) getErr(sql.Result) error { return nil }
func (e txExecResult) getTx() *Tx            { return e.tx }

type txQueryRows struct{ tx *Tx }

func (q txQueryRows) execStmt(stmt *sql.Stmt, ctx context.Context, args []any) (*sql.Rows, error) {
	return stmt.QueryContext(ctx, args...)
}

func (q txQueryRows) execDirect(_ *sql.DB, ctx context.Context, query string, args []any) (*sql.Rows, error) {
	q.tx.markQueryForCaching(query)
	return q.tx.tx.QueryContext(ctx, query, args...)
}

func (txQueryRows) getErr(r *sql.Rows) error {
	if r == nil {
		return nil
	}
	return r.Err()
}
func (q txQueryRows) getTx() *Tx { return q.tx }

// ExecContext executes a query within the transaction.
// Uses connection-specific statement cache when available. If statement is not cached,
// prepares it on the transaction and marks it for promotion to DB cache after commit.
func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return execWithRetry(t.db, ctx, query, args, txExecResult{tx: t})
}

// QueryContext executes a query within the transaction.
// Uses connection-specific statement cache when available. If statement is not cached,
// prepares it on the transaction and marks it for promotion to DB cache after commit.
func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return execWithRetry(t.db, ctx, query, args, txQueryRows{tx: t})
}

// QueryRowContext executes a query within the transaction.
// Uses connection-specific statement cache when available. If statement is not cached,
// prepares it on the transaction and marks it for promotion to DB cache after commit.
func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	stmt, err := t.db.getStmt(ctx, query, t)
	if err != nil {
		t.markQueryForCaching(query)
		return t.tx.QueryRowContext(ctx, query, args...)
	}

	row := stmt.QueryRowContext(ctx, args...)
	if row.Err() == nil || !strings.Contains(row.Err().Error(), stmtClosedErrMsg) {
		return row
	}

	// Statement was evicted and closed between prepare and exec; drop from cache and retry
	t.db.deleteStmt(query, t.isWriteTx)

	stmt, err = t.db.getStmt(ctx, query, t)
	if err != nil {
		t.markQueryForCaching(query)
		return t.tx.QueryRowContext(ctx, query, args...)
	}
	return stmt.QueryRowContext(ctx, args...)
}

// Commit commits the transaction and releases the writer mutex if this is a write transaction.
// Also promotes any transaction-prepared statements to the DB cache for future use.
// On failure, the transaction remains active - caller must call Rollback() to release the mutex.
func (t *Tx) Commit() error {
	err := t.tx.Commit()
	if err == nil {
		// Commit succeeded - promote statements to cache
		t.promoteStatementsToCache()
		// Release mutex only on successful commit (for write transactions)
		if t.unlockFn != nil {
			t.unlockOnce.Do(t.unlockFn)
		}
	}
	return err
}

// Rollback rolls back the transaction and releases the writer mutex if this is a write transaction.
// Always releases the mutex since the transaction is done (either rolled back successfully,
// or already closed from a prior failed commit returning ErrTxDone).
// Does NOT promote statements to cache since the transaction failed.
func (t *Tx) Rollback() error {
	err := t.tx.Rollback()
	// Always release mutex - transaction is done regardless of rollback result
	if t.unlockFn != nil {
		t.unlockOnce.Do(t.unlockFn)
	}
	return err
}

// promoteStatementsToCache prepares and caches statements that were used during the transaction
// but weren't already in the cache. This is called after successful commit.
func (t *Tx) promoteStatementsToCache() {
	t.txMu.Lock()
	queries := t.txStmts
	t.txStmts = nil // Clear the map
	t.txMu.Unlock()

	if len(queries) == 0 {
		return
	}

	// Prepare statements on the appropriate connection and add to cache
	// Use a background context since transaction is already committed
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for query := range queries {
		// Skip promotion during shutdown
		if t.db.closing.Load() {
			return
		}

		t.db.stmtMu.RLock()

		// Determine which cache and connection to use based on transaction type
		var stmts *ttlcache.Cache[string, *sql.Stmt]
		var conn *sql.DB
		if t.isWriteTx {
			stmts = t.db.writerStmts
			conn = t.db.writerConn
		} else {
			stmts = t.db.readerStmts
			conn = t.db.readerPool
		}

		if stmts == nil || conn == nil {
			t.db.stmtMu.RUnlock()
			return
		}

		// Double-check it's not already cached (race condition protection)
		if _, found := stmts.Get(query); found {
			t.db.stmtMu.RUnlock()
			continue
		}

		// Prepare and cache the statement
		stmt, err := conn.PrepareContext(ctx, query)
		if err != nil {
			t.db.stmtMu.RUnlock()
			continue // silently skip - caching is best-effort
		}

		stmts.Set(query, stmt, ttlcache.DefaultTTL)
		t.db.stmtMu.RUnlock()
	}
}

const (
	defaultBusyTimeout       = 5 * time.Second
	defaultBusyTimeoutMillis = int(defaultBusyTimeout / time.Millisecond)
	connectionSetupTimeout   = 5 * time.Second
)

var driverInit sync.Once

type pragmaExecFn func(ctx context.Context, stmt string) error

func registerConnectionHook() {
	driverInit.Do(func() {
		sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, dsn string) error {
			ctx, cancel := context.WithTimeout(context.Background(), connectionSetupTimeout)
			defer cancel()

			readOnly := isReadOnlyDSN(dsn)

			return applyConnectionPragmas(ctx, func(ctx context.Context, stmt string) error {
				_, err := conn.ExecContext(ctx, stmt, nil)
				if err != nil {
					return fmt.Errorf("connection hook exec %q: %w", stmt, err)
				}
				return nil
			}, readOnly)
		})
	})
}

func isReadOnlyDSN(dsn string) bool {
	queryStart := strings.IndexByte(dsn, '?')
	if queryStart == -1 {
		return false
	}
	query := dsn[queryStart+1:]
	for _, segment := range strings.FieldsFunc(query, func(r rune) bool {
		return r == '&' || r == ';'
	}) {
		if segment == "mode=ro" {
			return true
		}
	}
	return false
}

type pragmaDirective struct {
	stmt          string
	allowReadOnly bool
}

var connectionPragmas = []pragmaDirective{
	{stmt: "PRAGMA journal_mode = WAL", allowReadOnly: false},
	{stmt: "PRAGMA synchronous = NORMAL", allowReadOnly: false}, // NORMAL is safe with WAL and much faster than FULL
	{stmt: "PRAGMA mmap_size = 268435456", allowReadOnly: true}, // 256MB memory-mapped I/O for better performance
	{stmt: "PRAGMA page_size = 4096", allowReadOnly: false},     // Optimal page size for modern systems
	{stmt: "PRAGMA cache_size = -64000", allowReadOnly: true},   // 64MB cache (negative = KB, positive = pages)
	{stmt: "PRAGMA foreign_keys = ON", allowReadOnly: true},
	{stmt: fmt.Sprintf("PRAGMA busy_timeout = %d", defaultBusyTimeoutMillis), allowReadOnly: true},
	{stmt: "PRAGMA analysis_limit = 400", allowReadOnly: true},
}

func applyConnectionPragmas(ctx context.Context, exec pragmaExecFn, readOnly bool) error {
	for _, pragma := range connectionPragmas {
		if readOnly && !pragma.allowReadOnly {
			continue
		}
		if err := exec(ctx, pragma.stmt); err != nil {
			return fmt.Errorf("apply connection pragma %q: %w", pragma.stmt, err)
		}
	}
	return nil
}

func New(databasePath string) (*DB, error) {
	log.Info().Msgf("Initializing database at: %s", databasePath)

	// Ensure the directory exists
	dir := filepath.Dir(databasePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory %s: %w", dir, err)
	}
	log.Debug().Msgf("Database directory ensured: %s", dir)

	registerConnectionHook()

	// Open writer connection (single connection for all writes)
	writerConn, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open writer connection at %s: %w", databasePath, err)
	}

	// CRITICAL: Use only 1 connection for writer to serialize all writes
	writerConn.SetMaxOpenConns(1)
	writerConn.SetMaxIdleConns(1)
	writerConn.SetConnMaxLifetime(0)
	log.Debug().Msg("Writer connection opened with single connection")

	ctx, cancel := context.WithTimeout(context.Background(), connectionSetupTimeout)
	defer cancel()
	if err := applyConnectionPragmas(ctx, func(ctx context.Context, stmt string) error {
		_, execErr := writerConn.ExecContext(ctx, stmt)
		return execErr
	}, false); err != nil {
		writerConn.Close()
		return nil, err
	}

	if _, err := writerConn.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		writerConn.Close()
		return nil, fmt.Errorf("apply wal checkpoint: %w", err)
	}

	// Open reader pool (read-only connection pool for concurrent reads)
	readerDSN := fmt.Sprintf("file:%s?mode=ro", databasePath)
	readerPool, err := sql.Open("sqlite", readerDSN)
	if err != nil {
		writerConn.Close()
		return nil, fmt.Errorf("failed to open reader pool at %s: %w", databasePath, err)
	}

	// Configure reader pool for concurrent reads
	readerPool.SetMaxOpenConns(0) // unlimited connections
	readerPool.SetMaxIdleConns(5) // keep more idle connections for read concurrency
	readerPool.SetConnMaxLifetime(0)
	log.Debug().Msg("Reader pool opened in read-only mode")

	// Apply connection pragmas to reader pool
	ctx2, cancel2 := context.WithTimeout(context.Background(), connectionSetupTimeout)
	defer cancel2()
	if err := applyConnectionPragmas(ctx2, func(ctx context.Context, stmt string) error {
		_, execErr := readerPool.ExecContext(ctx, stmt)
		return execErr
	}, true); err != nil {
		writerConn.Close()
		readerPool.Close()
		return nil, err
	}

	// create ttlcache for prepared statements with 5 minute TTL and deallocation func
	writerStmtOpts := ttlcache.Options[string, *sql.Stmt]{}.SetDefaultTTL(5 * time.Minute).
		SetDeallocationFunc(func(k string, s *sql.Stmt, _ ttlcache.DeallocationReason) {
			if s != nil {
				_ = s.Close()
			}
		})
	readerStmtOpts := ttlcache.Options[string, *sql.Stmt]{}.SetDefaultTTL(5 * time.Minute).
		SetDeallocationFunc(func(k string, s *sql.Stmt, _ ttlcache.DeallocationReason) {
			if s != nil {
				_ = s.Close()
			}
		})

	writerStmtsCache := ttlcache.New(writerStmtOpts)
	readerStmtsCache := ttlcache.New(readerStmtOpts)

	db := &DB{
		writerConn:  writerConn,
		readerPool:  readerPool,
		writerStmts: writerStmtsCache,
		readerStmts: readerStmtsCache,
	}

	// Run migrations with writer connection
	if err := db.migrate(); err != nil {
		writerConn.Close()
		readerPool.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// start periodic string pool cleanup
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	db.cleanupCancel = cleanupCancel
	go db.stringPoolCleanupLoop(cleanupCtx)

	// Verify database file was created
	if _, err := os.Stat(databasePath); err != nil {
		writerConn.Close()
		readerPool.Close()
		return nil, fmt.Errorf("database file was not created at %s: %w", databasePath, err)
	}
	log.Info().Msgf("Database initialized successfully at: %s", databasePath)

	return db, nil
}

// getStmt returns a prepared statement for the given query, preparing and
// caching it if necessary. Statements are cached with TTL and automatically
// closed on eviction. This is safe for concurrent use.
// Uses writerStmts for write operations and readerStmts for read operations.
// If tx is provided, uses the transaction type to determine the cache and
// returns a transaction-specific statement. If tx is nil, uses query type
// to determine the cache and returns a regular statement.
// When tx is provided, only cached statements are returned - no preparation
// is done to avoid conflicts with active transactions.
func (db *DB) getStmt(ctx context.Context, query string, tx *Tx) (*sql.Stmt, error) {
	if db.closing.Load() {
		return nil, sql.ErrConnDone
	}

	db.stmtMu.RLock()
	defer db.stmtMu.RUnlock()

	// Determine which cache to use
	var stmts *ttlcache.Cache[string, *sql.Stmt]
	var conn *sql.DB

	if tx != nil {
		// Use transaction type to determine cache
		if tx.isWriteTx {
			stmts = db.writerStmts
			conn = db.writerConn
		} else {
			stmts = db.readerStmts
			conn = db.readerPool
		}
	} else {
		// No transaction, use query type
		if isWriteQuery(query) {
			stmts = db.writerStmts
			conn = db.writerConn
		} else {
			stmts = db.readerStmts
			conn = db.readerPool
		}
	}

	// Check cache first
	if stmts == nil || conn == nil {
		return nil, sql.ErrConnDone
	}
	if s, found := stmts.Get(query); found && s != nil {
		if tx != nil {
			// Return transaction-specific statement
			return tx.tx.StmtContext(ctx, s), nil
		}
		return s, nil
	} else if tx != nil && tx.isWriteTx {
		return nil, fmt.Errorf("statement not cached")
	}

	// Slow path: prepare new statement
	// Note: Multiple goroutines might prepare the same query simultaneously,
	// but this is acceptable since:
	// 1. It's rare (only on cache miss/eviction)
	// 2. The extra statements will be garbage collected
	// 3. TTL cache will eventually converge to one statement per query
	s, err := conn.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}

	// Cache the statement - if another goroutine already cached it,
	// that's fine, this one will be closed by the deallocation function
	stmts.Set(query, s, ttlcache.DefaultTTL)

	if tx != nil {
		// Return transaction-specific statement
		return tx.tx.StmtContext(ctx, s), nil
	}

	return s, nil
}

func (db *DB) deleteStmt(query string, isWrite bool) {
	db.stmtMu.RLock()
	defer db.stmtMu.RUnlock()

	var stmts *ttlcache.Cache[string, *sql.Stmt]
	if isWrite {
		stmts = db.writerStmts
	} else {
		stmts = db.readerStmts
	}
	if stmts == nil {
		return
	}
	stmts.Delete(query)
}

// isWriteQuery efficiently determines if a query is a write operation.
// This uses a fast byte-level check to avoid string allocation and case conversion.
func isWriteQuery(query string) bool {
	// Trim leading whitespace (covers spaces, tabs, newlines, etc.)
	q := strings.TrimLeftFunc(query, unicode.IsSpace)
	if q == "" {
		return false
	}

	// We only care about the first word. Convert to upper-case for
	// case-insensitive comparison and use HasPrefix to avoid allocations
	// beyond the ToUpper call.
	upper := strings.ToUpper(q)
	return strings.HasPrefix(upper, "INSERT") ||
		strings.HasPrefix(upper, "UPDATE") ||
		strings.HasPrefix(upper, "UPSERT") ||
		strings.HasPrefix(upper, "REPLACE") ||
		strings.HasPrefix(upper, "DELETE") ||
		strings.HasPrefix(upper, "COMMIT") ||
		strings.HasPrefix(upper, "ROLLBACK") ||
		strings.HasPrefix(upper, "BEGIN") ||
		strings.HasPrefix(upper, "CREATE") ||
		strings.HasPrefix(upper, "ALTER") ||
		strings.HasPrefix(upper, "DROP") ||
		strings.HasPrefix(upper, "VACUUM")
}

const sqliteNestedTxErrSubstring = "cannot start a transaction within a transaction"

func isSQLiteNestedTxErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), sqliteNestedTxErrSubstring)
}

const stmtClosedErrMsg = "statement is closed"

type stmtExecutor[T any] interface {
	execStmt(*sql.Stmt, context.Context, []any) (T, error)
	execDirect(*sql.DB, context.Context, string, []any) (T, error)
	getErr(T) error
	getTx() *Tx // Returns tx if this is a transaction executor, nil otherwise
}

type execResult struct{}

func (execResult) execStmt(stmt *sql.Stmt, ctx context.Context, args []any) (sql.Result, error) {
	return stmt.ExecContext(ctx, args...)
}

func (execResult) execDirect(conn *sql.DB, ctx context.Context, query string, args []any) (sql.Result, error) {
	return conn.ExecContext(ctx, query, args...)
}

func (execResult) getErr(sql.Result) error { return nil }
func (execResult) getTx() *Tx              { return nil }

type queryRows struct{}

func (queryRows) execStmt(stmt *sql.Stmt, ctx context.Context, args []any) (*sql.Rows, error) {
	return stmt.QueryContext(ctx, args...)
}

func (queryRows) execDirect(conn *sql.DB, ctx context.Context, query string, args []any) (*sql.Rows, error) {
	return conn.QueryContext(ctx, query, args...)
}

func (queryRows) getErr(r *sql.Rows) error {
	if r == nil {
		return nil
	}
	return r.Err()
}
func (queryRows) getTx() *Tx { return nil }

func execWithRetry[T any, E stmtExecutor[T]](db *DB, ctx context.Context, query string, args []any, executor E) (T, error) {
	stmt, err := db.getStmt(ctx, query, executor.getTx())
	if err != nil {
		if isWriteQuery(query) {
			return executor.execDirect(db.writerConn, ctx, query, args)
		}
		return executor.execDirect(db.readerPool, ctx, query, args)
	}

	result, execErr := executor.execStmt(stmt, ctx, args)
	resultErr := executor.getErr(result)
	if (execErr == nil || !strings.Contains(execErr.Error(), stmtClosedErrMsg)) &&
		(resultErr == nil || !strings.Contains(resultErr.Error(), stmtClosedErrMsg)) {
		return result, execErr
	}

	// Statement was evicted and closed between prepare and exec; drop from cache and retry
	if isWriteQuery(query) {
		db.deleteStmt(query, true)
	} else {
		db.deleteStmt(query, false)
	}

	stmt, err = db.getStmt(ctx, query, executor.getTx())
	if err != nil {
		if isWriteQuery(query) {
			return executor.execDirect(db.writerConn, ctx, query, args)
		}
		return executor.execDirect(db.readerPool, ctx, query, args)
	}

	result, execErr = executor.execStmt(stmt, ctx, args)
	return result, execErr
}

// ExecContext routes write queries to the single writer connection and
// read queries to the reader pool. Uses prepared statements when possible.
// Do NOT use this for queries with RETURNING clauses - use QueryRowContext or QueryContext instead.
func (db *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if !isWriteQuery(query) {
		return execWithRetry(db, ctx, query, args, execResult{})
	}

	db.writerMu.Lock()
	defer db.writerMu.Unlock()

	return execWithRetry(db, ctx, query, args, execResult{})
}

// QueryContext routes write queries to the single writer connection and
// read queries to the reader pool. Uses prepared statements when possible.
func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if !isWriteQuery(query) {
		return execWithRetry(db, ctx, query, args, queryRows{})
	}

	db.writerMu.Lock()
	defer db.writerMu.Unlock()

	return execWithRetry(db, ctx, query, args, queryRows{})
}

// QueryRowContext routes write queries to the single writer connection and
// read queries to the reader pool. Uses prepared statements when possible.
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if isWriteQuery(query) {
		db.writerMu.Lock()
		row := db.queryRowUnlocked(ctx, query, args...)
		db.writerMu.Unlock()
		return row
	}
	return db.queryRowUnlocked(ctx, query, args...)
}

func (db *DB) queryRowUnlocked(ctx context.Context, query string, args ...any) *sql.Row {
	stmt, err := db.getStmt(ctx, query, nil)
	if err != nil {
		if isWriteQuery(query) {
			return db.writerConn.QueryRowContext(ctx, query, args...)
		}
		return db.readerPool.QueryRowContext(ctx, query, args...)
	}

	row := stmt.QueryRowContext(ctx, args...)
	if row.Err() == nil || !strings.Contains(row.Err().Error(), stmtClosedErrMsg) {
		return row
	}

	// Statement was evicted and closed between prepare and exec; drop from cache and retry
	if isWriteQuery(query) {
		db.deleteStmt(query, true)
	} else {
		db.deleteStmt(query, false)
	}

	stmt, err = db.getStmt(ctx, query, nil)
	if err != nil {
		if isWriteQuery(query) {
			return db.writerConn.QueryRowContext(ctx, query, args...)
		}
		return db.readerPool.QueryRowContext(ctx, query, args...)
	}
	return stmt.QueryRowContext(ctx, args...)
}

// stringPoolCleanupLoop runs periodic cleanup of unused strings from string_pool
func (db *DB) stringPoolCleanupLoop(ctx context.Context) {
	// Run cleanup once per day
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Run initial cleanup after 1 hour of startup
	initialDelay := time.NewTimer(1 * time.Hour)
	defer initialDelay.Stop()

	// Track consecutive failures to adjust cleanup frequency
	consecutiveFailures := 0
	const maxConsecutiveFailures = 5

	for {
		select {
		case <-initialDelay.C:
			// Run first cleanup after initial delay
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if deleted, err := db.CleanupUnusedStrings(cleanupCtx); err != nil {
				consecutiveFailures++
				log.Warn().Err(err).Int("consecutiveFailures", consecutiveFailures).Msg("failed to cleanup unused strings")
				if consecutiveFailures >= maxConsecutiveFailures {
					log.Error().Int("consecutiveFailures", consecutiveFailures).Msg("string pool cleanup failing repeatedly - manual intervention may be needed")
				}
			} else {
				if deleted > 0 {
					log.Debug().Msgf("Initial string pool cleanup: removed %d unused strings", deleted)
				}
				consecutiveFailures = 0 // Reset on success
			}
			cancel()

		case <-ticker.C:
			// Run periodic cleanup
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if deleted, err := db.CleanupUnusedStrings(cleanupCtx); err != nil {
				consecutiveFailures++
				log.Warn().Err(err).Int("consecutiveFailures", consecutiveFailures).Msg("failed to cleanup unused strings")
				if consecutiveFailures >= maxConsecutiveFailures {
					log.Error().Int("consecutiveFailures", consecutiveFailures).Msg("string pool cleanup failing repeatedly - manual intervention may be needed")
				}
			} else {
				if deleted > 0 {
					log.Debug().Msgf("Periodic string pool cleanup: removed %d unused strings", deleted)
				}
				consecutiveFailures = 0 // Reset on success
			}
			cancel()

		case <-ctx.Done():
			return
		}
	}
}

// BeginTx starts a transaction. Returns a wrapped transaction that uses statement caching.
//
// CONCURRENCY MODEL:
// - Read-only transactions use the reader pool (concurrent)
// - Write transactions use the single writer connection (serialized via mutex + SQLite)
// - WAL mode allows concurrent readers during write transactions
//
// STATEMENT CACHING STRATEGY:
// Uses separate prepared statement caches for writer and reader connections:
// - writerStmts: Cache for statements prepared on writerConn (single connection)
// - readerStmts: Cache for statements prepared on readerPool (concurrent connections)
//
// Transaction methods check the appropriate connection-specific cache first.
// If a statement exists in the cache and was prepared on the same connection type
// as the transaction, it can be reused safely via tx.StmtContext().
//
// If not cached, statements are prepared directly on the transaction and tracked
// for promotion to the appropriate cache after successful commit.
//
// This ensures connection safety while providing caching benefits for repeated queries.
//
// ISOLATION LEVEL:
// - SQLite defaults to SERIALIZABLE isolation (strongest guarantee)
// - Pass nil for opts to use SERIALIZABLE (recommended for most cases)
// - For read-only transactions, use: &sql.TxOptions{ReadOnly: true}
// - Read-only transactions allow full concurrency with writers in WAL mode
//
// WHEN TO USE EACH:
// - Use ExecContext for simple, single-statement writes (INSERT, UPDATE, DELETE)
// - Use BeginTx for multi-statement operations that need atomicity
// - Use BeginTx when you need to read and write in a consistent snapshot
//
// GUARANTEES:
// - ExecContext: Sequential execution through single writer connection, no partial writes visible
// - BeginTx (write): ACID properties, full transaction isolation, serialized via mutex + single writer connection
// - BeginTx (read-only): ACID properties, concurrent with writes
// - All write operations: Serialized through mutex + single writer connection (no "transaction within transaction" errors)
//
// LIMITATIONS:
// - Write transactions are serialized (one at a time) due to mutex + single writer connection
// - Long-running write transactions will block other write transactions
// - Use read-only transactions when possible to avoid blocking writes
//
// NOTE ON SERIALIZATION:
// SQLite with SetMaxOpenConns=1 does NOT properly queue BeginTx calls - it fails immediately
// with "cannot start a transaction within a transaction" instead of waiting. The writerMu
// mutex serializes write transactions for their ENTIRE lifetime (BeginTx through Commit/Rollback)
// to prevent this error. The mutex is released only when the transaction completes (Commit or Rollback).
// This means write transactions are fully serialized, but that's acceptable since SQLite can only
// handle one write transaction at a time anyway.
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	// Determine if this is a read-only transaction
	isReadOnly := opts != nil && opts.ReadOnly

	if isReadOnly {
		// Read-only transactions use the reader pool (no mutex needed, unlimited concurrency)
		tx, err := db.readerPool.BeginTx(ctx, opts)
		if err != nil {
			return nil, err
		}
		return &Tx{tx: tx, db: db, ctx: ctx, isWriteTx: false, unlockFn: nil}, nil
	}

	// Write transactions: Lock mutex for the ENTIRE transaction lifetime.
	// The mutex will be unlocked by Commit() or Rollback().
	db.writerMu.Lock()

	tx, err := db.writerConn.BeginTx(ctx, opts)
	if err != nil {
		db.writerMu.Unlock()
		if isSQLiteNestedTxErr(err) {
			// This indicates a bug: a previous transaction failed to rollback properly,
			// leaving the connection wedged. Log with stack trace to help diagnose.
			recordWedgedTransaction()
			log.Error().
				Err(err).
				Str("stack", string(debug.Stack())).
				Msg("SQLite writer connection is wedged in a transaction - this indicates a bug where a previous transaction failed to rollback properly")
			return nil, fmt.Errorf("database connection wedged: %w", err)
		}
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Pass unlock function to Tx so it can release the mutex on Commit/Rollback
	return &Tx{
		tx:        tx,
		db:        db,
		ctx:       ctx,
		isWriteTx: true,
		unlockFn:  db.writerMu.Unlock,
	}, nil
}

func (db *DB) Close() error {
	db.closeOnce.Do(func() {
		db.closing.Store(true)

		// Cancel cleanup goroutine
		if db.cleanupCancel != nil {
			db.cleanupCancel()
		}

		// Run PRAGMA optimize on writer connection before closing
		ctx, cancel := context.WithTimeout(context.Background(), connectionSetupTimeout)
		defer cancel()
		if _, err := db.writerConn.ExecContext(ctx, "PRAGMA optimize"); err != nil {
			log.Warn().Err(err).Msg("failed to run PRAGMA optimize during close")
		}

		db.stmtMu.Lock()

		// Close statement caches (will close all prepared statements)
		// Track closed caches to avoid double-closing the same cache instance
		closedCaches := make(map[*ttlcache.Cache[string, *sql.Stmt]]bool)

		if db.writerStmts != nil && !closedCaches[db.writerStmts] {
			db.writerStmts.Close()
			closedCaches[db.writerStmts] = true
			db.writerStmts = nil
		}
		if db.readerStmts != nil && !closedCaches[db.readerStmts] {
			db.readerStmts.Close()
			closedCaches[db.readerStmts] = true
			db.readerStmts = nil
		}

		db.stmtMu.Unlock()

		// Close both connections
		if err := db.writerConn.Close(); err != nil {
			db.closeErr = err
		}
		if err := db.readerPool.Close(); err != nil && db.closeErr == nil {
			db.closeErr = err
		}
	})

	return db.closeErr
}

func (db *DB) Conn() *sql.DB {
	return db.writerConn
}

func (db *DB) migrate() error {
	ctx := context.Background()

	// Create migrations table if it doesn't exist
	if _, err := db.writerConn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			filename TEXT NOT NULL UNIQUE,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
		`); err != nil {
			return fmt.Errorf("failed to create migrations table: %w", err)
		}

	// Handle historical migration file renames.
	// If we ever rename an embedded migration file, we must update the filename
	// stored in the migrations table so existing databases don't re-run it.
	if err := db.normalizeMigrationFilenames(ctx); err != nil {
		return fmt.Errorf("failed to normalize migration filenames: %w", err)
	}

	// Get all migration files
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Sort migration files by name
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	// Find pending migrations
	pendingMigrations, err := db.findPendingMigrations(ctx, files)
	if err != nil {
		return fmt.Errorf("failed to find pending migrations: %w", err)
	}

	if len(pendingMigrations) == 0 {
		log.Debug().Msg("No pending migrations")
		return nil
	}

	// Apply all pending migrations in a single transaction
	if err := db.applyAllMigrations(ctx, pendingMigrations); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	return nil
}

func (db *DB) normalizeMigrationFilenames(ctx context.Context) error {
	renames := []struct {
		from string
		to   string
	}{
		{
			from: "052_add_dir_scan.sql",
			to:   "053_add_dir_scan.sql",
		},
	}

	for _, r := range renames {
		// If both entries exist, keep the new name.
		if _, err := db.writerConn.ExecContext(ctx, `
			DELETE FROM migrations
			WHERE filename = ?
			  AND EXISTS (SELECT 1 FROM migrations WHERE filename = ?)
		`, r.from, r.to); err != nil {
			return fmt.Errorf("failed to dedupe migration %s -> %s: %w", r.from, r.to, err)
		}

		// Rename old -> new if the new entry doesn't already exist.
		if _, err := db.writerConn.ExecContext(ctx, `
			UPDATE migrations
			SET filename = ?
			WHERE filename = ?
			  AND NOT EXISTS (SELECT 1 FROM migrations WHERE filename = ?)
		`, r.to, r.from, r.to); err != nil {
			return fmt.Errorf("failed to rename migration %s -> %s: %w", r.from, r.to, err)
		}
	}

	return nil
}

func (db *DB) findPendingMigrations(ctx context.Context, allFiles []string) ([]string, error) {
	var pendingMigrations []string

	for _, filename := range allFiles {
		var count int
		err := db.writerConn.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE filename = ?", filename).Scan(&count)
		if err != nil {
			return nil, fmt.Errorf("failed to check migration status for %s: %w", filename, err)
		}

		if count == 0 {
			pendingMigrations = append(pendingMigrations, filename)
		}
	}

	return pendingMigrations, nil
}

// applyAllMigrations applies pending database migrations in order.
//
// MIGRATION FAILURE HANDLING:
// If a migration fails, any strings interned during the migration will remain in string_pool.
// These orphaned strings will be automatically cleaned up by the periodic cleanup loop (24hrs).
// This is acceptable because:
// - Orphaned strings are harmless (just unused rows)
// - CleanupUnusedStrings runs automatically and will remove them
// - Manual cleanup can be triggered with: db.CleanupUnusedStrings(ctx)
// - The migration system prevents retrying failed migrations automatically
//
// For immediate cleanup after a failed migration, manually run:
//
//	db.CleanupUnusedStrings(context.Background())
func (db *DB) applyAllMigrations(ctx context.Context, migrations []string) error {
	// Migrations that need foreign keys disabled due to table recreation
	needsForeignKeysOff := map[string]bool{
		"010_add_files_cache_and_string_interning.sql": true,
	}

	// Begin single transaction for all migrations using BeginTx for proper connection handling
	tx, err := db.writerConn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// CRITICAL: Track whether we have an active transaction to rollback
	// This prevents double-rollback issues when recreating transactions mid-migration
	rollbackActive := func() {
		if tx != nil {
			tx.Rollback()
			tx = nil
		}
	}
	defer rollbackActive()

	// Apply each migration within the transaction
	for _, filename := range migrations {
		// Check if this migration needs foreign keys disabled
		if needsForeignKeysOff[filename] {
			// Commit current transaction before disabling foreign keys
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("failed to commit before %s: %w", filename, err)
			}
			tx = nil // Clear tx so rollbackActive() won't try to rollback committed tx

			// Ensure foreign keys are re-enabled even if migration fails
			defer func() {
				if _, err := db.writerConn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
					log.Error().Err(err).Msg("CRITICAL: Failed to re-enable foreign keys after migration - manual intervention required")
				}
			}()

			// Disable foreign keys (must be done outside transaction)
			if _, err := db.writerConn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
				return fmt.Errorf("failed to disable foreign keys for %s: %w", filename, err)
			}

			// Run migration within explicit transaction while foreign keys are disabled
			if err := func() error {
				fkTx, err := db.writerConn.BeginTx(ctx, nil)
				if err != nil {
					return fmt.Errorf("failed to begin migration transaction for %s: %w", filename, err)
				}
				committed := false
				defer func() {
					if !committed {
						if rollErr := fkTx.Rollback(); rollErr != nil && !errors.Is(rollErr, sql.ErrTxDone) {
							log.Error().Err(rollErr).Str("migration", filename).Msg("rollback failed for migration transaction")
						}
					}
				}()

				content, err := migrationsFS.ReadFile("migrations/" + filename)
				if err != nil {
					return fmt.Errorf("failed to read migration file %s: %w", filename, err)
				}

				if _, err := fkTx.ExecContext(ctx, string(content)); err != nil {
					return fmt.Errorf("failed to execute migration %s: %w", filename, err)
				}

				if _, err := fkTx.ExecContext(ctx, "INSERT INTO migrations (filename) VALUES (?)", filename); err != nil {
					return fmt.Errorf("failed to record migration %s: %w", filename, err)
				}

				if err := fkTx.Commit(); err != nil {
					return fmt.Errorf("failed to commit migration %s: %w", filename, err)
				}
				committed = true
				return nil
			}(); err != nil {
				return err
			}

			// Re-enable foreign keys explicitly
			if _, err := db.writerConn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
				return fmt.Errorf("failed to re-enable foreign keys after %s: %w", filename, err)
			}

			// Start new transaction for remaining migrations using BeginTx
			tx, err = db.writerConn.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to begin new transaction after %s: %w", filename, err)
			}
			// rollbackActive() will handle this new transaction via the defer
		} else {
			// Normal migration within transaction
			// Read migration file
			content, err := migrationsFS.ReadFile("migrations/" + filename)
			if err != nil {
				return fmt.Errorf("failed to read migration file %s: %w", filename, err)
			}

			// Execute migration
			if _, err := tx.ExecContext(ctx, string(content)); err != nil {
				return fmt.Errorf("failed to execute migration %s: %w", filename, err)
			}

			// Record migration
			if _, err := tx.ExecContext(ctx, "INSERT INTO migrations (filename) VALUES (?)", filename); err != nil {
				return fmt.Errorf("failed to record migration %s: %w", filename, err)
			}
		}
	}

	// Commit final transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migrations: %w", err)
	}
	tx = nil // Clear tx so rollbackActive() won't try to rollback committed tx

	log.Info().Msgf("Applied %d migrations successfully", len(migrations))
	return nil
}

// CleanupUnusedStrings removes strings from the string_pool table that are no longer
// referenced by any other table. This should be run periodically to reclaim storage space.
// Returns the number of strings deleted and any error encountered.
// Also clears the string ID cache to ensure consistency.
//
// IMPORTANT: This operation is protected against concurrent execution. If a cleanup is
// already running, subsequent calls will return immediately with (0, nil). This prevents
// excessive cache thrashing and database contention from overlapping cleanup operations.
//
// NOTE: Uses an optimized temp table approach with a single UNION ALL query to minimize
// transaction time while maintaining data consistency.
const referencedStringsInsertQuery = `
	SELECT torrent_hash_id AS string_id FROM torrent_files_cache WHERE torrent_hash_id IS NOT NULL
	UNION ALL
	SELECT name_id AS string_id FROM torrent_files_cache WHERE name_id IS NOT NULL
	UNION ALL
	SELECT torrent_hash_id AS string_id FROM torrent_files_sync WHERE torrent_hash_id IS NOT NULL
	UNION ALL
	SELECT torrent_hash_id AS string_id FROM instance_backup_items WHERE torrent_hash_id IS NOT NULL
	UNION ALL
	SELECT name_id AS string_id FROM instance_backup_items WHERE name_id IS NOT NULL
	UNION ALL
	SELECT category_id AS string_id FROM instance_backup_items WHERE category_id IS NOT NULL
	UNION ALL
	SELECT tags_id AS string_id FROM instance_backup_items WHERE tags_id IS NOT NULL
	UNION ALL
	SELECT archive_rel_path_id AS string_id FROM instance_backup_items WHERE archive_rel_path_id IS NOT NULL
	UNION ALL
	SELECT infohash_v1_id AS string_id FROM instance_backup_items WHERE infohash_v1_id IS NOT NULL
	UNION ALL
	SELECT infohash_v2_id AS string_id FROM instance_backup_items WHERE infohash_v2_id IS NOT NULL
	UNION ALL
	SELECT torrent_blob_path_id AS string_id FROM instance_backup_items WHERE torrent_blob_path_id IS NOT NULL
	UNION ALL
	SELECT kind_id AS string_id FROM instance_backup_runs WHERE kind_id IS NOT NULL
	UNION ALL
	SELECT status_id AS string_id FROM instance_backup_runs WHERE status_id IS NOT NULL
	UNION ALL
	SELECT requested_by_id AS string_id FROM instance_backup_runs WHERE requested_by_id IS NOT NULL
	UNION ALL
	SELECT error_message_id AS string_id FROM instance_backup_runs WHERE error_message_id IS NOT NULL
	UNION ALL
	SELECT archive_path_id AS string_id FROM instance_backup_runs WHERE archive_path_id IS NOT NULL
	UNION ALL
	SELECT manifest_path_id AS string_id FROM instance_backup_runs WHERE manifest_path_id IS NOT NULL
	UNION ALL
	SELECT name_id AS string_id FROM instances WHERE name_id IS NOT NULL
	UNION ALL
	SELECT host_id AS string_id FROM instances WHERE host_id IS NOT NULL
	UNION ALL
	SELECT username_id AS string_id FROM instances WHERE username_id IS NOT NULL
	UNION ALL
	SELECT basic_username_id AS string_id FROM instances WHERE basic_username_id IS NOT NULL
	UNION ALL
	SELECT name_id AS string_id FROM api_keys WHERE name_id IS NOT NULL
	UNION ALL
	SELECT client_name_id AS string_id FROM client_api_keys WHERE client_name_id IS NOT NULL
	UNION ALL
	SELECT error_type_id AS string_id FROM instance_errors WHERE error_type_id IS NOT NULL
	UNION ALL
	SELECT error_message_id AS string_id FROM instance_errors WHERE error_message_id IS NOT NULL
	UNION ALL
	SELECT name_id AS string_id FROM torznab_indexers WHERE name_id IS NOT NULL
	UNION ALL
	SELECT base_url_id AS string_id FROM torznab_indexers WHERE base_url_id IS NOT NULL
	UNION ALL
	SELECT indexer_id_string_id AS string_id FROM torznab_indexers WHERE indexer_id_string_id IS NOT NULL
	UNION ALL
	SELECT capability_type_id AS string_id FROM torznab_indexer_capabilities WHERE capability_type_id IS NOT NULL
	UNION ALL
	SELECT category_name_id AS string_id FROM torznab_indexer_categories WHERE category_name_id IS NOT NULL
	UNION ALL
	SELECT error_message_id AS string_id FROM torznab_indexer_errors WHERE error_message_id IS NOT NULL
	UNION ALL
	SELECT name_id AS string_id FROM arr_instances WHERE name_id IS NOT NULL
	UNION ALL
	SELECT base_url_id AS string_id FROM arr_instances WHERE base_url_id IS NOT NULL
`

func (db *DB) CleanupUnusedStrings(ctx context.Context) (int64, error) {
	// Prevent concurrent cleanup operations
	if !db.cleanupRunning.CompareAndSwap(false, true) {
		log.Debug().Msg("String pool cleanup already in progress, skipping")
		return 0, nil
	}
	defer db.cleanupRunning.Store(false)

	// Drop temp table if it exists from previous run
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS temp_referenced_strings")

	// Ensure temp table is cleaned up at the end
	defer func() {
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS temp_referenced_strings")
	}()

	// Create temp table for referenced string IDs (automatically indexed due to PRIMARY KEY)
	_, err := db.ExecContext(ctx, `
		CREATE TEMP TABLE temp_referenced_strings (
			string_id INTEGER PRIMARY KEY
		)
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to create temp table: %w", err)
	}

	// Begin transaction for the actual cleanup work
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// CRITICAL: Defer foreign key checks until end of transaction
	// All string_pool references use ON DELETE RESTRICT which would prevent deletion
	// even when there are no actual references. Deferring allows the transaction to
	// complete and verify constraints at commit time rather than immediately.
	if err := dbinterface.DeferForeignKeyChecks(tx); err != nil {
		return 0, fmt.Errorf("failed to defer foreign keys: %w", err)
	}

	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO temp_referenced_strings (string_id) SELECT string_id FROM (
`+referencedStringsInsertQuery+`
		) AS subquery`)
	if err != nil {
		return 0, fmt.Errorf("failed to populate temp table with referenced string IDs: %w", err)
	}

	// Delete strings not in the temp table - fast due to PRIMARY KEY index on temp table
	// Using NOT EXISTS instead of NOT IN to avoid any potential SQLite limitations
	result, err := tx.ExecContext(ctx, `
		DELETE FROM string_pool 
		WHERE NOT EXISTS (
			SELECT 1 FROM temp_referenced_strings trs WHERE trs.string_id = string_pool.id
		)
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to delete unused strings: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	// Update cleanup metrics
	if rowsAffected > 0 {
		db.cleanupDeleted.Add(uint64(rowsAffected))
		log.Debug().Msgf("Cleaned up %d unused strings from string_pool (total deleted: %d)",
			rowsAffected, db.cleanupDeleted.Load())
	}

	return rowsAffected, nil
}

// NewForTest wraps an existing sql.DB connection for testing purposes.
// This creates a minimal DB wrapper without running migrations or starting
// background goroutines. The caller is responsible for managing the underlying
// sql.DB connection lifecycle.
//
// IMPORTANT LIMITATIONS FOR TESTING:
// - Does NOT start the stringPoolCleanupLoop (automatic cleanup is disabled)
// - Tests must manually call CleanupUnusedStrings() if testing cleanup behavior
// - String pool may grow unbounded during test execution
// - Tests should use short-lived databases or manually clean up
// - Uses the same connection for both reads and writes (no separate reader pool)
//
// This differs from production where:
// - stringPoolCleanupLoop runs automatically every 24 hours
// - Unused strings are automatically removed
// - String pool size is bounded
// - Separate writer connection and reader pool for better concurrency
//
// Note: This function is intended for testing only and should not be used in
// production code. Use New() for production database initialization.
func NewForTest(conn *sql.DB) *DB {
	stmtOpts := ttlcache.Options[string, *sql.Stmt]{}.SetDefaultTTL(5 * time.Minute).
		SetDeallocationFunc(func(k string, s *sql.Stmt, _ ttlcache.DeallocationReason) {
			if s != nil {
				_ = s.Close()
			}
		})

	stmtsCache := ttlcache.New(stmtOpts)

	db := &DB{
		writerConn:  conn,
		readerPool:  conn,       // For tests, use same connection for both
		writerStmts: stmtsCache, // For tests, use same cache for both
		readerStmts: stmtsCache, // For tests, use same cache for both
	}

	// Note: stringPoolCleanupLoop is NOT started for tests
	// Tests that need cleanup must call CleanupUnusedStrings() explicitly

	return db
}

// GetStringPoolMetrics returns the current values of string pool cleanup metrics.
// These metrics track cleanup activity:
//   - cleanupDeleted: Total number of unused strings deleted since startup
//
// This counter is cumulative and never resets. Use it to track cleanup effectiveness.
func (db *DB) GetStringPoolMetrics() (cleanupDeleted uint64) {
	return db.cleanupDeleted.Load()
}
