// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	db dbinterface.DBWithStringInterning
}

func NewBackupStore(db dbinterface.DBWithStringInterning) *BackupStore {
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

	_, err := s.db.ExecContext(
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

	return err
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

	query := `
        INSERT INTO instance_backup_runs (
            instance_id, kind, status, requested_by, requested_at, started_at, completed_at,
            archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `

	categoryJSON, err := marshalCategoryCounts(run.CategoryCounts)
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(
		ctx,
		query,
		run.InstanceID,
		string(run.Kind),
		string(run.Status),
		run.RequestedBy,
		run.RequestedAt,
		run.StartedAt,
		run.CompletedAt,
		run.ArchivePath,
		run.ManifestPath,
		run.TotalBytes,
		run.TorrentCount,
		categoryJSON,
		run.categoriesJSON,
		run.tagsJSON,
		run.ErrorMessage,
	)
	if err != nil {
		return err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	run.ID = id
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	run, err := s.getRunForUpdate(ctx, tx, runID)
	if err != nil {
		return err
	}

	if err = updateFn(run); err != nil {
		return err
	}

	categoryJSON, err := marshalCategoryCounts(run.CategoryCounts)
	if err != nil {
		return err
	}

	query := `
        UPDATE instance_backup_runs SET
            status = ?,
            started_at = ?,
            completed_at = ?,
            archive_path = ?,
            manifest_path = ?,
            total_bytes = ?,
            torrent_count = ?,
            category_counts_json = ?,
            categories_json = ?,
            tags_json = ?,
            error_message = ?
        WHERE id = ?
    `

	categoriesJSON, err := marshalCategories(run.Categories)
	if err != nil {
		return err
	}
	run.categoriesJSON = categoriesJSON

	tagsJSON, err := marshalTags(run.Tags)
	if err != nil {
		return err
	}
	run.tagsJSON = tagsJSON

	_, err = tx.ExecContext(
		ctx,
		query,
		string(run.Status),
		run.StartedAt,
		run.CompletedAt,
		run.ArchivePath,
		run.ManifestPath,
		run.TotalBytes,
		run.TorrentCount,
		categoryJSON,
		categoriesJSON,
		tagsJSON,
		run.ErrorMessage,
		run.ID,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *BackupStore) getRunForUpdate(ctx context.Context, tx *sql.Tx, runID int64) (*BackupRun, error) {
	query := `
		SELECT id, instance_id, kind, status, requested_by, requested_at, started_at, completed_at,
		       archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message
		FROM instance_backup_runs
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
        FROM instance_backup_runs
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

	return results, rows.Err()
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
	return ids, rows.Err()
}

func (s *BackupStore) DeleteRun(ctx context.Context, runID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM instance_backup_runs WHERE id = ?", runID)
	return err
}

func (s *BackupStore) InsertItems(ctx context.Context, runID int64, items []BackupItem) error {
	if len(items) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO instance_backup_items (
			run_id, torrent_hash_id, name_id, category_id, size_bytes, archive_rel_path_id, infohash_v1, infohash_v2, tags_id, torrent_blob_path_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, item := range items {
		// Intern all string fields
		torrentHashID, err := s.db.GetOrCreateStringID(ctx, item.TorrentHash)
		if err != nil {
			_ = tx.Rollback()
			return err
		}

		nameID, err := s.db.GetOrCreateStringID(ctx, item.Name)
		if err != nil {
			_ = tx.Rollback()
			return err
		}

		var categoryID *int64
		if item.Category != nil && *item.Category != "" {
			id, err := s.db.GetOrCreateStringID(ctx, *item.Category)
			if err != nil {
				_ = tx.Rollback()
				return err
			}
			categoryID = &id
		}

		var tagsID *int64
		if item.Tags != nil && *item.Tags != "" {
			id, err := s.db.GetOrCreateStringID(ctx, *item.Tags)
			if err != nil {
				_ = tx.Rollback()
				return err
			}
			tagsID = &id
		}

		var archiveRelPathID *int64
		if item.ArchiveRelPath != nil && *item.ArchiveRelPath != "" {
			id, err := s.db.GetOrCreateStringID(ctx, *item.ArchiveRelPath)
			if err != nil {
				_ = tx.Rollback()
				return err
			}
			archiveRelPathID = &id
		}

		var torrentBlobPathID *int64
		if item.TorrentBlobPath != nil && *item.TorrentBlobPath != "" {
			id, err := s.db.GetOrCreateStringID(ctx, *item.TorrentBlobPath)
			if err != nil {
				_ = tx.Rollback()
				return err
			}
			torrentBlobPathID = &id
		}

		_, err = stmt.ExecContext(
			ctx,
			runID,
			torrentHashID,
			nameID,
			categoryID,
			item.SizeBytes,
			archiveRelPathID,
			item.InfoHashV1,
			item.InfoHashV2,
			tagsID,
			torrentBlobPathID,
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func (s *BackupStore) ListItems(ctx context.Context, runID int64) ([]*BackupItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, torrent_hash, name, category, size_bytes, archive_rel_path, infohash_v1, infohash_v2, tags, torrent_blob_path, created_at
		FROM instance_backup_items_view
		WHERE run_id = ?
		ORDER BY name COLLATE NOCASE
	`, runID)
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

	return items, rows.Err()
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
	return count, err
}

func (s *BackupStore) GetInstanceName(ctx context.Context, instanceID int) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, "SELECT name FROM instances WHERE id = ?", instanceID).Scan(&name)
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
		FROM instance_backup_runs
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

	return runs, rows.Err()
}

