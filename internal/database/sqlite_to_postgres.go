// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SQLiteToPostgresMigrationOptions struct {
	SQLitePath  string
	PostgresDSN string
	Apply       bool
}

type TableMigrationResult struct {
	Table        string
	SQLiteRows   int64
	PostgresRows int64
}

type SQLiteToPostgresMigrationReport struct {
	Applied               bool
	Tables                []TableMigrationResult
	MissingPostgresTables []string
}

type sqliteTableMeta struct {
	Name string
	Deps map[string]struct{}
}

func MigrateSQLiteToPostgres(ctx context.Context, opts SQLiteToPostgresMigrationOptions) (*SQLiteToPostgresMigrationReport, error) {
	sqlitePath := strings.TrimSpace(opts.SQLitePath)
	pgDSN := strings.TrimSpace(opts.PostgresDSN)
	if sqlitePath == "" {
		return nil, errors.New("sqlite path is required")
	}
	if pgDSN == "" {
		return nil, errors.New("postgres dsn is required")
	}
	if _, err := os.Stat(sqlitePath); err != nil {
		return nil, fmt.Errorf("stat sqlite file: %w", err)
	}

	sqliteDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro", sqlitePath))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	defer sqliteDB.Close()

	tables, err := listSQLiteTables(ctx, sqliteDB)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, errors.New("sqlite database has no importable tables")
	}

	sqliteCounts, err := countSQLiteRows(ctx, sqliteDB, tables)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.New(ctx, pgDSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	if !opts.Apply {
		report, err := buildDryRunReport(ctx, pool, tables, sqliteCounts)
		if err != nil {
			return nil, err
		}
		return report, nil
	}

	bootstrapDB, err := Open(OpenOptions{
		Engine:      string(DialectPostgres),
		PostgresDSN: pgDSN,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap postgres schema: %w", err)
	}
	if closeErr := bootstrapDB.Close(); closeErr != nil {
		return nil, fmt.Errorf("close bootstrap postgres connection: %w", closeErr)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire postgres connection: %w", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin postgres import transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(922337203685477001)"); err != nil {
		return nil, fmt.Errorf("acquire postgres import lock: %w", err)
	}

	if err := truncatePostgresTables(ctx, tx, tables); err != nil {
		return nil, err
	}

	report := &SQLiteToPostgresMigrationReport{
		Applied: true,
		Tables:  make([]TableMigrationResult, 0, len(tables)),
	}

	for _, table := range tables {
		copied, err := copySQLiteTableToPostgres(ctx, sqliteDB, tx, table)
		if err != nil {
			return nil, fmt.Errorf("copy table %s: %w", table, err)
		}

		var pgRows int64
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM "+quoteIdent(table)).Scan(&pgRows); err != nil {
			return nil, fmt.Errorf("count postgres rows for %s: %w", table, err)
		}

		report.Tables = append(report.Tables, TableMigrationResult{
			Table:        table,
			SQLiteRows:   sqliteCounts[table],
			PostgresRows: pgRows,
		})

		if sqliteCounts[table] != pgRows {
			return nil, fmt.Errorf("row count mismatch for table %s: sqlite=%d postgres=%d", table, sqliteCounts[table], pgRows)
		}
		if copied != pgRows {
			return nil, fmt.Errorf("copy count mismatch for table %s: copied=%d postgres=%d", table, copied, pgRows)
		}
	}

	if err := resetPostgresIdentities(ctx, tx); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit postgres import: %w", err)
	}
	committed = true

	return report, nil
}

func buildDryRunReport(ctx context.Context, pool *pgxpool.Pool, tables []string, sqliteCounts map[string]int64) (*SQLiteToPostgresMigrationReport, error) {
	pgTables, err := listPostgresTables(ctx, pool)
	if err != nil {
		return nil, err
	}

	report := &SQLiteToPostgresMigrationReport{
		Applied: false,
		Tables:  make([]TableMigrationResult, 0, len(tables)),
	}

	for _, table := range tables {
		pgRows := int64(0)
		if _, ok := pgTables[table]; ok {
			err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+quoteIdent(table)).Scan(&pgRows)
			if err != nil {
				return nil, fmt.Errorf("count postgres rows for %s: %w", table, err)
			}
		} else {
			report.MissingPostgresTables = append(report.MissingPostgresTables, table)
		}

		report.Tables = append(report.Tables, TableMigrationResult{
			Table:        table,
			SQLiteRows:   sqliteCounts[table],
			PostgresRows: pgRows,
		})
	}

	sort.Strings(report.MissingPostgresTables)
	return report, nil
}

