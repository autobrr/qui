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
	"github.com/autobrr/qui/internal/services/notifications"
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
	svc.getAppPreferencesProvider = func(context.Context, int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: base}, nil
	}
	svc.subcategoriesEnabledProvider = func(context.Context, int) (bool, error) { return false, nil }
	svc.getCategoriesProvider = func(context.Context, int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{}, nil
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

// Ignoring an unavailable path excludes it from scan coverage (issue #2483).
func TestExecuteScan_IgnorePathDropsUnreachableScanRoot(t *testing.T) {
	t.Parallel()

	svc, store, presentRoot, missingRoot := newScanTestService(t)

	partial := runScanForTest(t, svc, store)
	assert.Equal(t, "completed", partial.Status)
	assert.True(t, partial.Partial)
	assert.Contains(t, partial.ErrorMessage, missingRoot)
	assert.Contains(t, partial.ScanPaths, filepath.Clean(missingRoot))

	setIgnorePaths(t, store, []string{missingRoot})
	run := runScanForTest(t, svc, store)
	assert.Equal(t, "completed", run.Status)
	assert.Empty(t, run.ErrorMessage)
	assert.False(t, run.Partial)
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

type scanNotifier struct {
	events chan notifications.Event
}

func (n scanNotifier) Notify(_ context.Context, event notifications.Event) {
	n.events <- event
}

func TestExecuteScan_PathDisappearsDuringScan(t *testing.T) {
	for _, withOrphan := range []bool{false, true} {
		name := "no orphans"
		if withOrphan {
			name = "manual cleanup of partial results"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc, store, presentRoot, missingRoot := newScanTestService(t)
			setIgnorePaths(t, store, nil)
			settings, err := store.GetSettings(t.Context(), 1)
			require.NoError(t, err)
			settings.AutoCleanupEnabled = true
			_, err = store.UpsertSettings(t.Context(), settings)
			require.NoError(t, err)

			orphanPath := filepath.Join(presentRoot, "orphan.mkv")
			if withOrphan {
				require.NoError(t, os.WriteFile(orphanPath, []byte("orphan"), 0o600))
			}
			require.NoError(t, os.MkdirAll(missingRoot, 0o755))
			filesProvider := svc.getTorrentFilesBatchProvider
			svc.getTorrentFilesBatchProvider = func(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
				// The torrent snapshot still contains this path when its directory disappears.
				if err := os.RemoveAll(missingRoot); err != nil {
					return nil, err
				}
				return filesProvider(ctx, instanceID, hashes)
			}
			events := make(chan notifications.Event, 2)
			svc.notifier = scanNotifier{events: events}
			runID, err := store.CreateRunIfNoActive(t.Context(), 1, "scheduled")
			require.NoError(t, err)
			svc.executeScan(t.Context(), 1, runID)

			run, err := store.GetRun(t.Context(), runID)
			require.NoError(t, err)
			require.True(t, run.Partial)
			require.Contains(t, run.ErrorMessage, missingRoot)
			require.Contains(t, run.ErrorMessage, "Automatic cleanup is disabled")
			select {
			case event := <-events:
				require.Equal(t, notifications.EventOrphanScanCompleted, event.Type)
				require.True(t, event.OrphanScanPartial)
				require.Equal(t, run.ErrorMessage, event.ErrorMessage)
				require.Equal(t, run.FilesFound, event.OrphanScanFilesFound)
			default:
				t.Fatal("partial scan did not send a completion notification")
			}

			if !withOrphan {
				require.Equal(t, "completed", run.Status)
				require.Zero(t, run.FilesFound)
				return
			}
			require.Equal(t, "preview_ready", run.Status)
			require.Equal(t, 1, run.FilesFound)
			require.FileExists(t, orphanPath)
			// Automatic cleanup would hold the deletion lock or finish the run,
			// so a successful manual confirmation also checks that it did not start.
			require.NoError(t, svc.ConfirmDeletion(t.Context(), 1, runID))
			select {
			case event := <-events:
				require.Equal(t, notifications.EventOrphanScanCompleted, event.Type)
				require.Equal(t, 1, event.OrphanScanFilesDeleted)
				require.True(t, event.OrphanScanPartial)
			case <-time.After(5 * time.Second):
				t.Fatal("manual deletion did not complete")
			}
			require.NoFileExists(t, orphanPath)
			require.FileExists(t, filepath.Join(presentRoot, "owned.mkv"))
			runs, err := store.ListRuns(t.Context(), 1, 10)
			require.NoError(t, err)
			require.Len(t, runs, 1)
			require.Equal(t, "completed", runs[0].Status)
			require.True(t, runs[0].Partial)
			require.Contains(t, runs[0].ErrorMessage, missingRoot)
		})
	}
}

func TestExecuteScan_AllPathsUnavailableFails(t *testing.T) {
	t.Parallel()
	svc, store, presentRoot, missingRoot := newScanTestService(t)
	require.NoError(t, os.RemoveAll(presentRoot))
	run := runScanForTest(t, svc, store)
	require.Equal(t, "failed", run.Status)
	require.False(t, run.Partial)
	require.Contains(t, run.ErrorMessage, "No scan paths completed")
	require.Contains(t, run.ErrorMessage, presentRoot)
	require.Contains(t, run.ErrorMessage, missingRoot)
}

func TestExecuteScan_PrunedRootsWithFailedWalk(t *testing.T) {
	t.Parallel()
	svc, store, presentRoot, _ := newScanTestService(t)
	nestedRoot := filepath.Join(presentRoot, "nested")
	require.NoError(t, os.MkdirAll(nestedRoot, 0o755))
	torrentsProvider := svc.getAllTorrentsProvider
	svc.getAllTorrentsProvider = func(ctx context.Context, instanceID int) ([]qbt.Torrent, error) {
		torrents, err := torrentsProvider(ctx, instanceID)
		torrents[1].SavePath = nestedRoot
		return torrents, err
	}
	backend := &fakeWalkBackend{
		Backend: newTestBackend(),
		entries: []fsops.WalkEntry{{Path: presentRoot, Err: os.ErrPermission}},
	}
	svc.backendPool = fsops.NewPool(stubInstanceGetter{}, backend)
	require.Equal(t, []string{presentRoot}, pruneNestedScanRoots(t.Context(), []string{presentRoot, nestedRoot}, backend))

	run := runScanForTest(t, svc, store)
	require.Len(t, run.ScanPaths, 2)
	require.Equal(t, "failed", run.Status)
	require.False(t, run.Partial)
	require.Contains(t, run.ErrorMessage, presentRoot)
}

func TestExecuteScan_AbsentDeclaredRoot(t *testing.T) {
	for _, skippedTorrent := range []bool{false, true} {
		name := "unused default path is clean"
		if skippedTorrent {
			name = "skipped torrent path fails"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc, store, _, missingRoot := newScanTestService(t)
			declaredRoot := filepath.Join(t.TempDir(), "unused")
			svc.getAppPreferencesProvider = func(context.Context, int) (qbt.AppPreferences, error) {
				return qbt.AppPreferences{SavePath: declaredRoot}, nil
			}
			svc.getAllTorrentsProvider = func(context.Context, int) ([]qbt.Torrent, error) {
				if skippedTorrent {
					return []qbt.Torrent{{Hash: "missing", SavePath: missingRoot, State: qbt.TorrentStateCheckingResumeData}}, nil
				}
				return nil, nil
			}
			svc.getTorrentFilesBatchProvider = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
				return nil, nil
			}
			setIgnorePaths(t, store, nil)
			settings, err := store.GetSettings(t.Context(), 1)
			require.NoError(t, err)
			settings.ScanDefaultSavePath = true
			_, err = store.UpsertSettings(t.Context(), settings)
			require.NoError(t, err)

			run := runScanForTest(t, svc, store)
			require.False(t, run.Partial)
			if skippedTorrent {
				require.Equal(t, "failed", run.Status)
				require.Contains(t, run.ErrorMessage, missingRoot)
			} else {
				require.Equal(t, "completed", run.Status)
				require.Empty(t, run.ErrorMessage)
			}
		})
	}
}

