// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/autobrr/qui/internal/dbinterface"
)

type capturingTx struct {
	tx         *sql.Tx
	onExec     func(query string, args []any)
	onQueryRow func(query string, args []any)
}

func (t *capturingTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if t.onExec != nil {
		t.onExec(query, args)
	}
	return t.tx.ExecContext(ctx, query, args...)
}

func (t *capturingTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}

func (t *capturingTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if t.onQueryRow != nil {
		t.onQueryRow(query, args)
	}
	return t.tx.QueryRowContext(ctx, query, args...)
}

func (t *capturingTx) Commit() error {
	return t.tx.Commit()
}

func (t *capturingTx) Rollback() error {
	return t.tx.Rollback()
}

type capturingQuerier struct {
	db        *sql.DB
	onExec    func(query string, args []any)
	onBeginTx func(tx *capturingTx)
}

func (q *capturingQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return q.db.QueryRowContext(ctx, query, args...)
}

func (q *capturingQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if q.onExec != nil {
		q.onExec(query, args)
	}
	return q.db.ExecContext(ctx, query, args...)
}

func (q *capturingQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return q.db.QueryContext(ctx, query, args...)
}

func (q *capturingQuerier) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := q.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	capturing := &capturingTx{
		tx:     tx,
		onExec: q.onExec,
	}
	if q.onBeginTx != nil {
		q.onBeginTx(capturing)
	}
	return capturing, nil
}

func openSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), query, args...)
	require.NoError(t, err)
}

