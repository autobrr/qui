// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// OrphanScanSettings represents orphan scan settings for an instance.
type OrphanScanSettings struct {
	ID                 int64     `json:"id"`
	InstanceID         int       `json:"instanceId"`
	Enabled            bool      `json:"enabled"`
	GracePeriodMinutes int       `json:"gracePeriodMinutes"`
	IgnorePaths        []string  `json:"ignorePaths"`
	ScanIntervalHours  int       `json:"scanIntervalHours"`
	MaxFilesPerRun     int       `json:"maxFilesPerRun"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
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
		       scan_interval_hours, max_files_per_run, created_at, updated_at
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
		&settings.MaxFilesPerRun,
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
			(instance_id, enabled, grace_period_minutes, ignore_paths, scan_interval_hours, max_files_per_run)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET
			enabled = excluded.enabled,
			grace_period_minutes = excluded.grace_period_minutes,
			ignore_paths = excluded.ignore_paths,
			scan_interval_hours = excluded.scan_interval_hours,
			max_files_per_run = excluded.max_files_per_run
	`, settings.InstanceID, boolToInt(settings.Enabled), settings.GracePeriodMinutes,
		string(ignorePathsJSON), settings.ScanIntervalHours, settings.MaxFilesPerRun)
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
		return 0, err
	}
	return res.LastInsertId()
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

	if scanPathsJSON.Valid && scanPathsJSON.String != "" {
		if err := json.Unmarshal([]byte(scanPathsJSON.String), &run.ScanPaths); err != nil {
			return nil, err
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

	return &run, nil
}

// ListRuns lists recent runs for an instance.
func (s *OrphanScanStore) ListRuns(ctx context.Context, instanceID int, limit int) ([]*OrphanScanRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at
		FROM orphan_scan_runs
		WHERE instance_id = ?
		ORDER BY started_at DESC
		LIMIT ?
	`, instanceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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

		if scanPathsJSON.Valid && scanPathsJSON.String != "" {
			if err := json.Unmarshal([]byte(scanPathsJSON.String), &run.ScanPaths); err != nil {
				return nil, err
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

		runs = append(runs, &run)
	}

	return runs, rows.Err()
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
func (s *OrphanScanStore) HasActiveRun(ctx context.Context, instanceID int) (bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM orphan_scan_runs
		WHERE instance_id = ? AND status IN ('pending', 'scanning', 'preview_ready', 'deleting')
	`, instanceID)

	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
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

// MarkStuckRunsFailed marks old pending/scanning runs as failed.
func (s *OrphanScanStore) MarkStuckRunsFailed(ctx context.Context, threshold time.Duration, statuses []string) error {
	cutoff := time.Now().Add(-threshold)

	// Build placeholders for status list
	placeholders := ""
	args := make([]interface{}, 0, len(statuses)+1)
	args = append(args, cutoff)
	for i, status := range statuses {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "?"
		args = append(args, status)
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET status = 'failed', error_message = 'Marked failed after restart', completed_at = CURRENT_TIMESTAMP
		WHERE started_at < ? AND status IN (`+placeholders+`)
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
		end := i + batchSize
		if end > len(files) {
			end = len(files)
		}
		batch := files[i:end]

		query := `INSERT INTO orphan_scan_files (run_id, file_path, file_size, modified_at, status) VALUES `
		args := make([]interface{}, 0, len(batch)*5)
		for j, f := range batch {
			if j > 0 {
				query += ", "
			}
			query += "(?, ?, ?, ?, ?)"
			var modifiedAt interface{}
			if f.ModifiedAt != nil {
				modifiedAt = *f.ModifiedAt
			}
			args = append(args, runID, f.FilePath, f.FileSize, modifiedAt, f.Status)
		}

		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	return nil
}

// ListFiles lists orphan files for a run with pagination.
func (s *OrphanScanStore) ListFiles(ctx context.Context, runID int64, limit, offset int) ([]*OrphanScanFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, file_path, file_size, modified_at, status, error_message
		FROM orphan_scan_files
		WHERE run_id = ?
		ORDER BY file_size DESC
		LIMIT ? OFFSET ?
	`, runID, limit, offset)
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

// GetFilesForDeletion returns all pending files for a run.
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
	var errMsg interface{}
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
