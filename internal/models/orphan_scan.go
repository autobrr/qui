// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// OrphanScanSettings represents orphan scan settings for an instance.
type OrphanScanSettings struct {
	ID                  int64     `json:"id"`
	InstanceID          int       `json:"instanceId"`
	Enabled             bool      `json:"enabled"`
	GracePeriodMinutes  int       `json:"gracePeriodMinutes"`
	IgnorePaths         []string  `json:"ignorePaths"`
	ScanIntervalHours   int       `json:"scanIntervalHours"`
	PreviewSort         string    `json:"previewSort"`
	MaxFilesPerRun      int       `json:"maxFilesPerRun"`
	AutoCleanupEnabled  bool      `json:"autoCleanupEnabled"`
	AutoCleanupMaxFiles int       `json:"autoCleanupMaxFiles"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// OrphanScanRun represents an orphan scan run.
type OrphanScanRun struct {
	ID             int64      `json:"id"`
	InstanceID     int        `json:"instanceId"`
	Status         string     `json:"status"` // pending, scanning, preview_ready, deleting, completed, failed, canceled
	TriggeredBy    string     `json:"triggeredBy"`
	ScanPaths      []string   `json:"scanPaths"`
	FilesFound     int        `json:"filesFound"`
	FilesDeleted   int        `json:"filesDeleted"`
	FoldersDeleted int        `json:"foldersDeleted"`
	BytesReclaimed int64      `json:"bytesReclaimed"`
	Truncated      bool       `json:"truncated"`
	ErrorMessage   string     `json:"errorMessage,omitempty"`
	StartedAt      time.Time  `json:"startedAt"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
}

// OrphanScanFile represents an orphan file found in a scan.
type OrphanScanFile struct {
	ID           int64      `json:"id"`
	RunID        int64      `json:"runId"`
	FilePath     string     `json:"filePath"`
	FileSize     int64      `json:"fileSize"`
	ModifiedAt   *time.Time `json:"modifiedAt,omitempty"`
	Status       string     `json:"status"` // pending, deleted, skipped, failed
	ErrorMessage string     `json:"errorMessage,omitempty"`
}

// OrphanScanStore handles database operations for orphan scan.
type OrphanScanStore struct {
	db dbinterface.Querier
}

// NewOrphanScanStore creates a new OrphanScanStore.
func NewOrphanScanStore(db dbinterface.Querier) *OrphanScanStore {
	return &OrphanScanStore{db: db}
}

// GetSettings retrieves orphan scan settings for an instance.
// Returns nil if no settings exist.
func (s *OrphanScanStore) GetSettings(ctx context.Context, instanceID int) (*OrphanScanSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, enabled, grace_period_minutes, ignore_paths,
		       scan_interval_hours, preview_sort, max_files_per_run, auto_cleanup_enabled,
		       auto_cleanup_max_files, created_at, updated_at
		FROM orphan_scan_settings
		WHERE instance_id = ?
	`, instanceID)

	var settings OrphanScanSettings
	var ignorePathsJSON sql.NullString

	err := row.Scan(
		&settings.ID,
		&settings.InstanceID,
		&settings.Enabled,
		&settings.GracePeriodMinutes,
		&ignorePathsJSON,
		&settings.ScanIntervalHours,
		&settings.PreviewSort,
		&settings.MaxFilesPerRun,
		&settings.AutoCleanupEnabled,
		&settings.AutoCleanupMaxFiles,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if ignorePathsJSON.Valid && ignorePathsJSON.String != "" {
		if err := json.Unmarshal([]byte(ignorePathsJSON.String), &settings.IgnorePaths); err != nil {
			return nil, err
		}
	}
	if settings.IgnorePaths == nil {
		settings.IgnorePaths = []string{}
	}

	return &settings, nil
}

// UpsertSettings creates or updates orphan scan settings for an instance.
func (s *OrphanScanStore) UpsertSettings(ctx context.Context, settings *OrphanScanSettings) (*OrphanScanSettings, error) {
	if settings == nil {
		return nil, errors.New("settings is nil")
	}

	ignorePathsJSON, err := json.Marshal(settings.IgnorePaths)
	if err != nil {
		return nil, err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO orphan_scan_settings
				(instance_id, enabled, grace_period_minutes, ignore_paths, scan_interval_hours,
				 preview_sort, max_files_per_run, auto_cleanup_enabled, auto_cleanup_max_files)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET
			enabled = excluded.enabled,
			grace_period_minutes = excluded.grace_period_minutes,
			ignore_paths = excluded.ignore_paths,
			scan_interval_hours = excluded.scan_interval_hours,
			preview_sort = excluded.preview_sort,
			max_files_per_run = excluded.max_files_per_run,
			auto_cleanup_enabled = excluded.auto_cleanup_enabled,
			auto_cleanup_max_files = excluded.auto_cleanup_max_files
	`, settings.InstanceID, boolToInt(settings.Enabled), settings.GracePeriodMinutes,
		string(ignorePathsJSON), settings.ScanIntervalHours, settings.PreviewSort, settings.MaxFilesPerRun,
		boolToInt(settings.AutoCleanupEnabled), settings.AutoCleanupMaxFiles)
	if err != nil {
		return nil, err
	}

	return s.GetSettings(ctx, settings.InstanceID)
}

