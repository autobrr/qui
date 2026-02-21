// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/autobrr/qui/internal/dbinterface"
)

// MatchMode defines how files are matched to torrent files.
type MatchMode string

const (
	// MatchModeStrict requires files to match by name AND size.
	MatchModeStrict MatchMode = "strict"
	// MatchModeFlexible matches files by size only, ignoring filename differences.
	MatchModeFlexible MatchMode = "flexible"
)

// DirScanFileStatus defines the status of a scanned file.
type DirScanFileStatus string

// Status constants are type-safe enums; duplicate string values across types are intentional.
const (
	DirScanFileStatusPending        DirScanFileStatus = "pending" //nolint:goconst // type-safe enum values intentionally share strings with other status types
	DirScanFileStatusMatched        DirScanFileStatus = "matched"
	DirScanFileStatusNoMatch        DirScanFileStatus = "no_match"
	DirScanFileStatusError          DirScanFileStatus = "error"
	DirScanFileStatusAlreadySeeding DirScanFileStatus = "already_seeding"
	DirScanFileStatusInQBittorrent  DirScanFileStatus = "in_qbittorrent"
)

// DirScanRunStatus defines the status of a scan run.
type DirScanRunStatus string

// Run status constants are type-safe enums; duplicate string values across types are intentional.
const (
	DirScanRunStatusQueued    DirScanRunStatus = "queued"
	DirScanRunStatusScanning  DirScanRunStatus = "scanning"
	DirScanRunStatusSearching DirScanRunStatus = "searching"
	DirScanRunStatusInjecting DirScanRunStatus = "injecting"
	DirScanRunStatusSuccess   DirScanRunStatus = "success"  //nolint:goconst // type-safe enum values intentionally share strings with other status types
	DirScanRunStatusFailed    DirScanRunStatus = "failed"   //nolint:goconst // type-safe enum values intentionally share strings with other status types
	DirScanRunStatusCanceled  DirScanRunStatus = "canceled" //nolint:goconst // type-safe enum values intentionally share strings with other status types
)

// DirScanSettings represents global directory scanner settings.
type DirScanSettings struct {
	ID                           int       `json:"id"`
	Enabled                      bool      `json:"enabled"`
	MatchMode                    MatchMode `json:"matchMode"`
	SizeTolerancePercent         float64   `json:"sizeTolerancePercent"`
	MinPieceRatio                float64   `json:"minPieceRatio"`
	MaxSearcheesPerRun           int       `json:"maxSearcheesPerRun"`
	MaxSearcheeAgeDays           int       `json:"maxSearcheeAgeDays"`
	AllowPartial                 bool      `json:"allowPartial"`
	SkipPieceBoundarySafetyCheck bool      `json:"skipPieceBoundarySafetyCheck"`
	StartPaused                  bool      `json:"startPaused"`
	Category                     string    `json:"category"`
	Tags                         []string  `json:"tags"`
	CreatedAt                    time.Time `json:"createdAt"`
	UpdatedAt                    time.Time `json:"updatedAt"`
}

