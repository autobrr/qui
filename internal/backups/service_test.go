// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/autobrr/qui/internal/models"
)

func setupTestBackupDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file:queue_run_cleanup?mode=memory&cache=shared")
	require.NoError(t, err)

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	schema := []string{
		`CREATE TABLE IF NOT EXISTS instances (
		    id INTEGER PRIMARY KEY AUTOINCREMENT,
		    name TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS instance_backup_settings (
		    instance_id INTEGER PRIMARY KEY,
		    enabled BOOLEAN NOT NULL DEFAULT 0,
		    hourly_enabled BOOLEAN NOT NULL DEFAULT 0,
		    daily_enabled BOOLEAN NOT NULL DEFAULT 0,
		    weekly_enabled BOOLEAN NOT NULL DEFAULT 0,
		    monthly_enabled BOOLEAN NOT NULL DEFAULT 0,
		    keep_hourly INTEGER NOT NULL DEFAULT 0,
		    keep_daily INTEGER NOT NULL DEFAULT 7,
		    keep_weekly INTEGER NOT NULL DEFAULT 4,
		    keep_monthly INTEGER NOT NULL DEFAULT 12,
		    include_categories BOOLEAN NOT NULL DEFAULT 1,
		    include_tags BOOLEAN NOT NULL DEFAULT 1,
		    custom_path TEXT,
		    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS instance_backup_runs (
		    id INTEGER PRIMARY KEY AUTOINCREMENT,
		    instance_id INTEGER NOT NULL,
		    kind TEXT NOT NULL,
		    status TEXT NOT NULL DEFAULT 'pending',
		    requested_by TEXT NOT NULL DEFAULT 'system',
		    requested_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		    started_at TIMESTAMP,
		    completed_at TIMESTAMP,
		    archive_path TEXT,
		    manifest_path TEXT,
		    total_bytes INTEGER NOT NULL DEFAULT 0,
		    torrent_count INTEGER NOT NULL DEFAULT 0,
		    category_counts_json TEXT,
		    categories_json TEXT,
		    tags_json TEXT,
		    error_message TEXT,
		    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
		)`,
	}

	for _, stmt := range schema {
		_, err = db.Exec(stmt)
		require.NoError(t, err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

func TestQueueRunCleansPendingRunOnContextCancel(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name) VALUES (?)", "test-instance")
	require.NoError(t, err)

	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, Config{WorkerCount: 1})
	svc.jobs = make(chan job)
	svc.now = func() time.Time { return time.Unix(0, 0) }

	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)

	go func() {
		_, err := svc.QueueRun(runCtx, instanceID, models.BackupRunKindManual, "tester")
		errCh <- err
	}()

	var runID int64
	deadline := time.After(1 * time.Second)

	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for backup run to be created")
		default:
		}

		err := db.QueryRowContext(context.Background(), "SELECT id FROM instance_backup_runs LIMIT 1").Scan(&runID)
		if err == nil {
			break
		}
		if errors.Is(err, sql.ErrNoRows) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		require.NoError(t, err)
	}

	cancel()

	var queueErr error
	select {
	case queueErr = <-errCh:
	case <-time.After(time.Second):
		t.Fatal("QueueRun did not return after context cancellation")
	}
	require.ErrorIs(t, queueErr, context.Canceled)

	checkCtx, checkCancel := context.WithTimeout(context.Background(), time.Second)
	defer checkCancel()
	var count int
	require.NoError(t, db.QueryRowContext(checkCtx, "SELECT COUNT(*) FROM instance_backup_runs WHERE id = ?", runID).Scan(&count))
	require.Equal(t, 0, count, "pending run should be removed once context is canceled")

	svc.inflightMu.Lock()
	_, exists := svc.inflight[instanceID]
	svc.inflightMu.Unlock()
	require.False(t, exists, "instance inflight marker should be cleared")
}

func TestUpdateSettingsNormalizesRetention(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name) VALUES (?)", "retention-instance")
	require.NoError(t, err)

	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, Config{WorkerCount: 1})
	svc.jobs = make(chan job)
	svc.now = func() time.Time { return time.Unix(0, 0).UTC() }

	settings := &models.BackupSettings{
		InstanceID:        instanceID,
		Enabled:           true,
		HourlyEnabled:     true,
		DailyEnabled:      true,
		WeeklyEnabled:     false,
		MonthlyEnabled:    true,
		KeepHourly:        0,
		KeepDaily:         -2,
		KeepWeekly:        0,
		KeepMonthly:       0,
		IncludeCategories: true,
		IncludeTags:       true,
	}

	require.NoError(t, svc.UpdateSettings(ctx, settings))

	saved, err := svc.GetSettings(ctx, instanceID)
	require.NoError(t, err)
	require.Equal(t, 1, saved.KeepHourly)
	require.Equal(t, 1, saved.KeepDaily)
	require.Equal(t, 0, saved.KeepWeekly)
	require.Equal(t, 1, saved.KeepMonthly)

	_, err = db.ExecContext(ctx, `
		UPDATE instance_backup_settings
		SET keep_hourly = 0, keep_daily = 0, keep_monthly = 0
		WHERE instance_id = ?
	`, instanceID)
	require.NoError(t, err)

	reloaded, err := svc.GetSettings(ctx, instanceID)
	require.NoError(t, err)
	require.Equal(t, 1, reloaded.KeepHourly)
	require.Equal(t, 1, reloaded.KeepDaily)
	require.Equal(t, 1, reloaded.KeepMonthly)
}

func TestNormalizeAndPersistSettingsRepairsLegacyValues(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name) VALUES (?)", "legacy-retention")
	require.NoError(t, err)

	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, Config{WorkerCount: 1})

	legacy := &models.BackupSettings{
		InstanceID:     instanceID,
		Enabled:        true,
		HourlyEnabled:  true,
		DailyEnabled:   true,
		MonthlyEnabled: true,
		KeepHourly:     0,
		KeepDaily:      0,
		KeepMonthly:    0,
	}
	require.NoError(t, store.UpsertSettings(ctx, legacy))

	loaded, err := store.GetSettings(ctx, instanceID)
	require.NoError(t, err)
	require.Equal(t, 0, loaded.KeepHourly)
	require.Equal(t, 0, loaded.KeepDaily)
	require.Equal(t, 0, loaded.KeepMonthly)

	changed := svc.normalizeAndPersistSettings(ctx, loaded)
	require.True(t, changed)
	require.Equal(t, 1, loaded.KeepHourly)
	require.Equal(t, 1, loaded.KeepDaily)
	require.Equal(t, 1, loaded.KeepMonthly)

	saved, err := store.GetSettings(ctx, instanceID)
	require.NoError(t, err)
	require.Equal(t, 1, saved.KeepHourly)
	require.Equal(t, 1, saved.KeepDaily)
	require.Equal(t, 1, saved.KeepMonthly)
}

func TestUpdateSettingsClearsCustomPath(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name) VALUES (?)", "custom-path")
	require.NoError(t, err)

	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, Config{WorkerCount: 1})

	custom := "snapshots/daily"
	settings := &models.BackupSettings{
		InstanceID: instanceID,
		Enabled:    true,
		CustomPath: &custom,
	}

	require.NoError(t, svc.UpdateSettings(ctx, settings))

	saved, err := store.GetSettings(ctx, instanceID)
	require.NoError(t, err)
	require.Nil(t, saved.CustomPath)

	view, err := svc.GetSettings(ctx, instanceID)
	require.NoError(t, err)
	require.Nil(t, view.CustomPath)
}