func TestExecuteScan_IgnoredTransitionalPathDoesNotMakeScanPartial(t *testing.T) {
	t.Parallel()
	svc, store, _, missingRoot := newScanTestService(t)
	setIgnorePaths(t, store, []string{missingRoot})
	torrentsProvider := svc.getAllTorrentsProvider
	svc.getAllTorrentsProvider = func(ctx context.Context, instanceID int) ([]qbt.Torrent, error) {
		torrents, err := torrentsProvider(ctx, instanceID)
		for i := range torrents {
			if torrents[i].Hash == "missing" {
				torrents[i].State = qbt.TorrentStateCheckingResumeData
			}
		}
		return torrents, err
	}
	filesProvider := svc.getTorrentFilesBatchProvider
	svc.getTorrentFilesBatchProvider = func(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
		files, err := filesProvider(ctx, instanceID, hashes)
		delete(files, "missing")
		return files, err
	}
	run := runScanForTest(t, svc, store)
	require.Equal(t, "completed", run.Status)
	require.False(t, run.Partial)
	require.Empty(t, run.ErrorMessage)
}

// A scan root that is not on disk must be reported without failing the run.
// Scanning category paths turns every unused category into a root, and
// qBittorrent creates a category directory only on the first torrent.
func TestExecuteScan_MissingScanRootDoesNotFailRun(t *testing.T) {
	t.Parallel()

	svc, store, presentRoot, missingRoot := newScanTestService(t)

	run := runScanForTest(t, svc, store)

	assert.Equal(t, "completed", run.Status)
	assert.Contains(t, run.ErrorMessage, missingRoot)
	assert.Equal(t, 0, run.FilesFound)
	assert.Contains(t, run.ScanPaths, filepath.Clean(presentRoot))
}

