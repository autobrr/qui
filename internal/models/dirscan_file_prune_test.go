// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestDirScanStorePruneMissingFilesSQLite(t *testing.T) {
	t.Parallel()
	runDirScanPruneTests(t, testdb.NewMigratedSQLite(t, "dirscan-prune"))
}

func TestDirScanStorePruneMissingFilesPostgres(t *testing.T) {
	runDirScanPruneTests(t, testdb.NewMigratedPostgres(t, "dirscan-prune"))
}

func runDirScanPruneTests(t *testing.T, db *database.DB) {
	t.Helper()

	ctx := t.Context()

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	now := time.Now().UTC().Truncate(time.Second)

	newDirectory := func(t *testing.T, path string) int {
		t.Helper()
		dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
			Path:                path,
			Enabled:             true,
			TargetInstanceID:    instance.ID,
			ScanIntervalMinutes: 60,
		})
		require.NoError(t, err)
		return dir.ID
	}

	insertRun := func(t *testing.T, directoryID int, status models.DirScanRunStatus, startedAt time.Time) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO dir_scan_runs (directory_id, status, triggered_by, scan_root, started_at)
			VALUES (?, ?, ?, ?, ?)
		`, directoryID, status, "test", "/data", startedAt)
		require.NoError(t, err)
	}

	// trackFile records a file and forces its last_processed_at, standing in for the
	// scan that would have refreshed it.
	trackFile := func(t *testing.T, directoryID int, path string, processedAt time.Time) {
		t.Helper()
		require.NoError(t, store.UpsertFile(ctx, &models.DirScanFile{
			DirectoryID: directoryID,
			FilePath:    path,
			FileSize:    1,
			FileModTime: processedAt,
			Status:      models.DirScanFileStatusAlreadySeeding,
		}))
		_, err := db.ExecContext(ctx, `
			UPDATE dir_scan_files SET last_processed_at = ? WHERE directory_id = ? AND file_path = ?
		`, processedAt, directoryID, path)
		require.NoError(t, err)
	}

	trackedPaths := func(t *testing.T, directoryID int) []string {
		t.Helper()
		files, err := store.ListFiles(ctx, directoryID, nil, 100, 0)
		require.NoError(t, err)
		paths := make([]string, 0, len(files))
		for _, f := range files {
			paths = append(paths, f.FilePath)
		}
		return paths
	}

	daysAgo := func(d int) time.Time { return now.Add(-time.Duration(d) * 24 * time.Hour) }

	t.Run("removes files missed across the grace window", func(t *testing.T) {
		dirID := newDirectory(t, "/data/grace")
		for _, d := range []int{3, 2, 1} {
			insertRun(t, dirID, models.DirScanRunStatusSuccess, daysAgo(d))
		}

		trackFile(t, dirID, "/data/grace/live.mkv", daysAgo(1))
		trackFile(t, dirID, "/data/grace/deleted.mkv", daysAgo(10))

		removed, err := store.PruneMissingFiles(ctx, dirID)
		require.NoError(t, err)
		require.Equal(t, int64(1), removed)
		require.Equal(t, []string{"/data/grace/live.mkv"}, trackedPaths(t, dirID))
	})

	t.Run("keeps rows until the grace window passes", func(t *testing.T) {
		dirID := newDirectory(t, "/data/tooyoung")
		// Only two successful scans, so nothing has yet been missed by three.
		for _, d := range []int{2, 1} {
			insertRun(t, dirID, models.DirScanRunStatusSuccess, daysAgo(d))
		}

		trackFile(t, dirID, "/data/tooyoung/live.mkv", daysAgo(1))
		trackFile(t, dirID, "/data/tooyoung/deleted.mkv", daysAgo(10))

		removed, err := store.PruneMissingFiles(ctx, dirID)
		require.NoError(t, err)
		require.Zero(t, removed)
		require.Len(t, trackedPaths(t, dirID), 2)
	})

	// A filesystem outage produces failed runs. Those must not advance the grace
	// window, or an unreachable mount would orphan every row at once.
	t.Run("failed runs do not advance the grace window", func(t *testing.T) {
		dirID := newDirectory(t, "/data/outage")
		for _, d := range []int{5, 4, 3} {
			insertRun(t, dirID, models.DirScanRunStatusSuccess, daysAgo(d))
		}
		for _, d := range []int{2, 1, 0} {
			insertRun(t, dirID, models.DirScanRunStatusFailed, daysAgo(d))
		}

		// Every file was last seen by the most recent successful scan.
		trackFile(t, dirID, "/data/outage/one.mkv", daysAgo(3))
		trackFile(t, dirID, "/data/outage/two.mkv", daysAgo(3))

		removed, err := store.PruneMissingFiles(ctx, dirID)
		require.NoError(t, err)
		require.Zero(t, removed)
		require.Len(t, trackedPaths(t, dirID), 2)
	})

	// When nothing at all was refreshed, a directory whose files were all deleted and
	// a directory whose filesystem is mounted but empty look identical in the stored
	// state. Keeping the rows is the deliberate choice: guessing wrong would wipe a
	// live directory during an outage, and Reset Scan Progress clears a real one.
	t.Run("keeps rows when no file was refreshed at all", func(t *testing.T) {
		dirID := newDirectory(t, "/data/allstale")
		for _, d := range []int{3, 2, 1} {
			insertRun(t, dirID, models.DirScanRunStatusSuccess, daysAgo(d))
		}

		trackFile(t, dirID, "/data/allstale/one.mkv", daysAgo(10))
		trackFile(t, dirID, "/data/allstale/two.mkv", daysAgo(10))

		removed, err := store.PruneMissingFiles(ctx, dirID)
		require.NoError(t, err)
		require.Zero(t, removed)
		require.Len(t, trackedPaths(t, dirID), 2)
	})

	// A mount that is present but empty walks cleanly and completes, so run status
	// alone is not evidence that the scan saw real data.
	t.Run("skips when the latest scan refreshed too few rows", func(t *testing.T) {
		dirID := newDirectory(t, "/data/emptymount")
		for _, d := range []int{3, 2, 1} {
			insertRun(t, dirID, models.DirScanRunStatusSuccess, daysAgo(d))
		}

		// Ten rows sit inside the grace window but the latest scan refreshed one.
		for i := range 10 {
			trackFile(t, dirID, fmt.Sprintf("/data/emptymount/in-grace-%d.mkv", i), daysAgo(2))
		}
		trackFile(t, dirID, "/data/emptymount/refreshed.mkv", daysAgo(1))
		trackFile(t, dirID, "/data/emptymount/deleted.mkv", daysAgo(10))

		removed, err := store.PruneMissingFiles(ctx, dirID)
		require.NoError(t, err)
		require.Zero(t, removed)
		require.Len(t, trackedPaths(t, dirID), 12)
	})
}
