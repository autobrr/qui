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
	ScanDefaultSavePath bool      `json:"scanDefaultSavePath"`
	ScanCategoryPaths   bool      `json:"scanCategoryPaths"`
	DeleteAbandonedDirs bool      `json:"deleteAbandonedDirs"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type OrphanScanRun struct {
	ID                 int64          `json:"id"`
	InstanceID         int            `json:"instanceId"`
	Status             string         `json:"status"`
	TriggeredBy        string         `json:"triggeredBy"`
	ScanPaths          []string       `json:"scanPaths,omitzero"`
	ScanPathCount      int            `json:"scanPathCount"`
	FilesFound         int            `json:"filesFound"`
	FilesDeleted       int            `json:"filesDeleted"`
	FoldersDeleted     int            `json:"foldersDeleted"`
	BytesReclaimed     int64          `json:"bytesReclaimed"`
	Truncated          bool           `json:"truncated"`
	Partial            bool           `json:"partial,omitzero"`
	ErrorMessage       string         `json:"errorMessage,omitempty"`
	StartedAt          time.Time      `json:"startedAt"`
	CompletedAt        *time.Time     `json:"completedAt,omitempty"`
	FilesystemMode     FilesystemMode `json:"filesystemMode"`
	IdentityComplete   bool           `json:"identityComplete"`
	FilesystemEndpoint string         `json:"-"`
}

type OrphanScanFile struct {
	ID             int64      `json:"id"`
	RunID          int64      `json:"runId"`
	FilePath       string     `json:"filePath"`
	FileSize       int64      `json:"fileSize"`
	IsAbandonedDir bool       `json:"isAbandonedDir"`
	ModifiedAt     *time.Time `json:"modifiedAt,omitempty"`
	Status         string     `json:"status"`
	ErrorMessage   string     `json:"errorMessage,omitempty"`
}

type OrphanScanStore struct {
	db dbinterface.Querier
}

func NewOrphanScanStore(db dbinterface.Querier) *OrphanScanStore {
	return &OrphanScanStore{db: db}
}

func (s *OrphanScanStore) GetSettings(ctx context.Context, instanceID int) (*OrphanScanSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, enabled, grace_period_minutes, ignore_paths,
		       scan_interval_hours, preview_sort, max_files_per_run, auto_cleanup_enabled,
		       auto_cleanup_max_files, scan_default_save_path, scan_category_paths,
		       delete_abandoned_dirs, created_at, updated_at
		FROM orphan_scan_settings
		WHERE instance_id = ?
	`, instanceID)

	var settings OrphanScanSettings
	var ignorePathsJSON sql.NullString
	var enabled, autoCleanupEnabled, scanDefaultSavePath, scanCategoryPaths, deleteAbandonedDirs int

	err := row.Scan(
		&settings.ID,
		&settings.InstanceID,
		&enabled,
		&settings.GracePeriodMinutes,
		&ignorePathsJSON,
		&settings.ScanIntervalHours,
		&settings.PreviewSort,
		&settings.MaxFilesPerRun,
		&autoCleanupEnabled,
		&settings.AutoCleanupMaxFiles,
		&scanDefaultSavePath,
		&scanCategoryPaths,
		&deleteAbandonedDirs,
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
	settings.Enabled = SQLiteIntToBool(enabled)
	settings.AutoCleanupEnabled = SQLiteIntToBool(autoCleanupEnabled)
	settings.ScanDefaultSavePath = SQLiteIntToBool(scanDefaultSavePath)
	settings.ScanCategoryPaths = SQLiteIntToBool(scanCategoryPaths)
	settings.DeleteAbandonedDirs = SQLiteIntToBool(deleteAbandonedDirs)

	return &settings, nil
}

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
				 preview_sort, max_files_per_run, auto_cleanup_enabled, auto_cleanup_max_files,
				 scan_default_save_path, scan_category_paths, delete_abandoned_dirs)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET
			enabled = excluded.enabled,
			grace_period_minutes = excluded.grace_period_minutes,
			ignore_paths = excluded.ignore_paths,
			scan_interval_hours = excluded.scan_interval_hours,
			preview_sort = excluded.preview_sort,
			max_files_per_run = excluded.max_files_per_run,
			auto_cleanup_enabled = excluded.auto_cleanup_enabled,
			auto_cleanup_max_files = excluded.auto_cleanup_max_files,
			scan_default_save_path = excluded.scan_default_save_path,
			scan_category_paths = excluded.scan_category_paths,
			delete_abandoned_dirs = excluded.delete_abandoned_dirs
	`, settings.InstanceID, boolToInt(settings.Enabled), settings.GracePeriodMinutes,
		string(ignorePathsJSON), settings.ScanIntervalHours, settings.PreviewSort, settings.MaxFilesPerRun,
		boolToInt(settings.AutoCleanupEnabled), settings.AutoCleanupMaxFiles,
		boolToInt(settings.ScanDefaultSavePath), boolToInt(settings.ScanCategoryPaths),
		boolToInt(settings.DeleteAbandonedDirs))
	if err != nil {
		return nil, err
	}

	return s.GetSettings(ctx, settings.InstanceID)
}

var ErrRunAlreadyActive = errors.New("an active run already exists for this instance")

func (s *OrphanScanStore) CreateRunIfNoActive(ctx context.Context, instanceID int, triggeredBy string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin orphan scan run: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE orphan_scan_runs SET status = 'canceled'
		WHERE instance_id = ? AND status = 'preview_ready' AND files_found > 0
		  AND filesystem_mode = ? AND identity_complete = 0
	`, instanceID, string(FilesystemModeRemote)); err != nil {
		return 0, fmt.Errorf("cancel unconfirmable orphan scan preview: %w", err)
	}

	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO orphan_scan_runs (instance_id, status, triggered_by, filesystem_mode)
		SELECT ?, 'pending', ?, ?
		WHERE NOT EXISTS (
			SELECT 1 FROM orphan_scan_runs
			WHERE instance_id = ?
			  AND (status IN ('pending', 'scanning', 'deleting')
			       OR (status = 'preview_ready' AND files_found > 0))
		)
		RETURNING id
	`, instanceID, triggeredBy, string(FilesystemModeNone), instanceID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrRunAlreadyActive
		}
		return 0, fmt.Errorf("insert orphan scan run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit orphan scan run: %w", err)
	}
	return id, nil
}

func (s *OrphanScanStore) GetRun(ctx context.Context, runID int64) (*OrphanScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at, partial, filesystem_mode,
		       identity_complete, filesystem_endpoint
		FROM orphan_scan_runs
		WHERE id = ?
	`, runID)

	return s.scanRun(row)
}

