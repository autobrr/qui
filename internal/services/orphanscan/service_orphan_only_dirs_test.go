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

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type orphanOnlyFixture struct {
	svc             *Service
	store           *models.OrphanScanStore
	db              dbinterface.Querier
	runID           int64
	defaultSavePath string
	torrentSavePath string
	ownedFile       string
}

type orphanOnlyOptions struct {
	gracePeriodMinutes int
	maxFilesPerRun     int
	categories         func(defaultSavePath string) map[string]qbt.Category
	// torrentFileName is the seeded torrent's file, slash-delimited the way
	// qBittorrent reports it.
	torrentFileName string
	// scheduled triggers the run the way the scheduler does, which is the only
	// path that can auto-clean; autoCleanupMaxFiles turns auto-cleanup on.
	scheduled           bool
	autoCleanupMaxFiles int
}

// newOrphanOnlyFixture lays out defaultSavePath with one seeded torrent under
// mydata/, runs build over the tree, and scans it.
func newOrphanOnlyFixture(t *testing.T, dbName string, opts orphanOnlyOptions, build func(defaultSavePath string)) *orphanOnlyFixture {
	t.Helper()

	base := t.TempDir()
	defaultSavePath := filepath.Join(base, "torrents")
	torrentSavePath := filepath.Join(defaultSavePath, "mydata")

	torrentFileName := opts.torrentFileName
	if torrentFileName == "" {
		torrentFileName = "owned.mkv"
	}
	ownedFile := filepath.Join(torrentSavePath, filepath.FromSlash(torrentFileName))
	require.NoError(t, os.MkdirAll(filepath.Dir(ownedFile), 0o750))
	require.NoError(t, os.WriteFile(ownedFile, []byte("x"), 0o600))
	build(defaultSavePath)

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
		return map[string]qbt.TorrentFiles{"owned": {{Name: torrentFileName, Size: 1}}}, nil
	}
	svc.getAppPreferencesProvider = func(_ context.Context, _ int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: defaultSavePath}, nil
	}
	svc.subcategoriesEnabledProvider = func(_ context.Context, _ int) (bool, error) { return false, nil }
	svc.getCategoriesProvider = func(_ context.Context, _ int) (map[string]qbt.Category, error) {
		if opts.categories != nil {
			return opts.categories(defaultSavePath), nil
		}
		return map[string]qbt.Category{}, nil
	}

	maxFiles := opts.maxFilesPerRun
	if maxFiles == 0 {
		maxFiles = 1000
	}
	autoCleanupMaxFiles := opts.autoCleanupMaxFiles
	if autoCleanupMaxFiles == 0 {
		autoCleanupMaxFiles = 100
	}
	_, err = store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID:          1,
		GracePeriodMinutes:  opts.gracePeriodMinutes,
		IgnorePaths:         []string{},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      maxFiles,
		AutoCleanupEnabled:  opts.autoCleanupMaxFiles > 0,
		AutoCleanupMaxFiles: autoCleanupMaxFiles,
		ScanDefaultSavePath: true,
		DeleteAbandonedDirs: true,
	})
	require.NoError(t, err)

	trigger := "manual"
	if opts.scheduled {
		trigger = "scheduled"
	}
	runID, err := store.CreateRunIfNoActive(t.Context(), 1, trigger)
	require.NoError(t, err)
	svc.executeScan(context.Background(), 1, runID)

	run, err := store.GetRun(t.Context(), runID)
	require.NoError(t, err)
	// A scheduled run within the auto-cleanup threshold moves on by itself.
	require.Contains(t, []string{"preview_ready", "deleting", "completed"}, run.Status, "run error: %s", run.ErrorMessage)

	return &orphanOnlyFixture{
		svc:             svc,
		store:           store,
		db:              db,
		runID:           runID,
		defaultSavePath: defaultSavePath,
		torrentSavePath: torrentSavePath,
		ownedFile:       ownedFile,
	}
}

// previewedDirs returns the directories the preview offers to remove.
func (f *orphanOnlyFixture) previewedDirs(t *testing.T) []string {
	t.Helper()

	files, err := f.store.GetFilesForDeletion(t.Context(), f.runID)
	require.NoError(t, err)

	dirs := make([]string, 0, len(files))
	for _, file := range files {
		if file.IsAbandonedDir {
			dirs = append(dirs, file.FilePath)
		}
	}
	return dirs
}