// CreateRun creates a new orphan scan run.
func (s *OrphanScanStore) CreateRun(ctx context.Context, instanceID int, triggeredBy string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO orphan_scan_runs (instance_id, status, triggered_by)
		VALUES (?, 'pending', ?)
	`, instanceID, triggeredBy)
	if err != nil {
		return 0, fmt.Errorf("insert orphan scan run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return id, nil
}

// ErrRunAlreadyActive is returned when attempting to create a run while one is already active.
var ErrRunAlreadyActive = errors.New("an active run already exists for this instance")

// CreateRunIfNoActive atomically checks for active runs and creates a new one if none exist.
// This prevents race conditions between HasActiveRun and CreateRun.
func (s *OrphanScanStore) CreateRunIfNoActive(ctx context.Context, instanceID int, triggeredBy string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO orphan_scan_runs (instance_id, status, triggered_by)
		SELECT ?, 'pending', ?
		WHERE NOT EXISTS (
			SELECT 1 FROM orphan_scan_runs
			WHERE instance_id = ?
			  AND (status IN ('pending', 'scanning', 'deleting')
			       OR (status = 'preview_ready' AND files_found > 0))
		)
	`, instanceID, triggeredBy, instanceID)
	if err != nil {
		return 0, fmt.Errorf("insert orphan scan run: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return 0, ErrRunAlreadyActive
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return id, nil
}

// GetRun retrieves an orphan scan run by ID.
func (s *OrphanScanStore) GetRun(ctx context.Context, runID int64) (*OrphanScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at
		FROM orphan_scan_runs
		WHERE id = ?
	`, runID)

	return s.scanRun(row)
}

// GetRunByInstance retrieves a specific run for an instance.
func (s *OrphanScanStore) GetRunByInstance(ctx context.Context, instanceID int, runID int64) (*OrphanScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at
		FROM orphan_scan_runs
		WHERE id = ? AND instance_id = ?
	`, runID, instanceID)

	return s.scanRun(row)
}

