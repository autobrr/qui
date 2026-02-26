// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testdb"
)

// Helper function to insert a test instance with interned fields
func insertTestInstance(t *testing.T, db *database.DB, name string) int {
	t.Helper()
	ctx := context.Background()

	// Intern strings
	var nameID, hostID, usernameID int64
	err := db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) ON CONFLICT (value) DO UPDATE SET value = value RETURNING id", name).Scan(&nameID)
	require.NoError(t, err)
	err = db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) ON CONFLICT (value) DO UPDATE SET value = value RETURNING id", "http://localhost").Scan(&hostID)
	require.NoError(t, err)
	err = db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) ON CONFLICT (value) DO UPDATE SET value = value RETURNING id", "user").Scan(&usernameID)
	require.NoError(t, err)

	result, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (?, ?, ?, 'pass')", nameID, hostID, usernameID)
	require.NoError(t, err)
	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	return int(instanceID64)
}

func setupTestBackupDB(t *testing.T) *database.DB {
	t.Helper()

	// Create a unique database file for each test
	dbPath := testdb.PathFromTemplate(t, "backups-service", "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err, "Failed to initialize test database with migrations")

	// Allow multiple connections for tests that need concurrent access
	db.Conn().SetMaxOpenConns(5)
	db.Conn().SetMaxIdleConns(2)

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

func TestQueueRunCleansPendingRunOnContextCancel(t *testing.T) {
	db := setupTestBackupDB(t)

	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
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

func TestStartBlocksWhileRecoveringMissedBackups(t *testing.T) {
	db := setupTestBackupDB(t)

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	t.Cleanup(svc.Stop)

	instanceNames := []string{"instance-a", "instance-b", "instance-c"}
	for _, name := range instanceNames {
		instanceID := insertTestInstance(t, db, name)

		settings := &models.BackupSettings{
			InstanceID:    instanceID,
			Enabled:       true,
			HourlyEnabled: true,
			KeepHourly:    1,
		}
		require.NoError(t, store.UpsertSettings(context.Background(), settings))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(started)
	}()

	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()

	select {
	case <-started:
		// Expected with a fixed implementation.
	case <-timer.C:
		// Drain the queue to let Start finish before failing the test.
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			for range instanceNames {
				select {
				case <-svc.jobs:
				case <-time.After(100 * time.Millisecond):
					return
				}
			}
		}()
		<-drained
		cancel()
		<-started
		t.Fatal("Start blocked while recovering missed backups; workers never launched")
	}

	cancel()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Start did not exit after shutdown")
	}
}

func TestUpdateSettingsNormalizesRetention(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "retention-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
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
	instanceID := insertTestInstance(t, db, "legacy-retention")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)

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
	instanceID := insertTestInstance(t, db, "custom-path")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)

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