func listSQLiteTables(ctx context.Context, db *sql.DB) ([]string, error) {
	metas, err := listSQLiteTableMeta(ctx, db)
	if err != nil {
		return nil, err
	}

	sorted := topoSortSQLiteTables(metas)
	tables := make([]string, 0, len(sorted))
	for _, meta := range sorted {
		tables = append(tables, meta.Name)
	}
	return tables, nil
}

func listSQLiteTableMeta(ctx context.Context, db *sql.DB) ([]sqliteTableMeta, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table'
		  AND name NOT LIKE 'sqlite_%'
		  AND name != 'migrations'
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list sqlite tables: %w", err)
	}
	defer rows.Close()

	var metas []sqliteTableMeta
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan sqlite table name: %w", err)
		}

		deps, err := sqliteTableDependencies(ctx, db, name)
		if err != nil {
			return nil, err
		}

		metas = append(metas, sqliteTableMeta{
			Name: name,
			Deps: deps,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite tables: %w", err)
	}

	return metas, nil
}

func sqliteTableDependencies(ctx context.Context, db *sql.DB, table string) (map[string]struct{}, error) {
	query := fmt.Sprintf(`PRAGMA foreign_key_list(%s)`, quoteSQLiteString(table))
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list sqlite foreign keys for %s: %w", table, err)
	}
	defer rows.Close()

	deps := make(map[string]struct{})
	for rows.Next() {
		var (
			id, seq                                    int
			refTable, from, to, onUpdate, onDelete, mt string
		)
		if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &mt); err != nil {
			return nil, fmt.Errorf("scan sqlite foreign key for %s: %w", table, err)
		}
		if refTable != "" && refTable != table {
			deps[refTable] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite foreign keys for %s: %w", table, err)
	}
	return deps, nil
}

func topoSortSQLiteTables(metas []sqliteTableMeta) []sqliteTableMeta {
	if len(metas) == 0 {
		return nil
	}

	metaByName := make(map[string]sqliteTableMeta, len(metas))
	indegree := make(map[string]int, len(metas))
	children := make(map[string][]string, len(metas))

	for _, meta := range metas {
		metaByName[meta.Name] = meta
		indegree[meta.Name] = 0
	}

	for _, meta := range metas {
		for dep := range meta.Deps {
			if _, ok := indegree[dep]; !ok {
				continue
			}
			indegree[meta.Name]++
			children[dep] = append(children[dep], meta.Name)
		}
	}

	var queue []string
	for name, deg := range indegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)

	result := make([]sqliteTableMeta, 0, len(metas))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		result = append(result, metaByName[name])
		for _, child := range children[name] {
			indegree[child]--
			if indegree[child] == 0 {
				queue = append(queue, child)
				sort.Strings(queue)
			}
		}
	}

	if len(result) == len(metas) {
		return result
	}

	seen := make(map[string]struct{}, len(result))
	for _, meta := range result {
		seen[meta.Name] = struct{}{}
	}

	var remaining []string
	for _, meta := range metas {
		if _, ok := seen[meta.Name]; !ok {
			remaining = append(remaining, meta.Name)
		}
	}
	sort.Strings(remaining)
	for _, name := range remaining {
		result = append(result, metaByName[name])
	}

	return result
}

func countSQLiteRows(ctx context.Context, db *sql.DB, tables []string) (map[string]int64, error) {
	counts := make(map[string]int64, len(tables))
	for _, table := range tables {
		var count int64
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdent(table)).Scan(&count); err != nil {
			return nil, fmt.Errorf("count sqlite rows for %s: %w", table, err)
		}
		counts[table] = count
	}
	return counts, nil
}

