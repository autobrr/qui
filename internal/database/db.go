// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package database provides a SQLite database layer with string interning.
//
// STRING POOL SYSTEM:
//
// The string pool system uses the database string_pool table as the source of truth.
// String interning is handled through INSERT OR IGNORE operations with retry logic.
//
// RETRY LOGIC:
//
// GetOrCreateStringID retries up to 3 times with 10ms/20ms/40ms exponential backoff:
//   - Handles rare case where cleanup deletes string between INSERT and SELECT
//   - Backoff reduces contention with cleanup operations
//
// TRANSACTION-AWARE OPERATIONS:
//
// GetOrCreateStringID accepts tx parameter:
//   - When tx != nil, uses tx.ExecContext (transaction-local)
//   - When tx == nil, uses db.ExecContext (single writer channel)
//   - Callers MUST pass tx to avoid deadlocks
//
// CLEANUP CONCURRENCY PROTECTION:
//
// atomic.Bool prevents overlapping cleanup operations:
//   - Only one cleanup can run at a time
//   - Prevents database contention
//
// FAILURE MODES & RECOVERY:
//
// 1. Cleanup fails:
//   - Strings remain in database (orphaned but harmless)
//   - Next cleanup (24hrs) will retry
//   - Disk space gradually increases until next successful cleanup
//
// 2. GetOrCreateStringID exhausts retries:
//   - Returns error to caller
//   - Caller can retry or propagate error
//   - Does not corrupt database state
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
	"fmt"
	"os"
	"path/filepath"
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

// reader/writer control
type writeReq struct {
	ctx   context.Context
	query string
	args  []any
	resCh chan writeRes
}

type writeRes struct {
	result sql.Result
	err    error
}

// reader/writer fields on DB
type DB struct {
	conn      *sql.DB   // connection pool for reads
	writeConn *sql.Conn // dedicated connection for all writes
	writeCh   chan writeReq
	stmts     *ttlcache.Cache[string, *sql.Stmt]

	// Metrics for string pool cache performance
	cleanupDeleted atomic.Uint64 // Total strings deleted by cleanup

	// Cleanup coordination
	cleanupRunning atomic.Bool // Prevents concurrent cleanup operations

	stop          chan struct{}
	closeOnce     sync.Once
	writerWG      sync.WaitGroup
	closing       atomic.Bool
	closeErr      error
	writeBarrier  atomic.Value // stores chan struct{} to pause writer for tests
	barrierSignal atomic.Value // stores chan struct{} to signal writer pause
}

// Tx wraps sql.Tx to provide prepared statement caching for transaction queries
type Tx struct {
	tx *sql.Tx
	db *DB
}

// PrepareContext creates a new prepared statement within the transaction.
// Note: Unlike other methods, this doesn't use the cache since transaction-specific
// statements must be created from the transaction itself.
func (t *Tx) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return t.tx.PrepareContext(ctx, query)
}

// ExecContext executes a query within the transaction using cached prepared statements
func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	// Get or prepare statement from DB cache, then create transaction-specific statement
	stmt, err := t.db.getStmt(ctx, query)
	if err != nil {
		// Fallback to direct execution
		return t.tx.ExecContext(ctx, query, args...)
	}

	// Create a transaction-specific statement from the cached one using StmtContext for better cancellation
	txStmt := t.tx.StmtContext(ctx, stmt)
	defer txStmt.Close()
	return txStmt.ExecContext(ctx, args...)
}

// QueryContext executes a query within the transaction using cached prepared statements
func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	// Get or prepare statement from DB cache, then create transaction-specific statement
	stmt, err := t.db.getStmt(ctx, query)
	if err != nil {
		// Fallback to direct execution
		return t.tx.QueryContext(ctx, query, args...)
	}

	// Create a transaction-specific statement from the cached one using StmtContext for better cancellation
	txStmt := t.tx.StmtContext(ctx, stmt)
	defer txStmt.Close()
	return txStmt.QueryContext(ctx, args...)
}