func TestRecoverIncompleteRuns(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	// Create a pending run
	pendingRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusPending,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-5 * time.Minute),
	}
	require.NoError(t, store.CreateRun(ctx, pendingRun))

	// Create a running run
	runningRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindDaily,
		Status:      models.BackupRunStatusRunning,
		RequestedBy: "manual",
		RequestedAt: fixedTime.Add(-10 * time.Minute),
	}
	startedAt := fixedTime.Add(-9 * time.Minute)
	runningRun.StartedAt = &startedAt
	require.NoError(t, store.CreateRun(ctx, runningRun))

	// Create a successful run (should not be affected)
	successRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindWeekly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-1 * time.Hour),
	}
	completedAt := fixedTime.Add(-55 * time.Minute)
	successRun.CompletedAt = &completedAt
	require.NoError(t, store.CreateRun(ctx, successRun))

	// Verify incomplete runs exist
	incompleteRuns, err := store.FindIncompleteRuns(ctx)
	require.NoError(t, err)
	require.Len(t, incompleteRuns, 2, "should find 2 incomplete runs")

	// Run recovery
	err = svc.recoverIncompleteRuns(ctx)
	require.NoError(t, err)

	// Check that pending run is now failed
	recoveredPending, err := store.GetRun(ctx, pendingRun.ID)
	require.NoError(t, err)
	require.Equal(t, models.BackupRunStatusFailed, recoveredPending.Status)
	require.NotNil(t, recoveredPending.CompletedAt)
	require.Equal(t, fixedTime, *recoveredPending.CompletedAt)
	require.NotNil(t, recoveredPending.ErrorMessage)
	require.Equal(t, "Backup interrupted by application restart", *recoveredPending.ErrorMessage)

	// Check that running run is now failed
	recoveredRunning, err := store.GetRun(ctx, runningRun.ID)
	require.NoError(t, err)
	require.Equal(t, models.BackupRunStatusFailed, recoveredRunning.Status)
	require.NotNil(t, recoveredRunning.CompletedAt)
	require.Equal(t, fixedTime, *recoveredRunning.CompletedAt)
	require.NotNil(t, recoveredRunning.ErrorMessage)
	require.Equal(t, "Backup interrupted by application restart", *recoveredRunning.ErrorMessage)

	// Check that successful run is unchanged
	unchangedSuccess, err := store.GetRun(ctx, successRun.ID)
	require.NoError(t, err)
	require.Equal(t, models.BackupRunStatusSuccess, unchangedSuccess.Status)

	// Verify no incomplete runs remain
	remainingIncomplete, err := store.FindIncompleteRuns(ctx)
	require.NoError(t, err)
	require.Len(t, remainingIncomplete, 0, "should have no incomplete runs after recovery")
}

func TestCheckMissedBackups(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	// Enable all backup kinds
	settings := &models.BackupSettings{
		InstanceID:     instanceID,
		Enabled:        true,
		HourlyEnabled:  true,
		DailyEnabled:   true,
		WeeklyEnabled:  true,
		MonthlyEnabled: true,
		KeepHourly:     1,
		KeepDaily:      1,
		KeepWeekly:     1,
		KeepMonthly:    1,
	}
	require.NoError(t, store.UpsertSettings(ctx, settings))

	// Create successful runs that are now overdue
	hourlyRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-2 * time.Hour), // 2 hours ago, overdue
	}
	hourlyCompletedAt := fixedTime.Add(-2 * time.Hour)
	hourlyRun.CompletedAt = &hourlyCompletedAt
	require.NoError(t, store.CreateRun(ctx, hourlyRun))

	dailyRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindDaily,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-23 * time.Hour), // 23 hours ago, not overdue
	}
	dailyCompletedAt := fixedTime.Add(-23 * time.Hour)
	dailyRun.CompletedAt = &dailyCompletedAt
	require.NoError(t, store.CreateRun(ctx, dailyRun))

	// Weekly and monthly are not overdue yet
	weeklyRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindWeekly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-6 * 24 * time.Hour), // 6 days ago, not overdue
	}
	weeklyCompletedAt := fixedTime.Add(-6 * 24 * time.Hour)
	weeklyRun.CompletedAt = &weeklyCompletedAt
	require.NoError(t, store.CreateRun(ctx, weeklyRun))

	monthlyRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindMonthly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.AddDate(0, 0, -20), // 20 days ago, not overdue
	}
	monthlyCompletedAt := fixedTime.AddDate(0, 0, -20)
	monthlyRun.CompletedAt = &monthlyCompletedAt
	require.NoError(t, store.CreateRun(ctx, monthlyRun))

	// Run checkMissedBackups
	err := svc.checkMissedBackups(ctx)
	require.NoError(t, err)

	// Check that exactly one new run was queued
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM instance_backup_runs_view WHERE requested_by = 'startup-recovery'").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Check the kind of the queued run
	var kind string
	err = db.QueryRowContext(ctx, "SELECT kind FROM instance_backup_runs_view WHERE requested_by = 'startup-recovery'").Scan(&kind)
	require.NoError(t, err)
	require.Equal(t, string(models.BackupRunKindHourly), kind)
}

