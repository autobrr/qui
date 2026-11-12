// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests replace time.Local, which is process-global, so none of them may
// call t.Parallel.

// migrationSessionZone differs from UTC and from each process zone by more than an hour.
const migrationSessionZone = "Pacific/Honolulu"

// regression: #3943
func TestSQLiteToPostgresMigrationKeepsEachInstantPostgresIntegration(t *testing.T) {
	for _, zone := range []string{"America/New_York", "Asia/Tokyo"} {
		t.Run(zone, func(t *testing.T) {
			setMigrationProcessZone(t, zone)
			_, testDSN := openPostgresTestSchema(t)
			testDSN = dsnWithParam(t, testDSN, "timezone", migrationSessionZone)

			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			t.Cleanup(cancel)

			sqlitePath := filepath.Join(t.TempDir(), "fixture.db")
			sqliteDB, err := New(sqlitePath)
			require.NoError(t, err)
			_, err = sqliteDB.Conn().ExecContext(ctx, `CREATE TABLE utc_migration (label TEXT PRIMARY KEY, ts TIMESTAMP)`)
			require.NoError(t, err)
			// An older qui bound times in the process zone, so the driver stored the offset in the text.
			_, err = sqliteDB.Conn().ExecContext(ctx, `INSERT INTO utc_migration (label, ts) VALUES ('offset', '2026-03-01 07:34:56.123456 -0500 EST')`)
			require.NoError(t, err)
			_, err = sqliteDB.Conn().ExecContext(ctx, `INSERT INTO utc_migration (label, ts) VALUES ('clock', CURRENT_TIMESTAMP)`)
			require.NoError(t, err)
			require.NoError(t, sqliteDB.Close())

			pgDB, err := Open(OpenOptions{Engine: string(DialectPostgres), PostgresDSN: testDSN})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, pgDB.Close()) })
			_, err = pgDB.ExecContext(ctx, `CREATE TABLE utc_migration (label TEXT PRIMARY KEY, ts TIMESTAMP)`)
			require.NoError(t, err)

			_, err = MigrateSQLiteToPostgres(ctx, SQLiteToPostgresMigrationOptions{
				SQLitePath:  sqlitePath,
				PostgresDSN: testDSN,
				Apply:       true,
			})
			require.NoError(t, err)

			var offset, clock time.Time
			require.NoError(t, pgDB.QueryRowContext(ctx, `SELECT ts FROM utc_migration WHERE label = ?`, "offset").Scan(&offset))
			require.NoError(t, pgDB.QueryRowContext(ctx, `SELECT ts FROM utc_migration WHERE label = ?`, "clock").Scan(&clock))

			require.True(t, offset.Equal(time.Date(2026, 3, 1, 12, 34, 56, 123456000, time.UTC)), "offset row read back %s", offset)
			require.WithinDuration(t, time.Now(), clock, time.Minute)
		})
	}
}

// regression: #3943
func TestSQLiteToPostgresMigrationPoolUsesUTCPostgresIntegration(t *testing.T) {
	setMigrationProcessZone(t, "Asia/Tokyo")
	ctx, testDSN := openPostgresTestSchema(t)

	pool, err := openPostgresPool(ctx, dsnWithParam(t, testDSN, "timezone", migrationSessionZone))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	var sessionZone string
	require.NoError(t, pool.QueryRow(ctx, `SELECT current_setting('TimeZone')`).Scan(&sessionZone))
	require.Equal(t, "UTC", sessionZone)

	at := time.Date(2026, 3, 1, 21, 34, 56, 0, time.Local)
	var got time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT $1::timestamp`, at).Scan(&got))
	require.True(t, got.Equal(at), "bound %s, read back %s", at, got)
}

func setMigrationProcessZone(t *testing.T, name string) {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	original := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = original })
}