// backdate moves a path's mtime out of the grace period.
func backdate(t *testing.T, path string) {
	t.Helper()

	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
}

// TestExecuteScan_PreviewsDirectoryHoldingOnlyOrphans covers the generalised
// rule: every file below the directory is an orphan this run deletes, so the run
// empties it and has to say so up front.
func TestExecuteScan_PreviewsDirectoryHoldingOnlyOrphans(t *testing.T) {
	var leftover, orphan string
	f := newOrphanOnlyFixture(t, "orphanscan-orphan-only-dir", orphanOnlyOptions{}, func(defaultSavePath string) {
		leftover = filepath.Join(defaultSavePath, "leftover")
		orphan = filepath.Join(leftover, "junk.mkv")
		writeFile(t, orphan)
	})

	require.Equal(t, []string{leftover}, f.previewedDirs(t))

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.NoFileExists(t, orphan)
	require.NoDirExists(t, leftover, "the previewed directory should be removed")
}

func TestExecuteScan_PreviewsParentOfDiscWithMetadata(t *testing.T) {
	var outer, disc string
	f := newOrphanOnlyFixture(t, "orphanscan-disc-parent-metadata", orphanOnlyOptions{}, func(defaultSavePath string) {
		outer = filepath.Join(defaultSavePath, "outer")
		disc = filepath.Join(outer, "disc")
		writeFile(t, filepath.Join(disc, "BDMV", "STREAM", "00001.m2ts"))
		writeFile(t, filepath.Join(disc, ".DS_Store"))
	})

	require.Equal(t, []string{outer}, f.previewedDirs(t))
	f.svc.executeDeletion(t.Context(), 1, f.runID)
	require.NoDirExists(t, disc)
	require.NoDirExists(t, outer)

	run, err := f.store.GetRun(t.Context(), f.runID)
	require.NoError(t, err)
	require.Equal(t, 1, run.FilesDeleted)
	require.Equal(t, 1, run.FoldersDeleted)
}

func TestExecuteScan_DirectoryBeyondExactFileCapMarksRunTruncated(t *testing.T) {
	f := newOrphanOnlyFixture(t, "orphanscan-directory-at-cap", orphanOnlyOptions{maxFilesPerRun: 1}, func(defaultSavePath string) {
		writeFile(t, filepath.Join(defaultSavePath, "leftover", "orphan.mkv"))
	})

	require.Empty(t, f.previewedDirs(t))
	run, err := f.store.GetRun(t.Context(), f.runID)
	require.NoError(t, err)
	require.Equal(t, 1, run.FilesFound)
	require.True(t, run.Truncated)
}

// TestExecuteScan_PreviewsTheWholeCascade covers the case that proved the two
// removal paths disagreed: an orphan in a/ beside an empty a/b/. The old
// abandoned-directory pass previewed a/b only, then the follow-up cleanup removed
// a/ too, so the run removed a directory nobody had seen.
func TestExecuteScan_PreviewsTheWholeCascade(t *testing.T) {
	var dirA, dirB, orphan string
	f := newOrphanOnlyFixture(t, "orphanscan-orphan-only-cascade", orphanOnlyOptions{}, func(defaultSavePath string) {
		dirA = filepath.Join(defaultSavePath, "a")
		dirB = filepath.Join(dirA, "b")
		orphan = filepath.Join(dirA, "file.mkv")
		mkdirs(t, dirA, "b")
		writeFile(t, orphan)
	})

	previewed := f.previewedDirs(t)
	require.ElementsMatch(t, []string{dirA, dirB}, previewed)

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.NoDirExists(t, dirB)
	require.NoDirExists(t, dirA)

	run, err := f.store.GetRun(t.Context(), f.runID)
	require.NoError(t, err)
	require.Equal(t, len(previewed), run.FoldersDeleted, "the run must remove exactly the directories it previewed")
}

