// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/autobrr/go-cache/ttlcache"
	"github.com/rs/zerolog/log"
	"modernc.org/sqlite"

	"github.com/autobrr/qui/internal/dbinterface"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func SQLiteMigrations() fs.FS {
	return migrationsFS
}

type DB struct {
	writerConn  *sql.DB
	readerPool  *sql.DB
	writerStmts *ttlcache.Cache[string, *sql.Stmt]
	readerStmts *ttlcache.Cache[string, *sql.Stmt]
	stmtMu      sync.RWMutex
	dialect     Dialect
	sqlitePath  string

	writerSem chan struct{}

	cleanupDeleted atomic.Uint64

	cleanupRunning atomic.Bool

	cleanupCancel context.CancelFunc

	closing atomic.Bool

	closeOnce sync.Once
	closeErr  error
}

type migrationFilenameRename struct {
	from string
	to   string
}

var sharedMigrationFilenameRenames = []migrationFilenameRename{
	{
		from: "061_add_notifications.sql",
		to:   "062_add_notifications.sql",
	},
	{
		from: "052_add_dir_scan.sql",
		to:   "053_add_dir_scan.sql",
	},
	{
		from: "055_add_license_provider_dodo.sql",
		to:   "057_add_license_provider_dodo.sql",
	},
}

var sqliteMigrationFilenameRenames = []migrationFilenameRename{
	{
		from: "064_add_completion_bypass_torznab_cache.sql",
		to:   "066_add_completion_bypass_torznab_cache.sql",
	},
	{
		from: "065_add_completion_bypass_torznab_cache.sql",
		to:   "066_add_completion_bypass_torznab_cache.sql",
	},
	{
		from: "073_add_instance_api_key_auth.sql",
		to:   "074_add_instance_api_key_auth.sql",
	},
	{
		from: "068_add_season_pack_settings_and_runs.sql",
		to:   "075_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "069_add_season_pack_settings_and_runs.sql",
		to:   "075_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "069_add_season_pack_tags.sql",
		to:   "075_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "070_add_season_pack_settings_and_runs.sql",
		to:   "075_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "070_add_season_pack_tags.sql",
		to:   "075_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "070_add_season_pack_metadata_settings.sql",
		to:   "075_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "071_add_season_pack_settings_and_runs.sql",
		to:   "075_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "071_add_season_pack_metadata_settings.sql",
		to:   "075_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "072_add_season_pack_settings_and_runs.sql",
		to:   "075_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "073_add_season_pack_settings_and_runs.sql",
		to:   "075_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "074_add_season_pack_settings_and_runs.sql",
		to:   "075_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "075_add_season_pack_category.sql",
		to:   "076_add_season_pack_category.sql",
	},
	{
		from: "091_drop_unused_timestamp_indexes.sql",
		to:   "093_drop_unused_timestamp_indexes.sql",
	},
}

var postgresMigrationFilenameRenames = []migrationFilenameRename{
	{
		from: "066_add_completion_bypass_torznab_cache.sql",
		to:   "067_add_completion_bypass_torznab_cache.sql",
	},
	{
		from: "074_add_instance_api_key_auth.sql",
		to:   "075_add_instance_api_key_auth.sql",
	},
	{
		from: "069_add_season_pack_settings_and_runs.sql",
		to:   "076_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "070_add_season_pack_settings_and_runs.sql",
		to:   "076_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "070_add_season_pack_tags.sql",
		to:   "076_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "071_add_season_pack_settings_and_runs.sql",
		to:   "076_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "071_add_season_pack_tags.sql",
		to:   "076_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "071_add_season_pack_metadata_settings.sql",
		to:   "076_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "072_add_season_pack_settings_and_runs.sql",
		to:   "076_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "072_add_season_pack_metadata_settings.sql",
		to:   "076_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "073_add_season_pack_settings_and_runs.sql",
		to:   "076_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "074_add_season_pack_settings_and_runs.sql",
		to:   "076_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "075_add_season_pack_settings_and_runs.sql",
		to:   "076_add_season_pack_settings_and_runs.sql",
	},
	{
		from: "076_add_season_pack_category.sql",
		to:   "077_add_season_pack_category.sql",
	},
	{
		from: "093_drop_unused_timestamp_indexes.sql",
		to:   "094_drop_unused_timestamp_indexes.sql",
	},
}

