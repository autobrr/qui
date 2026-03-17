// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func setupDirScanTestDB(t *testing.T) *database.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "dirscan.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

func TestDirScanStore_CreateRunIfNoActive_CreatesQueuedRun(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	runID, err := store.CreateRunIfNoActive(ctx, dir.ID, "manual", "")
	require.NoError(t, err)
	require.Greater(t, runID, int64(0))

	run, err := store.GetRun(ctx, runID)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, models.DirScanRunStatusQueued, run.Status)

	active, err := store.HasActiveRun(ctx, dir.ID)
	require.NoError(t, err)
	require.True(t, active)

	activeRun, err := store.GetActiveRun(ctx, dir.ID)
	require.NoError(t, err)
	require.NotNil(t, activeRun)
	require.Equal(t, runID, activeRun.ID)
	require.Equal(t, models.DirScanRunStatusQueued, activeRun.Status)
}

func TestDirScanStore_MarkActiveRunsFailed_IncludesQueued(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	runID, err := store.CreateRunIfNoActive(ctx, dir.ID, "manual", "")
	require.NoError(t, err)

	affected, err := store.MarkActiveRunsFailed(ctx, "restart")
	require.NoError(t, err)
	require.EqualValues(t, 1, affected)

	run, err := store.GetRun(ctx, runID)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, models.DirScanRunStatusFailed, run.Status)
	require.Equal(t, "restart", run.ErrorMessage)
	require.NotNil(t, run.CompletedAt)
}

func TestDirScanStore_GetActiveRun_PrefersRunningOverQueued(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	runningID, err := store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-a")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, runningID, models.DirScanRunStatusScanning))

	queuedID, err := store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-b")
	require.NoError(t, err)

	activeRun, err := store.GetActiveRun(ctx, dir.ID)
	require.NoError(t, err)
	require.NotNil(t, activeRun)
	require.Equal(t, runningID, activeRun.ID)
	require.Equal(t, models.DirScanRunStatusScanning, activeRun.Status)

	queuedRun, err := store.GetQueuedRun(ctx, dir.ID)
	require.NoError(t, err)
	require.NotNil(t, queuedRun)
	require.Equal(t, queuedID, queuedRun.ID)
	require.Equal(t, models.DirScanRunStatusQueued, queuedRun.Status)
}

func TestDirScanStore_GetQueuedRun_PrefersNewestIDWhenStartedAtTies(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	firstID, err := store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-a")
	require.NoError(t, err)
	secondID, err := store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-b")
	require.NoError(t, err)

	tiedStartedAt := time.Date(2026, time.March, 16, 12, 0, 0, 0, time.UTC)
	_, err = db.ExecContext(ctx, `
		UPDATE dir_scan_runs
		SET started_at = ?
		WHERE id IN (?, ?)
	`, tiedStartedAt, firstID, secondID)
	require.NoError(t, err)

	activeRun, err := store.GetActiveRun(ctx, dir.ID)
	require.NoError(t, err)
	require.NotNil(t, activeRun)
	require.Equal(t, secondID, activeRun.ID)

	queuedRun, err := store.GetQueuedRun(ctx, dir.ID)
	require.NoError(t, err)
	require.NotNil(t, queuedRun)
	require.Equal(t, secondID, queuedRun.ID)
}
