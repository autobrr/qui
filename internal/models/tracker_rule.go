// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

type TrackerRule struct {
	ID                      int       `json:"id"`
	InstanceID              int       `json:"instanceId"`
	Name                    string    `json:"name"`
	TrackerPattern          string    `json:"trackerPattern"`
	TrackerDomains          []string  `json:"trackerDomains,omitempty"`
	Category                *string   `json:"category,omitempty"`
	Tag                     *string   `json:"tag,omitempty"`
	UploadLimitKiB          *int64    `json:"uploadLimitKiB,omitempty"`
	DownloadLimitKiB        *int64    `json:"downloadLimitKiB,omitempty"`
	RatioLimit              *float64  `json:"ratioLimit,omitempty"`
	SeedingTimeLimitMinutes *int64    `json:"seedingTimeLimitMinutes,omitempty"`
	DeleteMode              *string   `json:"deleteMode,omitempty"` // "none", "delete", "deleteWithFiles", "deleteWithFilesPreserveCrossSeeds"
	DeleteUnregistered      bool      `json:"deleteUnregistered"`
	Enabled                 bool      `json:"enabled"`
	SortOrder               int       `json:"sortOrder"`
	CreatedAt               time.Time `json:"createdAt"`
	UpdatedAt               time.Time `json:"updatedAt"`
}

type TrackerRuleStore struct {
	db dbinterface.Querier
}

func NewTrackerRuleStore(db dbinterface.Querier) *TrackerRuleStore {
	return &TrackerRuleStore{db: db}
}

func splitPatterns(pattern string) []string {
	if pattern == "" {
		return nil
	}

	rawParts := strings.FieldsFunc(pattern, func(r rune) bool {
		return r == ',' || r == ';' || r == '|'
	})

	seen := make(map[string]struct{})
	var parts []string
	for _, raw := range rawParts {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if _, exists := seen[p]; exists {
			continue
		}
		seen[p] = struct{}{}
		parts = append(parts, p)
	}
	return parts
}

func normalizeTrackerPattern(pattern string, domains []string) string {
	if len(domains) > 0 {
		pattern = strings.Join(domains, ",")
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}
	parts := splitPatterns(pattern)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ",")
}

func (s *TrackerRuleStore) ListByInstance(ctx context.Context, instanceID int) ([]*TrackerRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, name, tracker_pattern, category, tag, upload_limit_kib, download_limit_kib,
		       ratio_limit, seeding_time_limit_minutes, delete_mode, delete_unregistered, enabled, sort_order, created_at, updated_at
		FROM tracker_rules
		WHERE instance_id = ?
		ORDER BY sort_order ASC, id ASC
	`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*TrackerRule
	for rows.Next() {
		var rule TrackerRule
		var category, tag, deleteMode sql.NullString
		var upload, download sql.NullInt64
		var ratio sql.NullFloat64
		var seeding sql.NullInt64
		var deleteUnregistered int

		if err := rows.Scan(
			&rule.ID,
			&rule.InstanceID,
			&rule.Name,
			&rule.TrackerPattern,
			&category,
			&tag,
			&upload,
			&download,
			&ratio,
			&seeding,
			&deleteMode,
			&deleteUnregistered,
			&rule.Enabled,
			&rule.SortOrder,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if category.Valid {
			rule.Category = &category.String
		}
		if tag.Valid {
			rule.Tag = &tag.String
		}
		if upload.Valid {
			rule.UploadLimitKiB = &upload.Int64
		}
		if download.Valid {
			rule.DownloadLimitKiB = &download.Int64
		}
		if ratio.Valid {
			rule.RatioLimit = &ratio.Float64
		}
		if seeding.Valid {
			rule.SeedingTimeLimitMinutes = &seeding.Int64
		}
		if deleteMode.Valid && deleteMode.String != "none" {
			rule.DeleteMode = &deleteMode.String
		}
		rule.DeleteUnregistered = deleteUnregistered != 0

		rule.TrackerDomains = splitPatterns(rule.TrackerPattern)

		rules = append(rules, &rule)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return rules, nil
}

func (s *TrackerRuleStore) Get(ctx context.Context, id int) (*TrackerRule, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, name, tracker_pattern, category, tag, upload_limit_kib, download_limit_kib,
		       ratio_limit, seeding_time_limit_minutes, delete_mode, delete_unregistered, enabled, sort_order, created_at, updated_at
		FROM tracker_rules
		WHERE id = ?
	`, id)

	var rule TrackerRule
	var category, tag, deleteMode sql.NullString
	var upload, download sql.NullInt64
	var ratio sql.NullFloat64
	var seeding sql.NullInt64
	var deleteUnregistered int

	if err := row.Scan(
		&rule.ID,
		&rule.InstanceID,
		&rule.Name,
		&rule.TrackerPattern,
		&category,
		&tag,
		&upload,
		&download,
		&ratio,
		&seeding,
		&deleteMode,
		&deleteUnregistered,
		&rule.Enabled,
		&rule.SortOrder,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if category.Valid {
		rule.Category = &category.String
	}
	if tag.Valid {
		rule.Tag = &tag.String
	}
	if upload.Valid {
		rule.UploadLimitKiB = &upload.Int64
	}
	if download.Valid {
		rule.DownloadLimitKiB = &download.Int64
	}
	if ratio.Valid {
		rule.RatioLimit = &ratio.Float64
	}
	if seeding.Valid {
		rule.SeedingTimeLimitMinutes = &seeding.Int64
	}
	if deleteMode.Valid && deleteMode.String != "none" {
		rule.DeleteMode = &deleteMode.String
	}
	rule.DeleteUnregistered = deleteUnregistered != 0

	rule.TrackerDomains = splitPatterns(rule.TrackerPattern)

	return &rule, nil
}

