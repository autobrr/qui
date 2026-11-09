// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestCleanupOldLatencyDeletesRowsOlderThanTheWindow(t *testing.T) {
	t.Parallel()
	checkCleanupOldLatency(t, testdb.NewMigratedSQLite(t, "latency-cleanup"))
}

func TestCleanupOldLatencyDeletesRowsOlderThanTheWindowPostgresIntegration(t *testing.T) {
	t.Parallel()
	checkCleanupOldLatency(t, testdb.NewMigratedPostgres(t, "latency-cleanup"))
}

func checkCleanupOldLatency(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := t.Context()
	store, err := models.NewTorznabIndexerStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	indexer, err := store.Create(ctx, "Measured", "http://indexer.test/measured", "api-key", nil, nil, true, 0, 30)
	require.NoError(t, err)

	const window = 24 * time.Hour
	now := time.Now().UTC()
	// The store writes measured_at with the database clock, which has this form.
	for operation, measuredAt := range map[string]time.Time{
		"older": now.Add(-window - time.Hour),
		"newer": now.Add(-window + time.Hour),
	} {
		_, err = db.ExecContext(ctx, `
			INSERT INTO torznab_indexer_latency (indexer_id, operation_type, latency_ms, success, measured_at)
			VALUES (?, ?, 125, ?, ?)
		`, indexer.ID, operation, models.BoolToSQLite(true), measuredAt.Format(time.DateTime))
		require.NoError(t, err)
	}

	deleted, err := store.CleanupOldLatency(ctx, window)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var remaining string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT operation_type FROM torznab_indexer_latency`).Scan(&remaining))
	require.Equal(t, "newer", remaining)
}