func (s *OrphanScanStore) scanRun(row *sql.Row) (*OrphanScanRun, error) {
	var run OrphanScanRun
	var scanPathsJSON sql.NullString
	var errorMessage sql.NullString
	var completedAt sql.NullTime

	err := row.Scan(
		&run.ID,
		&run.InstanceID,
		&run.Status,
		&run.TriggeredBy,
		&scanPathsJSON,
		&run.FilesFound,
		&run.FilesDeleted,
		&run.FoldersDeleted,
		&run.BytesReclaimed,
		&run.Truncated,
		&errorMessage,
		&run.StartedAt,
		&completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := finalizeRun(&run, scanPathsJSON, errorMessage, completedAt); err != nil {
		return nil, err
	}

	return &run, nil
}

func finalizeRun(run *OrphanScanRun, scanPathsJSON, errorMessage sql.NullString, completedAt sql.NullTime) error {
	if scanPathsJSON.Valid && scanPathsJSON.String != "" {
		if err := json.Unmarshal([]byte(scanPathsJSON.String), &run.ScanPaths); err != nil {
			return fmt.Errorf("unmarshal scan paths: %w", err)
		}
	}
	if run.ScanPaths == nil {
		run.ScanPaths = []string{}
	}
	if errorMessage.Valid {
		run.ErrorMessage = errorMessage.String
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	return nil
}

func (s *OrphanScanStore) scanRunsFromRows(rows *sql.Rows) ([]*OrphanScanRun, error) {
	var runs []*OrphanScanRun
	for rows.Next() {
		var run OrphanScanRun
		var scanPathsJSON sql.NullString
		var errorMessage sql.NullString
		var completedAt sql.NullTime

		if err := rows.Scan(
			&run.ID,
			&run.InstanceID,
			&run.Status,
			&run.TriggeredBy,
			&scanPathsJSON,
			&run.FilesFound,
			&run.FilesDeleted,
			&run.FoldersDeleted,
			&run.BytesReclaimed,
			&run.Truncated,
			&errorMessage,
			&run.StartedAt,
			&completedAt,
		); err != nil {
			return nil, err
		}

		if err := finalizeRun(&run, scanPathsJSON, errorMessage, completedAt); err != nil {
			return nil, err
		}

		runs = append(runs, &run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return runs, nil
}

// ListRuns lists recent runs for an instance.
func (s *OrphanScanStore) ListRuns(ctx context.Context, instanceID, limit int) ([]*OrphanScanRun, error) {
	if limit <= 0 {
		limit = 10
	}

	recentRuns, err := s.listRunsRecent(ctx, instanceID, limit)
	if err != nil {
		return nil, err
	}
	activeRuns, err := s.listRunsActive(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	return mergeRuns(recentRuns, activeRuns, limit), nil
}

func (s *OrphanScanStore) listRunsRecent(ctx context.Context, instanceID, limit int) ([]*OrphanScanRun, error) {
	query := `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at
		FROM orphan_scan_runs
		WHERE instance_id = ?
		ORDER BY started_at DESC
		LIMIT ?
	`
	return s.scanRunsQuery(ctx, "query recent runs", query, instanceID, limit)
}

func (s *OrphanScanStore) listRunsActive(ctx context.Context, instanceID int) ([]*OrphanScanRun, error) {
	query := `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at
		FROM orphan_scan_runs
		WHERE instance_id = ?
		  AND (status IN ('pending', 'scanning', 'deleting')
		       OR (status = 'preview_ready' AND files_found > 0))
		ORDER BY started_at DESC
	`
	return s.scanRunsQuery(ctx, "query active runs", query, instanceID)
}

func (s *OrphanScanStore) scanRunsQuery(ctx context.Context, label, query string, args ...any) ([]*OrphanScanRun, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	defer rows.Close()

	return s.scanRunsFromRows(rows)
}

func mergeRuns(recentRuns, activeRuns []*OrphanScanRun, limit int) []*OrphanScanRun {
	activeIDs := make(map[int64]struct{}, len(activeRuns))
	byID := make(map[int64]*OrphanScanRun, len(recentRuns)+len(activeRuns))
	for _, r := range activeRuns {
		activeIDs[r.ID] = struct{}{}
		byID[r.ID] = r
	}
	for _, r := range recentRuns {
		if _, ok := byID[r.ID]; !ok {
			byID[r.ID] = r
		}
	}

	merged := make([]*OrphanScanRun, 0, len(byID))
	for _, r := range byID {
		merged = append(merged, r)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].StartedAt.Equal(merged[j].StartedAt) {
			return merged[i].ID > merged[j].ID
		}
		return merged[i].StartedAt.After(merged[j].StartedAt)
	})

	if len(activeIDs) == 0 {
		return limitRuns(merged, limit)
	}
	return limitNonActive(merged, activeIDs, limit)
}

func limitRuns(runs []*OrphanScanRun, limit int) []*OrphanScanRun {
	if len(runs) <= limit {
		return runs
	}
	return runs[:limit]
}

func limitNonActive(runs []*OrphanScanRun, activeIDs map[int64]struct{}, limit int) []*OrphanScanRun {
	out := make([]*OrphanScanRun, 0, len(runs))
	for _, r := range runs {
		if _, isActive := activeIDs[r.ID]; isActive {
			out = append(out, r)
			continue
		}
		if len(out) < limit {
			out = append(out, r)
		}
	}
	return out
}

// GetLastCompletedRun returns the last completed run for an instance.
func (s *OrphanScanStore) GetLastCompletedRun(ctx context.Context, instanceID int) (*OrphanScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at
		FROM orphan_scan_runs
		WHERE instance_id = ? AND status = 'completed'
		ORDER BY completed_at DESC
		LIMIT 1
	`, instanceID)

	return s.scanRun(row)
}

// HasActiveRun checks if there's an active run for an instance.
// Note: preview_ready with files_found=0 is excluded (legacy "clean" scans that should have been marked completed).
func (s *OrphanScanStore) HasActiveRun(ctx context.Context, instanceID int) (bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM orphan_scan_runs
		WHERE instance_id = ?
		  AND (status IN ('pending', 'scanning', 'deleting')
		       OR (status = 'preview_ready' AND files_found > 0))
	`, instanceID)

	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetMostRecentActiveRun returns the most recent active run for an instance.
// "Active" matches the same definition used by CreateRunIfNoActive.
func (s *OrphanScanStore) GetMostRecentActiveRun(ctx context.Context, instanceID int) (*OrphanScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at
		FROM orphan_scan_runs
		WHERE instance_id = ?
		  AND (status IN ('pending', 'scanning', 'deleting')
		       OR (status = 'preview_ready' AND files_found > 0))
		ORDER BY started_at DESC
		LIMIT 1
	`, instanceID)

	return s.scanRun(row)
}

// UpdateRunStatus updates the status of a run.
func (s *OrphanScanStore) UpdateRunStatus(ctx context.Context, runID int64, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs SET status = ? WHERE id = ?
	`, status, runID)
	return err
}

// UpdateRunScanPaths updates the scan paths for a run.
func (s *OrphanScanStore) UpdateRunScanPaths(ctx context.Context, runID int64, scanPaths []string) error {
	pathsJSON, err := json.Marshal(scanPaths)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs SET scan_paths = ? WHERE id = ?
	`, string(pathsJSON), runID)
	return err
}

// UpdateRunFilesFound updates the files found count and truncated flag.
func (s *OrphanScanStore) UpdateRunFilesFound(ctx context.Context, runID int64, filesFound int, truncated bool) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs SET files_found = ?, truncated = ? WHERE id = ?
	`, filesFound, boolToInt(truncated), runID)
	return err
}

// UpdateRunFoundStats updates the files found count, truncated flag, and preview bytes.
// bytesFound should represent the total size of orphan files found during the scan.
func (s *OrphanScanStore) UpdateRunFoundStats(ctx context.Context, runID int64, filesFound int, truncated bool, bytesFound int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET files_found = ?, truncated = ?, bytes_reclaimed = ?
		WHERE id = ?
	`, filesFound, boolToInt(truncated), bytesFound, runID)
	return err
}

// UpdateRunCompleted marks a run as completed with stats.
func (s *OrphanScanStore) UpdateRunCompleted(ctx context.Context, runID int64, filesDeleted, foldersDeleted int, bytesReclaimed int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET status = 'completed', files_deleted = ?, folders_deleted = ?, bytes_reclaimed = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, filesDeleted, foldersDeleted, bytesReclaimed, runID)
	return err
}

// UpdateRunFailed marks a run as failed with an error message.
func (s *OrphanScanStore) UpdateRunFailed(ctx context.Context, runID int64, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET status = 'failed', error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, errorMessage, runID)
	return err
}

// UpdateRunWarning sets a warning message on a run without changing its status.
func (s *OrphanScanStore) UpdateRunWarning(ctx context.Context, runID int64, warningMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs SET error_message = ? WHERE id = ?
	`, warningMessage, runID)
	return err
}