func (s *TrackerRuleStore) nextSortOrder(ctx context.Context, instanceID int) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) FROM tracker_rules WHERE instance_id = ?`, instanceID)
	var maxOrder int
	if err := row.Scan(&maxOrder); err != nil {
		return 0, err
	}
	return maxOrder + 1, nil
}

func (s *TrackerRuleStore) Create(ctx context.Context, rule *TrackerRule) (*TrackerRule, error) {
	if rule == nil {
		return nil, errors.New("rule is nil")
	}

	rule.TrackerPattern = normalizeTrackerPattern(rule.TrackerPattern, rule.TrackerDomains)

	sortOrder := rule.SortOrder
	if sortOrder == 0 {
		next, err := s.nextSortOrder(ctx, rule.InstanceID)
		if err != nil {
			return nil, err
		}
		sortOrder = next
	}

	// Default delete_mode to "none" if not set
	deleteMode := "none"
	if rule.DeleteMode != nil && *rule.DeleteMode != "" {
		deleteMode = *rule.DeleteMode
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO tracker_rules
			(instance_id, name, tracker_pattern, category, tag, upload_limit_kib, download_limit_kib, ratio_limit, seeding_time_limit_minutes, delete_mode, delete_unregistered, enabled, sort_order)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rule.InstanceID, rule.Name, rule.TrackerPattern, nullableString(rule.Category), nullableString(rule.Tag),
		nullableInt64(rule.UploadLimitKiB), nullableInt64(rule.DownloadLimitKiB), nullableFloat64(rule.RatioLimit),
		nullableInt64(rule.SeedingTimeLimitMinutes), deleteMode, boolToInt(rule.DeleteUnregistered), boolToInt(rule.Enabled), sortOrder)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, int(id))
}

func (s *TrackerRuleStore) Update(ctx context.Context, rule *TrackerRule) (*TrackerRule, error) {
	if rule == nil {
		return nil, errors.New("rule is nil")
	}

	rule.TrackerPattern = normalizeTrackerPattern(rule.TrackerPattern, rule.TrackerDomains)

	// Default delete_mode to "none" if not set
	deleteMode := "none"
	if rule.DeleteMode != nil && *rule.DeleteMode != "" {
		deleteMode = *rule.DeleteMode
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE tracker_rules
		SET name = ?, tracker_pattern = ?, category = ?, tag = ?, upload_limit_kib = ?, download_limit_kib = ?,
		    ratio_limit = ?, seeding_time_limit_minutes = ?, delete_mode = ?, delete_unregistered = ?, enabled = ?, sort_order = ?
		WHERE id = ? AND instance_id = ?
	`, rule.Name, rule.TrackerPattern, nullableString(rule.Category), nullableString(rule.Tag),
		nullableInt64(rule.UploadLimitKiB), nullableInt64(rule.DownloadLimitKiB), nullableFloat64(rule.RatioLimit),
		nullableInt64(rule.SeedingTimeLimitMinutes), deleteMode, boolToInt(rule.DeleteUnregistered), boolToInt(rule.Enabled), rule.SortOrder, rule.ID, rule.InstanceID)
	if err != nil {
		return nil, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, sql.ErrNoRows
	}

	return s.Get(ctx, rule.ID)
}

func (s *TrackerRuleStore) Delete(ctx context.Context, instanceID int, id int) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tracker_rules WHERE id = ? AND instance_id = ?`, id, instanceID)
	if err != nil {
		return err
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *TrackerRuleStore) Reorder(ctx context.Context, instanceID int, orderedIDs []int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for idx, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE tracker_rules SET sort_order = ? WHERE id = ? AND instance_id = ?`, idx+1, id, instanceID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat64(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