// A fresh file keeps its parent and ancestors, even when their timestamps are old.
func TestExecuteScan_KeepsDirectoryHoldingAFileHeldByTheGracePeriod(t *testing.T) {
	var mixed, orphan, fresh string
	f := newOrphanOnlyFixture(t, "orphanscan-orphan-only-grace", orphanOnlyOptions{gracePeriodMinutes: 60}, func(defaultSavePath string) {
		mixed = filepath.Join(defaultSavePath, "mixed")
		orphan = filepath.Join(mixed, "settled.mkv")
		fresh = filepath.Join(mixed, "nested", "downloading.mkv")
		require.NoError(t, os.MkdirAll(filepath.Dir(fresh), 0o750))
		require.NoError(t, os.WriteFile(orphan, []byte("junk"), 0o600))
		require.NoError(t, os.WriteFile(fresh, []byte("new"), 0o600))
		backdate(t, orphan)
		backdate(t, filepath.Dir(fresh))
		// Last, so writing the files does not leave the directory itself inside
		// the grace period and pass the test for the wrong reason.
		backdate(t, mixed)
	})

	require.Empty(t, f.previewedDirs(t))

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.NoFileExists(t, orphan, "the settled orphan should still be deleted")
	require.FileExists(t, fresh)
	require.DirExists(t, mixed)
}

// TestExecuteScan_KeepsDirectoryHoldingATorrentOwnedFile covers the same shape
// with a live torrent. The payload sits below the save path, so the directory
// under test is an ordinary one rather than a scan root protected anyway.
func TestExecuteScan_KeepsDirectoryHoldingATorrentOwnedFile(t *testing.T) {
	var payloadDir, orphan string
	f := newOrphanOnlyFixture(t, "orphanscan-orphan-only-owned", orphanOnlyOptions{
		torrentFileName: "show/owned.mkv",
	}, func(defaultSavePath string) {
		payloadDir = filepath.Join(defaultSavePath, "mydata", "show")
		orphan = filepath.Join(payloadDir, "junk.mkv")
		writeFile(t, orphan)
	})

	require.NotContains(t, f.previewedDirs(t), payloadDir)

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.NoFileExists(t, orphan, "the orphan beside the torrent should still be deleted")
	require.FileExists(t, f.ownedFile)
	require.DirExists(t, payloadDir)
}

// TestExecuteScan_KeepsDirectoryWhoseOrphansTheCapDropped covers the cap's
// interaction with the generalised rule. truncationLess ranks files ahead of
// directories, so a run that drops a file keeps no directory either, and a
// directory whose orphan is still on disk is never offered.
func TestExecuteScan_KeepsDirectoryWhoseOrphansTheCapDropped(t *testing.T) {
	var capped, big, small, elsewhere string
	f := newOrphanOnlyFixture(t, "orphanscan-orphan-only-cap", orphanOnlyOptions{maxFilesPerRun: 1}, func(defaultSavePath string) {
		capped = filepath.Join(defaultSavePath, "capped")
		big = filepath.Join(capped, "big.mkv")
		small = filepath.Join(capped, "small.mkv")
		elsewhere = filepath.Join(defaultSavePath, "empty")
		require.NoError(t, os.MkdirAll(capped, 0o750))
		require.NoError(t, os.MkdirAll(elsewhere, 0o750))
		require.NoError(t, os.WriteFile(big, []byte("aaaaaaaaaa"), 0o600))
		require.NoError(t, os.WriteFile(small, []byte("b"), 0o600))
	})

	run, err := f.store.GetRun(t.Context(), f.runID)
	require.NoError(t, err)
	require.True(t, run.Truncated, "the fixture must actually hit the cap")

	previewed := f.previewedDirs(t)
	require.NotContains(t, previewed, capped, "a directory whose orphan the cap dropped must not be previewed")
	require.NotContains(t, previewed, elsewhere, "the cap covers directories, and the file took the only slot")

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.NoFileExists(t, big)
	require.FileExists(t, small, "the orphan the cap dropped must survive")
	require.DirExists(t, capped, "the directory holding it must survive")
	require.DirExists(t, elsewhere)
}

// TestExecuteScan_NeverPreviewsCategoryDestinationsOrScanRoots covers the
// protections the generalised rule must not weaken: a category folder holding
// nothing but orphans now reaches the directory pass for the first time.
func TestExecuteScan_NeverPreviewsCategoryDestinationsOrScanRoots(t *testing.T) {
	var orphanInCategory string
	f := newOrphanOnlyFixture(t, "orphanscan-orphan-only-categories", orphanOnlyOptions{
		categories: func(defaultSavePath string) map[string]qbt.Category {
			return map[string]qbt.Category{
				"movies": {Name: "movies", SavePath: filepath.Join(defaultSavePath, "movies")},
				// Inherits, so its destination is defaultSavePath/tv.
				"tv": {Name: "tv", SavePath: ""},
			}
		},
	}, func(defaultSavePath string) {
		category := filepath.Join(defaultSavePath, "movies")
		orphanInCategory = filepath.Join(category, "junk.mkv")
		mkdirs(t, defaultSavePath, "tv")
		writeFile(t, orphanInCategory)
	})

	previewed := f.previewedDirs(t)
	require.NotContains(t, previewed, filepath.Join(f.defaultSavePath, "movies"))
	require.NotContains(t, previewed, filepath.Join(f.defaultSavePath, "tv"))
	require.NotContains(t, previewed, f.defaultSavePath)
	require.NotContains(t, previewed, f.torrentSavePath)

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.NoFileExists(t, orphanInCategory, "the orphan inside the category should still be deleted")
	require.DirExists(t, filepath.Join(f.defaultSavePath, "movies"), "emptying a category destination must not remove it")
	require.DirExists(t, filepath.Join(f.defaultSavePath, "tv"))
	require.DirExists(t, f.defaultSavePath)
	require.DirExists(t, f.torrentSavePath)
}