// DirScanDirectory represents a configured scan directory.
type DirScanDirectory struct {
	ID                  int        `json:"id"`
	Path                string     `json:"path"`
	QbitPathPrefix      string     `json:"qbitPathPrefix,omitempty"`
	Category            string     `json:"category,omitempty"`
	Tags                []string   `json:"tags"`
	Enabled             bool       `json:"enabled"`
	ArrInstanceID       *int       `json:"arrInstanceId,omitempty"`
	TargetInstanceID    int        `json:"targetInstanceId"`
	ScanIntervalMinutes int        `json:"scanIntervalMinutes"`
	LastScanAt          *time.Time `json:"lastScanAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

// DirScanRun represents a scan run history entry.
type DirScanRun struct {
	ID            int64            `json:"id"`
	DirectoryID   int              `json:"directoryId"`
	Status        DirScanRunStatus `json:"status"`
	TriggeredBy   string           `json:"triggeredBy"`
	FilesFound    int              `json:"filesFound"`
	FilesSkipped  int              `json:"filesSkipped"`
	MatchesFound  int              `json:"matchesFound"`
	TorrentsAdded int              `json:"torrentsAdded"`
	ErrorMessage  string           `json:"errorMessage,omitempty"`
	StartedAt     time.Time        `json:"startedAt"`
	CompletedAt   *time.Time       `json:"completedAt,omitempty"`
}

// DirScanRunInjectionStatus defines the status of an injection attempt.
type DirScanRunInjectionStatus string

const (
	DirScanRunInjectionStatusAdded  DirScanRunInjectionStatus = "added"
	DirScanRunInjectionStatusFailed DirScanRunInjectionStatus = "failed" //nolint:goconst // type-safe enum values intentionally share strings with other status types
)

// DirScanRunInjection represents a successful or failed injection attempt for a scan run.
type DirScanRunInjection struct {
	ID                 int64                     `json:"id"`
	RunID              int64                     `json:"runId"`
	DirectoryID        int                       `json:"directoryId"`
	Status             DirScanRunInjectionStatus `json:"status"`
	SearcheeName       string                    `json:"searcheeName"`
	TorrentName        string                    `json:"torrentName"`
	InfoHash           string                    `json:"infoHash"`
	ContentType        string                    `json:"contentType"`
	IndexerName        string                    `json:"indexerName,omitempty"`
	TrackerDomain      string                    `json:"trackerDomain,omitempty"`
	TrackerDisplayName string                    `json:"trackerDisplayName,omitempty"`
	LinkMode           string                    `json:"linkMode,omitempty"`
	SavePath           string                    `json:"savePath,omitempty"`
	Category           string                    `json:"category,omitempty"`
	Tags               []string                  `json:"tags"`
	ErrorMessage       string                    `json:"errorMessage,omitempty"`
	CreatedAt          time.Time                 `json:"createdAt"`
}

// DirScanFile represents a scanned file tracking entry.
type DirScanFile struct {
	ID                 int64             `json:"id"`
	DirectoryID        int               `json:"directoryId"`
	FilePath           string            `json:"filePath"`
	FileSize           int64             `json:"fileSize"`
	FileModTime        time.Time         `json:"fileModTime"`
	FileID             []byte            `json:"-"` // Platform-neutral FileID (dev+ino on Unix, vol+idx on Windows)
	Status             DirScanFileStatus `json:"status"`
	MatchedTorrentHash string            `json:"matchedTorrentHash,omitempty"`
	MatchedIndexerID   *int              `json:"matchedIndexerId,omitempty"`
	LastProcessedAt    *time.Time        `json:"lastProcessedAt,omitempty"`
}

// DirScanStore handles database operations for directory scanner.
type DirScanStore struct {
	db dbinterface.Querier
}

// NewDirScanStore creates a new DirScanStore.
func NewDirScanStore(db dbinterface.Querier) *DirScanStore {
	return &DirScanStore{db: db}
}

// --- Settings Operations ---

// GetSettings retrieves the global directory scanner settings.
func (s *DirScanStore) GetSettings(ctx context.Context) (*DirScanSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, enabled, match_mode, size_tolerance_percent, min_piece_ratio, max_searchees_per_run,
		       max_searchee_age_days,
		       allow_partial, skip_piece_boundary_safety_check, start_paused,
		       category, tags, created_at, updated_at
		FROM dir_scan_settings
		WHERE id = 1
	`)

	var settings DirScanSettings
	var category sql.NullString
	var tagsJSON sql.NullString

	err := row.Scan(
		&settings.ID,
		&settings.Enabled,
		&settings.MatchMode,
		&settings.SizeTolerancePercent,
		&settings.MinPieceRatio,
		&settings.MaxSearcheesPerRun,
		&settings.MaxSearcheeAgeDays,
		&settings.AllowPartial,
		&settings.SkipPieceBoundarySafetyCheck,
		&settings.StartPaused,
		&category,
		&tagsJSON,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan settings: %w", err)
	}

	if category.Valid {
		settings.Category = category.String
	}
	if tagsJSON.Valid && tagsJSON.String != "" {
		if err := json.Unmarshal([]byte(tagsJSON.String), &settings.Tags); err != nil {
			return nil, fmt.Errorf("unmarshal tags: %w", err)
		}
	}
	if settings.Tags == nil {
		settings.Tags = []string{}
	}

	// Store in DB uses a 0-1 ratio; API/UI expects percent (0-100).
	settings.MinPieceRatio = minPieceRatioToPercent(settings.MinPieceRatio)

	return &settings, nil
}