// MarkDeletingRunsFailed marks any runs currently in "deleting" as failed.
// This is intended to run at service startup so interrupted deletions don't
// remain stuck in a non-terminal state after a restart.
func (s *OrphanScanStore) MarkDeletingRunsFailed(ctx context.Context, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET status = 'failed', error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE status = 'deleting'
	`, errorMessage)
	return err
}

// MarkStuckRunsFailed marks old pending/scanning runs as failed.
func (s *OrphanScanStore) MarkStuckRunsFailed(ctx context.Context, threshold time.Duration, statuses []string) error {
	// orphan_scan_runs.started_at is set by SQLite using CURRENT_TIMESTAMP which yields a UTC
	// string like "YYYY-MM-DD HH:MM:SS". Comparing against a time.Time parameter can be
	// driver-dependent (e.g. RFC3339 with timezone), which breaks lexicographic comparisons.
	// Use the same UTC format as SQLite to ensure correct cutoff behavior.
	cutoff := time.Now().Add(-threshold).UTC().Format(time.DateTime)

	// Build placeholders for status list
	var placeholders strings.Builder
	args := make([]any, 0, len(statuses)+1)
	args = append(args, cutoff)
	for i, status := range statuses {
		if i > 0 {
			placeholders.WriteString(", ")
		}
		placeholders.WriteString("?")
		args = append(args, status)
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET status = 'failed', error_message = 'Marked failed after restart', completed_at = CURRENT_TIMESTAMP
		WHERE started_at < ? AND status IN (`+placeholders.String()+`)
	`, args...)
	return err
}

