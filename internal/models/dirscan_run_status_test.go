// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func setupDirScanTestDB(t *testing.T) *database.DB {
	t.Helper()

	return testdb.NewMigratedSQLite(t, "dirscan")
}

func TestDirScanStore_CreateRunIfNoActive_CreatesQueuedRun(t *testing.T) {
	ctx := t.Context()
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
	require.Positive(t, runID)

	run, err := store.GetRun(ctx, runID)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, models.DirScanRunStatusQueued, run.Status)

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

	_, err = store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-b")
	require.NoError(t, err)

	activeRun, err := store.GetActiveRun(ctx, dir.ID)
	require.NoError(t, err)
	require.NotNil(t, activeRun)
	require.Equal(t, runningID, activeRun.ID)
	require.Equal(t, models.DirScanRunStatusScanning, activeRun.Status)
}

func TestDirScanStore_GetActiveRun_PrefersNewestIDWhenStartedAtTies(t *testing.T) {
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

func createRunTestDirectory(t *testing.T) (*database.DB, *models.DirScanStore, *models.DirScanDirectory) {
	t.Helper()

	ctx := t.Context()
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

	return db, store, dir
}

func TestDirScanStore_CreateRunIfNoActive_IgnoresWebhookScans(t *testing.T) {
	ctx := t.Context()
	_, store, dir := createRunTestDirectory(t)

	webhookID, err := store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-a")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, webhookID, models.DirScanRunStatusSearching))

	fullID, err := store.CreateRunIfNoActive(ctx, dir.ID, "manual", "")
	require.NoError(t, err, "a webhook scan in progress does not block a full scan")
	require.NotEqual(t, webhookID, fullID)

	_, err = store.CreateRunIfNoActive(ctx, dir.ID, "scheduled", "")
	require.ErrorIs(t, err, models.ErrDirScanRunAlreadyActive, "a directory has at most one full scan")
}

func TestDirScanStore_CreateRun_TrimKeepsRunsInProgress(t *testing.T) {
	ctx := t.Context()
	_, store, dir := createRunTestDirectory(t)

	fullID, err := store.CreateRunIfNoActive(ctx, dir.ID, "manual", "")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, fullID, models.DirScanRunStatusSearching))

	for range 12 {
		runID, createErr := store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-a")
		require.NoError(t, createErr)
		require.NoError(t, store.UpdateRunCompleted(ctx, runID, 0, 0))
	}

	full, err := store.GetRun(ctx, fullID)
	require.NoError(t, err)
	require.NotNil(t, full, "the trim keeps the full scan that is in progress")

	runs, err := store.ListRuns(ctx, dir.ID, 100)
	require.NoError(t, err)
	require.Len(t, runs, 11, "the newest 10 runs plus the older run in progress")
}

func TestDirScanStore_ListActiveRuns(t *testing.T) {
	ctx := t.Context()
	_, store, dir := createRunTestDirectory(t)

	doneID, err := store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-a")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunCompleted(ctx, doneID, 0, 0))

	fullID, err := store.CreateRunIfNoActive(ctx, dir.ID, "scheduled", "")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, fullID, models.DirScanRunStatusSearching))
	webhookID, err := store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-b")
	require.NoError(t, err)

	runs, err := store.ListActiveRuns(ctx, dir.ID)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, fullID, runs[0].ID)
	require.Equal(t, webhookID, runs[1].ID)
}
