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
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type settingsFailingQuerier struct {
	dbinterface.Querier
}

func (q settingsFailingQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, "FROM orphan_scan_settings") {
		return q.Querier.QueryRowContext(ctx, "SELECT no_such_column FROM orphan_scan_settings")
	}
	return q.Querier.QueryRowContext(ctx, query, args...)
}

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

	var abandoned, strayFile, categoryFolder, orphanInCategory string
	f := newOrphanOnlyFixture(t, dbName, orphanOnlyOptions{}, func(defaultSavePath string) {
		abandoned = filepath.Join(defaultSavePath, "leftover")
		strayFile = filepath.Join(defaultSavePath, "stray.txt")
		categoryFolder = filepath.Join(defaultSavePath, "movies")
		orphanInCategory = filepath.Join(categoryFolder, "junk.txt")

		mkdirs(t, defaultSavePath, "leftover")
		writeFile(t, strayFile)
		writeFile(t, orphanInCategory)
	})

	return &deletionFixture{
		svc:              f.svc,
		store:            f.store,
		db:               f.db,
		runID:            f.runID,
		defaultSavePath:  f.defaultSavePath,
		abandoned:        abandoned,
		strayFile:        strayFile,
		categoryFolder:   categoryFolder,
		orphanInCategory: orphanInCategory,
	}
}

func TestExecuteDeletion_SkipsDirectoryThatBecameACategoryDestination(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-category-race")

	stubSync(f.svc).folders(qbittorrent.InstanceFolders{DefaultSavePath: f.defaultSavePath, CategorySaveFolders: []string{f.abandoned}})

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.DirExists(t, f.abandoned, "a directory that became a category destination must survive")
	require.NoFileExists(t, f.strayFile, "the orphan file should still have been deleted")
}

func TestExecuteDeletion_ProtectsCategoriesEvenWhenTheOptionWasTurnedOff(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-category-toggle-off")

	stubSync(f.svc).folders(qbittorrent.InstanceFolders{DefaultSavePath: f.defaultSavePath, CategorySaveFolders: []string{f.abandoned}})
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

func TestExecuteDeletion_DeletesAbandonedDirectoryWhenNothingClaimsIt(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-control")

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.NoDirExists(t, f.abandoned, "an unclaimed empty directory should be removed")
	require.NoFileExists(t, f.strayFile)
}

func TestExecuteDeletion_DirectoriesCountTowardsTheRunOutcome(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-dirs-count")

	pending, err := f.store.GetFilesForDeletion(t.Context(), f.runID)
	require.NoError(t, err)
	for _, file := range pending {
		if file.FilePath != f.abandoned {
			f.svc.updateFileStatus(t.Context(), file.ID, "skipped", "parked by the test")
		}
	}
	outside := filepath.Join(t.TempDir(), "unreachable.mkv")
	require.NoError(t, f.store.InsertFiles(t.Context(), f.runID, []models.OrphanScanFile{
		{FilePath: outside, FileSize: 1, Status: "pending"},
	}))

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	run, err := f.store.GetRun(t.Context(), f.runID)
	require.NoError(t, err)
	require.NoDirExists(t, f.abandoned, "the directory should still have been removed")
	require.Equal(t, "completed", run.Status, "a run that removed a directory did work, whatever the files did")
	require.Equal(t, 1, run.FoldersDeleted, "the folder count must survive into the run")
	require.Equal(t, 0, run.FilesDeleted)
	require.Contains(t, run.ErrorMessage, "1 item(s)", "the failed file still has to be reported as a warning")
}

func TestExecuteDeletion_FailsWhenSettingsCannotBeRead(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-settings-error")

	f.svc.store = models.NewOrphanScanStore(settingsFailingQuerier{Querier: f.db})

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.DirExists(t, f.abandoned, "nothing may be deleted when the scope cannot be determined")
	require.FileExists(t, f.strayFile, "nothing may be deleted when the scope cannot be determined")
}

func TestExecuteDeletion_FailsOnAnInvalidIgnorePath(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-invalid-ignore")

	_, err := f.store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID:          1,
		IgnorePaths:         []string{"relative/keep"},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupMaxFiles: 100,
		ScanDefaultSavePath: true,
		DeleteAbandonedDirs: true,
	})
	require.NoError(t, err)

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	run, err := f.store.GetRun(t.Context(), f.runID)
	require.NoError(t, err)
	require.Equal(t, "failed", run.Status)
	require.Contains(t, run.ErrorMessage, "invalid ignore paths")
	require.FileExists(t, f.strayFile, "nothing may be deleted under an invalid ignore path")
	require.FileExists(t, f.orphanInCategory, "nothing may be deleted under an invalid ignore path")
	require.DirExists(t, f.abandoned, "nothing may be deleted under an invalid ignore path")
}