// UpdateSettings updates the global directory scanner settings.
func (s *DirScanStore) UpdateSettings(ctx context.Context, settings *DirScanSettings) (*DirScanSettings, error) {
	if settings == nil {
		return nil, errors.New("settings is nil")
	}

	if settings.MaxSearcheesPerRun < 0 {
		return nil, errors.New("maxSearcheesPerRun must be >= 0")
	}
	if settings.MaxSearcheeAgeDays < 0 {
		return nil, errors.New("maxSearcheeAgeDays must be >= 0")
	}

	tagsJSON, err := json.Marshal(settings.Tags)
	if err != nil {
		return nil, fmt.Errorf("marshal tags: %w", err)
	}

	var category any
	if settings.Category != "" {
		category = settings.Category
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO dir_scan_settings (
			id, enabled, match_mode, size_tolerance_percent, min_piece_ratio,
			max_searchees_per_run, max_searchee_age_days,
			allow_partial, skip_piece_boundary_safety_check, start_paused,
			category, tags
		) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled,
			match_mode = excluded.match_mode,
			size_tolerance_percent = excluded.size_tolerance_percent,
			min_piece_ratio = excluded.min_piece_ratio,
			max_searchees_per_run = excluded.max_searchees_per_run,
			max_searchee_age_days = excluded.max_searchee_age_days,
			allow_partial = excluded.allow_partial,
			skip_piece_boundary_safety_check = excluded.skip_piece_boundary_safety_check,
			start_paused = excluded.start_paused,
			category = excluded.category,
			tags = excluded.tags
	`,
		boolToInt(settings.Enabled),
		settings.MatchMode,
		settings.SizeTolerancePercent,
		minPieceRatioToDB(settings.MinPieceRatio),
		settings.MaxSearcheesPerRun,
		settings.MaxSearcheeAgeDays,
		boolToInt(settings.AllowPartial),
		boolToInt(settings.SkipPieceBoundarySafetyCheck),
		boolToInt(settings.StartPaused),
		category,
		string(tagsJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("update settings: %w", err)
	}

	return s.GetSettings(ctx)
}

func minPieceRatioToPercent(value float64) float64 {
	// Backward-compatible: accept ratio values (0-1) and percent values (0-100).
	if value > 0 && value <= 1 {
		return value * 100
	}
	return value
}

func minPieceRatioToDB(value float64) float64 {
	// Store ratio (0-1) in DB; accept percent values (0-100) from API.
	if value > 1 {
		return value / 100
	}
	return value
}

// --- Directory Operations ---

// ErrDirectoryNotFound is returned when a directory is not found.
var ErrDirectoryNotFound = errors.New("directory not found")

// CreateDirectory creates a new scan directory.
func (s *DirScanStore) CreateDirectory(ctx context.Context, dir *DirScanDirectory) (*DirScanDirectory, error) {
	if dir == nil {
		return nil, errors.New("directory is nil")
	}

	var qbitPathPrefix any
	if dir.QbitPathPrefix != "" {
		qbitPathPrefix = dir.QbitPathPrefix
	}

	var category any
	if dir.Category != "" {
		category = dir.Category
	}

	if dir.Tags == nil {
		dir.Tags = []string{}
	}
	tagsJSON, err := json.Marshal(dir.Tags)
	if err != nil {
		return nil, fmt.Errorf("marshal tags: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO dir_scan_directories
			(path, qbit_path_prefix, category, tags, enabled, arr_instance_id, target_instance_id, scan_interval_minutes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, dir.Path, qbitPathPrefix, category, string(tagsJSON), boolToInt(dir.Enabled), dir.ArrInstanceID,
		dir.TargetInstanceID, dir.ScanIntervalMinutes)
	if err != nil {
		return nil, fmt.Errorf("insert directory: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get last insert id: %w", err)
	}

	return s.GetDirectory(ctx, int(id))
}

// GetDirectory retrieves a directory by ID.
func (s *DirScanStore) GetDirectory(ctx context.Context, id int) (*DirScanDirectory, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, path, qbit_path_prefix, category, tags, enabled, arr_instance_id, target_instance_id,
		       scan_interval_minutes, last_scan_at, created_at, updated_at
		FROM dir_scan_directories
		WHERE id = ?
	`, id)

	return s.scanDirectory(row)
}

type sqlScanner interface {
	Scan(dest ...any) error
}

func (s *DirScanStore) scanDirectory(row *sql.Row) (*DirScanDirectory, error) {
	dir, err := s.scanDirectoryFromScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDirectoryNotFound
	}
	return dir, err
}

func (s *DirScanStore) scanDirectoryFromScanner(scanner sqlScanner) (*DirScanDirectory, error) {
	var dir DirScanDirectory
	var qbitPathPrefix sql.NullString
	var category sql.NullString
	var tagsJSON sql.NullString
	var arrInstanceID sql.NullInt64
	var lastScanAt sql.NullTime

	if err := scanner.Scan(
		&dir.ID,
		&dir.Path,
		&qbitPathPrefix,
		&category,
		&tagsJSON,
		&dir.Enabled,
		&arrInstanceID,
		&dir.TargetInstanceID,
		&dir.ScanIntervalMinutes,
		&lastScanAt,
		&dir.CreatedAt,
		&dir.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan directory columns: %w", err)
	}

	if qbitPathPrefix.Valid {
		dir.QbitPathPrefix = qbitPathPrefix.String
	}
	if category.Valid {
		dir.Category = category.String
	}
	if tagsJSON.Valid && tagsJSON.String != "" {
		if err := json.Unmarshal([]byte(tagsJSON.String), &dir.Tags); err != nil {
			return nil, fmt.Errorf("unmarshal tags: %w", err)
		}
	}
	if dir.Tags == nil {
		dir.Tags = []string{}
	}
	if arrInstanceID.Valid {
		id := int(arrInstanceID.Int64)
		dir.ArrInstanceID = &id
	}
	if lastScanAt.Valid {
		dir.LastScanAt = &lastScanAt.Time
	}

	return &dir, nil
}

// scanDirectoriesFromRows scans directory rows and returns a slice of directories.
func (s *DirScanStore) scanDirectoriesFromRows(rows *sql.Rows) ([]*DirScanDirectory, error) {
	var directories []*DirScanDirectory
	for rows.Next() {
		dir, err := s.scanDirectoryFromScanner(rows)
		if err != nil {
			return nil, err
		}
		directories = append(directories, dir)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate directories: %w", err)
	}

	return directories, nil
}

// ListDirectories retrieves all scan directories.
func (s *DirScanStore) ListDirectories(ctx context.Context) ([]*DirScanDirectory, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, path, qbit_path_prefix, category, tags, enabled, arr_instance_id, target_instance_id,
		       scan_interval_minutes, last_scan_at, created_at, updated_at
		FROM dir_scan_directories
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query directories: %w", err)
	}
	defer rows.Close()

	return s.scanDirectoriesFromRows(rows)
}

