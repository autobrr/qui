// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"fmt"
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

func TestDirScanStore_CreateRun_TrimsOldRunsPerDirectory(t *testing.T) {
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

	for i := range 12 {
		runID, createErr := store.CreateRun(ctx, dir.ID, fmt.Sprintf("manual-%d", i), "")
		require.NoError(t, createErr)
		require.NoError(t, store.UpdateRunCompleted(ctx, runID, i, i))
	}

	runs, err := store.ListRuns(ctx, dir.ID, 100)
	require.NoError(t, err)
	require.Len(t, runs, 10)

	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dir_scan_runs WHERE directory_id = ?`, dir.ID).Scan(&count))
	require.Equal(t, 10, count)

	oldestRetained := runs[len(runs)-1]
	require.Equal(t, "manual-2", oldestRetained.TriggeredBy)
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

func TestDirScanStore_PruneRunHistory_TrimsLegacyRows(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	dirA, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media/a",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)
	dirB, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media/b",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	for i := range 12 {
		_, execErr := db.ExecContext(ctx, `
			INSERT INTO dir_scan_runs (directory_id, status, triggered_by, started_at, completed_at)
			VALUES (?, ?, ?, datetime('now', printf('-%d minutes', ?)), datetime('now', printf('-%d minutes', ?)))
		`, dirA.ID, models.DirScanRunStatusSuccess, fmt.Sprintf("legacy-a-%d", i), 12-i, 12-i)
		require.NoError(t, execErr)
	}
	for i := range 4 {
		_, execErr := db.ExecContext(ctx, `
			INSERT INTO dir_scan_runs (directory_id, status, triggered_by, started_at, completed_at)
			VALUES (?, ?, ?, datetime('now', printf('-%d minutes', ?)), datetime('now', printf('-%d minutes', ?)))
		`, dirB.ID, models.DirScanRunStatusSuccess, fmt.Sprintf("legacy-b-%d", i), 4-i, 4-i)
		require.NoError(t, execErr)
	}

	require.NoError(t, store.PruneRunHistory(ctx))

	runsA, err := store.ListRuns(ctx, dirA.ID, 100)
	require.NoError(t, err)
	require.Len(t, runsA, 10)
	require.Equal(t, "legacy-a-11", runsA[0].TriggeredBy)
	require.Equal(t, "legacy-a-2", runsA[len(runsA)-1].TriggeredBy)

	runsB, err := store.ListRuns(ctx, dirB.ID, 100)
	require.NoError(t, err)
	require.Len(t, runsB, 4)
}
