// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type stubInstanceGetter struct{}

func (stubInstanceGetter) Get(_ context.Context, id int) (*models.Instance, error) {
	return &models.Instance{ID: id, Name: "test", IsActive: true, HasLocalFilesystemAccess: true}, nil
}

// newScanTestService wires a service that scans one instance holding two
// torrents: one in an existing save path, one in a save path that does not
// exist on this host (issue #2483).
func newScanTestService(t *testing.T) (*Service, *models.OrphanScanStore, string, string) {
	t.Helper()

	base := t.TempDir()
	presentRoot := filepath.Join(base, "library")
	missingRoot := filepath.Join(base, "staging", "movies")
	require.NoError(t, os.MkdirAll(presentRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(presentRoot, "owned.mkv"), []byte("x"), 0o600))

	db := testdb.NewMigratedSQLite(t, "orphanscan-ignore-roots")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	require.Equal(t, 1, instance.ID)

	store := models.NewOrphanScanStore(db)

	svc := NewService(DefaultConfig(), nil, store, nil, nil, fsops.NewPool(stubInstanceGetter{}, local.NewBackend()))
	svc.getClientProvider = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-time.Minute)}, nil
	}
	svc.listInstancesProvider = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{{ID: 1, Name: "test", IsActive: true, HasLocalFilesystemAccess: true}}, nil
	}
	svc.getAllTorrentsProvider = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{
			{Hash: "present", SavePath: presentRoot, State: qbt.TorrentStatePausedUp},
			{Hash: "missing", SavePath: missingRoot, State: qbt.TorrentStatePausedUp},
		}, nil
	}
	svc.getTorrentFilesBatchProvider = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{
			"present": {{Name: "owned.mkv", Size: 1}},
			"missing": {{Name: "unreachable.mkv", Size: 1}},
		}, nil
	}

	return svc, store, presentRoot, missingRoot
}

func setIgnorePaths(t *testing.T, store *models.OrphanScanStore, ignorePaths []string) {
	t.Helper()

	defaults := DefaultSettings()
	_, err := store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID:          1,
		Enabled:             true,
		GracePeriodMinutes:  0,
		IgnorePaths:         ignorePaths,
		ScanIntervalHours:   defaults.ScanIntervalHours,
		PreviewSort:         defaults.PreviewSort,
		MaxFilesPerRun:      defaults.MaxFilesPerRun,
		AutoCleanupMaxFiles: defaults.AutoCleanupMaxFiles,
	})
	require.NoError(t, err)
}

func runScanForTest(t *testing.T, svc *Service, store *models.OrphanScanStore) *models.OrphanScanRun {
	t.Helper()

	ctx := t.Context()
	runID, err := store.CreateRunIfNoActive(ctx, 1, "manual")
	require.NoError(t, err)
	svc.executeScan(ctx, 1, runID)

	run, err := store.GetRun(ctx, runID)
	require.NoError(t, err)
	require.NotNil(t, run)
	return run
}

// An unreachable save path fails the whole run today. Ignoring that path must
// drop the scan root instead of walking it (issue #2483).
func TestExecuteScan_IgnorePathDropsUnreachableScanRoot(t *testing.T) {
	t.Parallel()

	svc, store, presentRoot, missingRoot := newScanTestService(t)

	failed := runScanForTest(t, svc, store)
	assert.Equal(t, "failed", failed.Status)
	assert.Contains(t, failed.ErrorMessage, missingRoot)

	setIgnorePaths(t, store, []string{missingRoot})
	run := runScanForTest(t, svc, store)
	assert.Equal(t, "completed", run.Status)
	assert.Empty(t, run.ErrorMessage)
	assert.Equal(t, []string{filepath.Clean(presentRoot)}, run.ScanPaths)
}

// Ignoring a parent of every save path leaves nothing to walk, and the run must
// say so instead of reporting that no torrent has an absolute save path.
func TestExecuteScan_IgnorePathsCoverEveryScanRoot(t *testing.T) {
	t.Parallel()

	svc, store, presentRoot, _ := newScanTestService(t)
	setIgnorePaths(t, store, []string{filepath.Dir(presentRoot)})

	run := runScanForTest(t, svc, store)
	assert.Equal(t, "failed", run.Status)
	assert.Equal(t, "no scan roots left: ignore paths cover every scan path", run.ErrorMessage)
}