// DirScanDirectoryUpdateParams holds optional fields for updating a directory.
type DirScanDirectoryUpdateParams struct {
	Path                *string
	QbitPathPrefix      *string
	Category            *string
	Tags                *[]string
	Enabled             *bool
	ArrInstanceID       *int // Use -1 to clear
	TargetInstanceID    *int
	ScanIntervalMinutes *int
}

// UpdateDirectory updates a scan directory.
func (s *DirScanStore) UpdateDirectory(ctx context.Context, id int, params *DirScanDirectoryUpdateParams) (*DirScanDirectory, error) {
	if params == nil {
		return s.GetDirectory(ctx, id)
	}

	existing, err := s.GetDirectory(ctx, id)
	if err != nil {
		return nil, err
	}

	applyDirectoryUpdateParams(existing, params)

	var qbitPathPrefix any
	if existing.QbitPathPrefix != "" {
		qbitPathPrefix = existing.QbitPathPrefix
	}

	var category any
	if existing.Category != "" {
		category = existing.Category
	}

	tagsJSON, err := json.Marshal(existing.Tags)
	if err != nil {
		return nil, fmt.Errorf("marshal tags: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE dir_scan_directories
		SET path = ?,
		    qbit_path_prefix = ?,
		    category = ?,
		    tags = ?,
		    enabled = ?,
		    arr_instance_id = ?,
		    target_instance_id = ?,
		    scan_interval_minutes = ?
		WHERE id = ?
	`, existing.Path, qbitPathPrefix, category, string(tagsJSON), boolToInt(existing.Enabled),
		existing.ArrInstanceID, existing.TargetInstanceID, existing.ScanIntervalMinutes, id)
	if err != nil {
		return nil, fmt.Errorf("update directory: %w", err)
	}

	return s.GetDirectory(ctx, id)
}

func applyDirectoryUpdateParams(existing *DirScanDirectory, params *DirScanDirectoryUpdateParams) {
	if existing == nil || params == nil {
		return
	}

	if params.Path != nil {
		existing.Path = *params.Path
	}
	if params.QbitPathPrefix != nil {
		existing.QbitPathPrefix = *params.QbitPathPrefix
	}
	if params.Category != nil {
		existing.Category = *params.Category
	}
	if params.Tags != nil {
		existing.Tags = *params.Tags
	}
	if params.Enabled != nil {
		existing.Enabled = *params.Enabled
	}
	if params.ArrInstanceID != nil {
		if *params.ArrInstanceID == -1 {
			existing.ArrInstanceID = nil
		} else {
			existing.ArrInstanceID = params.ArrInstanceID
		}
	}
	if params.TargetInstanceID != nil {
		existing.TargetInstanceID = *params.TargetInstanceID
	}
	if params.ScanIntervalMinutes != nil {
		existing.ScanIntervalMinutes = *params.ScanIntervalMinutes
	}
}

// DeleteDirectory deletes a scan directory.
func (s *DirScanStore) DeleteDirectory(ctx context.Context, id int) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM dir_scan_directories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete directory: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDirectoryNotFound
	}

	return nil
}

// UpdateDirectoryLastScan updates the last scan timestamp.
func (s *DirScanStore) UpdateDirectoryLastScan(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE dir_scan_directories SET last_scan_at = CURRENT_TIMESTAMP WHERE id = ?
	`, id)
	if err != nil {
		return fmt.Errorf("update directory last scan: %w", err)
	}
	return nil
}

// ListEnabledDirectories returns all enabled directories.
func (s *DirScanStore) ListEnabledDirectories(ctx context.Context) ([]*DirScanDirectory, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, path, qbit_path_prefix, category, tags, enabled, arr_instance_id, target_instance_id,
		       scan_interval_minutes, last_scan_at, created_at, updated_at
		FROM dir_scan_directories
		WHERE enabled = 1
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query enabled directories: %w", err)
	}
	defer rows.Close()

	return s.scanDirectoriesFromRows(rows)
}

// --- Run Operations ---

// ErrDirScanRunAlreadyActive is returned when attempting to create a run while one is active.
var ErrDirScanRunAlreadyActive = errors.New("an active scan run already exists for this directory")

// CreateRun creates a new scan run.
func (s *DirScanStore) CreateRun(ctx context.Context, directoryID int, triggeredBy string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO dir_scan_runs (directory_id, status, triggered_by)
		VALUES (?, ?, ?)
	`, directoryID, DirScanRunStatusQueued, triggeredBy)
	if err != nil {
		return 0, fmt.Errorf("insert run: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}

	return id, nil
}

// CreateRunIfNoActive atomically checks for active runs and creates a new one if none exist.
func (s *DirScanStore) CreateRunIfNoActive(ctx context.Context, directoryID int, triggeredBy string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO dir_scan_runs (directory_id, status, triggered_by)
		SELECT ?, ?, ?
		WHERE NOT EXISTS (
			SELECT 1 FROM dir_scan_runs
			WHERE directory_id = ?
			  AND status IN ('queued', 'scanning', 'searching', 'injecting')
		)
	`, directoryID, DirScanRunStatusQueued, triggeredBy, directoryID)
	if err != nil {
		return 0, fmt.Errorf("insert run: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return 0, ErrDirScanRunAlreadyActive
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}

	return id, nil
}