func (s *OrphanScanStore) GetRunByInstance(ctx context.Context, instanceID int, runID int64) (*OrphanScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at, partial, filesystem_mode,
		       identity_complete, filesystem_endpoint
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
	var truncated int

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
		&truncated,
		&errorMessage,
		&run.StartedAt,
		&completedAt,
		&run.Partial,
		&run.FilesystemMode,
		&run.IdentityComplete,
		&run.FilesystemEndpoint,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	run.Truncated = SQLiteIntToBool(truncated)
	if scanPathsJSON.Valid && scanPathsJSON.String != "" {
		if err := json.Unmarshal([]byte(scanPathsJSON.String), &run.ScanPaths); err != nil {
			return nil, fmt.Errorf("unmarshal scan paths: %w", err)
		}
	}
	if run.ScanPaths == nil {
		run.ScanPaths = []string{}
	}
	run.ScanPathCount = len(run.ScanPaths)
	finalizeRun(&run, errorMessage, completedAt)

	return &run, nil
}

func finalizeRun(run *OrphanScanRun, errorMessage sql.NullString, completedAt sql.NullTime) {
	if errorMessage.Valid {
		run.ErrorMessage = errorMessage.String
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
}

func (s *OrphanScanStore) scanRunsFromRows(rows *sql.Rows) ([]*OrphanScanRun, error) {
	var runs []*OrphanScanRun
	for rows.Next() {
		var run OrphanScanRun
		var errorMessage sql.NullString
		var completedAt sql.NullTime
		var truncated int

		if err := rows.Scan(
			&run.ID,
			&run.InstanceID,
			&run.Status,
			&run.TriggeredBy,
			&run.ScanPathCount,
			&run.FilesFound,
			&run.FilesDeleted,
			&run.FoldersDeleted,
			&run.BytesReclaimed,
			&truncated,
			&errorMessage,
			&run.StartedAt,
			&completedAt,
			&run.Partial,
			&run.FilesystemMode,
			&run.IdentityComplete,
			&run.FilesystemEndpoint,
		); err != nil {
			return nil, err
		}
		run.Truncated = SQLiteIntToBool(truncated)
		finalizeRun(&run, errorMessage, completedAt)

		runs = append(runs, &run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return runs, nil
}

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
	query := s.listRunsSelect() + `
		WHERE instance_id = ?
		ORDER BY started_at DESC
		LIMIT ?
	`
	return s.scanRunsQuery(ctx, "query recent runs", query, instanceID, limit)
}

func (s *OrphanScanStore) listRunsActive(ctx context.Context, instanceID int) ([]*OrphanScanRun, error) {
	query := s.listRunsSelect() + `
		WHERE instance_id = ?
		  AND (status IN ('pending', 'scanning', 'deleting')
		       OR (status = 'preview_ready' AND files_found > 0))
		ORDER BY started_at DESC
	`
	return s.scanRunsQuery(ctx, "query active runs", query, instanceID)
}

func (s *OrphanScanStore) listRunsSelect() string {
	scanPathCount := `COALESCE(json_array_length(NULLIF(scan_paths, '')), 0)`
	if dbinterface.DialectOf(s.db) == "postgres" {
		scanPathCount = `CASE WHEN json_typeof(NULLIF(scan_paths, '')::json) = 'array'
		     THEN json_array_length(NULLIF(scan_paths, '')::json) ELSE 0 END`
	}
	return `
		SELECT id, instance_id, status, triggered_by, ` + scanPathCount + `, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at, partial, filesystem_mode,
		       identity_complete, filesystem_endpoint
		FROM orphan_scan_runs`
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

func (s *OrphanScanStore) GetLastCompletedRun(ctx context.Context, instanceID int) (*OrphanScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at, partial, filesystem_mode,
		       identity_complete, filesystem_endpoint
		FROM orphan_scan_runs
		WHERE instance_id = ? AND status = 'completed'
		ORDER BY completed_at DESC
		LIMIT 1
	`, instanceID)

	return s.scanRun(row)
}

func (s *OrphanScanStore) GetLastFinishedScan(ctx context.Context, instanceID int) (*OrphanScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at, partial, filesystem_mode,
		       identity_complete, filesystem_endpoint
		FROM orphan_scan_runs
		WHERE instance_id = ? AND status IN ('completed', 'preview_ready')
		ORDER BY CASE WHEN status = 'completed' THEN completed_at ELSE started_at END DESC
		LIMIT 1
	`, instanceID)

	return s.scanRun(row)
}

func (s *OrphanScanStore) GetMostRecentActiveRun(ctx context.Context, instanceID int) (*OrphanScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, status, triggered_by, scan_paths, files_found,
		       files_deleted, folders_deleted, bytes_reclaimed, truncated,
		       error_message, started_at, completed_at, partial, filesystem_mode,
		       identity_complete, filesystem_endpoint
		FROM orphan_scan_runs
		WHERE instance_id = ?
		  AND (status IN ('pending', 'scanning', 'deleting')
		       OR (status = 'preview_ready' AND files_found > 0
		           AND NOT (filesystem_mode = ? AND identity_complete = 0)))
		ORDER BY started_at DESC
		LIMIT 1
	`, instanceID, string(FilesystemModeRemote))

	return s.scanRun(row)
}

func (s *OrphanScanStore) UpdateRunStatus(ctx context.Context, runID int64, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs SET status = ? WHERE id = ?
	`, status, runID)
	return err
}

func (s *OrphanScanStore) UpdateRunFilesystemMode(ctx context.Context, runID int64, mode FilesystemMode, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs SET filesystem_mode = ?, filesystem_endpoint = ? WHERE id = ?
	`, string(mode), endpoint, runID)
	return err
}

func (s *OrphanScanStore) UpdateRunIdentityComplete(ctx context.Context, runID int64, complete bool) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs SET identity_complete = ? WHERE id = ?
	`, boolToInt(complete), runID)
	return err
}

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