// QueryRowContext executes a query within the transaction using cached prepared statements
func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	// Get or prepare statement from DB cache, then create transaction-specific statement
	stmt, err := t.db.getStmt(ctx, query)
	if err != nil {
		// Fallback to direct execution
		return t.tx.QueryRowContext(ctx, query, args...)
	}

	// Create a transaction-specific statement from the cached one using StmtContext for better cancellation
	txStmt := t.tx.StmtContext(ctx, stmt)
	defer txStmt.Close()
	return txStmt.QueryRowContext(ctx, args...)
}

// Commit commits the transaction
func (t *Tx) Commit() error {
	return t.tx.Commit()
}

// Rollback rolls back the transaction
func (t *Tx) Rollback() error {
	return t.tx.Rollback()
}

const (
	defaultBusyTimeout       = 5 * time.Second
	defaultBusyTimeoutMillis = int(defaultBusyTimeout / time.Millisecond)
	connectionSetupTimeout   = 5 * time.Second
	writeChannelBuffer       = 256 // buffer for write operations to improve throughput
)

var driverInit sync.Once

type pragmaExecFn func(ctx context.Context, stmt string) error

func registerConnectionHook() {
	driverInit.Do(func() {
		sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, dsn string) error {
			ctx, cancel := context.WithTimeout(context.Background(), connectionSetupTimeout)
			defer cancel()

			return applyConnectionPragmas(ctx, func(ctx context.Context, stmt string) error {
				_, err := conn.ExecContext(ctx, stmt, nil)
				if err != nil {
					return fmt.Errorf("connection hook exec %q: %w", stmt, err)
				}
				return nil
			})
		})
	})
}

func applyConnectionPragmas(ctx context.Context, exec pragmaExecFn) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		fmt.Sprintf("PRAGMA busy_timeout = %d", defaultBusyTimeoutMillis),
		"PRAGMA analysis_limit = 400",
	}

	for _, pragma := range pragmas {
		if err := exec(ctx, pragma); err != nil {
			return fmt.Errorf("apply connection pragma %q: %w", pragma, err)
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

	// Open connection for migrations with single connection only
	// This prevents any connection pool issues during schema changes
	conn, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", databasePath, err)
	}

	// CRITICAL: Use only 1 connection during migrations to prevent stale schema issues
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	log.Debug().Msg("Database connection opened for migrations")

	ctx, cancel := context.WithTimeout(context.Background(), connectionSetupTimeout)
	defer cancel()
	if err := applyConnectionPragmas(ctx, func(ctx context.Context, stmt string) error {
		_, execErr := conn.ExecContext(ctx, stmt)
		return execErr
	}); err != nil {
		conn.Close()
		return nil, err
	}

	if _, err := conn.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply wal checkpoint: %w", err)
	}

	// create ttlcache for prepared statements with 5 minute TTL and deallocation func
	stmtOpts := ttlcache.Options[string, *sql.Stmt]{}.SetDefaultTTL(5 * time.Minute).
		SetDeallocationFunc(func(k string, s *sql.Stmt, _ ttlcache.DeallocationReason) {
			if s != nil {
				_ = s.Close()
			}
		})

	stmtsCache := ttlcache.New(stmtOpts)

	db := &DB{
		conn:    conn,
		writeCh: make(chan writeReq, writeChannelBuffer),
		stmts:   stmtsCache,
		stop:    make(chan struct{}),
	}
	db.writeBarrier.Store((chan struct{})(nil))
	db.barrierSignal.Store((chan struct{})(nil))

	// Run migrations with single connection
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Restore default connection pool configuration after migration lock-down
	conn.SetMaxOpenConns(0)
	conn.SetMaxIdleConns(2)
	conn.SetConnMaxLifetime(0)

	// Acquire dedicated write connection
	ctx2, cancel2 := context.WithTimeout(context.Background(), connectionSetupTimeout)
	defer cancel2()
	writeConn, err := conn.Conn(ctx2)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to acquire write connection: %w", err)
	}
	db.writeConn = writeConn
	log.Debug().Msg("Dedicated write connection acquired")

	// start single writer after migrations
	db.writerWG.Add(1)
	go db.writerLoop()

	// start periodic string pool cleanup
	db.writerWG.Add(1)
	go db.stringPoolCleanupLoop()

	// Verify database file was created
	if _, err := os.Stat(databasePath); err != nil {
		conn.Close()
		return nil, fmt.Errorf("database file was not created at %s: %w", databasePath, err)
	}
	log.Info().Msgf("Database initialized successfully at: %s", databasePath)

	return db, nil
}

