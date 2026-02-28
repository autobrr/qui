// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

type BackupSettings struct {
	InstanceID        int       `json:"instanceId"`
	Enabled           bool      `json:"enabled"`
	HourlyEnabled     bool      `json:"hourlyEnabled"`
	DailyEnabled      bool      `json:"dailyEnabled"`
	WeeklyEnabled     bool      `json:"weeklyEnabled"`
	MonthlyEnabled    bool      `json:"monthlyEnabled"`
	KeepHourly        int       `json:"keepHourly"`
	KeepDaily         int       `json:"keepDaily"`
	KeepWeekly        int       `json:"keepWeekly"`
	KeepMonthly       int       `json:"keepMonthly"`
	IncludeCategories bool      `json:"includeCategories"`
	IncludeTags       bool      `json:"includeTags"`
	CustomPath        *string   `json:"customPath,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func DefaultBackupSettings(instanceID int) *BackupSettings {
	return &BackupSettings{
		InstanceID:        instanceID,
		Enabled:           false,
		HourlyEnabled:     false,
		DailyEnabled:      false,
		WeeklyEnabled:     false,
		MonthlyEnabled:    false,
		KeepHourly:        0,
		KeepDaily:         7,
		KeepWeekly:        4,
		KeepMonthly:       12,
		IncludeCategories: true,
		IncludeTags:       true,
		CustomPath:        nil,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
}

type BackupRunStatus string

const (
	BackupRunStatusPending  BackupRunStatus = "pending"
	BackupRunStatusRunning  BackupRunStatus = "running"
	BackupRunStatusSuccess  BackupRunStatus = "success"
	BackupRunStatusFailed   BackupRunStatus = "failed"
	BackupRunStatusCanceled BackupRunStatus = "canceled"
)

type BackupRunKind string

const (
	BackupRunKindManual  BackupRunKind = "manual"
	BackupRunKindHourly  BackupRunKind = "hourly"
	BackupRunKindDaily   BackupRunKind = "daily"
	BackupRunKindWeekly  BackupRunKind = "weekly"
	BackupRunKindMonthly BackupRunKind = "monthly"
)

type BackupRun struct {
	ID             int64                       `json:"id"`
	InstanceID     int                         `json:"instanceId"`
	Kind           BackupRunKind               `json:"kind"`
	Status         BackupRunStatus             `json:"status"`
	RequestedBy    string                      `json:"requestedBy"`
	RequestedAt    time.Time                   `json:"requestedAt"`
	StartedAt      *time.Time                  `json:"startedAt,omitempty"`
	CompletedAt    *time.Time                  `json:"completedAt,omitempty"`
	ArchivePath    *string                     `json:"archivePath,omitempty"`
	ManifestPath   *string                     `json:"manifestPath,omitempty"`
	TotalBytes     int64                       `json:"totalBytes"`
	TorrentCount   int                         `json:"torrentCount"`
	CategoryCounts map[string]int              `json:"categoryCounts,omitempty"`
	ErrorMessage   *string                     `json:"errorMessage,omitempty"`
	Categories     map[string]CategorySnapshot `json:"categories,omitempty"`
	Tags           []string                    `json:"tags,omitempty"`
	categoriesJSON *string
	tagsJSON       *string
}

type BackupItem struct {
	ID              int64     `json:"id"`
	RunID           int64     `json:"runId"`
	TorrentHash     string    `json:"torrentHash"`
	Name            string    `json:"name"`
	Category        *string   `json:"category,omitempty"`
	SizeBytes       int64     `json:"sizeBytes"`
	ArchiveRelPath  *string   `json:"archiveRelPath,omitempty"`
	InfoHashV1      *string   `json:"infohashV1,omitempty"`
	InfoHashV2      *string   `json:"infohashV2,omitempty"`
	Tags            *string   `json:"tags,omitempty"`
	TorrentBlobPath *string   `json:"torrentBlobPath,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

type CategorySnapshot struct {
	SavePath string `json:"savePath,omitempty"`
}

type BackupStore struct {
	db dbinterface.Querier
}

func NewBackupStore(db dbinterface.Querier) *BackupStore {
	return &BackupStore{db: db}
}

func (s *BackupStore) GetSettings(ctx context.Context, instanceID int) (*BackupSettings, error) {
	query := `
        SELECT instance_id, enabled, hourly_enabled, daily_enabled, weekly_enabled, monthly_enabled,
               keep_hourly, keep_daily, keep_weekly, keep_monthly,
               include_categories, include_tags, custom_path, created_at, updated_at
        FROM instance_backup_settings
        WHERE instance_id = ?
    `

	row := s.db.QueryRowContext(ctx, query, instanceID)

	var settings BackupSettings
	var customPath sql.NullString
	var createdAt sql.NullTime
	var updatedAt sql.NullTime

	err := row.Scan(
		&settings.InstanceID,
		&settings.Enabled,
		&settings.HourlyEnabled,
		&settings.DailyEnabled,
		&settings.WeeklyEnabled,
		&settings.MonthlyEnabled,
		&settings.KeepHourly,
		&settings.KeepDaily,
		&settings.KeepWeekly,
		&settings.KeepMonthly,
		&settings.IncludeCategories,
		&settings.IncludeTags,
		&customPath,
		&createdAt,
		&updatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DefaultBackupSettings(instanceID), nil
		}
		return nil, err
	}

	if customPath.Valid {
		settings.CustomPath = &customPath.String
	}
	if createdAt.Valid {
		settings.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		settings.UpdatedAt = updatedAt.Time
	}

	return &settings, nil
}

