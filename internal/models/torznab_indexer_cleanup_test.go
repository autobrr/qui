// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type dialectCapturingQuerier struct {
	*capturingQuerier
	dialect string
}

func (q *dialectCapturingQuerier) Dialect() string {
	return q.dialect
}

func TestCleanupOldLatencyUsesSQLiteFormattedCutoff(t *testing.T) {
	db := openSQLiteDB(t)
	mustExec(t, db, `
		CREATE TABLE torznab_indexer_latency (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			measured_at TIMESTAMP NOT NULL
		)
	`)

	var deleteArgs []any
	q := &dialectCapturingQuerier{
		capturingQuerier: &capturingQuerier{
			db: db,
			onExec: func(query string, args []any) {
				if strings.Contains(query, "DELETE FROM torznab_indexer_latency") {
					deleteArgs = append([]any(nil), args...)
				}
			},
		},
		dialect: "sqlite",
	}

	store, err := NewTorznabIndexerStore(q, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	rows, err := store.CleanupOldLatency(context.Background(), 24*time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 0, rows)
	require.Len(t, deleteArgs, 1)

	cutoffArg, ok := deleteArgs[0].(string)
	require.Truef(t, ok, "expected sqlite cutoff arg as string, got %T", deleteArgs[0])
	_, err = time.Parse(time.DateTime, cutoffArg)
	require.NoError(t, err)
}

func TestCleanupOldLatencyUsesTimeValueForNonSQLite(t *testing.T) {
	db := openSQLiteDB(t)
	mustExec(t, db, `
		CREATE TABLE torznab_indexer_latency (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			measured_at TIMESTAMP NOT NULL
		)
	`)

	var deleteArgs []any
	q := &dialectCapturingQuerier{
		capturingQuerier: &capturingQuerier{
			db: db,
			onExec: func(query string, args []any) {
				if strings.Contains(query, "DELETE FROM torznab_indexer_latency") {
					deleteArgs = append([]any(nil), args...)
				}
			},
		},
		dialect: "postgres",
	}

	store, err := NewTorznabIndexerStore(q, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	rows, err := store.CleanupOldLatency(context.Background(), 24*time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 0, rows)
	require.Len(t, deleteArgs, 1)

	_, ok := deleteArgs[0].(time.Time)
	require.Truef(t, ok, "expected non-sqlite cutoff arg as time.Time, got %T", deleteArgs[0])
}
