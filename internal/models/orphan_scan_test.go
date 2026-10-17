// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestOrphanScanStore_CompletionRequiresWarning(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "orphan-completion")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(ctx, "test", "http://example.invalid", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)
	runID, err := store.CreateRunIfNoActive(ctx, instance.ID, "manual")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunPartial(ctx, runID, "Scan path unavailable"))
	require.NoError(t, store.UpdateRunStatus(ctx, runID, "deleting"))

	_, err = db.ExecContext(ctx, `
		CREATE TRIGGER reject_orphan_warning BEFORE UPDATE OF error_message ON orphan_scan_runs
		BEGIN SELECT RAISE(ABORT, 'warning write failed'); END
	`)
	require.NoError(t, err)
	warning := "Scan path unavailable\n\nDeletion failed for 1 item(s)"
	require.ErrorContains(t, store.UpdateRunCompleted(ctx, runID, 1, 2, 100, warning), "warning write failed")
	run, err := store.GetRun(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "deleting", run.Status)
	require.Equal(t, "Scan path unavailable", run.ErrorMessage)
	require.Zero(t, run.FilesDeleted)
	require.Zero(t, run.FoldersDeleted)
	require.Zero(t, run.BytesReclaimed)
	require.Nil(t, run.CompletedAt)

	_, err = db.ExecContext(ctx, "DROP TRIGGER reject_orphan_warning")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunCompleted(ctx, runID, 1, 2, 100, warning))
	run, err = store.GetRun(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "completed", run.Status)
	require.True(t, run.Partial)
	require.Equal(t, warning, run.ErrorMessage)
	require.Equal(t, 1, run.FilesDeleted)
	require.Equal(t, 2, run.FoldersDeleted)
	require.EqualValues(t, 100, run.BytesReclaimed)
	require.NotNil(t, run.CompletedAt)
}

func TestOrphanScanStore_FilesystemModeRoundTripsThroughEveryRunRead(t *testing.T) {
	t.Parallel()
	runFilesystemModeRoundTrip(t, testdb.NewMigratedSQLite)
}

func TestOrphanScanStore_FilesystemModeRoundTripsThroughEveryRunReadPostgresIntegration(t *testing.T) {
	runFilesystemModeRoundTrip(t, testdb.NewMigratedPostgres)
}

func runFilesystemModeRoundTrip(t *testing.T, newDB func(testing.TB, string) *database.DB) {
	t.Helper()
	ctx := t.Context()
	db := newDB(t, "orphan-filesystem-mode")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(ctx, "test", "http://example.invalid", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)

	completedID, err := store.CreateRunIfNoActive(ctx, instance.ID, "manual")
	require.NoError(t, err)
	created, err := store.GetRun(ctx, completedID)
	require.NoError(t, err)
	require.Equal(t, models.FilesystemModeNone, created.FilesystemMode, "a new run is not local until the scan says so")
	require.NoError(t, store.UpdateRunFilesystemMode(ctx, completedID, models.FilesystemModeRemote))
	require.NoError(t, store.UpdateRunCompleted(ctx, completedID, 0, 0, 0, ""))
	activeID, err := store.CreateRunIfNoActive(ctx, instance.ID, "manual")
	require.NoError(t, err)
	require.NoError(t, store.UpdateRunFilesystemMode(ctx, activeID, models.FilesystemModeRemote))

	reads := map[string]func() ([]*models.OrphanScanRun, error){
		"GetRun": func() ([]*models.OrphanScanRun, error) {
			r, err := store.GetRun(ctx, completedID)
			return []*models.OrphanScanRun{r}, err
		},
		"GetRunByInstance": func() ([]*models.OrphanScanRun, error) {
			r, err := store.GetRunByInstance(ctx, instance.ID, completedID)
			return []*models.OrphanScanRun{r}, err
		},
		"GetLastCompletedRun": func() ([]*models.OrphanScanRun, error) {
			r, err := store.GetLastCompletedRun(ctx, instance.ID)
			return []*models.OrphanScanRun{r}, err
		},
		"GetMostRecentActiveRun": func() ([]*models.OrphanScanRun, error) {
			r, err := store.GetMostRecentActiveRun(ctx, instance.ID)
			return []*models.OrphanScanRun{r}, err
		},
		// ListRuns reads both the recent and the active query.
		"ListRuns": func() ([]*models.OrphanScanRun, error) { return store.ListRuns(ctx, instance.ID, 10) },
	}
	for name, read := range reads {
		runs, err := read()
		require.NoError(t, err, name)
		require.NotEmpty(t, runs, name)
		for _, run := range runs {
			require.NotNil(t, run, name)
			require.Equal(t, models.FilesystemModeRemote, run.FilesystemMode, "%s run %d", name, run.ID)
		}
	}
}