// A category destination qBittorrent has not created yet is absent by design,
// so it must not raise a partial-scan warning. A save path a torrent points at
// is a different story and still gets reported.
func TestExecuteScan_AbsentCategoryPathStaysQuiet(t *testing.T) {
	t.Parallel()

	svc, store, _, missingRoot := newScanTestService(t)
	unusedCategory := filepath.Join(t.TempDir(), "unused")

	svc.getAppPreferencesProvider = func(context.Context, int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: filepath.Dir(missingRoot)}, nil
	}
	svc.subcategoriesEnabledProvider = func(context.Context, int) (bool, error) { return false, nil }
	svc.getCategoriesProvider = func(context.Context, int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{"unused": {Name: "unused", SavePath: unusedCategory}}, nil
	}

	defaults := DefaultSettings()
	_, err := store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID: 1, Enabled: true, GracePeriodMinutes: 0, IgnorePaths: []string{},
		ScanIntervalHours: defaults.ScanIntervalHours, PreviewSort: defaults.PreviewSort,
		MaxFilesPerRun: defaults.MaxFilesPerRun, AutoCleanupMaxFiles: defaults.AutoCleanupMaxFiles,
		ScanCategoryPaths: true,
	})
	require.NoError(t, err)

	run := runScanForTest(t, svc, store)

	assert.Equal(t, "completed", run.Status)
	assert.Contains(t, run.ScanPaths, filepath.Clean(unusedCategory))
	assert.NotContains(t, run.ErrorMessage, unusedCategory)
	assert.Contains(t, run.ErrorMessage, missingRoot)
}

// A category destination a torrent actually saves into is not expected to be
// absent: it went missing after the fact, which is the unmounted-volume case
// discussion #2483 is about, so the warning must survive.
func TestExecuteScan_AbsentCategoryPathWithTorrentStillWarns(t *testing.T) {
	t.Parallel()

	svc, store, _, missingRoot := newScanTestService(t)

	svc.getAppPreferencesProvider = func(context.Context, int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: filepath.Dir(missingRoot)}, nil
	}
	svc.subcategoriesEnabledProvider = func(context.Context, int) (bool, error) { return false, nil }
	svc.getCategoriesProvider = func(context.Context, int) (map[string]qbt.Category, error) {
		// The same path the "missing" torrent saves into.
		return map[string]qbt.Category{"movies": {Name: "movies", SavePath: missingRoot}}, nil
	}

	defaults := DefaultSettings()
	_, err := store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID: 1, Enabled: true, GracePeriodMinutes: 0, IgnorePaths: []string{},
		ScanIntervalHours: defaults.ScanIntervalHours, PreviewSort: defaults.PreviewSort,
		MaxFilesPerRun: defaults.MaxFilesPerRun, AutoCleanupMaxFiles: defaults.AutoCleanupMaxFiles,
		ScanCategoryPaths: true,
	})
	require.NoError(t, err)

	run := runScanForTest(t, svc, store)

	assert.Equal(t, "completed", run.Status)
	assert.Contains(t, run.ErrorMessage, missingRoot)
}