func (s *BackupStore) GetRun(ctx context.Context, runID int64) (*BackupRun, error) {
	query := `
        SELECT id, instance_id, kind, status, requested_by, requested_at, started_at, completed_at,
               archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message
        FROM instance_backup_runs
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

	return settings, rows.Err()
}

func (s *BackupStore) DeleteRunsOlderThan(ctx context.Context, instanceID int, kind BackupRunKind, keep int) ([]int64, error) {
	if keep <= 0 {
		query := `
            SELECT id FROM instance_backup_runs
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
		return ids, rows.Err()
	}

	query := `
        SELECT id FROM instance_backup_runs
        WHERE instance_id = ? AND kind = ?
        ORDER BY requested_at DESC
        LIMIT -1 OFFSET ?
    `

	rows, err := s.db.QueryContext(ctx, query, instanceID, string(kind), keep)
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

	return ids, rows.Err()
}

func (s *BackupStore) DeleteItemsByRunIDs(ctx context.Context, runIDs []int64) error {
	if len(runIDs) == 0 {
		return nil
	}

	query := "DELETE FROM instance_backup_items WHERE run_id IN (" + placeholders(len(runIDs)) + ")"

	args := make([]any, len(runIDs))
	for i, id := range runIDs {
		args[i] = id
	}

	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	s := "?"
	for i := 1; i < count; i++ {
		s += ",?"
	}
	return s
}

func (s *BackupStore) DeleteRunsByIDs(ctx context.Context, runIDs []int64) error {
	if len(runIDs) == 0 {
		return nil
	}

	query := "DELETE FROM instance_backup_runs WHERE id IN (" + placeholders(len(runIDs)) + ")"
	args := make([]any, len(runIDs))
	for i, id := range runIDs {
		args[i] = id
	}

	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *BackupStore) CountRunsByKind(ctx context.Context, instanceID int, kind BackupRunKind) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
        SELECT COUNT(*)
        FROM instance_backup_runs
        WHERE instance_id = ? AND kind = ?
    `, instanceID, string(kind)).Scan(&count)
	return count, err
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
	_, err := s.db.ExecContext(ctx, "DELETE FROM instance_backup_items WHERE run_id = ?", runID)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, "DELETE FROM instance_backup_runs WHERE id = ?", runID)
	return err
}

// RemoveFailedRunsBefore deletes failed runs older than the provided cutoff and returns the number of rows affected.
func (s *BackupStore) RemoveFailedRunsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
        DELETE FROM instance_backup_runs
        WHERE status = ? AND requested_at < ?
    `, string(BackupRunStatusFailed), cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// FindIncompleteRuns returns all backup runs that are in pending or running status.
// These are runs that were interrupted by a restart or crash.
func (s *BackupStore) FindIncompleteRuns(ctx context.Context) ([]*BackupRun, error) {
	query := `
        SELECT id, instance_id, kind, status, requested_by, requested_at, started_at, completed_at,
               archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message
        FROM instance_backup_runs
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

	return runs, rows.Err()
}