func TestOrphanScanStore_LegacyRunsBackfillAsLocal(t *testing.T) {
	t.Parallel()
	runLegacyRunBackfill(t, testdb.NewMigratedSQLite)
}

func TestOrphanScanStore_LegacyRunsBackfillAsLocalPostgresIntegration(t *testing.T) {
	runLegacyRunBackfill(t, testdb.NewMigratedPostgres)
}

// runLegacyRunBackfill writes a run the way code from before the column did.
func runLegacyRunBackfill(t *testing.T, newDB func(testing.TB, string) *database.DB) {
	t.Helper()
	ctx := t.Context()
	db := newDB(t, "orphan-legacy-mode")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(ctx, "test", "http://example.invalid", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	var runID int64
	require.NoError(t, db.QueryRowContext(ctx,
		`INSERT INTO orphan_scan_runs (instance_id, status, triggered_by) VALUES (?, 'preview_ready', 'manual') RETURNING id`,
		instance.ID,
	).Scan(&runID))

	run, err := models.NewOrphanScanStore(db).GetRun(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, models.FilesystemModeLocal, run.FilesystemMode)
}

func TestOrphanScanStore_FailPreviewReadyRunLeavesOtherStatusesAlone(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "orphan-fail-preview")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(ctx, "test", "http://example.invalid", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)
	runID, err := store.CreateRunIfNoActive(ctx, instance.ID, "manual")
	require.NoError(t, err)

	require.NoError(t, store.UpdateRunStatus(ctx, runID, "deleting"))
	failed, err := store.FailPreviewReadyRun(ctx, runID, "stale")
	require.NoError(t, err)
	require.False(t, failed)
	run, err := store.GetRun(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "deleting", run.Status, "a racing confirm must not fail a live deletion")

	require.NoError(t, store.UpdateRunStatus(ctx, runID, "preview_ready"))
	failed, err = store.FailPreviewReadyRun(ctx, runID, "stale")
	require.NoError(t, err)
	require.True(t, failed)
	run, err = store.GetRun(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "failed", run.Status)
	require.Equal(t, "stale", run.ErrorMessage)
}

func TestOrphanScanStore_NewRunReplacesOnlyARemotePreview(t *testing.T) {
	t.Parallel()
	runRemotePreviewReplacement(t, testdb.NewMigratedSQLite)
}

func TestOrphanScanStore_NewRunReplacesOnlyARemotePreviewPostgresIntegration(t *testing.T) {
	runRemotePreviewReplacement(t, testdb.NewMigratedPostgres)
}

