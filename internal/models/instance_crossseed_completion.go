// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	IndexerIDs        []int     `json:"indexerIds"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// InstanceCrossSeedCompletionStore manages persistence for InstanceCrossSeedCompletionSettings.
type InstanceCrossSeedCompletionStore struct {
	db dbinterface.Querier
}

// NewInstanceCrossSeedCompletionStore creates a new store.
func NewInstanceCrossSeedCompletionStore(db dbinterface.Querier) *InstanceCrossSeedCompletionStore {
	if db == nil {
		panic("db cannot be nil")
	}
	return &InstanceCrossSeedCompletionStore{db: db}
}

// GetCategories returns the categories filter.
func (s *InstanceCrossSeedCompletionSettings) GetCategories() []string { return s.Categories }

// GetTags returns the tags filter.
func (s *InstanceCrossSeedCompletionSettings) GetTags() []string { return s.Tags }

// GetExcludeCategories returns the excluded categories filter.
func (s *InstanceCrossSeedCompletionSettings) GetExcludeCategories() []string {
	return s.ExcludeCategories
}

// GetExcludeTags returns the excluded tags filter.
func (s *InstanceCrossSeedCompletionSettings) GetExcludeTags() []string { return s.ExcludeTags }

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
		IndexerIDs:        []int{},
	}
}

// Get returns settings for an instance, falling back to defaults if missing.
func (s *InstanceCrossSeedCompletionStore) Get(ctx context.Context, instanceID int) (*InstanceCrossSeedCompletionSettings, error) {
	const query = `SELECT instance_id, enabled, categories_json, tags_json,
		exclude_categories_json, exclude_tags_json, indexer_ids_json, updated_at
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
		exclude_categories_json, exclude_tags_json, indexer_ids_json, updated_at
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
	catJSON, err := EncodeStringSliceJSON(coerced.Categories)
	if err != nil {
		return nil, err
	}
	tagJSON, err := EncodeStringSliceJSON(coerced.Tags)
	if err != nil {
		return nil, err
	}
	excludeCatJSON, err := EncodeStringSliceJSON(coerced.ExcludeCategories)
	if err != nil {
		return nil, err
	}
	excludeTagJSON, err := EncodeStringSliceJSON(coerced.ExcludeTags)
	if err != nil {
		return nil, err
	}
	indexerJSON, err := encodeIntSlice(coerced.IndexerIDs)
	if err != nil {
		return nil, err
	}

	const stmt = `INSERT INTO instance_crossseed_completion_settings (
		instance_id, enabled, categories_json, tags_json, exclude_categories_json, exclude_tags_json, indexer_ids_json)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(instance_id) DO UPDATE SET
		enabled = excluded.enabled,
		categories_json = excluded.categories_json,
		tags_json = excluded.tags_json,
		exclude_categories_json = excluded.exclude_categories_json,
		exclude_tags_json = excluded.exclude_tags_json,
		indexer_ids_json = excluded.indexer_ids_json`

	_, err = s.db.ExecContext(ctx, stmt,
		coerced.InstanceID,
		BoolToSQLite(coerced.Enabled),
		catJSON,
		tagJSON,
		excludeCatJSON,
		excludeTagJSON,
		indexerJSON,
	)
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, coerced.InstanceID)
}

func sanitizeInstanceCrossSeedCompletionSettings(s *InstanceCrossSeedCompletionSettings) *InstanceCrossSeedCompletionSettings {
	clone := *s
	clone.Categories = SanitizeStringSlice(clone.Categories)
	clone.Tags = SanitizeStringSlice(clone.Tags)
	clone.ExcludeCategories = SanitizeStringSlice(clone.ExcludeCategories)
	clone.ExcludeTags = SanitizeStringSlice(clone.ExcludeTags)
	clone.IndexerIDs = sanitizePositiveInts(clone.IndexerIDs)
	return &clone
}

func sanitizePositiveInts(values []int) []int {
	if len(values) == 0 {
		return []int{}
	}
	seen := make(map[int]struct{}, len(values))
	normalized := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func scanInstanceCrossSeedCompletionSettings(scanner interface {
	Scan(dest ...any) error
}) (*InstanceCrossSeedCompletionSettings, error) {
	var (
		instanceID     int
		enabledInt     int
		catJSON        sql.NullString
		tagJSON        sql.NullString
		excludeCatJSON sql.NullString
		excludeTagJSON sql.NullString
		indexerJSON    sql.NullString
		updatedAt      sql.NullTime
	)

	if err := scanner.Scan(
		&instanceID,
		&enabledInt,
		&catJSON,
		&tagJSON,
		&excludeCatJSON,
		&excludeTagJSON,
		&indexerJSON,
		&updatedAt,
	); err != nil {
		return nil, err
	}

	categories, err := DecodeStringSliceJSON(catJSON)
	if err != nil {
		return nil, fmt.Errorf("decode categories: %w", err)
	}
	tags, err := DecodeStringSliceJSON(tagJSON)
	if err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}
	excludeCategories, err := DecodeStringSliceJSON(excludeCatJSON)
	if err != nil {
		return nil, fmt.Errorf("decode exclude categories: %w", err)
	}
	excludeTags, err := DecodeStringSliceJSON(excludeTagJSON)
	if err != nil {
		return nil, fmt.Errorf("decode exclude tags: %w", err)
	}
	indexerIDs, err := decodeIntSliceJSON(indexerJSON)
	if err != nil {
		return nil, fmt.Errorf("decode indexer ids: %w", err)
	}

	settings := &InstanceCrossSeedCompletionSettings{
		InstanceID:        instanceID,
		Enabled:           enabledInt == 1,
		Categories:        categories,
		Tags:              tags,
		ExcludeCategories: excludeCategories,
		ExcludeTags:       excludeTags,
		IndexerIDs:        indexerIDs,
	}

	if updatedAt.Valid {
		settings.UpdatedAt = updatedAt.Time
	}

	return settings, nil
}

func decodeIntSliceJSON(src sql.NullString) ([]int, error) {
	var values []int
	if err := decodeIntSlice(src, &values); err != nil {
		return nil, err
	}
	return sanitizePositiveInts(values), nil
}