func (s *OrphanScanStore) UpdateRunFoundStats(ctx context.Context, runID int64, filesFound int, truncated bool, bytesFound int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET files_found = ?, truncated = ?, bytes_reclaimed = ?
		WHERE id = ?
	`, filesFound, boolToInt(truncated), bytesFound, runID)
	return err
}

func (s *OrphanScanStore) UpdateRunCompleted(ctx context.Context, runID int64, filesDeleted, foldersDeleted int, bytesReclaimed int64, warningMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET status = 'completed', files_deleted = ?, folders_deleted = ?, bytes_reclaimed = ?, error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, filesDeleted, foldersDeleted, bytesReclaimed, warningMessage, runID)
	return err
}

func (s *OrphanScanStore) UpdateRunFailed(ctx context.Context, runID int64, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET status = 'failed', error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, errorMessage, runID)
	return err
}

func (s *OrphanScanStore) FailPreviewReadyRun(ctx context.Context, runID int64, errorMessage string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET status = 'failed', error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'preview_ready'
	`, errorMessage, runID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *OrphanScanStore) RemotePreviewRunIDs(ctx context.Context, instanceID int) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM orphan_scan_runs
		WHERE instance_id = ? AND status = 'preview_ready' AND filesystem_mode = ?
	`, instanceID, string(FilesystemModeRemote))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *OrphanScanStore) HasDeletingRun(ctx context.Context, instanceID int) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM orphan_scan_runs WHERE instance_id = ? AND status = 'deleting'
	`, instanceID).Scan(&n)
	return n > 0, err
}

