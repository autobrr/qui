// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"database/sql"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const sqliteTimeFormMigration = "113_store_times_in_sqlite_form.sql"

func TestSQLiteTimeFormMigrationRewritesGoTextForms(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := openSQLiteSchemaBefore(t, sqliteTimeFormMigration)

	tests := []struct {
		name   string
		stored string
		want   string
	}{
		{name: "utc", stored: "2026-11-09 11:41:31 +0000 UTC", want: "2026-11-09 11:41:31+00:00"},
		{name: "positive offset", stored: "2026-11-09 12:41:31.5 +0100 CET", want: "2026-11-09 11:41:31.5+00:00"},
		{name: "negative offset", stored: "2026-11-09 06:41:31 -0500 EST", want: "2026-11-09 11:41:31+00:00"},
		{name: "offset with minutes moves the date", stored: "2026-11-09 23:30:00.25 -0530 X", want: "2026-11-10 05:00:00.25+00:00"},
		{name: "numeric zone name", stored: "2026-11-09 14:41:31.000001 +0300 +03", want: "2026-11-09 11:41:31.000001+00:00"},
		{name: "nanoseconds", stored: "2026-11-09 11:41:31.947225572 +0000 UTC", want: "2026-11-09 11:41:31.947225572+00:00"},
		{name: "monotonic suffix", stored: "2026-11-09 11:41:31.947225572 +0000 UTC m=+12.5", want: "2026-11-09 11:41:31.947225572+00:00"},
		{name: "zero time", stored: "0001-01-01 00:00:00 +0000 UTC", want: "0001-01-01 00:00:00+00:00"},
		{name: "database clock form", stored: "2026-11-09 11:41:31", want: "2026-11-09 11:41:31"},
		{name: "sqlite form", stored: "2026-11-09 11:41:31.5+00:00", want: "2026-11-09 11:41:31.5+00:00"},
		{name: "not a time", stored: "2026-11-09 99:99:99 +0100 CET", want: "2026-11-09 99:99:99 +0100 CET"},
	}
	for _, tt := range tests {
		_, err := conn.ExecContext(ctx, `INSERT INTO string_pool (value, created_at) VALUES (?, ?)`, tt.name, tt.stored)
		require.NoError(t, err, tt.name)
	}

	runSQLiteMigration(t, conn, sqliteTimeFormMigration)

	for _, tt := range tests {
		var got string
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT created_at || '' FROM string_pool WHERE value = ?`, tt.name).Scan(&got), tt.name)
		require.Equal(t, tt.want, got, tt.name)
	}
}

// Each time column of every table gets a time with a local offset. The check
// then also fails when an update trigger replaces updated_at.
func TestSQLiteTimeFormMigrationCoversEveryTimeColumn(t *testing.T) {
	t.Parallel()

	const (
		goForm     = "2026-11-09 12:41:31.5 +0100 CET"
		sqliteForm = "2026-11-09 11:41:31.5+00:00"
	)
	ctx := t.Context()
	conn := openSQLiteSchemaBefore(t, sqliteTimeFormMigration)

	rows, err := conn.QueryContext(ctx, `
		SELECT m.name, p.name, upper(p.type) IN ('DATE', 'DATETIME', 'TIMESTAMP')
		FROM sqlite_master m, pragma_table_info(m.name) p
		WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'
		ORDER BY m.name, p.cid
	`)
	require.NoError(t, err)
	type tableColumns struct{ all, times, values []string }
	tables := map[string]*tableColumns{}
	for rows.Next() {
		var table, column string
		var isTime bool
		require.NoError(t, rows.Scan(&table, &column, &isTime))
		if tables[table] == nil {
			tables[table] = &tableColumns{}
		}
		value := "1"
		if isTime {
			value = "'" + goForm + "'"
			tables[table].times = append(tables[table].times, fmt.Sprintf("%q", column))
		}
		tables[table].all = append(tables[table].all, fmt.Sprintf("%q", column))
		tables[table].values = append(tables[table].values, value)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	maps.DeleteFunc(tables, func(_ string, columns *tableColumns) bool { return len(columns.times) == 0 })
	require.NotEmpty(t, tables)

	// One row per table, without the parent rows and the valid values that the
	// constraints ask for.
	_, err = conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF; PRAGMA ignore_check_constraints = ON`)
	require.NoError(t, err)
	for table, columns := range tables {
		_, err := conn.ExecContext(ctx, fmt.Sprintf(`INSERT OR REPLACE INTO %q (%s) VALUES (%s)`,
			table, strings.Join(columns.all, ", "), strings.Join(columns.values, ", ")))
		require.NoError(t, err, table)
	}
	schemaBefore := sqliteSchemaObjects(t, conn)

	runSQLiteMigration(t, conn, sqliteTimeFormMigration)

	for table, columns := range tables {
		for _, column := range columns.times {
			var got string
			require.NoError(t, conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s || '' FROM %q`, column, table)).Scan(&got), "%s.%s", table, column)
			require.Equal(t, sqliteForm, got, "%s.%s", table, column)
		}
	}
	require.Equal(t, schemaBefore, sqliteSchemaObjects(t, conn), "the migration changes only data")
}

// openSQLiteSchemaBefore returns one connection to a new database with the
// schema of every SQLite migration that sorts before the named one.
func openSQLiteSchemaBefore(t *testing.T, migration string) *sql.Conn {
	t.Helper()
	ctx := t.Context()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	require.NoError(t, err)
	require.Contains(t, files, "migrations/"+migration)
	for _, file := range files {
		if file >= "migrations/"+migration {
			break
		}
		body, err := migrationsFS.ReadFile(file)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, string(body))
		require.NoError(t, err, file)
	}
	return conn
}

func runSQLiteMigration(t *testing.T, conn *sql.Conn, migration string) {
	t.Helper()
	body, err := migrationsFS.ReadFile("migrations/" + migration)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(body))
	require.NoError(t, err)
}

func sqliteSchemaObjects(t *testing.T, conn *sql.Conn) []string {
	t.Helper()
	rows, err := conn.QueryContext(t.Context(), `
		SELECT type || ' ' || name || ' on ' || tbl_name || ': ' || coalesce(sql, '')
		FROM sqlite_master ORDER BY type, name
	`)
	require.NoError(t, err)
	defer rows.Close()
	var objects []string
	for rows.Next() {
		var object string
		require.NoError(t, rows.Scan(&object))
		objects = append(objects, object)
	}
	require.NoError(t, rows.Err())
	return objects
}