func TestCheckMissedBackupsMultipleMissed(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	// Enable all backup kinds
	settings := &models.BackupSettings{
		InstanceID:     instanceID,
		Enabled:        true,
		HourlyEnabled:  true,
		DailyEnabled:   true,
		WeeklyEnabled:  true,
		MonthlyEnabled: true,
		KeepHourly:     1,
		KeepDaily:      1,
		KeepWeekly:     1,
		KeepMonthly:    1,
	}
	require.NoError(t, store.UpsertSettings(ctx, settings))

	// Create successful runs that are all overdue
	hourlyRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-2 * time.Hour),
	}
	hourlyCompletedAt := fixedTime.Add(-2 * time.Hour)
	hourlyRun.CompletedAt = &hourlyCompletedAt
	require.NoError(t, store.CreateRun(ctx, hourlyRun))

	dailyRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindDaily,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-25 * time.Hour),
	}
	dailyCompletedAt := fixedTime.Add(-25 * time.Hour)
	dailyRun.CompletedAt = &dailyCompletedAt
	require.NoError(t, store.CreateRun(ctx, dailyRun))

	// Run checkMissedBackups
	err := svc.checkMissedBackups(ctx)
	require.NoError(t, err)

	// Should queue the first missed backup even when multiple are missed
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM instance_backup_runs_view WHERE requested_by = 'startup-recovery'").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Check the kind of the queued run (should be the first missed one, which is hourly)
	var kind string
	err = db.QueryRowContext(ctx, "SELECT kind FROM instance_backup_runs_view WHERE requested_by = 'startup-recovery'").Scan(&kind)
	require.NoError(t, err)
	require.Equal(t, string(models.BackupRunKindHourly), kind)
}

func TestCheckMissedBackupsNoneMissed(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	// Enable all backup kinds
	settings := &models.BackupSettings{
		InstanceID:     instanceID,
		Enabled:        true,
		HourlyEnabled:  true,
		DailyEnabled:   true,
		WeeklyEnabled:  true,
		MonthlyEnabled: true,
		KeepHourly:     1,
		KeepDaily:      1,
		KeepWeekly:     1,
		KeepMonthly:    1,
	}
	require.NoError(t, store.UpsertSettings(ctx, settings))

	// Create successful runs that are recent (not overdue)
	hourlyRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-30 * time.Minute), // 30 minutes ago, not overdue
	}
	hourlyCompletedAt := fixedTime.Add(-30 * time.Minute)
	hourlyRun.CompletedAt = &hourlyCompletedAt
	require.NoError(t, store.CreateRun(ctx, hourlyRun))

	dailyRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindDaily,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-2 * time.Hour), // 2 hours ago, not overdue for daily
	}
	dailyCompletedAt := fixedTime.Add(-2 * time.Hour)
	dailyRun.CompletedAt = &dailyCompletedAt
	require.NoError(t, store.CreateRun(ctx, dailyRun))

	weeklyRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindWeekly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-3 * 24 * time.Hour), // 3 days ago, not overdue for weekly
	}
	weeklyCompletedAt := fixedTime.Add(-3 * 24 * time.Hour)
	weeklyRun.CompletedAt = &weeklyCompletedAt
	require.NoError(t, store.CreateRun(ctx, weeklyRun))

	monthlyRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindMonthly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.AddDate(0, 0, -10), // 10 days ago, not overdue for monthly
	}
	monthlyCompletedAt := fixedTime.AddDate(0, 0, -10)
	monthlyRun.CompletedAt = &monthlyCompletedAt
	require.NoError(t, store.CreateRun(ctx, monthlyRun))

	// Run checkMissedBackups
	err := svc.checkMissedBackups(ctx)
	require.NoError(t, err)

	// Should not queue any backups since none are missed
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM instance_backup_runs_view WHERE requested_by = 'startup-recovery'").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestCheckMissedBackupsFirstRun(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	// Enable all backup kinds
	settings := &models.BackupSettings{
		InstanceID:     instanceID,
		Enabled:        true,
		HourlyEnabled:  true,
		DailyEnabled:   true,
		WeeklyEnabled:  true,
		MonthlyEnabled: true,
		KeepHourly:     1,
		KeepDaily:      1,
		KeepWeekly:     1,
		KeepMonthly:    1,
	}
	require.NoError(t, store.UpsertSettings(ctx, settings))

	// No previous runs exist - this is the first time qui is running

	// Run checkMissedBackups
	err := svc.checkMissedBackups(ctx)
	require.NoError(t, err)

	// Should queue the first backup (hourly) since no previous runs exist
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM instance_backup_runs_view WHERE requested_by = 'startup-recovery'").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Check the kind of the queued run (should be hourly as the first in the order)
	var kind string
	err = db.QueryRowContext(ctx, "SELECT kind FROM instance_backup_runs_view WHERE requested_by = 'startup-recovery'").Scan(&kind)
	require.NoError(t, err)
	require.Equal(t, string(models.BackupRunKindHourly), kind)
}

