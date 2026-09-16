// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"testing"

	"github.com/stretchr/testify/require"

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