// GetRun retrieves a run by ID.
func (s *DirScanStore) GetRun(ctx context.Context, runID int64) (*DirScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, directory_id, status, triggered_by, files_found, files_skipped,
		       matches_found, torrents_added, error_message, started_at, completed_at
		FROM dir_scan_runs
		WHERE id = ?
	`, runID)

	return s.scanRun(row)
}

func (s *DirScanStore) scanRun(row *sql.Row) (*DirScanRun, error) {
	run, err := scanRunFromScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return run, err
}

func scanRunFromScanner(scanner sqlScanner) (*DirScanRun, error) {
	var run DirScanRun
	var errorMessage sql.NullString
	var completedAt sql.NullTime

	if err := scanner.Scan(
		&run.ID,
		&run.DirectoryID,
		&run.Status,
		&run.TriggeredBy,
		&run.FilesFound,
		&run.FilesSkipped,
		&run.MatchesFound,
		&run.TorrentsAdded,
		&errorMessage,
		&run.StartedAt,
		&completedAt,
	); err != nil {
		return nil, fmt.Errorf("scan run columns: %w", err)
	}

	if errorMessage.Valid {
		run.ErrorMessage = errorMessage.String
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}

	return &run, nil
}

// ListRuns lists recent runs for a directory.
func (s *DirScanStore) ListRuns(ctx context.Context, directoryID, limit int) ([]*DirScanRun, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, directory_id, status, triggered_by, files_found, files_skipped,
		       matches_found, torrents_added, error_message, started_at, completed_at
		FROM dir_scan_runs
		WHERE directory_id = ?
		ORDER BY started_at DESC
		LIMIT ?
	`, directoryID, limit)
	if err != nil {
		return nil, fmt.Errorf("query runs: %w", err)
	}
	defer rows.Close()

	return scanRunsFromRows(rows)
}

func scanRunsFromRows(rows *sql.Rows) ([]*DirScanRun, error) {
	var runs []*DirScanRun
	for rows.Next() {
		run, err := scanRunFromScanner(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs: %w", err)
	}
	return runs, nil
}

// HasActiveRun checks if there's an active run for a directory.
func (s *DirScanStore) HasActiveRun(ctx context.Context, directoryID int) (bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM dir_scan_runs
		WHERE directory_id = ?
		  AND status IN ('queued', 'scanning', 'searching', 'injecting')
	`, directoryID)

	var count int
	if err := row.Scan(&count); err != nil {
		return false, fmt.Errorf("scan active run count: %w", err)
	}
	return count > 0, nil
}

// GetActiveRun returns the active run for a directory, if any.
func (s *DirScanStore) GetActiveRun(ctx context.Context, directoryID int) (*DirScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, directory_id, status, triggered_by, files_found, files_skipped,
		       matches_found, torrents_added, error_message, started_at, completed_at
		FROM dir_scan_runs
		WHERE directory_id = ?
		  AND status IN ('queued', 'scanning', 'searching', 'injecting')
		ORDER BY started_at DESC
		LIMIT 1
	`, directoryID)

	return s.scanRun(row)
}

// UpdateRunStatus updates the status of a run.
func (s *DirScanStore) UpdateRunStatus(ctx context.Context, runID int64, status DirScanRunStatus) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE dir_scan_runs SET status = ? WHERE id = ?
	`, status, runID)
	if err != nil {
		return fmt.Errorf("update run status: %w", err)
	}
	return nil
}

// UpdateRunStats updates the stats of a run.
func (s *DirScanStore) UpdateRunStats(ctx context.Context, runID int64, filesFound, filesSkipped, matchesFound, torrentsAdded int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE dir_scan_runs
		SET files_found = ?, files_skipped = ?, matches_found = ?, torrents_added = ?
		WHERE id = ?
	`, filesFound, filesSkipped, matchesFound, torrentsAdded, runID)
	if err != nil {
		return fmt.Errorf("update run stats: %w", err)
	}
	return nil
}

// UpdateRunCompleted marks a run as completed successfully.
func (s *DirScanStore) UpdateRunCompleted(ctx context.Context, runID int64, matchesFound, torrentsAdded int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE dir_scan_runs
		SET status = ?, matches_found = ?, torrents_added = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, DirScanRunStatusSuccess, matchesFound, torrentsAdded, runID)
	if err != nil {
		return fmt.Errorf("update run completed: %w", err)
	}
	return nil
}

// UpdateRunFailed marks a run as failed.
func (s *DirScanStore) UpdateRunFailed(ctx context.Context, runID int64, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE dir_scan_runs
		SET status = ?, error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, DirScanRunStatusFailed, errorMessage, runID)
	if err != nil {
		return fmt.Errorf("update run failed: %w", err)
	}
	return nil
}

// UpdateRunCanceled marks a run as canceled.
func (s *DirScanStore) UpdateRunCanceled(ctx context.Context, runID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE dir_scan_runs
		SET status = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, DirScanRunStatusCanceled, runID)
	if err != nil {
		return fmt.Errorf("update run canceled: %w", err)
	}
	return nil
}