func TestIsBackupMissedIgnoresFailedRuns(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	// Create a successful run 30 minutes ago (not overdue)
	successRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-30 * time.Minute),
	}
	successCompletedAt := fixedTime.Add(-30 * time.Minute)
	successRun.CompletedAt = &successCompletedAt
	require.NoError(t, store.CreateRun(ctx, successRun))

	// Create a failed run 10 minutes ago (should be ignored)
	failedRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusFailed,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-10 * time.Minute),
	}
	failedCompletedAt := fixedTime.Add(-10 * time.Minute)
	failedRun.CompletedAt = &failedCompletedAt
	require.NoError(t, store.CreateRun(ctx, failedRun))

	// Should not be missed because the successful run is recent
	missed := svc.isBackupMissed(ctx, instanceID, models.BackupRunKindHourly, true, fixedTime)
	require.False(t, missed)
}

func TestIsBackupMissedFailedRunsOnly(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	// Create only failed runs (no successful runs)
	failedRun1 := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusFailed,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-2 * time.Hour),
	}
	failedCompletedAt1 := fixedTime.Add(-2 * time.Hour)
	failedRun1.CompletedAt = &failedCompletedAt1
	require.NoError(t, store.CreateRun(ctx, failedRun1))

	failedRun2 := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusFailed,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-1 * time.Hour),
	}
	failedCompletedAt2 := fixedTime.Add(-1 * time.Hour)
	failedRun2.CompletedAt = &failedCompletedAt2
	require.NoError(t, store.CreateRun(ctx, failedRun2))

	// Should be missed because there are no successful runs (treated as first run)
	missed := svc.isBackupMissed(ctx, instanceID, models.BackupRunKindHourly, true, fixedTime)
	require.True(t, missed)
}

func TestIsBackupMissedMixedStatusRuns(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	// Create a successful run 30 minutes ago (not overdue)
	successRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-30 * time.Minute),
	}
	successCompletedAt := fixedTime.Add(-30 * time.Minute)
	successRun.CompletedAt = &successCompletedAt
	require.NoError(t, store.CreateRun(ctx, successRun))

	// Create various non-successful runs after the successful one
	runningRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusRunning,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-20 * time.Minute),
	}
	runningStartedAt := fixedTime.Add(-20 * time.Minute)
	runningRun.StartedAt = &runningStartedAt
	require.NoError(t, store.CreateRun(ctx, runningRun))

	pendingRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusPending,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-15 * time.Minute),
	}
	require.NoError(t, store.CreateRun(ctx, pendingRun))

	failedRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusFailed,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-10 * time.Minute),
	}
	failedCompletedAt := fixedTime.Add(-10 * time.Minute)
	failedRun.CompletedAt = &failedCompletedAt
	require.NoError(t, store.CreateRun(ctx, failedRun))

	// Should not be missed because the successful run is recent, ignoring all the non-successful runs
	missed := svc.isBackupMissed(ctx, instanceID, models.BackupRunKindHourly, true, fixedTime)
	require.False(t, missed)
}

