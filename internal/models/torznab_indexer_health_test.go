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

// regression: #2714
func TestTorznabIndexerHealthReadsLastMeasuredTime(t *testing.T) {
	t.Parallel()
	checkIndexerHealthLastMeasuredTime(t, testdb.NewMigratedSQLite(t, "torznab-indexer-health-timestamps"))
}

// regression: #2714
func TestTorznabIndexerHealthReadsLastMeasuredTimePostgresIntegration(t *testing.T) {
	t.Parallel()
	checkIndexerHealthLastMeasuredTime(t, testdb.NewMigratedPostgres(t, "torznab-indexer-health-timestamps"))
}

func checkIndexerHealthLastMeasuredTime(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := t.Context()
	store, err := models.NewTorznabIndexerStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	measured, err := store.Create(ctx, "Measured", "http://indexer/measured", "api-key", nil, nil, true, 0, 30)
	require.NoError(t, err)
	unmeasured, err := store.Create(ctx, "Unmeasured", "http://indexer/unmeasured", "api-key", nil, nil, true, 0, 30)
	require.NoError(t, err)

	measuredAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	// One row in the form that the database clock writes, and one bound time.
	for operation, value := range map[string]any{"caps": measuredAt.Format(time.DateTime), "search": measuredAt} {
		_, err = db.ExecContext(ctx, `
			INSERT INTO torznab_indexer_latency (indexer_id, operation_type, latency_ms, success, measured_at)
			VALUES (?, ?, ?, ?, ?)
		`, measured.ID, operation, 125, 1, value)
		require.NoError(t, err)
	}

	health, err := store.GetHealth(ctx, measured.ID)
	require.NoError(t, err)
	require.NotNil(t, health.LastMeasuredAt)
	require.Equal(t, measuredAt, *health.LastMeasuredAt)
	require.Equal(t, time.UTC, health.LastMeasuredAt.Location())

	unmeasuredHealth, err := store.GetHealth(ctx, unmeasured.ID)
	require.NoError(t, err)
	require.Nil(t, unmeasuredHealth.LastMeasuredAt)

	allHealth, err := store.GetAllHealth(ctx)
	require.NoError(t, err)
	require.Len(t, allHealth, 2)
	byID := make(map[int]models.TorznabIndexerHealth, len(allHealth))
	for _, item := range allHealth {
		byID[item.IndexerID] = item
	}
	require.NotNil(t, byID[measured.ID].LastMeasuredAt)
	require.Equal(t, measuredAt, *byID[measured.ID].LastMeasuredAt)
	require.Nil(t, byID[unmeasured.ID].LastMeasuredAt)

	stats, err := store.GetLatencyStats(ctx, measured.ID)
	require.NoError(t, err)
	require.Len(t, stats, 2)
	for _, stat := range stats {
		require.Contains(t, []string{"caps", "search"}, stat.OperationType)
		require.Equal(t, measuredAt, stat.LastMeasuredAt)
		require.Equal(t, time.UTC, stat.LastMeasuredAt.Location())
	}
}