func (s *BackupStore) UpsertSettings(ctx context.Context, settings *BackupSettings) error {
	if settings == nil {
		return errors.New("settings cannot be nil")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
        INSERT INTO instance_backup_settings (
            instance_id, enabled, hourly_enabled, daily_enabled, weekly_enabled, monthly_enabled,
            keep_hourly, keep_daily, keep_weekly, keep_monthly,
            include_categories, include_tags, custom_path
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(instance_id) DO UPDATE SET
            enabled = excluded.enabled,
            hourly_enabled = excluded.hourly_enabled,
            daily_enabled = excluded.daily_enabled,
            weekly_enabled = excluded.weekly_enabled,
            monthly_enabled = excluded.monthly_enabled,
            keep_hourly = excluded.keep_hourly,
            keep_daily = excluded.keep_daily,
            keep_weekly = excluded.keep_weekly,
            keep_monthly = excluded.keep_monthly,
            include_categories = excluded.include_categories,
            include_tags = excluded.include_tags,
            custom_path = excluded.custom_path
    `

	_, err = tx.ExecContext(
		ctx,
		query,
		settings.InstanceID,
		settings.Enabled,
		settings.HourlyEnabled,
		settings.DailyEnabled,
		settings.WeeklyEnabled,
		settings.MonthlyEnabled,
		maxInt(settings.KeepHourly, 0),
		maxInt(settings.KeepDaily, 0),
		maxInt(settings.KeepWeekly, 0),
		maxInt(settings.KeepMonthly, 0),
		settings.IncludeCategories,
		settings.IncludeTags,
		settings.CustomPath,
	)

	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func maxInt(v int, floor int) int {
	if v < floor {
		return floor
	}
	return v
}

func (s *BackupStore) CreateRun(ctx context.Context, run *BackupRun) error {
	if run == nil {
		return errors.New("run cannot be nil")
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern all strings in a single call (convert required strings to pointers)
	kind := string(run.Kind)
	status := string(run.Status)
	allIDs, err := dbinterface.InternStringNullable(ctx, tx, &kind, &status, &run.RequestedBy, run.ArchivePath, run.ManifestPath, run.ErrorMessage)
	if err != nil {
		return fmt.Errorf("failed to intern strings: %w", err)
	}

	categoryJSON, err := marshalCategoryCounts(run.CategoryCounts)
	if err != nil {
		return err
	}

	categoriesJSON, err := marshalCategories(run.Categories)
	if err != nil {
		return err
	}

	tagsJSON, err := marshalTags(run.Tags)
	if err != nil {
		return err
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO instance_backup_runs (
			instance_id, kind_id, status_id, requested_by_id, requested_at, started_at, completed_at,
			archive_path_id, manifest_path_id, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, run.InstanceID, allIDs[0], allIDs[1], allIDs[2], run.RequestedAt, run.StartedAt, run.CompletedAt,
		allIDs[3], allIDs[4], run.TotalBytes, run.TorrentCount, categoryJSON, categoriesJSON, tagsJSON, allIDs[5]).Scan(&run.ID)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func marshalCategoryCounts(counts map[string]int) (*string, error) {
	if len(counts) == 0 {
		return nil, nil
	}

	data, err := json.Marshal(counts)
	if err != nil {
		return nil, err
	}
	s := string(data)
	return &s, nil
}

func unmarshalCategoryCounts(raw sql.NullString) (map[string]int, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}

	var counts map[string]int
	if err := json.Unmarshal([]byte(raw.String), &counts); err != nil {
		return nil, err
	}
	return counts, nil
}

func marshalCategories(categories map[string]CategorySnapshot) (*string, error) {
	if len(categories) == 0 {
		return nil, nil
	}

	data, err := json.Marshal(categories)
	if err != nil {
		return nil, err
	}
	s := string(data)
	return &s, nil
}

func unmarshalCategories(raw sql.NullString) (map[string]CategorySnapshot, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}

	var categories map[string]CategorySnapshot
	if err := json.Unmarshal([]byte(raw.String), &categories); err != nil {
		return nil, err
	}
	return categories, nil
}

func marshalTags(tags []string) (*string, error) {
	if len(tags) == 0 {
		return nil, nil
	}

	data, err := json.Marshal(tags)
	if err != nil {
		return nil, err
	}
	s := string(data)
	return &s, nil
}

func unmarshalTags(raw sql.NullString) ([]string, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}

	var tags []string
	if err := json.Unmarshal([]byte(raw.String), &tags); err != nil {
		return nil, err
	}
	return tags, nil
}

func (s *BackupStore) UpdateRunMetadata(ctx context.Context, runID int64, updateFn func(*BackupRun) error) error {
	// Start a transaction first to avoid deadlocks
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Read the current state WITHIN the transaction
	run, err := s.getRunForUpdate(ctx, tx, runID)
	if err != nil {
		return err
	}

	// Apply the update function
	if err = updateFn(run); err != nil {
		return err
	}

	// Prepare JSON data
	categoryJSON, err := marshalCategoryCounts(run.CategoryCounts)
	if err != nil {
		return err
	}

	categoriesJSON, err := marshalCategories(run.Categories)
	if err != nil {
		return err
	}

	tagsJSON, err := marshalTags(run.Tags)
	if err != nil {
		return err
	}

	// Intern all strings in a single call
	status := string(run.Status)
	allIDs, err := dbinterface.InternStringNullable(ctx, tx, &status, run.ArchivePath, run.ManifestPath, run.ErrorMessage)
	if err != nil {
		return fmt.Errorf("failed to intern strings: %w", err)
	}

	// Execute UPDATE with interned IDs
	_, err = tx.ExecContext(ctx, `
        UPDATE instance_backup_runs SET
            status_id = ?,
            started_at = ?,
            completed_at = ?,
            archive_path_id = ?,
            manifest_path_id = ?,
            total_bytes = ?,
            torrent_count = ?,
            category_counts_json = ?,
            categories_json = ?,
            tags_json = ?,
            error_message_id = ?
        WHERE id = ?
	`, allIDs[0], run.StartedAt, run.CompletedAt, allIDs[1], allIDs[2],
		run.TotalBytes, run.TorrentCount, categoryJSON, categoriesJSON, tagsJSON, allIDs[3], runID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// UpdateMultipleRunsStatus updates the status of multiple backup runs in a single transaction
func (s *BackupStore) UpdateMultipleRunsStatus(ctx context.Context, runIDs []int64, status BackupRunStatus, completedAt *time.Time, errorMessage *string) error {
	if len(runIDs) == 0 {
		return nil
	}

	// Process in chunks to avoid hitting SQLite parameter limits
	const chunkSize = 900

	for i := 0; i < len(runIDs); i += chunkSize {
		end := i + chunkSize
		if end > len(runIDs) {
			end = len(runIDs)
		}
		chunk := runIDs[i:end]

		if err := s.updateMultipleRunsStatusChunk(ctx, chunk, status, completedAt, errorMessage); err != nil {
			return err
		}
	}

	return nil
}

func (s *BackupStore) updateMultipleRunsStatusChunk(ctx context.Context, runIDs []int64, status BackupRunStatus, completedAt *time.Time, errorMessage *string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Intern all strings in a single call
	statusStr := string(status)
	allIDs, err := dbinterface.InternStringNullable(ctx, tx, &statusStr, errorMessage)
	if err != nil {
		return fmt.Errorf("failed to intern strings: %w", err)
	}

	query := "UPDATE instance_backup_runs SET status_id = ?, completed_at = ?, error_message_id = ? WHERE id IN " + buildInPlaceholders(len(runIDs))

	args := []any{allIDs[0]}
	if completedAt != nil {
		args = append(args, *completedAt)
	} else {
		args = append(args, nil)
	}
	args = append(args, allIDs[1])

	for _, runID := range runIDs {
		args = append(args, runID)
	}

	_, err = tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to execute update query: %w", err)
	}

	return tx.Commit()
}

func (s *BackupStore) getRunForUpdate(ctx context.Context, tx dbinterface.TxQuerier, runID int64) (*BackupRun, error) {
	query := `
		SELECT id, instance_id, kind, status, requested_by, requested_at, started_at, completed_at,
		       archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message
		FROM instance_backup_runs_view
		WHERE id = ?
	`

	row := tx.QueryRowContext(ctx, query, runID)

	var run BackupRun
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	var archivePath sql.NullString
	var manifestPath sql.NullString
	var errorMessage sql.NullString
	var categoryJSON sql.NullString
	var categoriesJSON sql.NullString
	var tagsJSON sql.NullString

	err := row.Scan(
		&run.ID,
		&run.InstanceID,
		&run.Kind,
		&run.Status,
		&run.RequestedBy,
		&run.RequestedAt,
		&startedAt,
		&completedAt,
		&archivePath,
		&manifestPath,
		&run.TotalBytes,
		&run.TorrentCount,
		&categoryJSON,
		&categoriesJSON,
		&tagsJSON,
		&errorMessage,
	)
	if err != nil {
		return nil, err
	}

	if startedAt.Valid {
		run.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	if archivePath.Valid {
		run.ArchivePath = &archivePath.String
	}
	if manifestPath.Valid {
		run.ManifestPath = &manifestPath.String
	}
	if errorMessage.Valid {
		run.ErrorMessage = &errorMessage.String
	}

	counts, err := unmarshalCategoryCounts(categoryJSON)
	if err != nil {
		return nil, err
	}
	run.CategoryCounts = counts

	if categories, err := unmarshalCategories(categoriesJSON); err != nil {
		return nil, err
	} else {
		run.Categories = categories
	}

	if tagList, err := unmarshalTags(tagsJSON); err != nil {
		return nil, err
	} else {
		run.Tags = tagList
	}

	if categoriesJSON.Valid {
		run.categoriesJSON = &categoriesJSON.String
	}
	if tagsJSON.Valid {
		run.tagsJSON = &tagsJSON.String
	}

	return &run, nil
}

func (s *BackupStore) ListRuns(ctx context.Context, instanceID int, limit, offset int) ([]*BackupRun, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `
        SELECT id, instance_id, kind, status, requested_by, requested_at, started_at, completed_at,
               archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message
        FROM instance_backup_runs_view
        WHERE instance_id = ?
        ORDER BY requested_at DESC
        LIMIT ? OFFSET ?
    `

	rows, err := s.db.QueryContext(ctx, query, instanceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]*BackupRun, 0)

	for rows.Next() {
		var run BackupRun
		var startedAt sql.NullTime
		var completedAt sql.NullTime
		var archivePath sql.NullString
		var manifestPath sql.NullString
		var errorMessage sql.NullString
		var categoryJSON sql.NullString
		var categoriesJSON sql.NullString
		var tagsJSON sql.NullString

		if err := rows.Scan(
			&run.ID,
			&run.InstanceID,
			&run.Kind,
			&run.Status,
			&run.RequestedBy,
			&run.RequestedAt,
			&startedAt,
			&completedAt,
			&archivePath,
			&manifestPath,
			&run.TotalBytes,
			&run.TorrentCount,
			&categoryJSON,
			&categoriesJSON,
			&tagsJSON,
			&errorMessage,
		); err != nil {
			return nil, err
		}

		if startedAt.Valid {
			run.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			run.CompletedAt = &completedAt.Time
		}
		if archivePath.Valid {
			run.ArchivePath = &archivePath.String
		}
		if manifestPath.Valid {
			run.ManifestPath = &manifestPath.String
		}
		if errorMessage.Valid {
			run.ErrorMessage = &errorMessage.String
		}
		counts, err := unmarshalCategoryCounts(categoryJSON)
		if err != nil {
			return nil, err
		}
		run.CategoryCounts = counts
		if categories, err := unmarshalCategories(categoriesJSON); err != nil {
			return nil, err
		} else {
			run.Categories = categories
		}
		if tagList, err := unmarshalTags(tagsJSON); err != nil {
			return nil, err
		} else {
			run.Tags = tagList
		}
		if categoriesJSON.Valid {
			run.categoriesJSON = &categoriesJSON.String
		}
		if tagsJSON.Valid {
			run.tagsJSON = &tagsJSON.String
		}

		results = append(results, &run)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func (s *BackupStore) ListRunIDs(ctx context.Context, instanceID int) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM instance_backup_runs
		WHERE instance_id = ?
		ORDER BY requested_at DESC
	`, instanceID)
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

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return ids, nil
}

func (s *BackupStore) DeleteRun(ctx context.Context, runID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, "DELETE FROM instance_backup_runs WHERE id = ?", runID)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (s *BackupStore) InsertItems(ctx context.Context, runID int64, items []BackupItem) error {
	if len(items) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Optimize for large bulk inserts (e.g., 180k+ torrents)
	// Temporarily disable foreign key checks for massive performance boost
	if err := dbinterface.DeferForeignKeyChecks(ctx, tx); err != nil {
		return fmt.Errorf("failed to defer foreign keys: %w", err)
	}

	// Pre-deduplicate all strings before interning to minimize database operations
	// This is crucial for performance when dealing with thousands of items
	// Estimate capacity: torrents often share categories/tags, so less unique values than items
	estimatedUniqueStrings := len(items) / 10
	if estimatedUniqueStrings < 100 {
		estimatedUniqueStrings = 100
	}

	uniqueRequired := make(map[string]struct{}, estimatedUniqueStrings)
	uniqueOptional := make(map[string]struct{}, estimatedUniqueStrings)

	// Collect unique required strings (hash + name)
	for _, item := range items {
		uniqueRequired[item.TorrentHash] = struct{}{}
		uniqueRequired[item.Name] = struct{}{}
	}

	// Collect unique optional strings
	for _, item := range items {
		if item.Category != nil && *item.Category != "" {
			uniqueOptional[*item.Category] = struct{}{}
		}
		if item.ArchiveRelPath != nil && *item.ArchiveRelPath != "" {
			uniqueOptional[*item.ArchiveRelPath] = struct{}{}
		}
		if item.InfoHashV1 != nil && *item.InfoHashV1 != "" {
			uniqueOptional[*item.InfoHashV1] = struct{}{}
		}
		if item.InfoHashV2 != nil && *item.InfoHashV2 != "" {
			uniqueOptional[*item.InfoHashV2] = struct{}{}
		}
		if item.Tags != nil && *item.Tags != "" {
			uniqueOptional[*item.Tags] = struct{}{}
		}
		if item.TorrentBlobPath != nil && *item.TorrentBlobPath != "" {
			uniqueOptional[*item.TorrentBlobPath] = struct{}{}
		}
	}

	// Convert maps to slices for interning
	requiredStrings := make([]string, 0, len(uniqueRequired))
	for s := range uniqueRequired {
		requiredStrings = append(requiredStrings, s)
	}

	// Batch intern all required strings at once
	requiredIDs, err := dbinterface.InternStrings(ctx, tx, requiredStrings...)
	if err != nil {
		return fmt.Errorf("failed to batch intern required strings: %w", err)
	}

	// Build map from string -> ID with proper capacity
	totalUniqueStrings := len(uniqueRequired) + len(uniqueOptional)
	stringToID := make(map[string]int64, totalUniqueStrings)
	for i, s := range requiredStrings {
		stringToID[s] = requiredIDs[i]
	}

	// Intern optional strings if any exist
	if len(uniqueOptional) > 0 {
		optionalStrings := make([]string, 0, len(uniqueOptional))
		for s := range uniqueOptional {
			optionalStrings = append(optionalStrings, s)
		}

		optionalIDs, err := dbinterface.InternStrings(ctx, tx, optionalStrings...)
		if err != nil {
			return fmt.Errorf("failed to batch intern optional strings: %w", err)
		}

		// Add to the same map
		for i, s := range optionalStrings {
			stringToID[s] = optionalIDs[i]
		}
	}

	// Helper to get ID from map
	getID := func(s *string) sql.NullInt64 {
		if s == nil || *s == "" {
			return sql.NullInt64{Valid: false}
		}
		if id, ok := stringToID[*s]; ok {
			return sql.NullInt64{Int64: id, Valid: true}
		}
		return sql.NullInt64{Valid: false}
	}

	// Batch insert items with larger chunks for better performance
	// SQLite SQLITE_MAX_VARIABLE_NUMBER is typically 32766 on modern systems
	// but default is 999. Use 90 items * 10 params = 900 to stay safe
	const chunkSize = 90

	// Pre-build the query template for full chunks to avoid repeated string building in hot path
	queryTemplate := `INSERT INTO instance_backup_items (
		run_id, torrent_hash_id, name_id, category_id, size_bytes,
		archive_rel_path_id, infohash_v1_id, infohash_v2_id, tags_id, torrent_blob_path_id
	) VALUES %s`
	fullQuery := dbinterface.BuildQueryWithPlaceholders(queryTemplate, 10, chunkSize)

	for i := 0; i < len(items); i += chunkSize {
		end := i + chunkSize
		if end > len(items) {
			end = len(items)
		}
		chunk := items[i:end]

		// Use pre-built query for full chunks, build new one only for smaller final chunk
		query := fullQuery
		if len(chunk) < chunkSize {
			query = dbinterface.BuildQueryWithPlaceholders(queryTemplate, 10, len(chunk))
		}

		args := make([]any, 0, len(chunk)*10)
		for _, item := range chunk {
			// Get IDs from the stringToID map for required fields
			torrentHashID := stringToID[item.TorrentHash]
			nameID := stringToID[item.Name]

			args = append(args,
				runID,
				torrentHashID,
				nameID,
				getID(item.Category),
				item.SizeBytes,
				getID(item.ArchiveRelPath),
				getID(item.InfoHashV1),
				getID(item.InfoHashV2),
				getID(item.Tags),
				getID(item.TorrentBlobPath),
			)
		}

		_, err = tx.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("failed to batch insert items: %w", err)
		}
	}

	return tx.Commit()
}

