// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database_test

import (
	"database/sql"
	"database/sql/driver"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// These tests replace time.Local, which is process-global, so none of them may
// call t.Parallel.

var utcContractZones = []string{"America/New_York", "Asia/Tokyo"}

// postgresSessionZone is the zone that the test connection string asks for. It
// differs from UTC and from each process zone by more than an hour.
const postgresSessionZone = "Pacific/Honolulu"

type wrappedTime struct{ at time.Time }

func (w wrappedTime) Value() (driver.Value, error) { return w.at, nil }

// regression: #3942
func TestTimesReadBackAsUTCSQLite(t *testing.T) {
	for _, zone := range utcContractZones {
		t.Run(zone, func(t *testing.T) {
			setProcessZone(t, zone)
			checkTimesReadBackAsUTC(t, testdb.NewMigratedSQLite(t, "utc-times"))
		})
	}
}

// regression: #3942
func TestTimesReadBackAsUTCPostgresIntegration(t *testing.T) {
	for _, zone := range utcContractZones {
		t.Run(zone, func(t *testing.T) {
			setProcessZone(t, zone)
			checkTimesReadBackAsUTC(t, newPostgresInSessionZone(t, "utc-times"))
		})
	}
}

// regression: #3942
func TestOffsetTextRowReadsBackAsUTCSQLite(t *testing.T) {
	for _, zone := range utcContractZones {
		t.Run(zone, func(t *testing.T) {
			setProcessZone(t, zone)
			db := testdb.NewMigratedSQLite(t, "utc-offset-text")
			createTimeTable(t, db)

			// An older qui bound times in the process zone, so the driver stored the offset in the text.
			_, err := db.ExecContext(t.Context(), `INSERT INTO utc_contract (label, ts) VALUES ('old', '2026-03-01 07:34:56.123456 -0500 EST')`)
			require.NoError(t, err)

			got := readTime(t, db, "old")
			require.True(t, got.Equal(time.Date(2026, 3, 1, 12, 34, 56, 123456000, time.UTC)), "read back %s", got)
			require.Equal(t, time.UTC, got.Location())
		})
	}
}

func checkTimesReadBackAsUTC(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := t.Context()
	createTimeTable(t, db)

	at := time.Date(2026, 3, 1, 12, 34, 56, 123456000, time.Local)
	bound := map[string]any{
		"plain":    at,
		"pointer":  &at,
		"nulltime": sql.NullTime{Time: at, Valid: true},
		"wrapped":  wrappedTime{at: at},
	}
	for label, value := range bound {
		_, err := db.ExecContext(ctx, `INSERT INTO utc_contract (label, ts) VALUES (?, ?)`, label, value)
		require.NoError(t, err, label)
	}
	for label := range bound {
		got := readTime(t, db, label)
		require.True(t, got.Equal(at), "%s read back %s, want %s", label, got, at)
		require.Equal(t, time.UTC, got.Location(), label)
	}

	_, err := db.ExecContext(ctx, `INSERT INTO utc_contract (label, ts) VALUES ('clock', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	clock := readTime(t, db, "clock")
	require.WithinDuration(t, time.Now(), clock, 5*time.Second)
	require.Equal(t, time.UTC, clock.Location())

	now := time.Now()
	_, err = db.ExecContext(ctx, `INSERT INTO utc_contract (label, ts) VALUES ('past', ?), ('future', ?)`, now.Add(-3*time.Hour), now.Add(3*time.Hour))
	require.NoError(t, err)

	rows, err := db.QueryContext(ctx, `
		SELECT label FROM utc_contract
		WHERE label IN ('clock', 'past', 'future') AND ts >= ? AND ts < ?
		ORDER BY label
	`, now.Add(-time.Hour), now.Add(time.Hour))
	require.NoError(t, err)
	defer rows.Close()
	var selected []string
	for rows.Next() {
		var label string
		require.NoError(t, rows.Scan(&label))
		selected = append(selected, label)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"clock"}, selected, "a bound cutoff selects only the row that the database clock wrote now")
}

func createTimeTable(t *testing.T, db *database.DB) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `CREATE TABLE utc_contract (label TEXT PRIMARY KEY, ts TIMESTAMP)`)
	require.NoError(t, err)
}

func readTime(t *testing.T, db *database.DB, label string) time.Time {
	t.Helper()
	var got time.Time
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT ts FROM utc_contract WHERE label = ?`, label).Scan(&got))
	return got
}

func setProcessZone(t *testing.T, name string) {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	original := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = original })
}

// newPostgresInSessionZone opens a migrated Postgres schema whose connection
// string asks for postgresSessionZone.
func newPostgresInSessionZone(t *testing.T, name string) *database.DB {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("QUI_TEST_POSTGRES_DSN"))
	switch {
	case baseDSN == "":
	case strings.HasPrefix(baseDSN, "postgres://"), strings.HasPrefix(baseDSN, "postgresql://"):
		parsed, err := url.Parse(baseDSN)
		require.NoError(t, err)
		query := parsed.Query()
		query.Set("timezone", postgresSessionZone)
		parsed.RawQuery = query.Encode()
		t.Setenv("QUI_TEST_POSTGRES_DSN", parsed.String())
	default:
		t.Setenv("QUI_TEST_POSTGRES_DSN", baseDSN+" timezone="+postgresSessionZone)
	}
	return testdb.NewMigratedPostgres(t, name)
}
