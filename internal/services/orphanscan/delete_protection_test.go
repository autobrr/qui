// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// settingsFailingQuerier fails exactly the orphan_scan_settings read and passes
// every other statement to the real database, so the run reaches the settings
// load the way it would in production.
type settingsFailingQuerier struct {
	dbinterface.Querier
}

func (q settingsFailingQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, "FROM orphan_scan_settings") {
		return q.Querier.QueryRowContext(ctx, "SELECT no_such_column FROM orphan_scan_settings")
	}
	return q.Querier.QueryRowContext(ctx, query, args...)
}

// deletionFixture builds a run that has already reached preview_ready with one
// orphan file and one abandoned directory pending.
type deletionFixture struct {
	svc              *Service
	store            *models.OrphanScanStore
	db               dbinterface.Querier
	runID            int64
	defaultSavePath  string
	abandoned        string
	strayFile        string
	categoryFolder   string
	orphanInCategory string
}

func newDeletionFixture(t *testing.T, dbName string) *deletionFixture {
	t.Helper()

	base := t.TempDir()
	defaultSavePath := filepath.Join(base, "torrents")
	torrentSavePath := filepath.Join(defaultSavePath, "mydata")
	abandoned := filepath.Join(defaultSavePath, "leftover")
	strayFile := filepath.Join(defaultSavePath, "stray.txt")
	// A folder that holds an orphan is not file-free, so it never reaches the
	// abandoned-directory pass; it is emptied by the file deletion and only the
	// follow-up cleanup can remove it.
	categoryFolder := filepath.Join(defaultSavePath, "movies")
	orphanInCategory := filepath.Join(categoryFolder, "junk.txt")

	require.NoError(t, os.MkdirAll(torrentSavePath, 0o750))
	require.NoError(t, os.MkdirAll(abandoned, 0o750))
	require.NoError(t, os.MkdirAll(categoryFolder, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(torrentSavePath, "owned.mkv"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(strayFile, []byte("junk"), 0o600))
	require.NoError(t, os.WriteFile(orphanInCategory, []byte("junk"), 0o600))

	db := testdb.NewMigratedSQLite(t, dbName)
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
	svc.getCategoriesProvider = func(_ context.Context, _ int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{}, nil
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

	return &deletionFixture{
		svc:              svc,
		store:            store,
		db:               db,
		runID:            runID,
		defaultSavePath:  defaultSavePath,
		abandoned:        abandoned,
		strayFile:        strayFile,
		categoryFolder:   categoryFolder,
		orphanInCategory: orphanInCategory,
	}
}

// TestExecuteDeletion_SkipsDirectoryThatBecameACategoryDestination covers a
// category created between the preview and the confirmation. The directory was
// fair game when it was listed and must not be removed now that qBittorrent
// will write into it.
func TestExecuteDeletion_SkipsDirectoryThatBecameACategoryDestination(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-category-race")

	f.svc.getCategoriesProvider = func(_ context.Context, _ int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{"leftover": {Name: "leftover", SavePath: f.abandoned}}, nil
	}

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.DirExists(t, f.abandoned, "a directory that became a category destination must survive")
	require.NoFileExists(t, f.strayFile, "the orphan file should still have been deleted")
}

// TestExecuteDeletion_ProtectsCategoriesEvenWhenTheOptionWasTurnedOff covers the
// operator disabling abandoned-directory cleanup after previewing. Directories
// already pending are still judged against the category list.
func TestExecuteDeletion_ProtectsCategoriesEvenWhenTheOptionWasTurnedOff(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-category-toggle-off")

	f.svc.getCategoriesProvider = func(_ context.Context, _ int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{"leftover": {Name: "leftover", SavePath: f.abandoned}}, nil
	}
	_, err := f.store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID:          1,
		GracePeriodMinutes:  0,
		IgnorePaths:         []string{},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupMaxFiles: 100,
		ScanDefaultSavePath: true,
		DeleteAbandonedDirs: false,
	})
	require.NoError(t, err)

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.DirExists(t, f.abandoned, "category protection must not depend on the option still being enabled")
}

// TestExecuteDeletion_DeletesAbandonedDirectoryWhenNothingClaimsIt is the
// control: without a claim the same fixture is removed, so the tests above fail
// for the right reason.
func TestExecuteDeletion_DeletesAbandonedDirectoryWhenNothingClaimsIt(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-control")

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.NoDirExists(t, f.abandoned, "an unclaimed empty directory should be removed")
	require.NoFileExists(t, f.strayFile)
}

// TestExecuteDeletion_FailsWhenSettingsCannotBeRead covers a settings read that
// errors. Continuing would rebuild the protection map with an empty scope, so
// the run must stop instead of deleting against thinner protection.
func TestExecuteDeletion_FailsWhenSettingsCannotBeRead(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-settings-error")

	f.svc.store = models.NewOrphanScanStore(settingsFailingQuerier{Querier: f.db})

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.DirExists(t, f.abandoned, "nothing may be deleted when the scope cannot be determined")
	require.FileExists(t, f.strayFile, "nothing may be deleted when the scope cannot be determined")
}

// TestExecuteDeletion_SecondCleanupPassRespectsCategories covers a category
// folder emptied by deleting an orphan inside it. That folder never reaches the
// abandoned-directory pass, so the follow-up cleanup has to protect it too.
func TestExecuteDeletion_SecondCleanupPassRespectsCategories(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-second-pass")

	f.svc.getCategoriesProvider = func(_ context.Context, _ int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{"movies": {Name: "movies", SavePath: f.categoryFolder}}, nil
	}

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.NoFileExists(t, f.orphanInCategory, "the orphan inside the category folder should be deleted")
	require.DirExists(t, f.categoryFolder, "emptying a category folder must not remove it")
}

// TestExecuteDeletion_RefusesToRemoveADirectoryReplacedByAFile covers a file
// created where the preview listed an empty directory. Remove would delete it,
// and it is data nobody reviewed.
func TestExecuteDeletion_RefusesToRemoveADirectoryReplacedByAFile(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-dir-became-file")

	require.NoError(t, os.Remove(f.abandoned))
	require.NoError(t, os.WriteFile(f.abandoned, []byte("new data"), 0o600))

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.FileExists(t, f.abandoned, "a file standing where a directory was previewed must survive")
	content, err := os.ReadFile(f.abandoned)
	require.NoError(t, err)
	require.Equal(t, "new data", string(content))
}

// TestExecuteDeletion_ProtectsAnotherInstanceThatOverlapsThePreviewedRoots
// covers settings changing between preview and confirmation. Instance A's own
// torrents live outside the default save path, so once the declared root is
// dropped its roots no longer overlap instance B's and B stops being merged
// into the protection map. Deletion is still authorized by the roots the run
// recorded, so protection has to be computed against those roots too.
func TestExecuteDeletion_ProtectsAnotherInstanceThatOverlapsThePreviewedRoots(t *testing.T) {
	base := t.TempDir()
	defaultSavePath := filepath.Join(base, "shared")
	ownRoot := filepath.Join(base, "library")
	stray := filepath.Join(defaultSavePath, "stray.txt")

	require.NoError(t, os.MkdirAll(defaultSavePath, 0o750))
	require.NoError(t, os.MkdirAll(ownRoot, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(ownRoot, "owned.mkv"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(stray, []byte("junk"), 0o600))

	db := testdb.NewMigratedSQLite(t, "orphanscan-delete-cross-instance-race")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	_, err = instanceStore.Create(t.Context(), "a", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewOrphanScanStore(db)
	svc := NewService(DefaultConfig(), nil, store, nil, nil, fsops.NewPool(stubInstanceGetter{}, local.NewBackend()))
	svc.getClientProvider = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-time.Minute)}, nil
	}
	svc.getAppPreferencesProvider = func(_ context.Context, _ int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: defaultSavePath}, nil
	}
	svc.getCategoriesProvider = func(_ context.Context, _ int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{}, nil
	}

	// Only instance A exists while the preview is built.
	svc.listInstancesProvider = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{{ID: 1, Name: "a", IsActive: true, HasLocalFilesystemAccess: true}}, nil
	}
	svc.getAllTorrentsProvider = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "owned", SavePath: ownRoot, State: qbt.TorrentStatePausedUp}}, nil
	}
	svc.getTorrentFilesBatchProvider = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv", Size: 1}}}, nil
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
	require.Len(t, files, 1)
	require.Equal(t, stray, files[0].FilePath)

	// Instance B now seeds that file, and default-save-path scanning is turned
	// off, leaving A's live roots disjoint from B's.
	svc.listInstancesProvider = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "a", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "b", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}
	svc.getAllTorrentsProvider = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		if instanceID == 2 {
			return []qbt.Torrent{{Hash: "b", SavePath: defaultSavePath, State: qbt.TorrentStatePausedUp}}, nil
		}
		return []qbt.Torrent{{Hash: "owned", SavePath: ownRoot, State: qbt.TorrentStatePausedUp}}, nil
	}
	svc.getTorrentFilesBatchProvider = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
		if instanceID == 2 {
			return map[string]qbt.TorrentFiles{"b": {{Name: "stray.txt", Size: 4}}}, nil
		}
		return map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv", Size: 1}}}, nil
	}
	_, err = store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID:          1,
		GracePeriodMinutes:  0,
		IgnorePaths:         []string{},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupMaxFiles: 100,
		ScanDefaultSavePath: false,
	})
	require.NoError(t, err)

	svc.executeDeletion(context.Background(), 1, runID)

	require.FileExists(t, stray, "a file another local instance now seeds must not be deleted")
}