// MarkActiveRunsFailed marks any in-progress runs as failed (typically after a restart).
func (s *DirScanStore) MarkActiveRunsFailed(ctx context.Context, errorMessage string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE dir_scan_runs
		SET status = ?, error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE status IN ('queued', 'scanning', 'searching', 'injecting')
	`, DirScanRunStatusFailed, errorMessage)
	if err != nil {
		return 0, fmt.Errorf("mark active runs failed: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}

	return rows, nil
}

// --- Run Injection Operations ---

const dirScanRunInjectionMaxPerRun = 256

// CreateRunInjection records a successful or failed injection attempt for a run.
func (s *DirScanStore) CreateRunInjection(ctx context.Context, injection *DirScanRunInjection) error {
	if err := validateRunInjection(injection); err != nil {
		return err
	}

	tagsJSON, err := marshalDirScanTagsJSON(injection.Tags)
	if err != nil {
		return err
	}

	if err := s.insertRunInjection(ctx, injection, tagsJSON); err != nil {
		return err
	}

	s.trimRunInjectionsBestEffort(ctx, injection.RunID)

	return nil
}

func validateRunInjection(injection *DirScanRunInjection) error {
	if injection == nil {
		return errors.New("injection is nil")
	}
	if injection.RunID <= 0 {
		return errors.New("runID must be > 0")
	}
	if injection.DirectoryID <= 0 {
		return errors.New("directoryID must be > 0")
	}
	if injection.Status == "" {
		return errors.New("status is required")
	}
	if injection.SearcheeName == "" {
		return errors.New("searchee name is required")
	}
	if injection.TorrentName == "" {
		return errors.New("torrent name is required")
	}
	if injection.InfoHash == "" {
		return errors.New("info hash is required")
	}
	if injection.ContentType == "" {
		return errors.New("content type is required")
	}

	return nil
}

func marshalDirScanTagsJSON(tags []string) (string, error) {
	if tags == nil {
		tags = []string{}
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return "", fmt.Errorf("marshal tags: %w", err)
	}
	return string(data), nil
}

func optionalString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *DirScanStore) insertRunInjection(ctx context.Context, injection *DirScanRunInjection, tagsJSON string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dir_scan_run_injections
			(run_id, directory_id, status, searchee_name, torrent_name, info_hash, content_type,
			 indexer_name, tracker_domain, tracker_display_name, link_mode, save_path, category, tags, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, injection.RunID, injection.DirectoryID, injection.Status, injection.SearcheeName, injection.TorrentName,
		injection.InfoHash, injection.ContentType,
		optionalString(injection.IndexerName),
		optionalString(injection.TrackerDomain),
		optionalString(injection.TrackerDisplayName),
		optionalString(injection.LinkMode),
		optionalString(injection.SavePath),
		optionalString(injection.Category),
		tagsJSON,
		optionalString(injection.ErrorMessage),
	)
	if err != nil {
		return fmt.Errorf("insert run injection: %w", err)
	}
	return nil
}

func (s *DirScanStore) trimRunInjectionsBestEffort(ctx context.Context, runID int64) {
	// Keep a bounded number of entries per run to avoid unbounded DB growth.
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM dir_scan_run_injections
		WHERE run_id = ?
		  AND id NOT IN (
			  SELECT id FROM dir_scan_run_injections
			  WHERE run_id = ?
			  ORDER BY id DESC
			  LIMIT ?
		  )
	`, runID, runID, dirScanRunInjectionMaxPerRun); err != nil {
		_ = err
	}
}