func TestExecuteDeletion_SkipsCategoryFolderEmptiedByTheRun(t *testing.T) {
	f := newDeletionFixture(t, "orphanscan-delete-second-pass")

	stubSync(f.svc).folders(qbittorrent.InstanceFolders{DefaultSavePath: f.defaultSavePath, CategorySaveFolders: []string{f.categoryFolder}})

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.NoFileExists(t, f.orphanInCategory, "the orphan inside the category folder should be deleted")
	require.DirExists(t, f.categoryFolder, "emptying a category folder must not remove it")
}

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
	stubSync(svc).getClient = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-time.Minute)}, nil
	}
	stubSync(svc).folders(qbittorrent.InstanceFolders{DefaultSavePath: defaultSavePath})

	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{{ID: 1, Name: "a", IsActive: true, HasLocalFilesystemAccess: true}}, nil
	}
	stubSync(svc).getAllTorrents = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "owned", SavePath: ownRoot, State: qbt.TorrentStatePausedUp}}, nil
	}
	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
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

	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, Name: "a", IsActive: true, HasLocalFilesystemAccess: true},
			{ID: 2, Name: "b", IsActive: true, HasLocalFilesystemAccess: true},
		}, nil
	}
	stubSync(svc).getAllTorrents = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		if instanceID == 2 {
			return []qbt.Torrent{{Hash: "b", SavePath: defaultSavePath, State: qbt.TorrentStatePausedUp}}, nil
		}
		return []qbt.Torrent{{Hash: "owned", SavePath: ownRoot, State: qbt.TorrentStatePausedUp}}, nil
	}
	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, instanceID int, _ []string) (map[string]qbt.TorrentFiles, error) {
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

func TestExecuteDeletion_RemovesNoDirectoriesWhenTheScanPreviewedNone(t *testing.T) {
	base := t.TempDir()
	defaultSavePath := filepath.Join(base, "torrents")
	torrentSavePath := filepath.Join(defaultSavePath, "mydata")
	categoryFolder := filepath.Join(defaultSavePath, "movies")
	orphan := filepath.Join(categoryFolder, "junk.txt")

	require.NoError(t, os.MkdirAll(torrentSavePath, 0o750))
	require.NoError(t, os.MkdirAll(categoryFolder, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(torrentSavePath, "owned.mkv"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(orphan, []byte("junk"), 0o600))

	db := testdb.NewMigratedSQLite(t, "orphanscan-followup-categories")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	_, err = instanceStore.Create(t.Context(), "test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewOrphanScanStore(db)
	svc := NewService(DefaultConfig(), nil, store, nil, nil, fsops.NewPool(stubInstanceGetter{}, local.NewBackend()))
	stubSync(svc).getClient = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-time.Minute)}, nil
	}
	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{{ID: 1, Name: "test", IsActive: true, HasLocalFilesystemAccess: true}}, nil
	}
	stubSync(svc).getAllTorrents = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "owned", SavePath: torrentSavePath, State: qbt.TorrentStatePausedUp}}, nil
	}
	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv", Size: 1}}}, nil
	}
	stubSync(svc).folders(qbittorrent.InstanceFolders{DefaultSavePath: defaultSavePath, CategorySaveFolders: []string{categoryFolder}})

	_, err = store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
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

	runID, err := store.CreateRunIfNoActive(t.Context(), 1, "manual")
	require.NoError(t, err)
	svc.executeScan(context.Background(), 1, runID)

	files, err := store.GetFilesForDeletion(t.Context(), runID)
	require.NoError(t, err)
	require.Len(t, files, 1, "only the orphan file should be previewed")
	require.False(t, files[0].IsAbandonedDir)

	svc.executeDeletion(context.Background(), 1, runID)

	require.NoFileExists(t, orphan, "the orphan should be deleted")
	require.DirExists(t, categoryFolder, "a directory nobody previewed must not be removed")

	run, err := store.GetRun(t.Context(), runID)
	require.NoError(t, err)
	require.Zero(t, run.FoldersDeleted, "a run that previewed no directories must remove none")
}