func (s *BackupStore) ListItems(ctx context.Context, runID int64) ([]*BackupItem, error) {
	orderByName := "name COLLATE NOCASE"
	if dbinterface.DialectOf(s.db) == "postgres" {
		orderByName = "LOWER(name)"
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, run_id, torrent_hash, name, category, size_bytes, archive_rel_path, infohash_v1, infohash_v2, tags, torrent_blob_path, created_at
		FROM instance_backup_items_view
		WHERE run_id = ?
		ORDER BY %s
	`, orderByName), runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*BackupItem, 0)

	for rows.Next() {
		var item BackupItem
		var category sql.NullString
		var relPath sql.NullString
		var infohashV1 sql.NullString
		var infohashV2 sql.NullString
		var tags sql.NullString
		var blobPath sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.RunID,
			&item.TorrentHash,
			&item.Name,
			&category,
			&item.SizeBytes,
			&relPath,
			&infohashV1,
			&infohashV2,
			&tags,
			&blobPath,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if category.Valid {
			item.Category = &category.String
		}
		if relPath.Valid {
			item.ArchiveRelPath = &relPath.String
		}
		if infohashV1.Valid {
			item.InfoHashV1 = &infohashV1.String
		}
		if infohashV2.Valid {
			item.InfoHashV2 = &infohashV2.String
		}
		if tags.Valid {
			item.Tags = &tags.String
		}
		if blobPath.Valid {
			item.TorrentBlobPath = &blobPath.String
		}
		items = append(items, &item)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (s *BackupStore) ListItemsForRuns(ctx context.Context, runIDs []int64) ([]*BackupItem, error) {
	if len(runIDs) == 0 {
		return nil, nil
	}

	// Process in chunks to avoid hitting SQLite parameter limits
	const chunkSize = 900

	var allItems []*BackupItem

	for i := 0; i < len(runIDs); i += chunkSize {
		end := i + chunkSize
		if end > len(runIDs) {
			end = len(runIDs)
		}
		chunk := runIDs[i:end]

		items, err := s.listItemsForRunsChunk(ctx, chunk)
		if err != nil {
			return nil, err
		}
		allItems = append(allItems, items...)
	}

	return allItems, nil
}

func (s *BackupStore) listItemsForRunsChunk(ctx context.Context, runIDs []int64) ([]*BackupItem, error) {
	orderByName := "name COLLATE NOCASE"
	if dbinterface.DialectOf(s.db) == "postgres" {
		orderByName = "LOWER(name)"
	}

	args := make([]interface{}, len(runIDs))
	for i, id := range runIDs {
		args[i] = id
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, run_id, torrent_hash, name, category, size_bytes, archive_rel_path, infohash_v1, infohash_v2, tags, torrent_blob_path, created_at
		FROM instance_backup_items_view
		WHERE run_id IN `+buildInPlaceholders(len(runIDs))+`
		ORDER BY run_id, %s
	`, orderByName), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*BackupItem, 0)

	for rows.Next() {
		var item BackupItem
		var category sql.NullString
		var relPath sql.NullString
		var infohashV1 sql.NullString
		var infohashV2 sql.NullString
		var tags sql.NullString
		var blobPath sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.RunID,
			&item.TorrentHash,
			&item.Name,
			&category,
			&item.SizeBytes,
			&relPath,
			&infohashV1,
			&infohashV2,
			&tags,
			&blobPath,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if category.Valid {
			item.Category = &category.String
		}
		if relPath.Valid {
			item.ArchiveRelPath = &relPath.String
		}
		if infohashV1.Valid {
			item.InfoHashV1 = &infohashV1.String
		}
		if infohashV2.Valid {
			item.InfoHashV2 = &infohashV2.String
		}
		if tags.Valid {
			item.Tags = &tags.String
		}
		if blobPath.Valid {
			item.TorrentBlobPath = &blobPath.String
		}
		items = append(items, &item)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (s *BackupStore) GetItemByHash(ctx context.Context, runID int64, hash string) (*BackupItem, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, run_id, torrent_hash, name, category, size_bytes, archive_rel_path, infohash_v1, infohash_v2, tags, torrent_blob_path, created_at
		FROM instance_backup_items_view
		WHERE run_id = ? AND torrent_hash = ?
		LIMIT 1
	`, runID, hash)

	var item BackupItem
	var category sql.NullString
	var relPath sql.NullString
	var infohashV1 sql.NullString
	var infohashV2 sql.NullString
	var tags sql.NullString
	var blobPath sql.NullString

	if err := row.Scan(
		&item.ID,
		&item.RunID,
		&item.TorrentHash,
		&item.Name,
		&category,
		&item.SizeBytes,
		&relPath,
		&infohashV1,
		&infohashV2,
		&tags,
		&blobPath,
		&item.CreatedAt,
	); err != nil {
		return nil, err
	}

	if category.Valid {
		item.Category = &category.String
	}
	if relPath.Valid {
		item.ArchiveRelPath = &relPath.String
	}
	if infohashV1.Valid {
		item.InfoHashV1 = &infohashV1.String
	}
	if infohashV2.Valid {
		item.InfoHashV2 = &infohashV2.String
	}
	if tags.Valid {
		item.Tags = &tags.String
	}
	if blobPath.Valid {
		item.TorrentBlobPath = &blobPath.String
	}

	return &item, nil
}

func (s *BackupStore) FindCachedTorrentBlob(ctx context.Context, instanceID int, hash string) (*string, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT i.torrent_blob_path
		FROM instance_backup_items_view i
		JOIN instance_backup_runs r ON r.id = i.run_id
		WHERE r.instance_id = ?
		  AND i.torrent_hash = ?
		  AND i.torrent_blob_path IS NOT NULL
		ORDER BY i.created_at DESC
		LIMIT 1
	`, instanceID, hash)

	var path sql.NullString
	if err := row.Scan(&path); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if !path.Valid {
		return nil, nil
	}

	trimmed := strings.TrimSpace(path.String)
	if trimmed == "" {
		return nil, nil
	}

	return &trimmed, nil
}

