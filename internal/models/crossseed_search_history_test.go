// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Postgres rejects a multi-row ON CONFLICT DO UPDATE that hits the same
// conflict target twice, so the upsert must deduplicate the indexer IDs.
// SQLite tolerates duplicates, so this asserts on the built statement instead.
func TestUpsertIndexerSearchHistoryDeduplicatesIDs(t *testing.T) {
	db := openSQLiteDB(t)
	mustExec(t, db, `
		CREATE TABLE cross_seed_search_history_indexers (
			instance_id INTEGER NOT NULL,
			torrent_hash TEXT NOT NULL,
			indexer_id INTEGER NOT NULL,
			last_searched_at DATETIME NOT NULL,
			PRIMARY KEY (instance_id, torrent_hash, indexer_id)
		)
	`)

	var execArgs []any
	q := &capturingQuerier{
		db: db,
		onExec: func(_ string, args []any) {
			execArgs = append([]any(nil), args...)
		},
	}

	key := make([]byte, 32)
	store, err := NewCrossSeedStore(q, key)
	require.NoError(t, err)

	err = store.UpsertIndexerSearchHistory(context.Background(), 1, "aaaa", []int{2, 1, 2, 1}, time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, execArgs, 8, "duplicate indexer IDs must collapse to one row each")
	require.Equal(t, 1, execArgs[2])
	require.Equal(t, 2, execArgs[6])
}