// TestExecuteDeletion_DirectoryWhoseFileWasKeptIsSkippedNotFailed covers a
// previewed directory whose only file the run ends up keeping: here an ignore
// path added between preview and confirmation. Nothing went wrong, so the
// directory is reported skipped and the run completes, instead of the
// directory failing on rmdir and, with nothing else deleted, failing the run.
func TestExecuteDeletion_DirectoryWhoseFileWasKeptIsSkippedNotFailed(t *testing.T) {
	var leftover, orphan string
	f := newOrphanOnlyFixture(t, "orphanscan-orphan-only-kept-at-deletion", orphanOnlyOptions{}, func(defaultSavePath string) {
		leftover = filepath.Join(defaultSavePath, "leftover")
		orphan = filepath.Join(leftover, "junk.mkv")
		writeFile(t, orphan)
	})
	require.Equal(t, []string{leftover}, f.previewedDirs(t))

	_, err := f.store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID:          1,
		GracePeriodMinutes:  0,
		IgnorePaths:         []string{orphan},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupMaxFiles: 100,
		ScanDefaultSavePath: true,
		DeleteAbandonedDirs: true,
	})
	require.NoError(t, err)

	f.svc.executeDeletion(context.Background(), 1, f.runID)

	require.FileExists(t, orphan)
	require.DirExists(t, leftover)

	run, err := f.store.GetRun(t.Context(), f.runID)
	require.NoError(t, err)
	require.Equal(t, "completed", run.Status, "a skip is not a failure: %s", run.ErrorMessage)
	require.Empty(t, run.ErrorMessage)
	require.Zero(t, run.FoldersDeleted)

	files, err := f.store.ListFiles(t.Context(), f.runID, 100, 0, "size_desc")
	require.NoError(t, err)
	for _, file := range files {
		require.Equal(t, "skipped", file.Status, "%s must be skipped, not %s: %s", file.FilePath, file.Status, file.ErrorMessage)
	}
}

// TestExecuteScan_AutoCleanupThresholdCountsFilesNotDirectories covers the
// scheduled-scan threshold. Every orphan now tends to bring its parent chain
// into the preview, and counting those entries would push a small cleanup over
// a threshold the settings describe as a file count.
func TestExecuteScan_AutoCleanupThresholdCountsFilesNotDirectories(t *testing.T) {
	var orphans, dirs []string
	f := newOrphanOnlyFixture(t, "orphanscan-orphan-only-autocleanup", orphanOnlyOptions{
		scheduled:           true,
		autoCleanupMaxFiles: 2,
	}, func(defaultSavePath string) {
		for _, name := range []string{"one", "two"} {
			dir := filepath.Join(defaultSavePath, name)
			dirs = append(dirs, dir)
			orphans = append(orphans, filepath.Join(dir, "junk.mkv"))
			writeFile(t, orphans[len(orphans)-1])
		}
	})
	require.Eventually(t, func() bool {
		run, err := f.store.GetRun(t.Context(), f.runID)
		return err == nil && run.Status == "completed"
	}, 10*time.Second, 50*time.Millisecond, "two orphan files are within a threshold of two, whatever their directories add")

	// Two files and two directories: four entries, two of them files.
	entries, err := f.store.ListFiles(t.Context(), f.runID, 100, 0, "size_desc")
	require.NoError(t, err)
	require.Len(t, entries, 4)

	for _, orphan := range orphans {
		require.NoFileExists(t, orphan)
	}
	for _, dir := range dirs {
		require.NoDirExists(t, dir)
	}
}