// ListRunInjections returns injection attempts for a run (newest first).
func (s *DirScanStore) ListRunInjections(ctx context.Context, directoryID int, runID int64, limit, offset int) ([]*DirScanRunInjection, error) {
	limit, offset, err := normalizePaginationArgs(directoryID, runID, limit, offset)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, directory_id, status, searchee_name, torrent_name, info_hash, content_type,
		       indexer_name, tracker_domain, tracker_display_name, link_mode, save_path, category, tags,
		       error_message, created_at
		FROM dir_scan_run_injections
		WHERE directory_id = ? AND run_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?
		`, directoryID, runID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query run injections: %w", err)
	}
	defer rows.Close()

	return s.scanRunInjectionsFromRows(rows)
}

func normalizePaginationArgs(directoryID int, runID int64, limit, offset int) (normalizedLimit, normalizedOffset int, err error) {
	if runID <= 0 {
		return 0, 0, errors.New("runID must be > 0")
	}
	if directoryID <= 0 {
		return 0, 0, errors.New("directoryID must be > 0")
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset, nil
}

func (s *DirScanStore) scanRunInjectionsFromRows(rows *sql.Rows) ([]*DirScanRunInjection, error) {
	injections := make([]*DirScanRunInjection, 0)
	for rows.Next() {
		inj, err := scanRunInjectionFromScanner(rows)
		if err != nil {
			return nil, err
		}
		injections = append(injections, inj)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run injections: %w", err)
	}
	return injections, nil
}

func scanRunInjectionFromScanner(scanner sqlScanner) (*DirScanRunInjection, error) {
	var inj DirScanRunInjection
	var indexerName sql.NullString
	var trackerDomain sql.NullString
	var trackerDisplayName sql.NullString
	var linkMode sql.NullString
	var savePath sql.NullString
	var category sql.NullString
	var tagsJSON sql.NullString
	var errorMessage sql.NullString

	if err := scanner.Scan(
		&inj.ID,
		&inj.RunID,
		&inj.DirectoryID,
		&inj.Status,
		&inj.SearcheeName,
		&inj.TorrentName,
		&inj.InfoHash,
		&inj.ContentType,
		&indexerName,
		&trackerDomain,
		&trackerDisplayName,
		&linkMode,
		&savePath,
		&category,
		&tagsJSON,
		&errorMessage,
		&inj.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan run injection columns: %w", err)
	}

	if indexerName.Valid {
		inj.IndexerName = indexerName.String
	}
	if trackerDomain.Valid {
		inj.TrackerDomain = trackerDomain.String
	}
	if trackerDisplayName.Valid {
		inj.TrackerDisplayName = trackerDisplayName.String
	}
	if linkMode.Valid {
		inj.LinkMode = linkMode.String
	}
	if savePath.Valid {
		inj.SavePath = savePath.String
	}
	if category.Valid {
		inj.Category = category.String
	}
	if errorMessage.Valid {
		inj.ErrorMessage = errorMessage.String
	}

	tags, err := unmarshalTags(tagsJSON)
	if err != nil {
		return nil, fmt.Errorf("unmarshal tags: %w", err)
	}
	if tags == nil {
		tags = []string{}
	}
	inj.Tags = tags

	return &inj, nil
}

// --- File Operations ---

func (s *DirScanStore) deleteDuplicateFileIDRows(ctx context.Context, directoryID int, fileID []byte, keepPath string) error {
	if s == nil || s.db == nil || directoryID <= 0 || len(fileID) == 0 || keepPath == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		DELETE FROM dir_scan_files
		WHERE directory_id = ? AND file_id = ? AND file_path <> ?
	`, directoryID, fileID, keepPath)
	if err != nil {
		return fmt.Errorf("dedupe file_id rows: %w", err)
	}
	return nil
}

