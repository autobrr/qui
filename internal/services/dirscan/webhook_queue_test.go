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

func TestStartWebhookScan_QueuesAndMergesFollowUpRuns(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "webhook-queue")

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	instance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	service := NewService(DefaultConfig(), store, nil, instanceStore, nil, nil, nil, nil, nil, nil)

	dirPath := t.TempDir()
	created, err := service.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                dirPath,
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	activeRunID, err := store.CreateRun(ctx, created.ID, "webhook", filepath.Join(dirPath, "Show", "Season 01"))
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, activeRunID, models.DirScanRunStatusScanning))

	dirMu := service.getDirectoryMutex(created.ID)
	dirMu.Lock()

	firstQueuedID, err := service.StartWebhookScan(ctx, created.ID, filepath.Join(dirPath, "Show", "Season 01"))
	require.NoError(t, err)
	require.NotZero(t, firstQueuedID)
	require.NotEqual(t, activeRunID, firstQueuedID)

	secondQueuedID, err := service.StartWebhookScan(ctx, created.ID, filepath.Join(dirPath, "Show", "Season 02"))
	require.NoError(t, err)
	require.Equal(t, firstQueuedID, secondQueuedID)

	queuedRun, err := store.GetQueuedRun(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, queuedRun)
	require.Equal(t, filepath.Join(dirPath, "Show"), queuedRun.ScanRoot)

	require.NoError(t, service.CancelScan(ctx, created.ID))
	dirMu.Unlock()

	require.Eventually(t, func() bool {
		run, getErr := store.GetRun(ctx, queuedRun.ID)
		require.NoError(t, getErr)
		return run != nil && run.Status == models.DirScanRunStatusCanceled
	}, 5*time.Second, 50*time.Millisecond)
}
