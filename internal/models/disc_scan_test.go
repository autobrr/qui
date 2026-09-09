// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestDiscScanStoreSQLite(t *testing.T) {
	t.Parallel()
	runDiscScanStoreTests(t, testdb.NewMigratedSQLite(t, "disc-scan"))
}

func TestDiscScanStorePostgres(t *testing.T) {
	runDiscScanStoreTests(t, testdb.NewMigratedPostgres(t, "disc-scan"))
}

func runDiscScanStoreTests(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := t.Context()

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDiscScanStore(db)

	first, err := store.Create(ctx, instance.ID, "hash-a", "Box/Disc 1", "/data/Box/Disc 1")
	require.NoError(t, err)
	second, err := store.Create(ctx, instance.ID, "hash-a", "Box/Disc 2", "/data/Box/Disc 2")
	require.NoError(t, err)

	// Latest by cache key sees the newest row for that resolved path only.
	latest, err := store.Latest(ctx, instance.ID, "/data/Box/Disc 2")
	require.NoError(t, err)
	require.Equal(t, second, latest.ID)
	require.Equal(t, models.DiscScanStatusPending, latest.Status)
	require.Equal(t, 2, latest.QueuePosition)
	missing, err := store.Latest(ctx, instance.ID, "/data/Box/Disc 3")
	require.NoError(t, err)
	require.Nil(t, missing)

	// FIFO pick.
	next, err := store.NextPending(ctx)
	require.NoError(t, err)
	require.Equal(t, first, next.ID)
	require.Equal(t, 1, next.QueuePosition)

	started, err := store.MarkScanning(ctx, first)
	require.NoError(t, err)
	require.True(t, started)
	again, err := store.MarkScanning(ctx, first)
	require.NoError(t, err)
	require.False(t, again)

	require.NoError(t, store.UpdateProgress(ctx, first, 10, 100))
	done, err := store.MarkCompleted(ctx, first, "report", "quick", "forum")
	require.NoError(t, err)
	require.True(t, done)

	run, err := store.GetByInstance(ctx, instance.ID, first)
	require.NoError(t, err)
	require.Equal(t, models.DiscScanStatusCompleted, run.Status)
	require.Equal(t, int64(100), run.ProcessedBytes)
	require.Equal(t, "quick", run.QuickSummary)
	require.Equal(t, "forum", run.ForumsBlock)
	require.NotNil(t, run.StartedAt)
	require.NotNil(t, run.CompletedAt)

	// A completed row cannot be canceled or overwritten.
	canceled, err := store.MarkCanceled(ctx, first)
	require.NoError(t, err)
	require.False(t, canceled)

	// Newest per disc path for the torrent: a rescan of Disc 1 hides the old row.
	third, err := store.Create(ctx, instance.ID, "hash-a", "Box/Disc 1", "/data/Box/Disc 1")
	require.NoError(t, err)
	runs, err := store.ListNewestForTorrent(ctx, instance.ID, "hash-a")
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, third, runs[0].ID)
	require.Equal(t, second, runs[1].ID)

	// A canceled scan never gets a report, even when the scanner finishes late.
	started, err = store.MarkScanning(ctx, third)
	require.NoError(t, err)
	require.True(t, started)
	canceled, err = store.MarkCanceled(ctx, third)
	require.NoError(t, err)
	require.True(t, canceled)
	done, err = store.MarkCompleted(ctx, third, "late", "late", "late")
	require.NoError(t, err)
	require.False(t, done)
	run, err = store.GetByInstance(ctx, instance.ID, third)
	require.NoError(t, err)
	require.Equal(t, models.DiscScanStatusCanceled, run.Status)
	require.Empty(t, run.Report)

	// Restart recovery fails scanning rows and leaves pending rows queued.
	started, err = store.MarkScanning(ctx, second)
	require.NoError(t, err)
	require.True(t, started)
	require.NoError(t, store.MarkInterrupted(ctx, "interrupted"))
	run, err = store.GetByInstance(ctx, instance.ID, second)
	require.NoError(t, err)
	require.Equal(t, models.DiscScanStatusFailed, run.Status)
	require.Equal(t, "interrupted", run.ErrorMessage)
	next, err = store.NextPending(ctx)
	require.NoError(t, err)
	require.Nil(t, next)
}