// UpsertFile inserts or updates a scanned file.
func (s *DirScanStore) UpsertFile(ctx context.Context, file *DirScanFile) error {
	if file == nil {
		return errors.New("file is nil")
	}

	var matchedIndexerID any
	if file.MatchedIndexerID != nil {
		matchedIndexerID = *file.MatchedIndexerID
	}

	var matchedTorrentHash any
	if file.MatchedTorrentHash != "" {
		matchedTorrentHash = file.MatchedTorrentHash
	}

	// Handle renames via FileID where possible: if we have a platform-neutral FileID
	// and a tracked row exists for it, update its file_path instead of creating a new row.
	if len(file.FileID) > 0 {
		res, err := s.db.ExecContext(ctx, `
			UPDATE dir_scan_files
			SET file_path = ?,
			    file_size = ?,
			    file_mod_time = ?,
			    file_id = ?,
			    status = ?,
			    matched_torrent_hash = ?,
			    matched_indexer_id = ?,
			    last_processed_at = CURRENT_TIMESTAMP
			WHERE directory_id = ? AND file_id = ?
		`, file.FilePath, file.FileSize, file.FileModTime, file.FileID, file.Status,
			matchedTorrentHash, matchedIndexerID, file.DirectoryID, file.FileID)
		if err != nil {
			// If the target path is already tracked, fall back to the path-upsert which will merge state.
			var sqlErr *sqlite.Error
			if !errors.As(err, &sqlErr) || sqlErr.Code() != sqlite3.SQLITE_CONSTRAINT_UNIQUE {
				return fmt.Errorf("update by file_id: %w", err)
			}
		} else {
			rows, rowsErr := res.RowsAffected()
			if rowsErr != nil {
				return fmt.Errorf("rows affected: %w", rowsErr)
			}
			if rows > 0 {
				if err := s.deleteDuplicateFileIDRows(ctx, file.DirectoryID, file.FileID, file.FilePath); err != nil {
					return err
				}
				return nil
			}
		}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dir_scan_files
			(directory_id, file_path, file_size, file_mod_time, file_id, status,
			 matched_torrent_hash, matched_indexer_id, last_processed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(directory_id, file_path) DO UPDATE SET
			file_size = excluded.file_size,
			file_mod_time = excluded.file_mod_time,
			file_id = excluded.file_id,
			status = excluded.status,
			matched_torrent_hash = excluded.matched_torrent_hash,
			matched_indexer_id = excluded.matched_indexer_id,
			last_processed_at = CURRENT_TIMESTAMP
	`, file.DirectoryID, file.FilePath, file.FileSize, file.FileModTime, file.FileID,
		file.Status, matchedTorrentHash, matchedIndexerID)
	if err != nil {
		return fmt.Errorf("upsert file: %w", err)
	}

	if err := s.deleteDuplicateFileIDRows(ctx, file.DirectoryID, file.FileID, file.FilePath); err != nil {
		return err
	}

	return nil
}

// GetFileByPath retrieves a file by its path within a directory.
func (s *DirScanStore) GetFileByPath(ctx context.Context, directoryID int, filePath string) (*DirScanFile, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, directory_id, file_path, file_size, file_mod_time, file_id, status,
		       matched_torrent_hash, matched_indexer_id, last_processed_at
		FROM dir_scan_files
		WHERE directory_id = ? AND file_path = ?
	`, directoryID, filePath)

	return s.scanFile(row)
}

// GetFileByFileID retrieves a file by its FileID within a directory.
func (s *DirScanStore) GetFileByFileID(ctx context.Context, directoryID int, fileID []byte) (*DirScanFile, error) {
	if fileID == nil {
		return nil, nil
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, directory_id, file_path, file_size, file_mod_time, file_id, status,
		       matched_torrent_hash, matched_indexer_id, last_processed_at
		FROM dir_scan_files
		WHERE directory_id = ? AND file_id = ?
	`, directoryID, fileID)

	return s.scanFile(row)
}

func (s *DirScanStore) scanFile(row *sql.Row) (*DirScanFile, error) {
	file, err := scanFileFromScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return file, err
}

func scanFileFromScanner(scanner sqlScanner) (*DirScanFile, error) {
	var file DirScanFile
	var fileID []byte
	var matchedTorrentHash sql.NullString
	var matchedIndexerID sql.NullInt64
	var lastProcessedAt sql.NullTime

	if err := scanner.Scan(
		&file.ID,
		&file.DirectoryID,
		&file.FilePath,
		&file.FileSize,
		&file.FileModTime,
		&fileID,
		&file.Status,
		&matchedTorrentHash,
		&matchedIndexerID,
		&lastProcessedAt,
	); err != nil {
		return nil, fmt.Errorf("scan file columns: %w", err)
	}

	file.FileID = fileID
	if matchedTorrentHash.Valid {
		file.MatchedTorrentHash = matchedTorrentHash.String
	}
	if matchedIndexerID.Valid {
		id := int(matchedIndexerID.Int64)
		file.MatchedIndexerID = &id
	}
	if lastProcessedAt.Valid {
		file.LastProcessedAt = &lastProcessedAt.Time
	}

	return &file, nil
}

// ListFiles lists files for a directory with optional status filter.
func (s *DirScanStore) ListFiles(ctx context.Context, directoryID int, status *DirScanFileStatus, limit, offset int) ([]*DirScanFile, error) {
	if limit <= 0 {
		limit = 100
	}

	var query string
	var args []any

	if status != nil {
		query = `
			SELECT id, directory_id, file_path, file_size, file_mod_time, file_id, status,
			       matched_torrent_hash, matched_indexer_id, last_processed_at
			FROM dir_scan_files
			WHERE directory_id = ? AND status = ?
			ORDER BY file_path
			LIMIT ? OFFSET ?
		`
		args = []any{directoryID, *status, limit, offset}
	} else {
		query = `
			SELECT id, directory_id, file_path, file_size, file_mod_time, file_id, status,
			       matched_torrent_hash, matched_indexer_id, last_processed_at
			FROM dir_scan_files
			WHERE directory_id = ?
			ORDER BY file_path
			LIMIT ? OFFSET ?
		`
		args = []any{directoryID, limit, offset}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query files: %w", err)
	}
	defer rows.Close()

	return scanFilesFromRows(rows)
}

func scanFilesFromRows(rows *sql.Rows) ([]*DirScanFile, error) {
	var files []*DirScanFile
	for rows.Next() {
		file, err := scanFileFromScanner(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate files: %w", err)
	}
	return files, nil
}

// UpdateFileStatus updates the status of a file.
func (s *DirScanStore) UpdateFileStatus(ctx context.Context, fileID int64, status DirScanFileStatus) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE dir_scan_files SET status = ?, last_processed_at = CURRENT_TIMESTAMP WHERE id = ?
	`, status, fileID)
	if err != nil {
		return fmt.Errorf("update file status: %w", err)
	}
	return nil
}

// UpdateFileMatch updates the match info for a file.
func (s *DirScanStore) UpdateFileMatch(ctx context.Context, fileID int64, torrentHash string, indexerID int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE dir_scan_files
		SET status = ?, matched_torrent_hash = ?, matched_indexer_id = ?, last_processed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, DirScanFileStatusMatched, torrentHash, indexerID, fileID)
	if err != nil {
		return fmt.Errorf("update file match: %w", err)
	}
	return nil
}

// DeleteFilesForDirectory deletes all tracked files for a directory.
func (s *DirScanStore) DeleteFilesForDirectory(ctx context.Context, directoryID int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dir_scan_files WHERE directory_id = ?`, directoryID)
	if err != nil {
		return fmt.Errorf("delete files for directory: %w", err)
	}
	return nil
}

// CountFilesByStatus returns counts of files by status for a directory.
func (s *DirScanStore) CountFilesByStatus(ctx context.Context, directoryID int) (map[DirScanFileStatus]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT status, COUNT(*)
		FROM dir_scan_files
		WHERE directory_id = ?
		GROUP BY status
	`, directoryID)
	if err != nil {
		return nil, fmt.Errorf("query file counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[DirScanFileStatus]int)
	for rows.Next() {
		var status DirScanFileStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan count row: %w", err)
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate counts: %w", err)
	}

	return counts, nil
}