// DeleteRun deletes a run and its files (cascade).
func (s *OrphanScanStore) DeleteRun(ctx context.Context, runID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM orphan_scan_runs WHERE id = ?`, runID)
	return err
}

// InsertFiles inserts orphan files for a run in batches.
func (s *OrphanScanStore) InsertFiles(ctx context.Context, runID int64, files []OrphanScanFile) error {
	if len(files) == 0 {
		return nil
	}

	// Insert in batches of 100
	const batchSize = 100
	for i := 0; i < len(files); i += batchSize {
		end := min(i+batchSize, len(files))
		batch := files[i:end]

		var query strings.Builder
		query.WriteString(`INSERT INTO orphan_scan_files (run_id, file_path, file_size, modified_at, status) VALUES `)
		args := make([]any, 0, len(batch)*5)
		for j, f := range batch {
			if j > 0 {
				query.WriteString(", ")
			}
			query.WriteString("(?, ?, ?, ?, ?)")
			var modifiedAt any
			if f.ModifiedAt != nil {
				modifiedAt = *f.ModifiedAt
			}
			args = append(args, runID, f.FilePath, f.FileSize, modifiedAt, f.Status)
		}

		if _, err := s.db.ExecContext(ctx, query.String(), args...); err != nil {
			return err
		}
	}

	return nil
}

// ListFiles lists orphan files for a run with pagination.

// scanOrphanScanFile scans a single row into an OrphanScanFile struct.
func scanOrphanScanFile(rows *sql.Rows) (*OrphanScanFile, error) {
	var f OrphanScanFile
	var modifiedAt sql.NullTime
	var errorMessage sql.NullString

	if err := rows.Scan(&f.ID, &f.RunID, &f.FilePath, &f.FileSize, &modifiedAt, &f.Status, &errorMessage); err != nil {
		return nil, fmt.Errorf("scan orphan file row: %w", err)
	}
	if modifiedAt.Valid {
		f.ModifiedAt = &modifiedAt.Time
	}
	if errorMessage.Valid {
		f.ErrorMessage = errorMessage.String
	}
	return &f, nil
}

// collectOrphanScanFiles reads all rows into a slice using the shared scanner.
func collectOrphanScanFiles(rows *sql.Rows) ([]*OrphanScanFile, error) {
	var files []*OrphanScanFile
	for rows.Next() {
		f, err := scanOrphanScanFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orphan file rows: %w", err)
	}
	return files, nil
}

// listFilesDirectorySorted loads all files and sorts by directory then size (in-memory).
func (s *OrphanScanStore) listFilesDirectorySorted(ctx context.Context, runID int64, limit, offset int) ([]*OrphanScanFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, file_path, file_size, modified_at, status, error_message
		FROM orphan_scan_files
		WHERE run_id = ?
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("query orphan files: %w", err)
	}
	defer rows.Close()

	all, err := collectOrphanScanFiles(rows)
	if err != nil {
		return nil, err
	}

	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		da := strings.ToLower(filepath.Clean(filepath.Dir(a.FilePath)))
		db := strings.ToLower(filepath.Clean(filepath.Dir(b.FilePath)))
		if da != db {
			return da < db
		}
		if a.FileSize != b.FileSize {
			return a.FileSize > b.FileSize
		}
		return strings.ToLower(a.FilePath) < strings.ToLower(b.FilePath)
	})

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}
	if offset >= len(all) {
		return []*OrphanScanFile{}, nil
	}
	end := min(offset+limit, len(all))
	return all[offset:end], nil
}

// listFilesSizeSorted uses SQL ordering for efficiency.
func (s *OrphanScanStore) listFilesSizeSorted(ctx context.Context, runID int64, limit, offset int) ([]*OrphanScanFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, file_path, file_size, modified_at, status, error_message
		FROM orphan_scan_files
		WHERE run_id = ?
		ORDER BY file_size DESC, file_path ASC
		LIMIT ? OFFSET ?
	`, runID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query orphan files: %w", err)
	}
	defer rows.Close()

	return collectOrphanScanFiles(rows)
}