type Tx struct {
	tx         *sql.Tx
	db         *DB
	ctx        context.Context
	isWriteTx  bool
	unlockFn   func()
	unlockOnce sync.Once

	txStmts    map[string]struct{}
	tempTables map[string]struct{}
	txMu       sync.Mutex
}

func (t *Tx) markQueryForCaching(query string) {
	t.txMu.Lock()
	defer t.txMu.Unlock()

	if name, ok := tempTableNameFromCreate(query); ok {
		if t.tempTables == nil {
			t.tempTables = make(map[string]struct{})
		}
		t.tempTables[name] = struct{}{}
		return
	}

	if name, ok := tableNameFromDrop(query); ok {
		delete(t.tempTables, name)
		return
	}

	if queryReferencesTempTable(query, t.tempTables) {
		return
	}

	if t.txStmts == nil {
		t.txStmts = make(map[string]struct{})
	}
	t.txStmts[query] = struct{}{}
}

func (t *Tx) shouldBypassStatementCache(query string) bool {
	t.txMu.Lock()
	defer t.txMu.Unlock()

	if _, ok := tempTableNameFromCreate(query); ok {
		return true
	}

	if name, ok := tableNameFromDrop(query); ok {
		if _, exists := t.tempTables[name]; exists {
			return true
		}
	}

	return queryReferencesTempTable(query, t.tempTables)
}

type txExecResult struct{ tx *Tx }

func (e txExecResult) execStmt(ctx context.Context, stmt *sql.Stmt, args []any) (sql.Result, error) {
	return stmt.ExecContext(ctx, args...)
}

func (e txExecResult) execDirect(ctx context.Context, _ *sql.DB, query string, args []any) (sql.Result, error) {
	result, err := e.tx.tx.ExecContext(ctx, e.tx.db.bindQuery(query), args...)
	if err == nil {
		e.tx.markQueryForCaching(query)
	}
	return result, err
}

func (txExecResult) getErr(sql.Result) error { return nil }
func (e txExecResult) getTx() *Tx            { return e.tx }

type txQueryRows struct{ tx *Tx }

func (q txQueryRows) execStmt(ctx context.Context, stmt *sql.Stmt, args []any) (*sql.Rows, error) {
	return stmt.QueryContext(ctx, args...)
}

func (q txQueryRows) execDirect(ctx context.Context, _ *sql.DB, query string, args []any) (*sql.Rows, error) {
	rows, err := q.tx.tx.QueryContext(ctx, q.tx.db.bindQuery(query), args...)
	if err == nil {
		q.tx.markQueryForCaching(query)
	}
	return rows, err
}

func (txQueryRows) getErr(r *sql.Rows) error {
	if r == nil {
		return nil
	}
	return r.Err()
}
func (q txQueryRows) getTx() *Tx { return q.tx }

func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return execWithRetry(ctx, t.db, query, args, txExecResult{tx: t})
}

func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return execWithRetry(ctx, t.db, query, args, txQueryRows{tx: t})
}

func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	stmt, err := t.db.getStmt(ctx, query, t)
	if err != nil {
		row := t.tx.QueryRowContext(ctx, t.db.bindQuery(query), args...)
		if row.Err() == nil {
			t.markQueryForCaching(query)
		}
		return row
	}

	row := stmt.QueryRowContext(ctx, args...)
	if row.Err() == nil || !strings.Contains(row.Err().Error(), stmtClosedErrMsg) {
		return row
	}

	t.db.deleteStmt(query, t.isWriteTx)

	stmt, err = t.db.getStmt(ctx, query, t)
	if err != nil {
		row := t.tx.QueryRowContext(ctx, t.db.bindQuery(query), args...)
		if row.Err() == nil {
			t.markQueryForCaching(query)
		}
		return row
	}
	return stmt.QueryRowContext(ctx, args...)
}

