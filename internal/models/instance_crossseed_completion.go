// Copyright (c) 2025, s0up and the autobrr contributors.
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

// InstanceCrossSeedCompletionSettings stores per-instance cross-seed completion configuration.
type InstanceCrossSeedCompletionSettings struct {
	InstanceID        int       `json:"instanceId"`
	Enabled           bool      `json:"enabled"`
	Categories        []string  `json:"categories"`
	Tags              []string  `json:"tags"`
	ExcludeCategories []string  `json:"excludeCategories"`
	ExcludeTags       []string  `json:"excludeTags"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// InstanceCrossSeedCompletionStore manages persistence for InstanceCrossSeedCompletionSettings.
type InstanceCrossSeedCompletionStore struct {
	db dbinterface.Querier
}

// NewInstanceCrossSeedCompletionStore creates a new store.
func NewInstanceCrossSeedCompletionStore(db dbinterface.Querier) *InstanceCrossSeedCompletionStore {
	return &InstanceCrossSeedCompletionStore{db: db}
}

// DefaultInstanceCrossSeedCompletionSettings returns default values for a new instance.
// Completion is disabled by default for safety.
func DefaultInstanceCrossSeedCompletionSettings(instanceID int) *InstanceCrossSeedCompletionSettings {
	return &InstanceCrossSeedCompletionSettings{
		InstanceID:        instanceID,
		Enabled:           false,
		Categories:        []string{},
		Tags:              []string{},
		ExcludeCategories: []string{},
		ExcludeTags:       []string{},
	}
}

// Get returns settings for an instance, falling back to defaults if missing.
func (s *InstanceCrossSeedCompletionStore) Get(ctx context.Context, instanceID int) (*InstanceCrossSeedCompletionSettings, error) {
	const query = `SELECT instance_id, enabled, categories_json, tags_json,
		exclude_categories_json, exclude_tags_json, updated_at
		FROM instance_crossseed_completion_settings WHERE instance_id = ?`

	row := s.db.QueryRowContext(ctx, query, instanceID)
	settings, err := scanInstanceCrossSeedCompletionSettings(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DefaultInstanceCrossSeedCompletionSettings(instanceID), nil
		}
		return nil, err
	}
	return settings, nil
}

// List returns settings for all instances that have overrides. Instances without overrides are omitted.
func (s *InstanceCrossSeedCompletionStore) List(ctx context.Context) ([]*InstanceCrossSeedCompletionSettings, error) {
	const query = `SELECT instance_id, enabled, categories_json, tags_json,
		exclude_categories_json, exclude_tags_json, updated_at
		FROM instance_crossseed_completion_settings`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*InstanceCrossSeedCompletionSettings
	for rows.Next() {
		settings, err := scanInstanceCrossSeedCompletionSettings(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, settings)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// Upsert saves settings for an instance, creating or updating as needed.
func (s *InstanceCrossSeedCompletionStore) Upsert(ctx context.Context, settings *InstanceCrossSeedCompletionSettings) (*InstanceCrossSeedCompletionSettings, error) {
	if settings == nil {
		return nil, fmt.Errorf("settings cannot be nil")
	}

	coerced := sanitizeInstanceCrossSeedCompletionSettings(settings)
	catJSON, err := encodeCompletionStringSliceJSON(coerced.Categories)
	if err != nil {
		return nil, err
	}
	tagJSON, err := encodeCompletionStringSliceJSON(coerced.Tags)
	if err != nil {
		return nil, err
	}
	excludeCatJSON, err := encodeCompletionStringSliceJSON(coerced.ExcludeCategories)
	if err != nil {
		return nil, err
	}
	excludeTagJSON, err := encodeCompletionStringSliceJSON(coerced.ExcludeTags)
	if err != nil {
		return nil, err
	}

	const stmt = `INSERT INTO instance_crossseed_completion_settings (
		instance_id, enabled, categories_json, tags_json, exclude_categories_json, exclude_tags_json)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(instance_id) DO UPDATE SET
		enabled = excluded.enabled,
		categories_json = excluded.categories_json,
		tags_json = excluded.tags_json,
		exclude_categories_json = excluded.exclude_categories_json,
		exclude_tags_json = excluded.exclude_tags_json`

	_, err = s.db.ExecContext(ctx, stmt,
		coerced.InstanceID,
		completionBoolToSQLite(coerced.Enabled),
		catJSON,
		tagJSON,
		excludeCatJSON,
		excludeTagJSON,
	)
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, coerced.InstanceID)
}

func completionBoolToSQLite(v bool) int {
	if v {
		return 1
	}
	return 0
}

func sanitizeInstanceCrossSeedCompletionSettings(s *InstanceCrossSeedCompletionSettings) *InstanceCrossSeedCompletionSettings {
	clone := *s
	clone.Categories = sanitizeCompletionStringSlice(clone.Categories)
	clone.Tags = sanitizeCompletionStringSlice(clone.Tags)
	clone.ExcludeCategories = sanitizeCompletionStringSlice(clone.ExcludeCategories)
	clone.ExcludeTags = sanitizeCompletionStringSlice(clone.ExcludeTags)
	return &clone
}

func sanitizeCompletionStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	var result []string
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func encodeCompletionStringSliceJSON(values []string) (string, error) {
	if len(values) == 0 {
		return "[]", nil
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func decodeCompletionStringSliceJSON(raw sql.NullString) ([]string, error) {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw.String), &values); err != nil {
		return nil, err
	}
	return sanitizeCompletionStringSlice(values), nil
}

func scanInstanceCrossSeedCompletionSettings(scanner interface {
	Scan(dest ...any) error
}) (*InstanceCrossSeedCompletionSettings, error) {
	var (
		instanceID        int
		enabledInt        int
		catJSON           sql.NullString
		tagJSON           sql.NullString
		excludeCatJSON    sql.NullString
		excludeTagJSON    sql.NullString
		updatedAt         sql.NullTime
	)

	if err := scanner.Scan(
		&instanceID,
		&enabledInt,
		&catJSON,
		&tagJSON,
		&excludeCatJSON,
		&excludeTagJSON,
		&updatedAt,
	); err != nil {
		return nil, err
	}

	categories, err := decodeCompletionStringSliceJSON(catJSON)
	if err != nil {
		return nil, fmt.Errorf("decode categories: %w", err)
	}
	tags, err := decodeCompletionStringSliceJSON(tagJSON)
	if err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}
	excludeCategories, err := decodeCompletionStringSliceJSON(excludeCatJSON)
	if err != nil {
		return nil, fmt.Errorf("decode exclude categories: %w", err)
	}
	excludeTags, err := decodeCompletionStringSliceJSON(excludeTagJSON)
	if err != nil {
		return nil, fmt.Errorf("decode exclude tags: %w", err)
	}

	settings := &InstanceCrossSeedCompletionSettings{
		InstanceID:        instanceID,
		Enabled:           enabledInt == 1,
		Categories:        categories,
		Tags:              tags,
		ExcludeCategories: excludeCategories,
		ExcludeTags:       excludeTags,
	}

	if updatedAt.Valid {
		settings.UpdatedAt = updatedAt.Time
	}

	return settings, nil
}