func (s *OrphanScanStore) ListFiles(ctx context.Context, runID int64, limit, offset int, sortMode string) ([]*OrphanScanFile, error) {
	if sortMode == "" {
		sortMode = "size_desc"
	}

	switch sortMode {
	case "directory_size_desc":
		return s.listFilesDirectorySorted(ctx, runID, limit, offset)
	default:
		return s.listFilesSizeSorted(ctx, runID, limit, offset)
	}
}

// GetFilesForDeletion returns all pending files for a run.
// Note: loads all files into memory. If memory usage becomes a concern with very
// large orphan sets, consider adding batched retrieval here.
func (s *OrphanScanStore) GetFilesForDeletion(ctx context.Context, runID int64) ([]*OrphanScanFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, file_path, file_size, modified_at, status, error_message
		FROM orphan_scan_files
		WHERE run_id = ? AND status = 'pending'
		ORDER BY file_path
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*OrphanScanFile
	for rows.Next() {
		var f OrphanScanFile
		var modifiedAt sql.NullTime
		var errorMessage sql.NullString

		if err := rows.Scan(
			&f.ID,
			&f.RunID,
			&f.FilePath,
			&f.FileSize,
			&modifiedAt,
			&f.Status,
			&errorMessage,
		); err != nil {
			return nil, err
		}

		if modifiedAt.Valid {
			f.ModifiedAt = &modifiedAt.Time
		}
		if errorMessage.Valid {
			f.ErrorMessage = errorMessage.String
		}

		files = append(files, &f)
	}

	return files, rows.Err()
}

// UpdateFileStatus updates the status of a single file.
func (s *OrphanScanStore) UpdateFileStatus(ctx context.Context, fileID int64, status string, errorMessage string) error {
	var errMsg any
	if errorMessage != "" {
		errMsg = errorMessage
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_files SET status = ?, error_message = ? WHERE id = ?
	`, status, errMsg, fileID)
	return err
}

// CountFiles returns the total number of files for a run.
func (s *OrphanScanStore) CountFiles(ctx context.Context, runID int64) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orphan_scan_files WHERE run_id = ?`, runID)
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