func (t *Tx) Commit() error {
	err := t.tx.Commit()
	if err == nil {
		t.promoteStatementsToCache()
		if t.unlockFn != nil {
			t.unlockOnce.Do(t.unlockFn)
		}
	}
	return err
}

func (t *Tx) Rollback() error {
	err := t.tx.Rollback()
	if t.unlockFn != nil {
		t.unlockOnce.Do(t.unlockFn)
	}
	return err
}

func (t *Tx) promoteStatementsToCache() {
	t.txMu.Lock()
	queries := t.txStmts
	t.txStmts = nil
	t.txMu.Unlock()

	if len(queries) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for query := range queries {
		boundQuery := t.db.bindQuery(query)
		if t.db.closing.Load() {
			return
		}

		t.db.stmtMu.RLock()

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

		if _, found := stmts.Get(boundQuery); found {
			t.db.stmtMu.RUnlock()
			continue
		}

		stmt, err := conn.PrepareContext(ctx, boundQuery)
		if err != nil {
			t.db.stmtMu.RUnlock()
			continue
		}

		if _, loaded := stmts.GetOrSet(boundQuery, stmt, ttlcache.DefaultTTL); loaded {
			_ = stmt.Close()
		}
		t.db.stmtMu.RUnlock()
	}
}

const (
	defaultBusyTimeout       = 5 * time.Second
	defaultBusyTimeoutMillis = int(defaultBusyTimeout / time.Millisecond)
	connectionSetupTimeout   = 5 * time.Second

	startupCheckpointWarnAfter = 3 * time.Second
)

const sqliteTimeParams = "_timezone=UTC&_time_format=sqlite"

type pragmaExecFn func(ctx context.Context, stmt string) error

func init() {
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
}

func isReadOnlyDSN(dsn string) bool {
	_, after, ok := strings.Cut(dsn, "?")
	if !ok {
		return false
	}
	query := after
	return slices.Contains(strings.FieldsFunc(query, func(r rune) bool {
		return r == '&' || r == ';'
	}), "mode=ro")
}

type pragmaDirective struct {
	stmt          string
	allowReadOnly bool
}

var connectionPragmas = []pragmaDirective{
	{stmt: "PRAGMA journal_mode = WAL", allowReadOnly: false},
	{stmt: "PRAGMA synchronous = NORMAL", allowReadOnly: false},
	{stmt: "PRAGMA mmap_size = 268435456", allowReadOnly: true},
	{stmt: "PRAGMA page_size = 4096", allowReadOnly: false},
	{stmt: "PRAGMA cache_size = -2000", allowReadOnly: true},
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

func secureDatabaseFiles(databasePath string) error {
	f, err := os.OpenFile(databasePath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create database file %s: %w", databasePath, err)
	}
	_ = f.Close()

	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := databasePath + suffix
		if _, statErr := os.Stat(p); statErr != nil {
			continue
		}
		if err := os.Chmod(p, 0o600); err != nil {
			return fmt.Errorf("failed to secure database file %s: %w", p, err)
		}
	}
	return nil
}

func sqliteReaderPoolSize() int {
	return min(max(runtime.GOMAXPROCS(0), 8), 16)
}

func newStmtCache() *ttlcache.Cache[string, *sql.Stmt] {
	return ttlcache.New[string, *sql.Stmt](
		ttlcache.SetDefaultTTL(5*time.Minute),
		ttlcache.SetDeallocationFunc(func(_ string, s *sql.Stmt, _ ttlcache.DeallocationReason) {
			_ = s.Close()
		}),
	)
}

func openSQLiteReader(databasePath string) (*sql.DB, error) {
	readerPool, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro&%s", databasePath, sqliteTimeParams))
	if err != nil {
		return nil, fmt.Errorf("open sqlite reader at %s: %w", databasePath, err)
	}
	return readerPool, nil
}