func (s *BackupStore) CountBlobReferences(ctx context.Context, relPath string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM instance_backup_items_view
		WHERE torrent_blob_path = ?
	`, relPath).Scan(&count)

	if err != nil {
		return 0, err
	}

	return count, nil
}

func (s *BackupStore) CountBlobReferencesBatch(ctx context.Context, relPaths []string) (map[string]int, error) {
	return s.countBlobReferencesBatchChunk(ctx, relPaths)
}

func (s *BackupStore) countBlobReferencesBatchChunk(ctx context.Context, relPaths []string) (map[string]int, error) {
	if len(relPaths) == 0 {
		return nil, nil
	}

	result := make(map[string]int)

	// Process in chunks to avoid hitting SQLite parameter limits
	const chunkSize = 900

	for i := 0; i < len(relPaths); i += chunkSize {
		end := i + chunkSize
		if end > len(relPaths) {
			end = len(relPaths)
		}
		chunk := relPaths[i:end]

		chunkResult, err := s.countBlobReferencesChunk(ctx, chunk)
		if err != nil {
			return nil, err
		}

		// Merge results
		for path, count := range chunkResult {
			result[path] = count
		}
	}

	return result, nil
}

func (s *BackupStore) countBlobReferencesChunk(ctx context.Context, relPaths []string) (map[string]int, error) {
	args := make([]interface{}, len(relPaths))
	for i, path := range relPaths {
		args[i] = path
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT torrent_blob_path, COUNT(*) as ref_count
		FROM instance_backup_items_view
		WHERE torrent_blob_path IN `+buildInPlaceholders(len(relPaths))+`
		GROUP BY torrent_blob_path
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var path string
		var count int
		if err := rows.Scan(&path, &count); err != nil {
			return nil, err
		}
		result[path] = count
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *BackupStore) GetInstanceName(ctx context.Context, instanceID int) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `
		SELECT sp.value
		FROM instances i
		JOIN string_pool sp ON i.name_id = sp.id
		WHERE i.id = ?
	`, instanceID).Scan(&name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrInstanceNotFound
		}
		return "", err
	}

	return strings.TrimSpace(name), nil
}

func (s *BackupStore) ListRunsByKind(ctx context.Context, instanceID int, kind BackupRunKind, limit int) ([]*BackupRun, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `
		SELECT id, instance_id, kind, status, requested_by, requested_at, started_at, completed_at,
		       archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message
		FROM instance_backup_runs_view
		WHERE instance_id = ? AND kind = ?
		ORDER BY requested_at DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, instanceID, string(kind), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]*BackupRun, 0)
	for rows.Next() {
		var run BackupRun
		var startedAt sql.NullTime
		var completedAt sql.NullTime
		var archivePath sql.NullString
		var manifestPath sql.NullString
		var errorMessage sql.NullString
		var categoryJSON sql.NullString
		var categoriesJSON sql.NullString
		var tagsJSON sql.NullString

		if err := rows.Scan(
			&run.ID,
			&run.InstanceID,
			&run.Kind,
			&run.Status,
			&run.RequestedBy,
			&run.RequestedAt,
			&startedAt,
			&completedAt,
			&archivePath,
			&manifestPath,
			&run.TotalBytes,
			&run.TorrentCount,
			&categoryJSON,
			&categoriesJSON,
			&tagsJSON,
			&errorMessage,
		); err != nil {
			return nil, err
		}

		if startedAt.Valid {
			run.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			run.CompletedAt = &completedAt.Time
		}
		if archivePath.Valid {
			run.ArchivePath = &archivePath.String
		}
		if manifestPath.Valid {
			run.ManifestPath = &manifestPath.String
		}
		if errorMessage.Valid {
			run.ErrorMessage = &errorMessage.String
		}

		counts, err := unmarshalCategoryCounts(categoryJSON)
		if err != nil {
			return nil, err
		}
		run.CategoryCounts = counts
		if categories, err := unmarshalCategories(categoriesJSON); err != nil {
			return nil, err
		} else {
			run.Categories = categories
		}
		if tagList, err := unmarshalTags(tagsJSON); err != nil {
			return nil, err
		} else {
			run.Tags = tagList
		}
		if categoriesJSON.Valid {
			run.categoriesJSON = &categoriesJSON.String
		}
		if tagsJSON.Valid {
			run.tagsJSON = &tagsJSON.String
		}

		runs = append(runs, &run)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return runs, nil
}