// getStmt returns a prepared statement for the given query, preparing and
// caching it if necessary. Statements are cached with TTL and automatically
// closed on eviction. This is safe for concurrent use.
func (db *DB) getStmt(ctx context.Context, query string) (*sql.Stmt, error) {
	// Fast path: check cache first
	if s, found := db.stmts.Get(query); found && s != nil {
		return s, nil
	}

	// Slow path: prepare new statement
	// Note: Multiple goroutines might prepare the same query simultaneously,
	// but this is acceptable since:
	// 1. It's rare (only on cache miss/eviction)
	// 2. The extra statements will be garbage collected
	// 3. TTL cache will eventually converge to one statement per query
	s, err := db.conn.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}

	// Cache the statement - if another goroutine already cached it,
	// that's fine, this one will be closed by the deallocation function
	db.stmts.Set(query, s, ttlcache.DefaultTTL)

	return s, nil
}

// execWrite executes a write query using ExecContext. If a prepared stmt
// is provided it will be used, otherwise the write connection is used directly.
func (db *DB) execWrite(ctx context.Context, stmt *sql.Stmt, query string, args []any) (sql.Result, error) {
	if stmt != nil {
		return stmt.ExecContext(ctx, args...)
	}
	return db.writeConn.ExecContext(ctx, query, args...)
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
		strings.HasPrefix(upper, "DELETE")
}

// ExecContext routes write queries through the single writer goroutine and
// uses prepared statements when possible. Do NOT use this for queries with
// RETURNING clauses - use QueryRowContext or QueryContext instead.
func (db *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	// Fast write detection without string allocation
	if !isWriteQuery(query) {
		// treat as reader and use prepared stmt when possible
		stmt, err := db.getStmt(ctx, query)
		if err != nil {
			// fallback to direct Exec
			return db.conn.ExecContext(ctx, query, args...)
		}
		return stmt.ExecContext(ctx, args...)
	}

	if db.closing.Load() {
		return nil, fmt.Errorf("db stopping")
	}

	// route through writer
	resCh := make(chan writeRes, 1)
	req := writeReq{ctx: ctx, query: query, args: args, resCh: resCh}
	select {
	case db.writeCh <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-db.stop:
		return nil, fmt.Errorf("db stopping")
	}

	res := <-resCh
	return res.result, res.err
}

// writerLoop processes write requests sequentially
func (db *DB) writerLoop() {
	defer db.writerWG.Done()

	draining := false
	for {
		if draining {
			select {
			case req, ok := <-db.writeCh:
				if !ok {
					return
				}
				db.processWrite(req)
			default:
				return
			}
			continue
		}

		select {
		case req, ok := <-db.writeCh:
			if !ok {
				return
			}
			db.processWrite(req)
		case <-db.stop:
			draining = true
		}
	}
}