func New(databasePath string) (*DB, error) {
	log.Info().Msgf("Initializing database at: %s", databasePath)

	dir := filepath.Dir(databasePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create database directory %s: %w", dir, err)
	}
	log.Debug().Msgf("Database directory ensured: %s", dir)

	if err := secureDatabaseFiles(databasePath); err != nil {
		return nil, err
	}

	writerConn, err := sql.Open("sqlite", databasePath+"?"+sqliteTimeParams)
	if err != nil {
		return nil, fmt.Errorf("failed to open writer connection at %s: %w", databasePath, err)
	}

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

	slowCheckpoint := time.AfterFunc(startupCheckpointWarnAfter, func() {
		log.Warn().Msgf("The WAL checkpoint of %s is still running after %s. The disk may be slow or busy, or another program may have the database open", databasePath, startupCheckpointWarnAfter)
	})
	_, err = writerConn.ExecContext(context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)")
	slowCheckpoint.Stop()
	if err != nil {
		writerConn.Close()
		return nil, fmt.Errorf("apply wal checkpoint: %w", err)
	}

	readerPool, err := openSQLiteReader(databasePath)
	if err != nil {
		writerConn.Close()
		return nil, err
	}

	readerPoolSize := sqliteReaderPoolSize()
	readerPool.SetMaxOpenConns(readerPoolSize)
	readerPool.SetMaxIdleConns(readerPoolSize)
	readerPool.SetConnMaxLifetime(0)
	log.Debug().Msg("Reader pool opened in read-only mode")

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

	db := &DB{
		writerConn:  writerConn,
		readerPool:  readerPool,
		writerStmts: newStmtCache(),
		readerStmts: newStmtCache(),
		dialect:     DialectSQLite,
		sqlitePath:  databasePath,
		writerSem:   make(chan struct{}, 1),
	}

	if err := db.migrate(); err != nil {
		writerConn.Close()
		readerPool.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	db.cleanupCancel = cleanupCancel
	go db.stringPoolCleanupLoop(cleanupCtx)

	if _, err := os.Stat(databasePath); err != nil {
		writerConn.Close()
		readerPool.Close()
		return nil, fmt.Errorf("database file was not created at %s: %w", databasePath, err)
	}
	log.Info().Msgf("Database initialized successfully at: %s", databasePath)

	return db, nil
}

func (db *DB) getStmt(ctx context.Context, query string, tx *Tx) (*sql.Stmt, error) {
	if db.closing.Load() {
		return nil, sql.ErrConnDone
	}
	query = db.bindQuery(query)

	if tx != nil {
		if tx.shouldBypassStatementCache(query) {
			if tx.isWriteTx {
				db.deleteStmt(query, true)
			} else {
				db.deleteStmt(query, false)
			}
			return nil, errors.New("statement not cacheable")
		}
	} else if isTempTableDDL(query) {
		db.deleteStmt(query, true)
		return nil, errors.New("statement not cacheable")
	}

	db.stmtMu.RLock()
	defer db.stmtMu.RUnlock()

	var stmts *ttlcache.Cache[string, *sql.Stmt]
	var conn *sql.DB

	if tx != nil {
		if tx.isWriteTx {
			stmts = db.writerStmts
			conn = db.writerConn
		} else {
			stmts = db.readerStmts
			conn = db.readerPool
		}
	} else {
		if isWriteQuery(query) {
			stmts = db.writerStmts
			conn = db.writerConn
		} else {
			stmts = db.readerStmts
			conn = db.readerPool
		}
	}

	if stmts == nil || conn == nil {
		return nil, sql.ErrConnDone
	}
	if s, found := stmts.Get(query); found && s != nil {
		if tx != nil {
			return tx.tx.StmtContext(ctx, s), nil
		}
		return s, nil
	} else if tx != nil {
		return nil, errors.New("statement not cached")
	}

	s, err := conn.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}

	if existing, loaded := stmts.GetOrSet(query, s, ttlcache.DefaultTTL); loaded {
		_ = s.Close()
		s = existing
	}

	if tx != nil {
		return tx.tx.StmtContext(ctx, s), nil
	}

	return s, nil
}

