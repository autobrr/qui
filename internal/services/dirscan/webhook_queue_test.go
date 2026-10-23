// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestMergeWebhookScanRoots(t *testing.T) {
	dir := "/data/media/tv"

	require.Equal(t, dir, mergeWebhookScanRoots(dir, "", dir))
	require.Equal(t, path.Join(dir, "Show"), mergeWebhookScanRoots(
		dir,
		path.Join(dir, "Show", "Season 01"),
		path.Join(dir, "Show", "Season 02"),
	))
	require.Equal(t, dir, mergeWebhookScanRoots(
		dir,
		path.Join(dir, "Show A", "Season 01"),
		path.Join(dir, "Show B", "Season 01"),
	))
}

func newDirScanTestService(t *testing.T) (*Service, *models.DirScanStore, *models.DirScanDirectory) {
	t.Helper()
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "webhook-queue")

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	instance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	service := NewService(DefaultConfig(), store, nil, instanceStore, nil, nil, nil, nil, nil, nil, nil)

	dirPath := t.TempDir()
	created, err := service.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                dirPath,
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	return service, store, created
}

func TestStartWebhookScan_QueuesAndMergesFollowUpRuns(t *testing.T) {
	ctx := t.Context()
	service, store, created := newDirScanTestService(t)
	dirPath := created.Path

	activeRunID, err := store.CreateRun(ctx, created.ID, "webhook", filepath.Join(dirPath, "Show", "Season 01"))
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, activeRunID, models.DirScanRunStatusScanning))

	dirMu := service.getDirectoryMutex(created.ID, true)
	dirMu.Lock()

	firstQueuedID, err := service.StartWebhookScan(ctx, created.ID, filepath.Join(dirPath, "Show", "Season 01"))
	require.NoError(t, err)
	require.NotZero(t, firstQueuedID)
	require.NotEqual(t, activeRunID, firstQueuedID)

	secondQueuedID, err := service.StartWebhookScan(ctx, created.ID, filepath.Join(dirPath, "Show", "Season 02"))
	require.NoError(t, err)
	require.Equal(t, firstQueuedID, secondQueuedID)

	queuedRun, err := store.GetRun(ctx, firstQueuedID)
	require.NoError(t, err)
	require.NotNil(t, queuedRun)
	require.Equal(t, models.DirScanRunStatusQueued, queuedRun.Status)
	require.Equal(t, filepath.Join(dirPath, "Show"), queuedRun.ScanRoot)

	require.NoError(t, service.CancelScan(ctx, created.ID))
	dirMu.Unlock()

	require.Eventually(t, func() bool {
		run, getErr := store.GetRun(ctx, queuedRun.ID)
		require.NoError(t, getErr)
		return run != nil && run.Status == models.DirScanRunStatusCanceled
	}, 5*time.Second, 50*time.Millisecond)
}

// waitForRunFailed waits until the run fails and returns it. The test service
// has no filesystem backend, so each run that starts fails at the walk.
func waitForRunFailed(t *testing.T, store *models.DirScanStore, runID int64) *models.DirScanRun {
	t.Helper()
	var run *models.DirScanRun
	require.Eventually(t, func() bool {
		var err error
		run, err = store.GetRun(t.Context(), runID)
		require.NoError(t, err)
		return run != nil && run.Status == models.DirScanRunStatusFailed
	}, 5*time.Second, 20*time.Millisecond)
	return run
}

func TestStartWebhookScan_RunsWhileFullScanHoldsRunSlotAndLock(t *testing.T) {
	ctx := t.Context()
	service, store, created := newDirScanTestService(t)

	fullID, err := store.CreateRunIfNoActive(ctx, created.ID, "scheduled", "")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, fullID, models.DirScanRunStatusSearching))
	service.runSem <- struct{}{}
	fullMu := service.getDirectoryMutex(created.ID, false)
	fullMu.Lock()
	defer fullMu.Unlock()

	webhookID, err := service.StartWebhookScan(ctx, created.ID, filepath.Join(created.Path, "Show"))
	require.NoError(t, err)

	run := waitForRunFailed(t, store, webhookID)
	require.Equal(t, "backend pool not configured", run.ErrorMessage)
}

func TestStartWebhookScan_DoesNotMergeIntoQueuedFullScan(t *testing.T) {
	ctx := t.Context()
	service, store, created := newDirScanTestService(t)

	fullID, err := store.CreateRunIfNoActive(ctx, created.ID, "manual", "")
	require.NoError(t, err)

	webhookID, err := service.StartWebhookScan(ctx, created.ID, filepath.Join(created.Path, "Show"))
	require.NoError(t, err)
	require.NotEqual(t, fullID, webhookID)

	full, err := store.GetRun(ctx, fullID)
	require.NoError(t, err)
	require.Equal(t, models.DirScanRunStatusQueued, full.Status, "the full scan stays in the queue")
	require.Empty(t, full.ScanRoot)

	webhook := waitForRunFailed(t, store, webhookID)
	require.Equal(t, filepath.Join(created.Path, "Show"), webhook.ScanRoot)
}