func TestInstanceCreateUsesIntegerBooleanArgs(t *testing.T) {
	db := openSQLiteDB(t)

	mustExec(t, db, `
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	mustExec(t, db, `
		CREATE TABLE instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name_id INTEGER NOT NULL,
			host_id INTEGER NOT NULL,
			username_id INTEGER NOT NULL,
			password_encrypted TEXT NOT NULL,
			basic_username_id INTEGER,
			basic_password_encrypted TEXT,
			tls_skip_verify INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			has_local_filesystem_access INTEGER NOT NULL DEFAULT 0
		)
	`)

	var insertArgs []any
	q := &capturingQuerier{
		db: db,
		onBeginTx: func(tx *capturingTx) {
			tx.onQueryRow = func(query string, args []any) {
				if strings.Contains(query, "INSERT INTO instances") {
					insertArgs = append([]any(nil), args...)
				}
			}
		},
	}

	store, err := NewInstanceStore(q, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	ctx := context.Background()
	hasLocalAccess := true
	_, err = store.Create(ctx, "main", "http://localhost:8080", "admin", "secret", nil, nil, true, &hasLocalAccess)
	require.NoError(t, err)
	require.Len(t, insertArgs, 8)

	tlsArg, ok := insertArgs[6].(int)
	require.Truef(t, ok, "expected int arg for tls_skip_verify, got %T", insertArgs[6])
	require.Equal(t, 1, tlsArg)

	localAccessArg, ok := insertArgs[7].(int)
	require.Truef(t, ok, "expected int arg for has_local_filesystem_access, got %T", insertArgs[7])
	require.Equal(t, 1, localAccessArg)
}

func TestTorznabCreateUsesIntegerEnabledArg(t *testing.T) {
	db := openSQLiteDB(t)

	mustExec(t, db, `
		CREATE TABLE string_pool (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			value TEXT NOT NULL UNIQUE
		)
	`)
	mustExec(t, db, `
		CREATE TABLE torznab_indexers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name_id INTEGER NOT NULL,
			base_url_id INTEGER NOT NULL,
			indexer_id_string_id INTEGER,
			api_key_encrypted TEXT NOT NULL,
			backend TEXT NOT NULL DEFAULT 'jackett',
			enabled INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 0,
			timeout_seconds INTEGER NOT NULL DEFAULT 30,
			limit_default INTEGER NOT NULL DEFAULT 100,
			limit_max INTEGER NOT NULL DEFAULT 100,
			basic_username_id INTEGER,
			basic_password_encrypted TEXT,
			last_test_at TIMESTAMP,
			last_test_status TEXT DEFAULT 'unknown',
			last_test_error TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	mustExec(t, db, `
		CREATE VIEW torznab_indexers_view AS
		SELECT
			ti.id,
			sp_name.value AS name,
			sp_base.value AS base_url,
			sp_indexer.value AS indexer_id,
			sp_basic_user.value AS basic_username,
			ti.basic_password_encrypted,
			ti.backend,
			ti.api_key_encrypted,
			ti.enabled,
			ti.priority,
			ti.timeout_seconds,
			ti.limit_default,
			ti.limit_max,
			ti.last_test_at,
			ti.last_test_status,
			ti.last_test_error,
			ti.created_at,
			ti.updated_at
		FROM torznab_indexers ti
		JOIN string_pool sp_name ON ti.name_id = sp_name.id
		JOIN string_pool sp_base ON ti.base_url_id = sp_base.id
		LEFT JOIN string_pool sp_indexer ON ti.indexer_id_string_id = sp_indexer.id
		LEFT JOIN string_pool sp_basic_user ON ti.basic_username_id = sp_basic_user.id
	`)
	mustExec(t, db, `
		CREATE TABLE torznab_indexer_capabilities_view (
			indexer_id INTEGER NOT NULL,
			capability_type TEXT NOT NULL
		)
	`)
	mustExec(t, db, `
		CREATE TABLE torznab_indexer_categories_view (
			indexer_id INTEGER NOT NULL,
			category_id INTEGER NOT NULL,
			category_name TEXT NOT NULL,
			parent_category_id INTEGER
		)
	`)

	var insertArgs []any
	q := &capturingQuerier{
		db: db,
		onBeginTx: func(tx *capturingTx) {
			tx.onQueryRow = func(query string, args []any) {
				if strings.Contains(query, "INSERT INTO torznab_indexers") {
					insertArgs = append([]any(nil), args...)
				}
			}
		},
	}

	store, err := NewTorznabIndexerStore(q, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	ctx := context.Background()
	_, err = store.Create(ctx, "Aither", "https://indexer.example", "apikey", nil, nil, true, 10, 30)
	require.NoError(t, err)
	require.Len(t, insertArgs, 12)

	enabledArg, ok := insertArgs[7].(int)
	require.Truef(t, ok, "expected int arg for enabled, got %T", insertArgs[7])
	require.Equal(t, 1, enabledArg)
}

func TestArrIDCacheSetUsesIntegerNegativeArg(t *testing.T) {
	db := openSQLiteDB(t)

	mustExec(t, db, `
		CREATE TABLE arr_id_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title_hash TEXT NOT NULL,
			content_type TEXT NOT NULL,
			arr_instance_id INTEGER,
			imdb_id TEXT,
			tmdb_id INTEGER,
			tvdb_id INTEGER,
			tvmaze_id INTEGER,
			is_negative INTEGER DEFAULT 0,
			cached_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NOT NULL,
			UNIQUE(title_hash, content_type)
		)
	`)

	var insertArgs []any
	q := &capturingQuerier{
		db: db,
		onExec: func(query string, args []any) {
			if strings.Contains(query, "INSERT INTO arr_id_cache") {
				insertArgs = append([]any(nil), args...)
			}
		},
	}

	store := NewArrIDCacheStore(q)
	err := store.Set(context.Background(), "title-hash", "movie", nil, nil, false, time.Hour)
	require.NoError(t, err)
	require.Len(t, insertArgs, 9)

	isNegativeArg, ok := insertArgs[7].(int)
	require.Truef(t, ok, "expected int arg for is_negative, got %T", insertArgs[7])
	require.Equal(t, 0, isNegativeArg)
}

func TestBackupUpsertSettingsUsesIntegerBooleanArgs(t *testing.T) {
	db := openSQLiteDB(t)

	mustExec(t, db, `
		CREATE TABLE instance_backup_settings (
			instance_id INTEGER PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 0,
			hourly_enabled INTEGER NOT NULL DEFAULT 0,
			daily_enabled INTEGER NOT NULL DEFAULT 0,
			weekly_enabled INTEGER NOT NULL DEFAULT 0,
			monthly_enabled INTEGER NOT NULL DEFAULT 0,
			keep_hourly INTEGER NOT NULL DEFAULT 0,
			keep_daily INTEGER NOT NULL DEFAULT 7,
			keep_weekly INTEGER NOT NULL DEFAULT 4,
			keep_monthly INTEGER NOT NULL DEFAULT 12,
			include_categories INTEGER NOT NULL DEFAULT 1,
			include_tags INTEGER NOT NULL DEFAULT 1,
			custom_path TEXT
		)
	`)

	var insertArgs []any
	q := &capturingQuerier{
		db: db,
		onBeginTx: func(tx *capturingTx) {
			tx.onExec = func(query string, args []any) {
				if strings.Contains(query, "INSERT INTO instance_backup_settings") {
					insertArgs = append([]any(nil), args...)
				}
			}
		},
	}

	store := NewBackupStore(q)
	settings := &BackupSettings{
		InstanceID:        1,
		Enabled:           true,
		HourlyEnabled:     false,
		DailyEnabled:      true,
		WeeklyEnabled:     false,
		MonthlyEnabled:    true,
		KeepHourly:        0,
		KeepDaily:         5,
		KeepWeekly:        2,
		KeepMonthly:       3,
		IncludeCategories: true,
		IncludeTags:       false,
	}

	err := store.UpsertSettings(context.Background(), settings)
	require.NoError(t, err)
	require.Len(t, insertArgs, 13)

	boolIndexes := []int{1, 2, 3, 4, 5, 10, 11}
	for _, idx := range boolIndexes {
		_, ok := insertArgs[idx].(int)
		require.Truef(t, ok, "expected int arg at index %d, got %T", idx, insertArgs[idx])
	}
}

func TestCrossSeedUpsertSettingsUsesIntegerBooleanArgs(t *testing.T) {
	db := openSQLiteDB(t)

	mustExec(t, db, `
		CREATE TABLE cross_seed_settings (
			id INTEGER PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 0,
			run_interval_minutes INTEGER NOT NULL DEFAULT 120,
			start_paused INTEGER NOT NULL DEFAULT 1,
			category TEXT,
			target_instance_ids TEXT NOT NULL DEFAULT '[]',
			target_indexer_ids TEXT NOT NULL DEFAULT '[]',
			max_results_per_run INTEGER NOT NULL DEFAULT 50,
			rss_source_categories TEXT NOT NULL DEFAULT '[]',
			rss_source_tags TEXT NOT NULL DEFAULT '[]',
			rss_source_exclude_categories TEXT NOT NULL DEFAULT '[]',
			rss_source_exclude_tags TEXT NOT NULL DEFAULT '[]',
			webhook_source_categories TEXT NOT NULL DEFAULT '[]',
			webhook_source_tags TEXT NOT NULL DEFAULT '[]',
			webhook_source_exclude_categories TEXT NOT NULL DEFAULT '[]',
			webhook_source_exclude_tags TEXT NOT NULL DEFAULT '[]',
			find_individual_episodes INTEGER NOT NULL DEFAULT 0,
			size_mismatch_tolerance_percent REAL NOT NULL DEFAULT 5.0,
			use_category_from_indexer INTEGER NOT NULL DEFAULT 0,
			run_external_program_id INTEGER,
			rss_automation_tags TEXT NOT NULL DEFAULT '["cross-seed"]',
			seeded_search_tags TEXT NOT NULL DEFAULT '["cross-seed"]',
			completion_search_tags TEXT NOT NULL DEFAULT '["cross-seed"]',
			webhook_tags TEXT NOT NULL DEFAULT '["cross-seed"]',
			inherit_source_tags INTEGER NOT NULL DEFAULT 0,
			use_cross_category_affix INTEGER NOT NULL DEFAULT 1,
			category_affix_mode TEXT NOT NULL DEFAULT 'suffix',
			category_affix TEXT NOT NULL DEFAULT '.cross',
			use_custom_category INTEGER NOT NULL DEFAULT 0,
			custom_category TEXT NOT NULL DEFAULT '',
			skip_auto_resume_rss INTEGER NOT NULL DEFAULT 0,
			skip_auto_resume_seeded_search INTEGER NOT NULL DEFAULT 0,
			skip_auto_resume_completion INTEGER NOT NULL DEFAULT 0,
			skip_auto_resume_webhook INTEGER NOT NULL DEFAULT 0,
			skip_recheck INTEGER NOT NULL DEFAULT 0,
			skip_piece_boundary_safety_check INTEGER NOT NULL DEFAULT 1,
			gazelle_enabled INTEGER NOT NULL DEFAULT 0,
			redacted_api_key_encrypted TEXT NOT NULL DEFAULT '',
			orpheus_api_key_encrypted TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)

	var insertArgs []any
	q := &capturingQuerier{
		db: db,
		onExec: func(query string, args []any) {
			if strings.Contains(query, "INSERT INTO cross_seed_settings") {
				insertArgs = append([]any(nil), args...)
			}
		},
	}

	store, err := NewCrossSeedStore(q, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	settings := DefaultCrossSeedAutomationSettings()
	settings.RedactedAPIKey = "redacted-key"
	settings.OrpheusAPIKey = "orpheus-key"

	_, err = store.UpsertSettings(context.Background(), settings)
	require.NoError(t, err)
	require.Len(t, insertArgs, 39)

	boolIndexes := []int{1, 3, 16, 18, 24, 25, 28, 30, 31, 32, 33, 34, 35, 36}
	for _, idx := range boolIndexes {
		_, ok := insertArgs[idx].(int)
		require.Truef(t, ok, "expected int arg at index %d, got %T", idx, insertArgs[idx])
	}
}

func TestAutomationReadsIntegerBooleanColumns(t *testing.T) {
	db := openSQLiteDB(t)
	mustExec(t, db, `
		CREATE TABLE automations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			tracker_pattern TEXT NOT NULL,
			conditions TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			dry_run INTEGER NOT NULL DEFAULT 0,
			notify INTEGER NOT NULL DEFAULT 1,
			sort_order INTEGER NOT NULL DEFAULT 0,
			interval_seconds INTEGER,
			free_space_source TEXT,
			sorting_config TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	mustExec(t, db, `
		INSERT INTO automations (instance_id, name, tracker_pattern, conditions, enabled, dry_run, notify, sort_order)
		VALUES (1, 'Auto rule', 'tracker.example', '{}', 1, 0, 0, 1)
	`)

	store := NewAutomationStore(&capturingQuerier{db: db})
	items, err := store.ListByInstance(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.True(t, items[0].Enabled)
	require.False(t, items[0].DryRun)
	require.False(t, items[0].Notify)

	item, err := store.Get(context.Background(), 1, items[0].ID)
	require.NoError(t, err)
	require.True(t, item.Enabled)
	require.False(t, item.DryRun)
	require.False(t, item.Notify)
}

func TestOrphanScanReadsIntegerBooleanColumns(t *testing.T) {
	db := openSQLiteDB(t)
	mustExec(t, db, `
		CREATE TABLE orphan_scan_settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id INTEGER NOT NULL UNIQUE,
			enabled INTEGER NOT NULL DEFAULT 0,
			grace_period_minutes INTEGER NOT NULL DEFAULT 60,
			ignore_paths TEXT NOT NULL DEFAULT '[]',
			scan_interval_hours INTEGER NOT NULL DEFAULT 12,
			preview_sort TEXT NOT NULL DEFAULT 'modified_desc',
			max_files_per_run INTEGER NOT NULL DEFAULT 0,
			auto_cleanup_enabled INTEGER NOT NULL DEFAULT 0,
			auto_cleanup_max_files INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	mustExec(t, db, `
		CREATE TABLE orphan_scan_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			triggered_by TEXT NOT NULL,
			scan_paths TEXT NOT NULL DEFAULT '[]',
			files_found INTEGER NOT NULL DEFAULT 0,
			files_deleted INTEGER NOT NULL DEFAULT 0,
			folders_deleted INTEGER NOT NULL DEFAULT 0,
			bytes_reclaimed INTEGER NOT NULL DEFAULT 0,
			truncated INTEGER NOT NULL DEFAULT 0,
			error_message TEXT,
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			completed_at TIMESTAMP
		)
	`)
	mustExec(t, db, `
		INSERT INTO orphan_scan_settings
			(instance_id, enabled, grace_period_minutes, ignore_paths, scan_interval_hours, preview_sort, max_files_per_run, auto_cleanup_enabled, auto_cleanup_max_files)
		VALUES
			(1, 1, 120, '[]', 24, 'modified_desc', 100, 1, 25)
	`)
	mustExec(t, db, `
		INSERT INTO orphan_scan_runs
			(instance_id, status, triggered_by, scan_paths, files_found, files_deleted, folders_deleted, bytes_reclaimed, truncated)
		VALUES
			(1, 'completed', 'manual', '[]', 10, 5, 1, 1024, 1)
	`)

	store := NewOrphanScanStore(&capturingQuerier{db: db})
	settings, err := store.GetSettings(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, settings.Enabled)
	require.True(t, settings.AutoCleanupEnabled)

	run, err := store.GetRun(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, run.Truncated)
}

func TestDirScanReadsIntegerBooleanColumns(t *testing.T) {
	db := openSQLiteDB(t)
	mustExec(t, db, `
		CREATE TABLE dir_scan_settings (
			id INTEGER PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 0,
			match_mode TEXT NOT NULL DEFAULT 'strict',
			size_tolerance_percent REAL NOT NULL DEFAULT 5.0,
			min_piece_ratio REAL NOT NULL DEFAULT 0.80,
			max_searchees_per_run INTEGER NOT NULL DEFAULT 5000,
			max_searchee_age_days INTEGER NOT NULL DEFAULT 3650,
			allow_partial INTEGER NOT NULL DEFAULT 0,
			skip_piece_boundary_safety_check INTEGER NOT NULL DEFAULT 0,
			start_paused INTEGER NOT NULL DEFAULT 0,
			download_missing_files INTEGER NOT NULL DEFAULT 1,
			category TEXT,
			tags TEXT NOT NULL DEFAULT '[]',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	mustExec(t, db, `
		CREATE TABLE dir_scan_directories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL UNIQUE,
			qbit_path_prefix TEXT,
			category TEXT,
			tags TEXT NOT NULL DEFAULT '[]',
			enabled INTEGER NOT NULL DEFAULT 1,
			arr_instance_id INTEGER,
			target_instance_id INTEGER NOT NULL,
			scan_interval_minutes INTEGER NOT NULL DEFAULT 60,
			last_scan_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	mustExec(t, db, `
		INSERT INTO dir_scan_settings
			(id, enabled, match_mode, size_tolerance_percent, min_piece_ratio, max_searchees_per_run, max_searchee_age_days, allow_partial, skip_piece_boundary_safety_check, start_paused, download_missing_files, category, tags)
		VALUES
			(1, 1, 'strict', 5.0, 0.85, 500, 365, 1, 1, 0, 1, 'movies', '["tag1"]')
	`)
	mustExec(t, db, `
		INSERT INTO dir_scan_directories
			(path, qbit_path_prefix, category, tags, enabled, arr_instance_id, target_instance_id, scan_interval_minutes)
		VALUES
			('/data/incoming', '/data', 'movies', '["tag1"]', 1, NULL, 1, 30)
	`)

	store := NewDirScanStore(&capturingQuerier{db: db})
	settings, err := store.GetSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.Enabled)
	require.True(t, settings.AllowPartial)
	require.True(t, settings.SkipPieceBoundarySafetyCheck)
	require.False(t, settings.StartPaused)
	require.True(t, settings.DownloadMissingFiles)

	dir, err := store.GetDirectory(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, dir.Enabled)
}