func (s *BackupStore) GetRun(ctx context.Context, runID int64) (*BackupRun, error) {
	query := `
        SELECT id, instance_id, kind, status, requested_by, requested_at, started_at, completed_at,
               archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message
        FROM instance_backup_runs_view
        WHERE id = ?
    `

	var run BackupRun
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	var archivePath sql.NullString
	var manifestPath sql.NullString
	var errorMessage sql.NullString
	var categoryJSON sql.NullString
	var categoriesJSON sql.NullString
	var tagsJSON sql.NullString

	err := s.db.QueryRowContext(ctx, query, runID).Scan(
		&run.ID,
		&run.InstanceID,
		&run.Kind,
		&run.Status,
		&run.RequestedBy,
		&run.RequestedAt,
		&startedAt,
		&completedAt,
		&archivePath,
		&manifestPath,
		&run.TotalBytes,
		&run.TorrentCount,
		&categoryJSON,
		&categoriesJSON,
		&tagsJSON,
		&errorMessage,
	)
	if err != nil {
		return nil, err
	}

	if startedAt.Valid {
		run.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	if archivePath.Valid {
		run.ArchivePath = &archivePath.String
	}
	if manifestPath.Valid {
		run.ManifestPath = &manifestPath.String
	}
	if errorMessage.Valid {
		run.ErrorMessage = &errorMessage.String
	}

	counts, err := unmarshalCategoryCounts(categoryJSON)
	if err != nil {
		return nil, err
	}
	run.CategoryCounts = counts
	if categories, err := unmarshalCategories(categoriesJSON); err != nil {
		return nil, err
	} else {
		run.Categories = categories
	}
	if tagList, err := unmarshalTags(tagsJSON); err != nil {
		return nil, err
	} else {
		run.Tags = tagList
	}
	if categoriesJSON.Valid {
		run.categoriesJSON = &categoriesJSON.String
	}
	if tagsJSON.Valid {
		run.tagsJSON = &tagsJSON.String
	}

	return &run, nil
}

func (s *BackupStore) GetRuns(ctx context.Context, runIDs []int64) ([]*BackupRun, error) {
	if len(runIDs) == 0 {
		return nil, nil
	}

	// Process in chunks to avoid hitting SQLite parameter limits
	const chunkSize = 900

	var allRuns []*BackupRun

	for i := 0; i < len(runIDs); i += chunkSize {
		end := i + chunkSize
		if end > len(runIDs) {
			end = len(runIDs)
		}
		chunk := runIDs[i:end]

		runs, err := s.getRunsChunk(ctx, chunk)
		if err != nil {
			return nil, err
		}
		allRuns = append(allRuns, runs...)
	}

	// Reorder to match input order
	runMap := make(map[int64]*BackupRun)
	for _, run := range allRuns {
		runMap[run.ID] = run
	}
	orderedRuns := make([]*BackupRun, len(runIDs))
	for i, id := range runIDs {
		orderedRuns[i] = runMap[id]
	}

	return orderedRuns, nil
}

func (s *BackupStore) getRunsChunk(ctx context.Context, runIDs []int64) ([]*BackupRun, error) {
	args := make([]interface{}, len(runIDs))
	for i, id := range runIDs {
		args[i] = id
	}

	query := `
        SELECT id, instance_id, kind, status, requested_by, requested_at, started_at, completed_at,
               archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message
        FROM instance_backup_runs_view
        WHERE id IN ` + buildInPlaceholders(len(runIDs))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]*BackupRun, 0, len(runIDs))

	for rows.Next() {
		var run BackupRun
		var startedAt sql.NullTime
		var completedAt sql.NullTime
		var archivePath sql.NullString
		var manifestPath sql.NullString
		var errorMessage sql.NullString
		var categoryJSON sql.NullString
		var categoriesJSON sql.NullString
		var tagsJSON sql.NullString

		err := rows.Scan(
			&run.ID,
			&run.InstanceID,
			&run.Kind,
			&run.Status,
			&run.RequestedBy,
			&run.RequestedAt,
			&startedAt,
			&completedAt,
			&archivePath,
			&manifestPath,
			&run.TotalBytes,
			&run.TorrentCount,
			&categoryJSON,
			&categoriesJSON,
			&tagsJSON,
			&errorMessage,
		)
		if err != nil {
			return nil, err
		}

		if startedAt.Valid {
			run.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			run.CompletedAt = &completedAt.Time
		}
		if archivePath.Valid {
			run.ArchivePath = &archivePath.String
		}
		if manifestPath.Valid {
			run.ManifestPath = &manifestPath.String
		}
		if errorMessage.Valid {
			run.ErrorMessage = &errorMessage.String
		}

		counts, err := unmarshalCategoryCounts(categoryJSON)
		if err != nil {
			return nil, err
		}
		run.CategoryCounts = counts
		if categories, err := unmarshalCategories(categoriesJSON); err != nil {
			return nil, err
		} else {
			run.Categories = categories
		}
		if tagList, err := unmarshalTags(tagsJSON); err != nil {
			return nil, err
		} else {
			run.Tags = tagList
		}
		if categoriesJSON.Valid {
			run.categoriesJSON = &categoriesJSON.String
		}
		if tagsJSON.Valid {
			run.tagsJSON = &tagsJSON.String
		}

		runs = append(runs, &run)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return runs, nil
}