func (db *DB) processWrite(req writeReq) {
	// Check for test write barrier with timeout to prevent production hangs
	if barrier, ok := db.writeBarrier.Load().(chan struct{}); ok && barrier != nil {
		if signal, ok := db.barrierSignal.Load().(chan struct{}); ok && signal != nil {
			select {
			case signal <- struct{}{}:
			default:
			}
		}
		// Add timeout to prevent indefinite blocking if test cleanup fails
		timeout := time.NewTimer(30 * time.Second)
		defer timeout.Stop()

		select {
		case <-barrier:
		case <-req.ctx.Done():
			log.Warn().Msg("Write barrier: request context cancelled")
		case <-timeout.C:
			// Write barrier should only be set in tests. If timeout triggers in production,
			// it indicates a bug where the barrier was not properly cleared.
			log.Fatal().Msg("FATAL: Write barrier timeout exceeded in production - write barrier should only be used in tests")
		}
	}

	// use prepared stmt if possible
	stmt, err := db.getStmt(req.ctx, req.query)
	if err != nil {
		// if we couldn't prepare a statement, execWrite will use
		// the connection directly
		res, execErr := db.execWrite(req.ctx, nil, req.query, req.args)
		select {
		case req.resCh <- writeRes{result: res, err: execErr}:
		default:
		}
		return
	}

	res, execErr := db.execWrite(req.ctx, stmt, req.query, req.args)
	select {
	case req.resCh <- writeRes{result: res, err: execErr}:
	default:
	}
}

// stringPoolCleanupLoop runs periodic cleanup of unused strings from string_pool
func (db *DB) stringPoolCleanupLoop() {
	defer db.writerWG.Done()

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
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if deleted, err := db.CleanupUnusedStrings(ctx); err != nil {
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
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if deleted, err := db.CleanupUnusedStrings(ctx); err != nil {
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

		case <-db.stop:
			return
		}
	}
}

// QueryContext uses reader pool and prepared statements
func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	// try to use prepared statement, fall back to db pool
	stmt, err := db.getStmt(ctx, query)
	if err != nil {
		return db.conn.QueryContext(ctx, query, args...)
	}
	return stmt.QueryContext(ctx, args...)
}

// QueryRowContext uses QueryContext and scans first row
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	// prepare statement and use QueryRow on it (no reader release necessary because Row scans and doesn't return Rows)
	stmt, err := db.getStmt(ctx, query)
	if err != nil {
		return db.conn.QueryRowContext(ctx, query, args...)
	}
	return stmt.QueryRowContext(ctx, args...)
}

// BeginTx starts a transaction. Returns a wrapped transaction that uses prepared
// statement caching for better performance.
//
// CONCURRENCY MODEL:
// - Write transactions (opts == nil or opts.ReadOnly == false) use the dedicated write connection
// - Read-only transactions (opts.ReadOnly == true) use the connection pool for concurrency
// - All write operations are serialized through the single write connection
// - WAL mode allows concurrent readers during write transactions
//
// ISOLATION LEVEL:
// - SQLite defaults to SERIALIZABLE isolation (strongest guarantee)
// - Pass nil for opts to use SERIALIZABLE (recommended for most cases)
// - For read-only transactions, use: &sql.TxOptions{ReadOnly: true}
// - Read-only transactions allow full concurrency with writers in WAL mode
// - Write transactions are serialized through the single write connection
//
// WHEN TO USE EACH:
// - Use ExecContext for simple, single-statement writes (INSERT, UPDATE, DELETE)
// - Use BeginTx for multi-statement operations that need atomicity
// - Use BeginTx when you need to read and write in a consistent snapshot
//
// GUARANTEES:
// - ExecContext: Sequential execution through single writer, no partial writes visible
// - BeginTx (write): ACID properties, full transaction isolation, serialized writes
// - BeginTx (read-only): ACID properties, concurrent with writes
// - All writes: Serialized through single write connection
//
// PREPARED STATEMENTS:
// - Transaction queries automatically use the DB's prepared statement cache
// - Statements are adapted to the transaction context using Tx.Stmt()
// - This improves performance for transactions that execute the same queries multiple times
//
// LIMITATIONS:
// - Write transactions are serialized through the single write connection
// - Long-running write transactions will block other writes
// - Use read-only transactions when possible to avoid blocking writes
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	// Determine if this is a read-only transaction
	isReadOnly := opts != nil && opts.ReadOnly

	var tx *sql.Tx
	var err error

	if isReadOnly {
		// Read-only transactions can use the connection pool for concurrency
		tx, err = db.conn.BeginTx(ctx, opts)
	} else {
		// Write transactions use the dedicated write connection for serialization
		tx, err = db.writeConn.BeginTx(ctx, opts)
	}

	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, db: db}, nil
}

