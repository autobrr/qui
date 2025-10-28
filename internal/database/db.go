// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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
	conn          *sql.DB
	writeCh       chan writeReq
	stmts         *ttlcache.Cache[string, *sql.Stmt]
	stop          chan struct{}
	closeOnce     sync.Once
	writerWG      sync.WaitGroup
	closing       atomic.Bool
	closeErr      error
	writeBarrier  atomic.Value // stores chan struct{} to pause writer for tests
	barrierSignal atomic.Value // stores chan struct{} to signal writer pause
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
	opts := ttlcache.Options[string, *sql.Stmt]{}.SetDefaultTTL(5 * time.Minute).
		SetDeallocationFunc(func(k string, s *sql.Stmt, _ ttlcache.DeallocationReason) {
			if s != nil {
				_ = s.Close()
			}
		})

	stmtsCache := ttlcache.New(opts)

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

	// start single writer after migrations
	db.writerWG.Add(1)
	go db.writerLoop()

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
// is provided it will be used, otherwise the connection is used directly.
func (db *DB) execWrite(ctx context.Context, stmt *sql.Stmt, query string, args []any) (sql.Result, error) {
	if stmt != nil {
		return stmt.ExecContext(ctx, args...)
	}
	return db.conn.ExecContext(ctx, query, args...)
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

const stmtClosedErrMsg = "statement is closed"

func isStmtClosedErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), stmtClosedErrMsg)
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
		res, execErr := stmt.ExecContext(ctx, args...)
		if !isStmtClosedErr(execErr) {
			return res, execErr
		}

		// statement was evicted and closed between prepare and exec; drop from cache and retry
		db.stmts.Delete(query)

		stmt, err = db.getStmt(ctx, query)
		if err != nil {
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
	if barrier, ok := db.writeBarrier.Load().(chan struct{}); ok && barrier != nil {
		if signal, ok := db.barrierSignal.Load().(chan struct{}); ok && signal != nil {
			select {
			case signal <- struct{}{}:
			default:
			}
		}
		select {
		case <-barrier:
		case <-req.ctx.Done():
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
	if isStmtClosedErr(execErr) {
		db.stmts.Delete(req.query)
		stmt, err = db.getStmt(req.ctx, req.query)
		if err != nil {
			res, execErr = db.execWrite(req.ctx, nil, req.query, req.args)
		} else {
			res, execErr = db.execWrite(req.ctx, stmt, req.query, req.args)
		}
	}

	select {
	case req.resCh <- writeRes{result: res, err: execErr}:
	default:
	}
}

// QueryContext uses reader pool and prepared statements
func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	// try to use prepared statement, fall back to db pool
	stmt, err := db.getStmt(ctx, query)
	if err != nil {
		return db.conn.QueryContext(ctx, query, args...)
	}
	rows, queryErr := stmt.QueryContext(ctx, args...)
	if !isStmtClosedErr(queryErr) {
		return rows, queryErr
	}

	// Statement closed underneath us; drop cache entry and retry
	db.stmts.Delete(query)

	stmt, err = db.getStmt(ctx, query)
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
	row := stmt.QueryRowContext(ctx, args...)
	if !isStmtClosedErr(row.Err()) {
		return row
	}

	db.stmts.Delete(query)

	stmt, err = db.getStmt(ctx, query)
	if err != nil {
		return db.conn.QueryRowContext(ctx, query, args...)
	}
	return stmt.QueryRowContext(ctx, args...)
}

// BeginTx starts a transaction. Note that transactions bypass the single writer
// and use the underlying connection pool directly. This is safe because SQLite
// handles transaction serialization internally.
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return db.conn.BeginTx(ctx, opts)
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

func (db *DB) applyAllMigrations(ctx context.Context, migrations []string) error {
	// Begin single transaction for all migrations
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	// defer Rollback - will be no-op if Commit succeeds
	defer tx.Rollback()

	// Apply each migration within the transaction
	for _, filename := range migrations {
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

	// Commit all migrations at once
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migrations: %w", err)
	}

	log.Info().Msgf("Applied %d migrations successfully", len(migrations))
	return nil
}