func TestIsBackupMissedOverdueWithFailedRunsAfterSuccess(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	// Create a successful run 2 hours ago (overdue for hourly)
	successRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-2 * time.Hour),
	}
	successCompletedAt := fixedTime.Add(-2 * time.Hour)
	successRun.CompletedAt = &successCompletedAt
	require.NoError(t, store.CreateRun(ctx, successRun))

	// Create failed runs after the successful one (should be ignored)
	failedRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusFailed,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-30 * time.Minute),
	}
	failedCompletedAt := fixedTime.Add(-30 * time.Minute)
	failedRun.CompletedAt = &failedCompletedAt
	require.NoError(t, store.CreateRun(ctx, failedRun))

	// Should be missed because the successful run is overdue, even though there are failed runs after it
	missed := svc.isBackupMissed(ctx, instanceID, models.BackupRunKindHourly, true, fixedTime)
	require.True(t, missed)
}

func TestIsBackupMissedPendingRunBlocksScheduling(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	pendingRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusPending,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-2 * time.Hour),
	}
	require.NoError(t, store.CreateRun(ctx, pendingRun))

	missed := svc.isBackupMissed(ctx, instanceID, models.BackupRunKindHourly, true, fixedTime)
	require.False(t, missed)
}

func TestIsBackupMissedRunningRunBlocksScheduling(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	runningRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusRunning,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-2 * time.Hour),
	}
	startedAt := fixedTime.Add(-2 * time.Hour)
	runningRun.StartedAt = &startedAt
	require.NoError(t, store.CreateRun(ctx, runningRun))

	missed := svc.isBackupMissed(ctx, instanceID, models.BackupRunKindHourly, true, fixedTime)
	require.False(t, missed)
}

func TestIsBackupMissedCanceledRunWithinCooldownBlocksScheduling(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1, FailureCooldown: 10 * time.Minute}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	canceledRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusCanceled,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-5 * time.Minute),
	}
	require.NoError(t, store.CreateRun(ctx, canceledRun))

	missed := svc.isBackupMissed(ctx, instanceID, models.BackupRunKindHourly, true, fixedTime)
	require.False(t, missed)
}

func TestIsBackupMissedFailedRunOutsideCooldownIsMissed(t *testing.T) {
	db := setupTestBackupDB(t)

	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "test-instance")

	store := models.NewBackupStore(db)
	svc := NewService(store, nil, nil, Config{WorkerCount: 1, FailureCooldown: 10 * time.Minute}, nil)
	fixedTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedTime }

	failedRun := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusFailed,
		RequestedBy: "scheduler",
		RequestedAt: fixedTime.Add(-30 * time.Minute),
	}
	failedCompletedAt := fixedTime.Add(-30 * time.Minute)
	failedRun.CompletedAt = &failedCompletedAt
	require.NoError(t, store.CreateRun(ctx, failedRun))

	missed := svc.isBackupMissed(ctx, instanceID, models.BackupRunKindHourly, true, fixedTime)
	require.True(t, missed)
}

func TestWaitForExportThrottleNoopWithoutThrottle(t *testing.T) {
	require.NoError(t, waitForExportThrottle(context.Background(), nil))
}

func TestWaitForExportThrottleReturnsOnTick(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	throttle := make(chan time.Time, 1)
	throttle <- time.Now()

	require.NoError(t, waitForExportThrottle(ctx, throttle))
}

func TestWaitForExportThrottleRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	throttle := make(chan time.Time)

	err := waitForExportThrottle(ctx, throttle)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