func (s *OrphanScanStore) StartRunDeletion(ctx context.Context, runID int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs SET status = 'deleting' WHERE id = ? AND status = 'preview_ready'
	`, runID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *OrphanScanStore) UpdateRunPartial(ctx context.Context, runID int64, warningMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs SET partial = 1, error_message = ? WHERE id = ?
	`, warningMessage, runID)
	return err
}

func (s *OrphanScanStore) UpdateRunWarning(ctx context.Context, runID int64, warningMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs SET error_message = ? WHERE id = ?
	`, warningMessage, runID)
	return err
}

func (s *OrphanScanStore) MarkDeletingRunsFailed(ctx context.Context, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET status = 'failed', error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE status = 'deleting'
	`, errorMessage)
	return err
}

func (s *OrphanScanStore) MarkInterruptedScansFailed(ctx context.Context, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orphan_scan_runs
		SET status = 'failed', error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE status IN ('pending', 'scanning')
	`, errorMessage)
	return err
}

func (s *OrphanScanStore) InsertFiles(ctx context.Context, runID int64, files []OrphanScanFile) error {
	if len(files) == 0 {
		return nil
	}

	const batchSize = 100
	for i := 0; i < len(files); i += batchSize {
		end := min(i+batchSize, len(files))
		batch := files[i:end]

		var query strings.Builder
		query.WriteString(`INSERT INTO orphan_scan_files (run_id, file_path, file_size, modified_at, status, is_abandoned_dir) VALUES `)
		args := make([]any, 0, len(batch)*6)
		for j, f := range batch {
			if j > 0 {
				query.WriteString(", ")
			}
			query.WriteString("(?, ?, ?, ?, ?, ?)")
			var modifiedAt any
			if f.ModifiedAt != nil {
				modifiedAt = *f.ModifiedAt
			}
			args = append(args, runID, f.FilePath, f.FileSize, modifiedAt, f.Status, boolToInt(f.IsAbandonedDir))
		}

		if _, err := s.db.ExecContext(ctx, query.String(), args...); err != nil {
			return err
		}
	}

	return nil
}

func scanOrphanScanFile(rows *sql.Rows) (*OrphanScanFile, error) {
	var f OrphanScanFile
	var modifiedAt sql.NullTime
	var errorMessage sql.NullString
	var isAbandonedDir int

	if err := rows.Scan(&f.ID, &f.RunID, &f.FilePath, &f.FileSize, &modifiedAt, &f.Status, &errorMessage, &isAbandonedDir); err != nil {
		return nil, fmt.Errorf("scan orphan file row: %w", err)
	}
	f.IsAbandonedDir = SQLiteIntToBool(isAbandonedDir)
	if modifiedAt.Valid {
		f.ModifiedAt = &modifiedAt.Time
	}
	if errorMessage.Valid {
		f.ErrorMessage = errorMessage.String
	}
	return &f, nil
}

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

func (s *OrphanScanStore) listFilesDirectorySorted(ctx context.Context, runID int64, limit, offset int) ([]*OrphanScanFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, file_path, file_size, modified_at, status, error_message, is_abandoned_dir
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

func (s *OrphanScanStore) listFilesSizeSorted(ctx context.Context, runID int64, limit, offset int) ([]*OrphanScanFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, file_path, file_size, modified_at, status, error_message, is_abandoned_dir
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

func (s *OrphanScanStore) GetFilesForDeletion(ctx context.Context, runID int64) ([]*OrphanScanFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, file_path, file_size, modified_at, status, error_message, is_abandoned_dir
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
		var isAbandonedDir int

		if err := rows.Scan(
			&f.ID,
			&f.RunID,
			&f.FilePath,
			&f.FileSize,
			&modifiedAt,
			&f.Status,
			&errorMessage,
			&isAbandonedDir,
		); err != nil {
			return nil, err
		}
		f.IsAbandonedDir = SQLiteIntToBool(isAbandonedDir)

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