func (db *DB) deleteStmt(query string, isWrite bool) {
	query = db.bindQuery(query)
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

func isWriteQuery(query string) bool {
	q := strings.TrimLeftFunc(query, unicode.IsSpace)
	if q == "" {
		return false
	}

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

func isTempTableDDL(query string) bool {
	_, ok := tempTableNameFromCreate(query)
	return ok
}

func tempTableNameFromCreate(query string) (string, bool) {
	fields := strings.Fields(query)
	if len(fields) < 4 || !strings.EqualFold(fields[0], "create") {
		return "", false
	}

	next := 1
	if !strings.EqualFold(fields[next], "temp") && !strings.EqualFold(fields[next], "temporary") {
		return "", false
	}
	next++

	if next >= len(fields) || !strings.EqualFold(fields[next], "table") {
		return "", false
	}
	next++

	if next+2 < len(fields) &&
		strings.EqualFold(fields[next], "if") &&
		strings.EqualFold(fields[next+1], "not") &&
		strings.EqualFold(fields[next+2], "exists") {
		next += 3
	}
	if next >= len(fields) {
		return "", false
	}

	name := normalizeIdentifier(trimIdentifierToken(fields[next]))
	if name == "" {
		return "", false
	}
	return name, true
}

func tableNameFromDrop(query string) (string, bool) {
	fields := strings.Fields(query)
	if len(fields) < 3 || !strings.EqualFold(fields[0], "drop") || !strings.EqualFold(fields[1], "table") {
		return "", false
	}

	next := 2
	if next+1 < len(fields) && strings.EqualFold(fields[next], "if") && strings.EqualFold(fields[next+1], "exists") {
		next += 2
	}
	if next >= len(fields) {
		return "", false
	}

	name := normalizeIdentifier(trimIdentifierToken(fields[next]))
	if name == "" {
		return "", false
	}
	return name, true
}

func queryReferencesTempTable(query string, tempTables map[string]struct{}) bool {
	if len(tempTables) == 0 {
		return false
	}

	for _, token := range sqlKeywordTokens(query) {
		if _, ok := tempTables[token]; ok {
			return true
		}
	}

	return false
}

func sqlKeywordTokens(query string) []string {
	return strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
}

func normalizeIdentifier(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, ";,(")
	if raw == "" {
		return ""
	}

	parts := strings.Split(raw, ".")
	name := strings.TrimSpace(parts[len(parts)-1])
	name = strings.Trim(name, "\"`[]")
	return strings.ToLower(name)
}

func trimIdentifierToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if idx := strings.IndexAny(raw, "(,;"); idx >= 0 {
		raw = raw[:idx]
	}

	return strings.TrimSpace(raw)
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
	execStmt(context.Context, *sql.Stmt, []any) (T, error)
	execDirect(context.Context, *sql.DB, string, []any) (T, error)
	getErr(T) error
	getTx() *Tx
}

type execResult struct{}

func (execResult) execStmt(ctx context.Context, stmt *sql.Stmt, args []any) (sql.Result, error) {
	return stmt.ExecContext(ctx, args...)
}

func (execResult) execDirect(ctx context.Context, conn *sql.DB, query string, args []any) (sql.Result, error) {
	return conn.ExecContext(ctx, query, args...)
}

func (execResult) getErr(sql.Result) error { return nil }
func (execResult) getTx() *Tx              { return nil }

type queryRows struct{}

func (queryRows) execStmt(ctx context.Context, stmt *sql.Stmt, args []any) (*sql.Rows, error) {
	return stmt.QueryContext(ctx, args...)
}

func (queryRows) execDirect(ctx context.Context, conn *sql.DB, query string, args []any) (*sql.Rows, error) {
	return conn.QueryContext(ctx, query, args...)
}

func (queryRows) getErr(r *sql.Rows) error {
	if r == nil {
		return nil
	}
	return r.Err()
}
func (queryRows) getTx() *Tx { return nil }

