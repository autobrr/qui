// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestMergeWebhookScanRoots(t *testing.T) {
	dir := "/data/media/tv"

	d := fsops.SlashPaths
	require.Equal(t, dir, mergeWebhookScanRoots(d, dir, "", dir))
	require.Equal(t, path.Join(dir, "Show"), mergeWebhookScanRoots(
		d,
		dir,
		path.Join(dir, "Show", "Season 01"),
		path.Join(dir, "Show", "Season 02"),
	))
	require.Equal(t, dir, mergeWebhookScanRoots(
		d,
		dir,
		path.Join(dir, "Show A", "Season 01"),
		path.Join(dir, "Show B", "Season 01"),
	))
}

// regression: #2919
func TestMergeWebhookScanRootsKeepsABackslashInASlashName(t *testing.T) {
	t.Parallel()

	const dir, root = "/data/lib", `/data/lib/x\..\..\..\etc`
	_, target, ok := PathWithinDirectory(fsops.SlashPaths, dir, root)
	require.True(t, ok)
	require.Equal(t, root, mergeWebhookScanRoots(fsops.SlashPaths, dir, target, target))
	require.Equal(t, dir, mergeWebhookScanRoots(fsops.SlashPaths, dir, target, "/data/lib/other"))
}

// regression: #2919
func TestMergeWebhookScanRootsStaysInsideAnEditedDirectory(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, dir, existing, next string }{
		{name: "queued root beside the directory", dir: "/new", existing: "/new2/A", next: "/new2/B"},
		{name: "queued root elsewhere", dir: "/new", existing: "/old/A", next: "/new/B"},
		{name: "relative queued root", dir: "/new", existing: "old/A", next: "/new/B"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.dir, mergeWebhookScanRoots(fsops.SlashPaths, tt.dir, tt.existing, tt.next))
		})
	}
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
	service := NewService(DefaultConfig(), store, nil, nil, nil, nil, nil, nil, nil, nil)

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

// On Windows only the slash grammar keeps these roots as the remote host writes them. Linux reads both the same.
func TestStartWebhookScan_MergesInTheDirectorysOwnGrammar(t *testing.T) {
	ctx := t.Context()
	service, store, created := newDirScanTestService(t)
	host := newRecordingBackend()
	service.backendPool = fsops.NewPoolWithRemote(&fakeInstanceStore{instance: remoteInstance(models.SSHTierExec)}, host, func(*models.Instance) fsops.Backend { return host })
	dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{Path: "/data/lib", Enabled: true, TargetInstanceID: created.TargetInstanceID, ScanIntervalMinutes: 60})
	require.NoError(t, err)

	activeRunID, err := store.CreateRun(ctx, dir.ID, triggeredByWebhook, "/data/lib/Show/Season 01")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunStatus(ctx, activeRunID, models.DirScanRunStatusScanning))
	dirMu := service.getDirectoryMutex(dir.ID, true)
	dirMu.Lock()

	queuedID, err := service.StartWebhookScan(ctx, dir.ID, "/data/lib/Show/Season 01")
	require.NoError(t, err)
	_, err = service.StartWebhookScan(ctx, dir.ID, "/data/lib/Show/Season 02")
	require.NoError(t, err)

	queued, err := store.GetRun(ctx, queuedID)
	require.NoError(t, err)
	require.Equal(t, "/data/lib/Show", queued.ScanRoot)

	require.NoError(t, service.CancelScan(ctx, dir.ID))
	dirMu.Unlock()
	require.Eventually(t, func() bool {
		run, getErr := store.GetRun(ctx, queuedID)
		require.NoError(t, getErr)
		return run != nil && run.Status == models.DirScanRunStatusCanceled
	}, 5*time.Second, 50*time.Millisecond)
}

func TestWebhookScanRootWithoutABackendPool(t *testing.T) {
	t.Parallel()

	const path = "/data/lib/Show/ep01.mkv"
	require.Equal(t, path, (&Service{}).WebhookScanRoot(t.Context(), &models.DirScanDirectory{TargetInstanceID: 1}, path))
}

// waitForRunFailed waits until the run fails and returns it. The test service
// has no filesystem backend, so each run that starts fails at validation.
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

func TestPathWithinDirectory(t *testing.T) {
	t.Parallel()

	root := string(filepath.Separator)
	tests := []struct {
		name        string
		d           fsops.PathDialect
		dir, target string
		wantDir     string
		wantTarget  string
		want        bool
	}{
		{name: "same path matches", d: fsops.SlashPaths, dir: "/data/media/tv", target: "/data/media/tv", wantDir: "/data/media/tv", wantTarget: "/data/media/tv", want: true},
		{name: "child path matches", d: fsops.SlashPaths, dir: "/data/media/tv", target: "/data/media/tv/Show Name", wantDir: "/data/media/tv", wantTarget: "/data/media/tv/Show Name", want: true},
		{name: "sibling path does not match", d: fsops.SlashPaths, dir: "/data/media/tv", target: "/data/media/tv-shows", wantDir: "/data/media/tv", wantTarget: "/data/media/tv-shows"},
		{name: "filesystem root matches descendants", d: fsops.SlashPaths, dir: "/", target: "/data/media", wantDir: "/", wantTarget: "/data/media", want: true},
		{name: "a dot dot climbs out", d: fsops.SlashPaths, dir: "/data/media/tv", target: "/data/media/tv/../movies", wantDir: "/data/media/tv", wantTarget: "/data/media/movies"},
		{name: "unclean paths are cleaned", d: fsops.SlashPaths, dir: "/data//media/tv/", target: "/data/media/tv/./Show/", wantDir: "/data/media/tv", wantTarget: "/data/media/tv/Show", want: true},
		{name: "a backslash stays part of a slash name", d: fsops.SlashPaths, dir: "/data", target: `/data/a\b`, wantDir: "/data", wantTarget: `/data/a\b`, want: true},
		{name: "a name starting with two dots is within", d: fsops.SlashPaths, dir: "/data", target: "/data/..hidden", wantDir: "/data", wantTarget: "/data/..hidden", want: true},
		{name: "the parent is not within", d: fsops.SlashPaths, dir: "/data/media", target: "/data", wantDir: "/data/media", wantTarget: "/data"},
		{name: "a relative target is not within", d: fsops.SlashPaths, dir: "/data", target: "data/x", wantDir: "/data", wantTarget: "data/x"},
		{name: "host grammar for any other directory", d: fsops.HostPaths, dir: filepath.Join("relative", "tv"), target: filepath.Join("relative", "tv", "Show"), wantDir: filepath.Join("relative", "tv"), wantTarget: filepath.Join("relative", "tv", "Show"), want: true},
		{name: "host grammar sibling", d: fsops.HostPaths, dir: filepath.Join("relative", "tv"), target: filepath.Join("relative", "tv-shows"), wantDir: filepath.Join("relative", "tv"), wantTarget: filepath.Join("relative", "tv-shows")},
		{name: "host root", d: fsops.HostPaths, dir: root, target: filepath.Join(root, "data"), wantDir: filepath.Clean(root), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir, target, ok := PathWithinDirectory(tt.d, tt.dir, tt.target)
			require.Equal(t, tt.want, ok)
			require.Equal(t, tt.wantDir, dir)
			if tt.wantTarget != "" {
				require.Equal(t, tt.wantTarget, target)
			}
		})
	}
}
