// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestTorznabIndexerHealthParsesSQLiteAggregateTimestamps(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "torznab-indexer-health-timestamps")
	store, err := models.NewTorznabIndexerStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	measured, err := store.Create(ctx, "Measured", "http://indexer/measured", "api-key", nil, nil, true, 0, 30)
	require.NoError(t, err)
	unmeasured, err := store.Create(ctx, "Unmeasured", "http://indexer/unmeasured", "api-key", nil, nil, true, 0, 30)
	require.NoError(t, err)

	measuredAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	for _, operation := range []string{"caps", "search"} {
		_, err = db.ExecContext(ctx, `
			INSERT INTO torznab_indexer_latency (indexer_id, operation_type, latency_ms, success, measured_at)
			VALUES (?, ?, ?, ?, ?)
		`, measured.ID, operation, 125, 1, measuredAt.Format(time.DateTime))
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