func execWithRetry[T any, E stmtExecutor[T]](ctx context.Context, db *DB, query string, args []any, executor E) (T, error) {
	stmt, err := db.getStmt(ctx, query, executor.getTx())
	if err != nil {
		boundQuery := db.bindQuery(query)
		if isWriteQuery(query) {
			return executor.execDirect(ctx, db.writerConn, boundQuery, args)
		}
		return executor.execDirect(ctx, db.readerPool, boundQuery, args)
	}

	result, execErr := executor.execStmt(ctx, stmt, args)
	resultErr := executor.getErr(result)
	if (execErr == nil || !strings.Contains(execErr.Error(), stmtClosedErrMsg)) &&
		(resultErr == nil || !strings.Contains(resultErr.Error(), stmtClosedErrMsg)) {
		return result, execErr
	}

	if isWriteQuery(query) {
		db.deleteStmt(query, true)
	} else {
		db.deleteStmt(query, false)
	}

	stmt, err = db.getStmt(ctx, query, executor.getTx())
	if err != nil {
		boundQuery := db.bindQuery(query)
		if isWriteQuery(query) {
			return executor.execDirect(ctx, db.writerConn, boundQuery, args)
		}
		return executor.execDirect(ctx, db.readerPool, boundQuery, args)
	}

	result, execErr = executor.execStmt(ctx, stmt, args)
	return result, execErr
}

func (db *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if !isWriteQuery(query) {
		return execWithRetry(ctx, db, query, args, execResult{})
	}

	if err := db.lockWriter(ctx); err != nil {
		return nil, err
	}
	defer db.unlockWriter()

	return execWithRetry(ctx, db, query, args, execResult{})
}

func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if !isWriteQuery(query) {
		return execWithRetry(ctx, db, query, args, queryRows{})
	}

	if err := db.lockWriter(ctx); err != nil {
		return nil, err
	}
	defer db.unlockWriter()

	return execWithRetry(ctx, db, query, args, queryRows{})
}

func (db *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if isWriteQuery(query) {
		if db.lockWriter(ctx) != nil {
			return db.writerConn.QueryRowContext(ctx, db.bindQuery(query), args...)
		}
		row := db.queryRowUnlocked(ctx, query, args...)
		db.unlockWriter()
		return row
	}
	return db.queryRowUnlocked(ctx, query, args...)
}

func (db *DB) lockWriter(ctx context.Context) error {
	if db.writerSem == nil {
		return nil
	}
	select {
	case db.writerSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (db *DB) unlockWriter() {
	if db.writerSem != nil {
		<-db.writerSem
	}
}

func (db *DB) queryRowUnlocked(ctx context.Context, query string, args ...any) *sql.Row {
	stmt, err := db.getStmt(ctx, query, nil)
	if err != nil {
		boundQuery := db.bindQuery(query)
		if isWriteQuery(query) {
			return db.writerConn.QueryRowContext(ctx, boundQuery, args...)
		}
		return db.readerPool.QueryRowContext(ctx, boundQuery, args...)
	}

	row := stmt.QueryRowContext(ctx, args...)
	if row.Err() == nil || !strings.Contains(row.Err().Error(), stmtClosedErrMsg) {
		return row
	}

	if isWriteQuery(query) {
		db.deleteStmt(query, true)
	} else {
		db.deleteStmt(query, false)
	}

	stmt, err = db.getStmt(ctx, query, nil)
	if err != nil {
		boundQuery := db.bindQuery(query)
		if isWriteQuery(query) {
			return db.writerConn.QueryRowContext(ctx, boundQuery, args...)
		}
		return db.readerPool.QueryRowContext(ctx, boundQuery, args...)
	}
	return stmt.QueryRowContext(ctx, args...)
}

func (db *DB) stringPoolCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	initialDelay := time.NewTimer(1 * time.Hour)
	defer initialDelay.Stop()

	consecutiveFailures := 0
	const maxConsecutiveFailures = 5

	for {
		select {
		case <-initialDelay.C:
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
				consecutiveFailures = 0
			}
			cancel()

		case <-ticker.C:
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
				consecutiveFailures = 0
			}
			cancel()

		case <-ctx.Done():
			return
		}
	}
}