func (s *BackupStore) ListEnabledSettings(ctx context.Context) ([]*BackupSettings, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT instance_id, enabled, hourly_enabled, daily_enabled, weekly_enabled, monthly_enabled,
		       keep_hourly, keep_daily, keep_weekly, keep_monthly,
		       include_categories, include_tags, custom_path, created_at, updated_at
		FROM instance_backup_settings
		WHERE enabled = 1
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make([]*BackupSettings, 0)

	for rows.Next() {
		var s BackupSettings
		var customPath sql.NullString
		var createdAt sql.NullTime
		var updatedAt sql.NullTime

		if err := rows.Scan(
			&s.InstanceID,
			&s.Enabled,
			&s.HourlyEnabled,
			&s.DailyEnabled,
			&s.WeeklyEnabled,
			&s.MonthlyEnabled,
			&s.KeepHourly,
			&s.KeepDaily,
			&s.KeepWeekly,
			&s.KeepMonthly,
			&s.IncludeCategories,
			&s.IncludeTags,
			&customPath,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}

		if customPath.Valid {
			s.CustomPath = &customPath.String
		}
		if createdAt.Valid {
			s.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			s.UpdatedAt = updatedAt.Time
		}

		settings = append(settings, &s)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return settings, nil
}

func (s *BackupStore) DeleteRunsOlderThan(ctx context.Context, instanceID int, kind BackupRunKind, keep int) ([]int64, error) {
	if keep <= 0 {
		// Use view to query by kind string value
		query := `
            SELECT id FROM instance_backup_runs_view
            WHERE instance_id = ? AND kind = ?
        `
		rows, err := s.db.QueryContext(ctx, query, instanceID, string(kind))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		ids := make([]int64, 0)
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}

		if err = rows.Err(); err != nil {
			return nil, err
		}

		return ids, nil
	}

	// Use view to query by kind string value
	query := deleteRunsOlderThanQuery(dbinterface.DialectOf(s.db))
	args := []any{instanceID, string(kind), keep}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return ids, nil
}