func runRemotePreviewReplacement(t *testing.T, newDB func(testing.TB, string) *database.DB) {
	t.Helper()
	ctx := t.Context()
	db := newDB(t, "orphan-replace-remote")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)
	newInstance := func() int {
		instance, err := instances.Create(ctx, "test", "http://example.invalid", "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)
		return instance.ID
	}
	preview := func(instanceID int, mode models.FilesystemMode) int64 {
		runID, err := store.CreateRunIfNoActive(ctx, instanceID, "manual")
		require.NoError(t, err)
		require.NoError(t, store.UpdateRunFilesystemMode(ctx, runID, mode))
		require.NoError(t, store.UpdateRunFoundStats(ctx, runID, 3, false, 30))
		require.NoError(t, store.UpdateRunStatus(ctx, runID, "preview_ready"))
		return runID
	}
	status := func(runID int64) string {
		run, err := store.GetRun(ctx, runID)
		require.NoError(t, err)
		return run.Status
	}

	t.Run("remote preview is canceled", func(t *testing.T) {
		instanceID := newInstance()
		old := preview(instanceID, models.FilesystemModeRemote)
		active, err := store.GetMostRecentActiveRun(ctx, instanceID)
		require.NoError(t, err)
		require.Nil(t, active, "a remote preview does not block")

		runID, err := store.CreateRunIfNoActive(ctx, instanceID, "scheduled")
		require.NoError(t, err)
		require.Equal(t, "canceled", status(old))
		require.Equal(t, "pending", status(runID))
	})

	t.Run("local preview still blocks", func(t *testing.T) {
		instanceID := newInstance()
		old := preview(instanceID, models.FilesystemModeLocal)
		_, err := store.CreateRunIfNoActive(ctx, instanceID, "manual")
		require.ErrorIs(t, err, models.ErrRunAlreadyActive)
		require.Equal(t, "preview_ready", status(old))
		active, err := store.GetMostRecentActiveRun(ctx, instanceID)
		require.NoError(t, err)
		require.Equal(t, old, active.ID)
	})

	t.Run("refused run keeps the remote preview", func(t *testing.T) {
		instanceID := newInstance()
		old := preview(instanceID, models.FilesystemModeRemote)
		_, err := db.ExecContext(ctx,
			`INSERT INTO orphan_scan_runs (instance_id, status, triggered_by, filesystem_mode) VALUES (?, 'scanning', 'manual', 'local')`,
			instanceID)
		require.NoError(t, err)

		_, err = store.CreateRunIfNoActive(ctx, instanceID, "manual")
		require.ErrorIs(t, err, models.ErrRunAlreadyActive)
		require.Equal(t, "preview_ready", status(old))
	})

	t.Run("concurrent triggers start one run", func(t *testing.T) {
		instanceID := newInstance()
		old := preview(instanceID, models.FilesystemModeRemote)

		const triggers = 8
		errs := make(chan error, triggers)
		var wg sync.WaitGroup
		for range triggers {
			wg.Go(func() {
				_, err := store.CreateRunIfNoActive(ctx, instanceID, "manual")
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		started := 0
		for err := range errs {
			if err == nil {
				started++
				continue
			}
			require.ErrorIs(t, err, models.ErrRunAlreadyActive)
		}
		require.Equal(t, 1, started)
		require.Equal(t, "canceled", status(old))
	})
}

func TestOrphanScanStore_LastFinishedScan(t *testing.T) {
	t.Parallel()
	runLastFinishedScan(t, testdb.NewMigratedSQLite)
}

func TestOrphanScanStore_LastFinishedScanPostgresIntegration(t *testing.T) {
	runLastFinishedScan(t, testdb.NewMigratedPostgres)
}

func runLastFinishedScan(t *testing.T, newDB func(testing.TB, string) *database.DB) {
	t.Helper()
	ctx := t.Context()
	db := newDB(t, "orphan-last-finished")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(ctx, "test", "http://example.invalid", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)
	insert := func(status, startedAt string, completedAt any) int64 {
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `
			INSERT INTO orphan_scan_runs (instance_id, status, triggered_by, files_found, filesystem_mode, started_at, completed_at)
			VALUES (?, ?, 'scheduled', 3, 'remote', ?, ?) RETURNING id`,
			instance.ID, status, startedAt, completedAt,
		).Scan(&id))
		return id
	}
	lastID := func() int64 {
		run, err := store.GetLastFinishedScan(ctx, instance.ID)
		require.NoError(t, err)
		if run == nil {
			return 0
		}
		return run.ID
	}

	require.Zero(t, lastID(), "no runs")

	completed := insert("completed", "2026-01-01 00:00:00", "2026-01-01 02:00:00")
	require.Equal(t, completed, lastID())

	preview := insert("preview_ready", "2026-01-02 00:00:00", nil)
	require.Equal(t, preview, lastID(), "a preview counts as a finished walk")

	insert("canceled", "2026-01-03 00:00:00", nil)
	insert("failed", "2026-01-04 00:00:00", "2026-01-04 00:10:00")
	insert("scanning", "2026-01-05 00:00:00", nil)
	require.Equal(t, preview, lastID(), "unfinished, failed and canceled runs do not pace the schedule")

	// A deletion that finished after a later preview started is the newer result.
	late := insert("completed", "2026-01-01 12:00:00", "2026-01-02 06:00:00")
	require.Equal(t, late, lastID())
}