func (db *DB) Close() error {
	db.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), connectionSetupTimeout)
		defer cancel()
		if _, err := db.conn.ExecContext(ctx, "PRAGMA optimize"); err != nil {
			log.Warn().Err(err).Msg("failed to run PRAGMA optimize during close")
		}

		db.closing.Store(true)

		select {
		case <-db.stop:
		default:
			close(db.stop)
		}

		if barrier, ok := db.writeBarrier.Load().(chan struct{}); ok && barrier != nil {
			select {
			case <-barrier:
			default:
				close(barrier)
			}
			db.writeBarrier.Store((chan struct{})(nil))
		}
		db.barrierSignal.Store((chan struct{})(nil))

		db.writerWG.Wait()

		db.stmts.Close()

		// Close the dedicated write connection first
		if db.writeConn != nil {
			if err := db.writeConn.Close(); err != nil {
				log.Warn().Err(err).Msg("failed to close write connection")
			}
		}

		// deallocation of cached statements is handled by ttlcache
		db.closeErr = db.conn.Close()
	})

	return db.closeErr
}

func (db *DB) Conn() *sql.DB {
	return db.conn
}

func (db *DB) migrate() error {
	ctx := context.Background()

	// Create migrations table if it doesn't exist
	if _, err := db.conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			filename TEXT NOT NULL UNIQUE,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
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

func (db *DB) findPendingMigrations(ctx context.Context, allFiles []string) ([]string, error) {
	var pendingMigrations []string

	for _, filename := range allFiles {
		var count int
		err := db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE filename = ?", filename).Scan(&count)
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

	// Begin single transaction for all migrations
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	// defer Rollback - will be no-op if Commit succeeds
	defer tx.Rollback()

	// Apply each migration within the transaction
	for _, filename := range migrations {
		// Check if this migration needs foreign keys disabled
		if needsForeignKeysOff[filename] {
			// Commit current transaction before disabling foreign keys
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("failed to commit before %s: %w", filename, err)
			}

			// Ensure foreign keys are re-enabled even if migration fails
			defer func() {
				if _, err := db.conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
					log.Error().Err(err).Msg("CRITICAL: Failed to re-enable foreign keys after migration - manual intervention required")
				}
			}()

			// Disable foreign keys (must be done outside transaction)
			if _, err := db.conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
				return fmt.Errorf("failed to disable foreign keys for %s: %w", filename, err)
			}

			// Read and execute migration outside transaction
			content, err := migrationsFS.ReadFile("migrations/" + filename)
			if err != nil {
				return fmt.Errorf("failed to read migration file %s: %w", filename, err)
			}

			if _, err := db.conn.ExecContext(ctx, string(content)); err != nil {
				return fmt.Errorf("failed to execute migration %s: %w", filename, err)
			}

			// Record migration
			if _, err := db.conn.ExecContext(ctx, "INSERT INTO migrations (filename) VALUES (?)", filename); err != nil {
				return fmt.Errorf("failed to record migration %s: %w", filename, err)
			}

			// Re-enable foreign keys explicitly
			if _, err := db.conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
				return fmt.Errorf("failed to re-enable foreign keys after %s: %w", filename, err)
			}

			// Start new transaction for remaining migrations
			tx, err = db.conn.Begin()
			if err != nil {
				return fmt.Errorf("failed to begin new transaction after %s: %w", filename, err)
			}
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

	// Commit all migrations at once
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migrations: %w", err)
	}

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
func (db *DB) CleanupUnusedStrings(ctx context.Context) (int64, error) {
	// Prevent concurrent cleanup operations
	if !db.cleanupRunning.CompareAndSwap(false, true) {
		log.Debug().Msg("String pool cleanup already in progress, skipping")
		return 0, nil
	}
	defer db.cleanupRunning.Store(false)

	// Use a transaction to make the cleanup atomic
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Build a temporary table of referenced IDs for better performance
	// This is much faster than the massive UNION query
	_, err = tx.ExecContext(ctx, `
		CREATE TEMP TABLE IF NOT EXISTS temp_referenced_strings (
			string_id INTEGER PRIMARY KEY
		)
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to create temp table: %w", err)
	}
	defer tx.ExecContext(ctx, "DROP TABLE IF EXISTS temp_referenced_strings")

	// Populate temp table with all referenced string IDs
	// Using INSERT OR IGNORE to handle duplicates efficiently
	insertQueries := []string{
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT torrent_hash_id FROM torrent_files_cache WHERE torrent_hash_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT name_id FROM torrent_files_cache WHERE name_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT torrent_hash_id FROM torrent_files_sync WHERE torrent_hash_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT torrent_hash_id FROM instance_backup_items WHERE torrent_hash_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT name_id FROM instance_backup_items WHERE name_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT category_id FROM instance_backup_items WHERE category_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT tags_id FROM instance_backup_items WHERE tags_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT archive_rel_path_id FROM instance_backup_items WHERE archive_rel_path_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT infohash_v1_id FROM instance_backup_items WHERE infohash_v1_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT infohash_v2_id FROM instance_backup_items WHERE infohash_v2_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT torrent_blob_path_id FROM instance_backup_items WHERE torrent_blob_path_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT kind_id FROM instance_backup_runs WHERE kind_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT status_id FROM instance_backup_runs WHERE status_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT requested_by_id FROM instance_backup_runs WHERE requested_by_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT error_message_id FROM instance_backup_runs WHERE error_message_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT archive_path_id FROM instance_backup_runs WHERE archive_path_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT manifest_path_id FROM instance_backup_runs WHERE manifest_path_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT name_id FROM instances WHERE name_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT host_id FROM instances WHERE host_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT username_id FROM instances WHERE username_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT basic_username_id FROM instances WHERE basic_username_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT name_id FROM api_keys WHERE name_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT client_name_id FROM client_api_keys WHERE client_name_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT error_type_id FROM instance_errors WHERE error_type_id IS NOT NULL",
		"INSERT OR IGNORE INTO temp_referenced_strings SELECT DISTINCT error_message_id FROM instance_errors WHERE error_message_id IS NOT NULL",
	}

	for _, query := range insertQueries {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return 0, fmt.Errorf("failed to populate temp table: %w", err)
		}
	}

	// Now delete strings not in the temp table - much faster than UNION
	result, err := tx.ExecContext(ctx, `
		DELETE FROM string_pool 
		WHERE id NOT IN (SELECT string_id FROM temp_referenced_strings)
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup unused strings: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit cleanup transaction: %w", err)
	}

	// Update cleanup metrics
	if rowsAffected > 0 {
		db.cleanupDeleted.Add(uint64(rowsAffected))
		log.Debug().Msgf("Cleaned up %d unused strings from string_pool (cache cleared, total deleted: %d)",
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
//
// This differs from production where:
// - stringPoolCleanupLoop runs automatically every 24 hours
// - Unused strings are automatically removed
// - String pool size is bounded
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

	// Acquire dedicated write connection
	ctx, cancel := context.WithTimeout(context.Background(), connectionSetupTimeout)
	defer cancel()
	writeConn, err := conn.Conn(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to acquire write connection in NewForTest")
	}

	db := &DB{
		conn:      conn,
		writeConn: writeConn,
		writeCh:   make(chan writeReq, writeChannelBuffer),
		stmts:     stmtsCache,
		stop:      make(chan struct{}),
	}
	db.writeBarrier.Store((chan struct{})(nil))
	db.barrierSignal.Store((chan struct{})(nil))

	// Start single writer goroutine
	db.writerWG.Add(1)
	go db.writerLoop()

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
