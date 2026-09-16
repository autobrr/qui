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

// sqliteTimestampLayout is the shape CURRENT_TIMESTAMP writes on SQLite. Production
// fills these columns with that function, and SQLite compares timestamps as text, so
// a fixture binding a Go time.Time would store the 30-character time.Time.String()
// form and never exercise the comparison production actually performs.
const sqliteTimestampLayout = "2006-01-02 15:04:05"

// storedTimestamp renders a time the way the running engine stores it: text on
// SQLite, a real timestamp on PostgreSQL.
func storedTimestamp(db *database.DB, at time.Time) any {
	if db.Dialect() == string(database.DialectSQLite) {
		return at.Format(sqliteTimestampLayout)
	}
	return at
}

func TestDirScanStorePruneMissingFilesSQLite(t *testing.T) {
	t.Parallel()
	runDirScanPruneTests(t, testdb.NewMigratedSQLite(t, "dirscan-prune"))
}

func TestDirScanStorePruneMissingFilesPostgresIntegration(t *testing.T) {
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

	insertRunWithRoot := func(t *testing.T, directoryID int, status models.DirScanRunStatus, startedAt time.Time, scanRoot any) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO dir_scan_runs (directory_id, status, triggered_by, scan_root, started_at)
			VALUES (?, ?, ?, ?, ?)
		`, directoryID, status, "test", scanRoot, storedTimestamp(db, startedAt))
		require.NoError(t, err)
	}

	// A full-directory run leaves scan_root NULL, as CreateRunIfNoActive does for a
	// scheduled scan.
	insertRun := func(t *testing.T, directoryID int, status models.DirScanRunStatus, startedAt time.Time) {
		t.Helper()
		insertRunWithRoot(t, directoryID, status, startedAt, nil)
	}

	// trackFile records a file and forces its last_processed_at, standing in for the
	// scan that would have refreshed it.
	trackFile := func(t *testing.T, directoryID int, path string, processedAt time.Time, status models.DirScanFileStatus) {
		t.Helper()
		require.NoError(t, store.UpsertFile(ctx, &models.DirScanFile{
			DirectoryID: directoryID,
			FilePath:    path,
			FileSize:    1,
			FileModTime: processedAt,
			Status:      status,
		}))
		_, err := db.ExecContext(ctx, `
			UPDATE dir_scan_files SET last_processed_at = ? WHERE directory_id = ? AND file_path = ?
		`, storedTimestamp(db, processedAt), directoryID, path)
		require.NoError(t, err)
	}

	trackSeedingFile := func(t *testing.T, directoryID int, path string, processedAt time.Time) {
		t.Helper()
		trackFile(t, directoryID, path, processedAt, models.DirScanFileStatusAlreadySeeding)
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

		trackSeedingFile(t, dirID, "/data/grace/live.mkv", daysAgo(1))
		trackSeedingFile(t, dirID, "/data/grace/deleted.mkv", daysAgo(10))

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

		trackSeedingFile(t, dirID, "/data/tooyoung/live.mkv", daysAgo(1))
		trackSeedingFile(t, dirID, "/data/tooyoung/deleted.mkv", daysAgo(10))

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
		trackSeedingFile(t, dirID, "/data/outage/one.mkv", daysAgo(3))
		trackSeedingFile(t, dirID, "/data/outage/two.mkv", daysAgo(3))

		removed, err := store.PruneMissingFiles(ctx, dirID)
		require.NoError(t, err)
		require.Zero(t, removed)
		require.Len(t, trackedPaths(t, dirID), 2)
	})

	// A webhook or manual run can carry a scan_root that narrows the walk to one
	// subfolder, refreshing only that subfolder's rows. Such a run must not define the
	// prune window, or three of them in a row would delete every live row outside it.
	t.Run("subroot runs do not justify a directory-wide prune", func(t *testing.T) {
		dirID := newDirectory(t, "/data/webhook")
		insertRun(t, dirID, models.DirScanRunStatusSuccess, daysAgo(10))
		for _, d := range []int{3, 2, 1} {
			insertRunWithRoot(t, dirID, models.DirScanRunStatusSuccess, daysAgo(d), "/data/webhook/show")
		}

		// The subfolder refreshes on every webhook run; the rest of the directory was
		// last seen by the full scan and is still very much alive on disk.
		trackSeedingFile(t, dirID, "/data/webhook/show/ep.mkv", daysAgo(1))
		trackSeedingFile(t, dirID, "/data/webhook/other/a.mkv", daysAgo(10))
		trackSeedingFile(t, dirID, "/data/webhook/other/b.mkv", daysAgo(10))

		removed, err := store.PruneMissingFiles(ctx, dirID)
		require.NoError(t, err)
		require.Zero(t, removed)
		require.Len(t, trackedPaths(t, dirID), 3)
	})

	// A requeue can land between a run's started_at and its prune. It never visits the
	// filesystem, so it must not make untouched rows look freshly seen and talk the
	// coverage guard into a delete the scan itself did not justify.
	t.Run("a concurrent requeue does not inflate scan coverage", func(t *testing.T) {
		dirID := newDirectory(t, "/data/requeue")
		for _, d := range []int{3, 2, 1} {
			insertRun(t, dirID, models.DirScanRunStatusSuccess, daysAgo(d))
		}

		// Ten no_match rows sit inside the grace window; the latest scan saw one file.
		for i := range 10 {
			trackFile(t, dirID, fmt.Sprintf("/data/requeue/in-grace-%d.mkv", i), daysAgo(2), models.DirScanFileStatusNoMatch)
		}
		trackSeedingFile(t, dirID, "/data/requeue/refreshed.mkv", daysAgo(1))
		trackSeedingFile(t, dirID, "/data/requeue/deleted.mkv", daysAgo(10))

		requeued, err := store.RequeueNoMatchFiles(ctx, dirID)
		require.NoError(t, err)
		require.Equal(t, int64(10), requeued)

		removed, err := store.PruneMissingFiles(ctx, dirID)
		require.NoError(t, err)
		require.Zero(t, removed)
		require.Len(t, trackedPaths(t, dirID), 12)
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

		trackSeedingFile(t, dirID, "/data/allstale/one.mkv", daysAgo(10))
		trackSeedingFile(t, dirID, "/data/allstale/two.mkv", daysAgo(10))

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
			trackSeedingFile(t, dirID, fmt.Sprintf("/data/emptymount/in-grace-%d.mkv", i), daysAgo(2))
		}
		trackSeedingFile(t, dirID, "/data/emptymount/refreshed.mkv", daysAgo(1))
		trackSeedingFile(t, dirID, "/data/emptymount/deleted.mkv", daysAgo(10))

		removed, err := store.PruneMissingFiles(ctx, dirID)
		require.NoError(t, err)
		require.Zero(t, removed)
		require.Len(t, trackedPaths(t, dirID), 12)
	})
}
