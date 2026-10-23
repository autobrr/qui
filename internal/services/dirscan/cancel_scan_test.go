// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

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

func setupDirScanServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	return testdb.NewMigratedSQLite(t, "dirscan-service")
}

func TestService_CancelScan_QueuedRunBumpsLastScanAt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := setupDirScanServiceTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	localFS := true
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, &localFS)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)
	require.Nil(t, dir.LastScanAt)

	runID, err := store.CreateRunIfNoActive(ctx, dir.ID, "scheduled", "")
	require.NoError(t, err)
	require.Positive(t, runID)

	svc := &Service{store: store}
	require.NoError(t, svc.CancelScan(ctx, dir.ID))

	run, err := store.GetRun(ctx, runID)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, models.DirScanRunStatusCanceled, run.Status)

	updatedDir, err := store.GetDirectory(ctx, dir.ID)
	require.NoError(t, err)
	require.NotNil(t, updatedDir.LastScanAt, "queued cancel should bump last_scan_at to avoid immediate re-queue")
	require.WithinDuration(t, time.Now(), *updatedDir.LastScanAt, 5*time.Second)
	require.False(t, svc.isDueForScan(updatedDir), "directory should not be immediately due after queued cancel")

	activeRun, err := store.GetActiveRun(ctx, dir.ID)
	require.NoError(t, err)
	require.Nil(t, activeRun)
}

func TestService_CancelScan_StopsEveryRunOfTheDirectory(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc, store, dir := newDirScanTestService(t)

	fullID, err := store.CreateRunIfNoActive(ctx, dir.ID, "manual", "")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, fullID, models.DirScanRunStatusSearching))
	webhookID, err := store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-a")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, webhookID, models.DirScanRunStatusSearching))
	queuedID, err := store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-b")
	require.NoError(t, err)

	fullCtx, cancelFull := context.WithCancel(ctx)
	defer cancelFull()
	webhookCtx, cancelWebhook := context.WithCancel(ctx)
	defer cancelWebhook()
	svc.cancelFuncs[fullID] = cancelFull
	svc.cancelFuncs[webhookID] = cancelWebhook

	require.NoError(t, svc.CancelScan(ctx, dir.ID))

	require.Error(t, fullCtx.Err(), "the full scan stops")
	require.Error(t, webhookCtx.Err(), "the webhook scan stops")
	for _, runID := range []int64{fullID, webhookID, queuedID} {
		run, getErr := store.GetRun(ctx, runID)
		require.NoError(t, getErr)
		require.Equal(t, models.DirScanRunStatusCanceled, run.Status)
	}
}

func TestService_CancelScan_RunningRunBumpsLastScanAt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc, store, dir := newDirScanTestService(t)
	require.True(t, svc.isDueForScan(dir))

	runID, err := store.CreateRunIfNoActive(ctx, dir.ID, "scheduled", "")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, runID, models.DirScanRunStatusSearching))

	require.NoError(t, svc.CancelScan(ctx, dir.ID))

	updatedDir, err := store.GetDirectory(ctx, dir.ID)
	require.NoError(t, err)
	require.False(t, svc.isDueForScan(updatedDir), "a full scan that outlasted the interval stays paused")
}

func TestService_CreateScheduledRun_SkipsDirectoryThatPauseMadeNotDue(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc, store, dir := newDirScanTestService(t)
	require.True(t, svc.isDueForScan(dir), "the scheduler listed the directory as due")

	// Pause runs after the scheduler listed the directory.
	require.NoError(t, store.UpdateDirectoryLastScan(ctx, dir.ID))

	runID, err := svc.createScheduledRun(ctx, dir.ID)
	require.NoError(t, err)
	require.Zero(t, runID)

	activeRun, err := store.GetActiveRun(ctx, dir.ID)
	require.NoError(t, err)
	require.Nil(t, activeRun, "no scan starts right after Pause")
}

func TestService_GetStatus_ListsEveryRunInProgress(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc, store, dir := newDirScanTestService(t)

	runs, err := svc.GetStatus(ctx, dir.ID)
	require.NoError(t, err)
	require.Empty(t, runs, "a directory without runs is idle")

	fullID, err := store.CreateRunIfNoActive(ctx, dir.ID, "scheduled", "")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, fullID, models.DirScanRunStatusSearching))
	webhookID, err := store.CreateRun(ctx, dir.ID, "webhook", "/data/media/show-a")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, webhookID, models.DirScanRunStatusSearching))
	svc.setRunProgress(webhookID, 2, 1)

	runs, err = svc.GetStatus(ctx, dir.ID)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, fullID, runs[0].ID)
	require.Equal(t, webhookID, runs[1].ID)
	require.Equal(t, 2, runs[1].MatchesFound, "a run in progress shows its live counts")
	require.Equal(t, 1, runs[1].TorrentsAdded)

	require.NoError(t, store.UpdateRunCompleted(ctx, fullID, 0, 0))
	require.NoError(t, store.UpdateRunCompleted(ctx, webhookID, 3, 1))

	runs, err = svc.GetStatus(ctx, dir.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1, "an idle directory shows its most recent run")
	require.Equal(t, webhookID, runs[0].ID)
}

func TestService_Start_PrunesLegacyRunHistory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupDirScanServiceTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	localFS := true
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, &localFS)
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
		_, execErr := db.ExecContext(ctx, `
			INSERT INTO dir_scan_runs (directory_id, status, triggered_by, started_at, completed_at)
			VALUES (?, ?, ?, datetime('now', printf('-%d minutes', ?)), datetime('now', printf('-%d minutes', ?)))
		`, dir.ID, models.DirScanRunStatusSuccess, fmt.Sprintf("legacy-%d", i), 12-i, 12-i)
		require.NoError(t, execErr)
	}

	svc := NewService(DefaultConfig(), store, nil, instanceStore, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, svc.Start(ctx))
	svc.Stop()

	runs, err := store.ListRuns(ctx, dir.ID, 100)
	require.NoError(t, err)
	require.Len(t, runs, 10)
	require.Equal(t, "legacy-11", runs[0].TriggeredBy)
	require.Equal(t, "legacy-2", runs[len(runs)-1].TriggeredBy)
}