func deleteRunsOlderThanQuery(dialect string) string {
	base := `
        SELECT id FROM instance_backup_runs_view
        WHERE instance_id = ? AND kind = ?
        ORDER BY requested_at DESC
    `
	if strings.EqualFold(strings.TrimSpace(dialect), "postgres") {
		// PostgreSQL rejects LIMIT -1; OFFSET without LIMIT is valid.
		return base + `
        OFFSET ?
    `
	}

	// SQLite requires LIMIT when using OFFSET; LIMIT -1 means "no limit".
	return base + `
        LIMIT -1 OFFSET ?
    `
}

func (s *BackupStore) DeleteItemsByRunIDs(ctx context.Context, runIDs []int64) error {
	if len(runIDs) == 0 {
		return nil
	}

	// Process in chunks to avoid hitting SQLite parameter limits
	const chunkSize = 900

	for i := 0; i < len(runIDs); i += chunkSize {
		end := i + chunkSize
		if end > len(runIDs) {
			end = len(runIDs)
		}
		chunk := runIDs[i:end]

		if err := s.deleteItemsByRunIDsChunk(ctx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (s *BackupStore) deleteItemsByRunIDsChunk(ctx context.Context, runIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := "DELETE FROM instance_backup_items WHERE run_id IN " + buildInPlaceholders(len(runIDs))

	args := make([]any, len(runIDs))
	for i, id := range runIDs {
		args[i] = id
	}

	_, err = tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func buildInPlaceholders(count int) string {
	if count <= 0 {
		return "()"
	}
	return dbinterface.BuildQueryWithPlaceholders("%s", count, 1)
}

func (s *BackupStore) DeleteRunsByIDs(ctx context.Context, runIDs []int64) error {
	if len(runIDs) == 0 {
		return nil
	}

	// Process in chunks to avoid hitting SQLite parameter limits
	const chunkSize = 900

	for i := 0; i < len(runIDs); i += chunkSize {
		end := i + chunkSize
		if end > len(runIDs) {
			end = len(runIDs)
		}
		chunk := runIDs[i:end]

		if err := s.deleteRunsByIDsChunk(ctx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (s *BackupStore) deleteRunsByIDsChunk(ctx context.Context, runIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := "DELETE FROM instance_backup_runs WHERE id IN " + buildInPlaceholders(len(runIDs))
	args := make([]any, len(runIDs))
	for i, id := range runIDs {
		args[i] = id
	}

	_, err = tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (s *BackupStore) CountRunsByKind(ctx context.Context, instanceID int, kind BackupRunKind) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
        SELECT COUNT(*)
        FROM instance_backup_runs_view
        WHERE instance_id = ? AND kind = ?
    `, instanceID, string(kind)).Scan(&count)

	if err != nil {
		return 0, err
	}

	return count, nil
}

func (s *BackupStore) LatestRunByKind(ctx context.Context, instanceID int, kind BackupRunKind) (*BackupRun, error) {
	runs, err := s.ListRunsByKind(ctx, instanceID, kind, 1)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, sql.ErrNoRows
	}
	return runs[0], nil
}

func (s *BackupStore) CleanupRun(ctx context.Context, runID int64) error {
	return s.CleanupRuns(ctx, []int64{runID})
}

func (s *BackupStore) CleanupRuns(ctx context.Context, runIDs []int64) error {
	if len(runIDs) == 0 {
		return nil
	}

	// Process in chunks to avoid hitting SQLite parameter limits
	const chunkSize = 900 // Well under typical SQLite limits

	for i := 0; i < len(runIDs); i += chunkSize {
		end := i + chunkSize
		if end > len(runIDs) {
			end = len(runIDs)
		}
		chunk := runIDs[i:end]

		if err := s.cleanupRunsChunk(ctx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (s *BackupStore) cleanupRunsChunk(ctx context.Context, runIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	args := make([]interface{}, len(runIDs))
	for i, id := range runIDs {
		args[i] = id
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM instance_backup_items WHERE run_id IN "+buildInPlaceholders(len(runIDs)), args...)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM instance_backup_runs WHERE id IN "+buildInPlaceholders(len(runIDs)), args...)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// RemoveFailedRunsBefore deletes failed runs older than the provided cutoff and returns the number of rows affected.
func (s *BackupStore) RemoveFailedRunsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	// Start a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern the status string
	status := string(BackupRunStatusFailed)
	ids, err := dbinterface.InternStringNullable(ctx, tx, &status)
	if err != nil {
		return 0, fmt.Errorf("failed to intern status: %w", err)
	}

	// Execute DELETE with interned ID
	res, err := tx.ExecContext(ctx, `
		DELETE FROM instance_backup_runs
		WHERE status_id = ? AND requested_at < ?
	`, ids[0], cutoff)
	if err != nil {
		return 0, err
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return res.RowsAffected()
}

// FindIncompleteRuns returns all backup runs that are in pending or running status.
// These are runs that were interrupted by a restart or crash.
func (s *BackupStore) FindIncompleteRuns(ctx context.Context) ([]*BackupRun, error) {
	query := `
        SELECT id, instance_id, kind, status, requested_by, requested_at, started_at, completed_at,
               archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message
        FROM instance_backup_runs_view
        WHERE status IN (?, ?)
        ORDER BY requested_at ASC
    `

	rows, err := s.db.QueryContext(ctx, query, string(BackupRunStatusPending), string(BackupRunStatusRunning))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []*BackupRun
	for rows.Next() {
		var run BackupRun
		var startedAt sql.NullTime
		var completedAt sql.NullTime
		var archivePath sql.NullString
		var manifestPath sql.NullString
		var errorMessage sql.NullString
		var categoryJSON sql.NullString
		var categoriesJSON sql.NullString
		var tagsJSON sql.NullString

		if err := rows.Scan(
			&run.ID,
			&run.InstanceID,
			&run.Kind,
			&run.Status,
			&run.RequestedBy,
			&run.RequestedAt,
			&startedAt,
			&completedAt,
			&archivePath,
			&manifestPath,
			&run.TotalBytes,
			&run.TorrentCount,
			&categoryJSON,
			&categoriesJSON,
			&tagsJSON,
			&errorMessage,
		); err != nil {
			return nil, err
		}

		if startedAt.Valid {
			run.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			run.CompletedAt = &completedAt.Time
		}
		if archivePath.Valid {
			run.ArchivePath = &archivePath.String
		}
		if manifestPath.Valid {
			run.ManifestPath = &manifestPath.String
		}
		if errorMessage.Valid {
			run.ErrorMessage = &errorMessage.String
		}
		counts, err := unmarshalCategoryCounts(categoryJSON)
		if err != nil {
			return nil, err
		}
		run.CategoryCounts = counts
		if categories, err := unmarshalCategories(categoriesJSON); err != nil {
			return nil, err
		} else {
			run.Categories = categories
		}
		if tagList, err := unmarshalTags(tagsJSON); err != nil {
			return nil, err
		} else {
			run.Tags = tagList
		}
		if categoriesJSON.Valid {
			run.categoriesJSON = &categoriesJSON.String
		}
		if tagsJSON.Valid {
			run.tagsJSON = &tagsJSON.String
		}

		runs = append(runs, &run)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return runs, nil
}