func listPostgresTables(ctx context.Context, pool *pgxpool.Pool) (map[string]struct{}, error) {
	rows, err := pool.Query(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = 'public'
	`)
	if err != nil {
		return nil, fmt.Errorf("list postgres tables: %w", err)
	}
	defer rows.Close()

	set := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan postgres table name: %w", err)
		}
		set[name] = struct{}{}
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate postgres table names: %w", rows.Err())
	}
	return set, nil
}

func truncatePostgresTables(ctx context.Context, tx pgx.Tx, tables []string) error {
	if len(tables) == 0 {
		return nil
	}

	quoted := make([]string, 0, len(tables))
	for _, table := range tables {
		quoted = append(quoted, quoteIdent(table))
	}

	query := fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE", strings.Join(quoted, ", "))
	if _, err := tx.Exec(ctx, query); err != nil {
		return fmt.Errorf("truncate postgres tables: %w", err)
	}
	return nil
}

func copySQLiteTableToPostgres(ctx context.Context, sqliteDB *sql.DB, tx pgx.Tx, table string) (int64, error) {
	columns, sqliteTypes, err := sqliteTableColumns(ctx, sqliteDB, table)
	if err != nil {
		return 0, err
	}
	if len(columns) == 0 {
		return 0, nil
	}

	// #nosec G201 -- identifiers are quoted and derived from sqlite schema metadata.
	selectQuery := fmt.Sprintf("SELECT %s FROM %s", joinQuoted(columns), quoteIdent(table))
	rows, err := sqliteDB.QueryContext(ctx, selectQuery)
	if err != nil {
		return 0, fmt.Errorf("query sqlite table %s: %w", table, err)
	}
	defer rows.Close()

	const batchSize = 1_000
	batch := make([][]any, 0, batchSize)
	copied := int64(0)

	for rows.Next() {
		raw, err := scanRowValues(rows)
		if err != nil {
			return 0, fmt.Errorf("scan sqlite row for %s: %w", table, err)
		}

		row := make([]any, len(raw))
		for i := range raw {
			row[i] = normalizeSQLiteValue(raw[i], sqliteTypes[i])
		}
		batch = append(batch, row)

		if len(batch) == batchSize {
			n, err := tx.CopyFrom(ctx, pgx.Identifier{table}, columns, pgx.CopyFromRows(batch))
			if err != nil {
				return 0, fmt.Errorf("copy batch for table %s: %w", table, err)
			}
			copied += n
			batch = batch[:0]
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate sqlite rows for %s: %w", table, err)
	}

	if len(batch) > 0 {
		n, err := tx.CopyFrom(ctx, pgx.Identifier{table}, columns, pgx.CopyFromRows(batch))
		if err != nil {
			return 0, fmt.Errorf("copy final batch for table %s: %w", table, err)
		}
		copied += n
	}

	return copied, nil
}

func sqliteTableColumns(ctx context.Context, db *sql.DB, table string) ([]string, []string, error) {
	query := fmt.Sprintf(`PRAGMA table_info(%s)`, quoteSQLiteString(table))
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, nil, fmt.Errorf("list sqlite columns for %s: %w", table, err)
	}
	defer rows.Close()

	var (
		columns []string
		types   []string
	)

	for rows.Next() {
		var (
			cid      int
			name     string
			colType  string
			notNull  int
			defaultV any
			primaryK int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultV, &primaryK); err != nil {
			return nil, nil, fmt.Errorf("scan sqlite column for %s: %w", table, err)
		}
		columns = append(columns, name)
		types = append(types, strings.ToUpper(strings.TrimSpace(colType)))
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate sqlite columns for %s: %w", table, err)
	}

	return columns, types, nil
}

func scanRowValues(rows *sql.Rows) ([]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	raw := make([]any, len(columns))
	dest := make([]any, len(columns))
	for i := range raw {
		dest[i] = &raw[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	return raw, nil
}

func normalizeSQLiteValue(v any, sqliteType string) any {
	if v == nil {
		return nil
	}

	switch value := v.(type) {
	case bool:
		if value {
			return int64(1)
		}
		return int64(0)
	case []byte:
		if strings.Contains(sqliteType, "BLOB") || strings.Contains(sqliteType, "BYTEA") {
			buf := make([]byte, len(value))
			copy(buf, value)
			return buf
		}
		return string(value)
	default:
		return value
	}
}

func resetPostgresIdentities(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `
		SELECT table_name, column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND is_identity = 'YES'
	`)
	if err != nil {
		return fmt.Errorf("list postgres identity columns: %w", err)
	}

	type identityColumn struct {
		table  string
		column string
	}
	identityColumns := make([]identityColumn, 0, 16)
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			rows.Close()
			return fmt.Errorf("scan postgres identity column: %w", err)
		}
		identityColumns = append(identityColumns, identityColumn{table: table, column: column})
	}
	if rows.Err() != nil {
		rows.Close()
		return fmt.Errorf("iterate postgres identity columns: %w", rows.Err())
	}
	rows.Close()

	for _, identity := range identityColumns {
		fullTable := "public." + identity.table
		query := fmt.Sprintf(`
			SELECT setval(
				pg_get_serial_sequence($1, $2),
				COALESCE((SELECT MAX(%s) FROM %s), 0) + 1,
				false
			)
		`, quoteIdent(identity.column), quoteIdent(identity.table))
		if _, err := tx.Exec(ctx, query, fullTable, identity.column); err != nil {
			return fmt.Errorf("reset identity for %s.%s: %w", identity.table, identity.column, err)
		}
	}

	return nil
}

func joinQuoted(columns []string) string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, quoteIdent(column))
	}
	return strings.Join(quoted, ", ")
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteSQLiteString(name string) string {
	return "'" + strings.ReplaceAll(name, "'", "''") + "'"
}
