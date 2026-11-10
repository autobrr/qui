// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestBoundTimeIsStoredInSQLiteForm(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "sqlite-time-form")
	ctx := t.Context()
	createTimeTable(t, db)

	at := time.Date(2026, 11, 9, 12, 41, 31, 947225000, time.FixedZone("CET", 3600))
	_, err := db.ExecContext(ctx, `INSERT INTO utc_contract (label, ts) VALUES ('bound', ?)`, at)
	require.NoError(t, err)

	var stored string
	var unixSeconds int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT ts || '', unixepoch(ts) FROM utc_contract WHERE label = 'bound'`).Scan(&stored, &unixSeconds))
	require.Equal(t, "2026-11-09 11:41:31.947225+00:00", stored)
	require.Equal(t, at.Unix(), unixSeconds, "SQL time functions read the stored form")

	var boundOnRead string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT ? || ''`, at).Scan(&boundOnRead))
	require.Equal(t, stored, boundOnRead, "the read pool binds a time in the same form as the writer")
}

func TestStoredTimeFormsSortByInstantSQLite(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "sqlite-time-order")
	ctx := t.Context()
	createTimeTable(t, db)

	second := time.Date(2026, 11, 9, 11, 41, 31, 0, time.UTC)
	_, err := db.ExecContext(ctx, `
		INSERT INTO utc_contract (label, ts) VALUES
			('4 clock, next second', '2026-11-09 11:41:32'),
			('3 bound, half second', ?),
			('1 clock', '2026-11-09 11:41:31'),
			('2 bound, quarter second', ?)
	`, second.Add(500*time.Millisecond), second.Add(250*time.Millisecond))
	require.NoError(t, err)

	rows, err := db.QueryContext(ctx, `SELECT label FROM utc_contract ORDER BY ts`)
	require.NoError(t, err)
	defer rows.Close()
	var labels []string
	for rows.Next() {
		var label string
		require.NoError(t, rows.Scan(&label))
		labels = append(labels, label)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"1 clock", "2 bound, quarter second", "3 bound, half second", "4 clock, next second"}, labels)
}
