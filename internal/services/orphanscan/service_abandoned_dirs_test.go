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
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// TestExecuteScan_AbandonedDirsSurviveTheRoundTrip covers the whole path a
// directory takes through a run: found by the walk, stored, and read back still
// marked as a directory. An entry that loses that flag is deleted as a file and
// never reaches the directory pass.
func TestExecuteScan_AbandonedDirsSurviveTheRoundTrip(t *testing.T) {
	base := t.TempDir()
	defaultSavePath := filepath.Join(base, "torrents")
	torrentSavePath := filepath.Join(defaultSavePath, "mydata")
	categoryPath := filepath.Join(defaultSavePath, "movies")
	// A category that inherits the default save path reports an empty savePath;
	// its destination is still defaultSavePath/<name> and must be protected.
	inheritedCategoryPath := filepath.Join(defaultSavePath, "test")
	abandoned := filepath.Join(defaultSavePath, "leftover", "season")

	require.NoError(t, os.MkdirAll(torrentSavePath, 0o750))
	require.NoError(t, os.MkdirAll(categoryPath, 0o750))
	require.NoError(t, os.MkdirAll(inheritedCategoryPath, 0o750))
	require.NoError(t, os.MkdirAll(abandoned, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(torrentSavePath, "owned.mkv"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(defaultSavePath, "stray.txt"), []byte("junk"), 0o600))

	db := testdb.NewMigratedSQLite(t, "orphanscan-abandoned-dirs")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	_, err = instanceStore.Create(t.Context(), "test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewOrphanScanStore(db)
	svc := NewService(DefaultConfig(), nil, store, nil, nil, fsops.NewPool(stubInstanceGetter{}, local.NewBackend()))
	svc.getClientProvider = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-time.Minute)}, nil
	}
	svc.listInstancesProvider = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{{ID: 1, Name: "test", IsActive: true, HasLocalFilesystemAccess: true}}, nil
	}
	svc.getAllTorrentsProvider = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "owned", SavePath: torrentSavePath, State: qbt.TorrentStatePausedUp}}, nil
	}
	svc.getTorrentFilesBatchProvider = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv", Size: 1}}}, nil
	}
	svc.getAppPreferencesProvider = func(_ context.Context, _ int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: defaultSavePath}, nil
	}
	svc.subcategoriesEnabledProvider = func(_ context.Context, _ int) (bool, error) { return false, nil }
	svc.getCategoriesProvider = func(_ context.Context, _ int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{
			"movies": {Name: "movies", SavePath: categoryPath},
			"test":   {Name: "test", SavePath: ""},
		}, nil
	}

	_, err = store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID:          1,
		GracePeriodMinutes:  0,
		IgnorePaths:         []string{},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupMaxFiles: 100,
		ScanDefaultSavePath: true,
		DeleteAbandonedDirs: true,
	})
	require.NoError(t, err)

	runID, err := store.CreateRunIfNoActive(t.Context(), 1, "manual")
	require.NoError(t, err)
	svc.executeScan(context.Background(), 1, runID)

	run, err := store.GetRun(t.Context(), runID)
	require.NoError(t, err)
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	files, err := store.GetFilesForDeletion(t.Context(), runID)
	require.NoError(t, err)

	found := make(map[string]bool, len(files))
	for _, f := range files {
		found[f.FilePath] = f.IsAbandonedDir
	}

	isAbandonedDir, ok := found[abandoned]
	require.True(t, ok, "abandoned directory not reported: %v", found)
	require.True(t, isAbandonedDir, "abandoned directory lost its is_abandoned_dir flag on the round trip")

	isAbandonedDir, ok = found[filepath.Join(defaultSavePath, "stray.txt")]
	require.True(t, ok, "orphan file in the default save path not reported: %v", found)
	require.False(t, isAbandonedDir, "an orphan file must not be marked as an abandoned directory")

	require.NotContains(t, found, categoryPath, "empty category destination must never be reported")
	require.NotContains(t, found, inheritedCategoryPath, "a category inheriting the default save path must be protected too")
	require.NotContains(t, found, torrentSavePath, "a torrent save path is a scan root and must never be reported")
	require.NotContains(t, found, defaultSavePath, "the default save path is a scan root and must never be reported")
}