func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	isReadOnly := opts != nil && opts.ReadOnly

	if isReadOnly {
		tx, err := db.readerPool.BeginTx(ctx, opts)
		if err != nil {
			return nil, err
		}
		return &Tx{tx: tx, db: db, ctx: ctx, isWriteTx: false, unlockFn: nil}, nil
	}

	if err := db.lockWriter(ctx); err != nil {
		return nil, err
	}

	tx, err := db.writerConn.BeginTx(ctx, opts)
	if err != nil {
		db.unlockWriter()
		if isSQLiteNestedTxErr(err) {
			recordWedgedTransaction()
			log.Error().
				Err(err).
				Str("stack", string(debug.Stack())).
				Msg("SQLite writer connection is wedged in a transaction - this indicates a bug where a previous transaction failed to rollback properly")
			return nil, fmt.Errorf("database connection wedged: %w", err)
		}
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return &Tx{
		tx:        tx,
		db:        db,
		ctx:       ctx,
		isWriteTx: true,
		unlockFn:  db.unlockWriter,
	}, nil
}

func (db *DB) Close() error {
	db.closeOnce.Do(func() {
		db.closing.Store(true)

		if db.cleanupCancel != nil {
			db.cleanupCancel()
		}

		if db.dialect == DialectSQLite {
			ctx, cancel := context.WithTimeout(context.Background(), connectionSetupTimeout)
			defer cancel()
			if _, err := db.writerConn.ExecContext(ctx, "PRAGMA optimize"); err != nil {
				log.Warn().Err(err).Msg("failed to run PRAGMA optimize during close")
			}
		}

		db.stmtMu.Lock()

		if db.writerStmts != nil {
			db.writerStmts.Close()
			db.writerStmts = nil
		}
		if db.readerStmts != nil {
			db.readerStmts.Close()
			db.readerStmts = nil
		}

		db.stmtMu.Unlock()

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

	if _, err := db.writerConn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			filename TEXT NOT NULL UNIQUE,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
		`); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	if err := db.normalizeMigrationFilenames(ctx); err != nil {
		return fmt.Errorf("failed to normalize migration filenames: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	pendingMigrations, err := db.findPendingMigrations(ctx, files)
	if err != nil {
		return fmt.Errorf("failed to find pending migrations: %w", err)
	}

	if len(pendingMigrations) == 0 {
		log.Debug().Msg("No pending migrations")
		return nil
	}

	if err := db.applyAllMigrations(ctx, pendingMigrations); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	return nil
}

func (db *DB) normalizeMigrationFilenames(ctx context.Context) error {
	return db.normalizeMigrationFilenamesWithExecer(ctx, db.writerConn, sharedMigrationFilenameRenames, sqliteMigrationFilenameRenames)
}

func (db *DB) normalizeMigrationFilenamesWithExecer(
	ctx context.Context,
	execer interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	},
	renameSets ...[]migrationFilenameRename,
) error {
	for _, renames := range renameSets {
		for _, r := range renames {
			if _, err := execer.ExecContext(ctx, db.bindQuery(`
				DELETE FROM migrations
				WHERE filename = ?
				  AND EXISTS (SELECT 1 FROM migrations WHERE filename = ?)
			`), r.from, r.to); err != nil {
				return fmt.Errorf("failed to dedupe migration %s -> %s: %w", r.from, r.to, err)
			}

			if _, err := execer.ExecContext(ctx, db.bindQuery(`
				UPDATE migrations
				SET filename = ?
				WHERE filename = ?
				  AND NOT EXISTS (SELECT 1 FROM migrations WHERE filename = ?)
			`), r.to, r.from, r.to); err != nil {
				return fmt.Errorf("failed to rename migration %s -> %s: %w", r.from, r.to, err)
			}
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

func (db *DB) applyAllMigrations(ctx context.Context, migrations []string) error {
	needsForeignKeysOff := map[string]bool{
		"010_add_files_cache_and_string_interning.sql": true,
	}

	tx, err := db.writerConn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	rollbackActive := func() {
		if tx != nil {
			_ = tx.Rollback()
			tx = nil
		}
	}
	defer rollbackActive()

	for _, filename := range migrations {
		if needsForeignKeysOff[filename] {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("failed to commit before %s: %w", filename, err)
			}
			tx = nil

			defer func() {
				if _, err := db.writerConn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
					log.Error().Err(err).Msg("CRITICAL: Failed to re-enable foreign keys after migration - manual intervention required")
				}
			}()

			if err := db.applyMigrationWithForeignKeysOff(ctx, filename); err != nil {
				return err
			}

			tx, err = db.writerConn.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to begin new transaction after %s: %w", filename, err)
			}
		} else {
			if err := execMigration(ctx, tx, filename); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migrations: %w", err)
	}
	tx = nil

	log.Info().Msgf("Applied %d migrations successfully", len(migrations))
	return nil
}

func (db *DB) applyMigrationWithForeignKeysOff(ctx context.Context, filename string) error {
	if _, err := db.writerConn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("failed to disable foreign keys for %s: %w", filename, err)
	}

	if err := db.execMigrationInOwnTx(ctx, filename); err != nil {
		return err
	}

	if _, err := db.writerConn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("failed to re-enable foreign keys after %s: %w", filename, err)
	}
	return nil
}

func (db *DB) execMigrationInOwnTx(ctx context.Context, filename string) error {
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

	if err := execMigration(ctx, fkTx, filename); err != nil {
		return err
	}

	if err := fkTx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration %s: %w", filename, err)
	}
	committed = true
	return nil
}

func execMigration(ctx context.Context, tx *sql.Tx, filename string) error {
	content, err := migrationsFS.ReadFile("migrations/" + filename)
	if err != nil {
		return fmt.Errorf("failed to read migration file %s: %w", filename, err)
	}

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("failed to execute migration %s: %w", filename, err)
	}

	if _, err := tx.ExecContext(ctx, "INSERT INTO migrations (filename) VALUES (?)", filename); err != nil {
		return fmt.Errorf("failed to record migration %s: %w", filename, err)
	}
	return nil
}

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
	SELECT basic_username_id AS string_id FROM torznab_indexers WHERE basic_username_id IS NOT NULL
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
	UNION ALL
	SELECT basic_username_id AS string_id FROM arr_instances WHERE basic_username_id IS NOT NULL
`

func (db *DB) CleanupUnusedStrings(ctx context.Context) (int64, error) {
	if !db.cleanupRunning.CompareAndSwap(false, true) {
		log.Debug().Msg("String pool cleanup already in progress, skipping")
		return 0, nil
	}
	defer db.cleanupRunning.Store(false)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := dbinterface.DeferForeignKeyChecks(ctx, tx); err != nil {
		return 0, fmt.Errorf("failed to defer foreign keys: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		CREATE TEMP TABLE temp_referenced_strings (
			string_id BIGINT PRIMARY KEY
		)
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to create temp table: %w", err)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO temp_referenced_strings (string_id) SELECT DISTINCT string_id FROM (
`+referencedStringsInsertQuery+`
		) AS subquery`)
	if err != nil {
		return 0, fmt.Errorf("failed to populate temp table with referenced string IDs: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		DELETE FROM string_pool
		WHERE NOT EXISTS (
			SELECT 1 FROM temp_referenced_strings trs WHERE trs.string_id = string_pool.id
		)
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to delete unused strings: %w", err)
	}

	_, err = tx.ExecContext(ctx, `DROP TABLE temp_referenced_strings`)
	if err != nil {
		return 0, fmt.Errorf("failed to drop temp table: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected > 0 {
		db.cleanupDeleted.Add(uint64(rowsAffected))
		log.Debug().Msgf("Cleaned up %d unused strings from string_pool (total deleted: %d)",
			rowsAffected, db.cleanupDeleted.Load())
	}

	return rowsAffected, nil
}

func (db *DB) GetStringPoolMetrics() (cleanupDeleted uint64) {
	return db.cleanupDeleted.Load()
}
