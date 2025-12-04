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

const (
	defaultInitialWaitSeconds     = 15
	defaultReannounceIntervalSecs = 7
	defaultMaxAgeSeconds          = 600
	defaultMaxRetries             = 50
)

// InstanceReannounceSettings stores per-instance tracker reannounce configuration.
type InstanceReannounceSettings struct {
	InstanceID                int       `json:"instanceId"`
	Enabled                   bool      `json:"enabled"`
	InitialWaitSeconds        int       `json:"initialWaitSeconds"`
	ReannounceIntervalSeconds int       `json:"reannounceIntervalSeconds"`
	MaxAgeSeconds             int       `json:"maxAgeSeconds"`
	MaxRetries                int       `json:"maxRetries"`
	Aggressive                bool      `json:"aggressive"`
	MonitorAll                bool      `json:"monitorAll"`
	ExcludeCategories         bool      `json:"excludeCategories"`
	Categories                []string  `json:"categories"`
	ExcludeTags               bool      `json:"excludeTags"`
	Tags                      []string  `json:"tags"`
	ExcludeTrackers           bool      `json:"excludeTrackers"`
	Trackers                  []string  `json:"trackers"`
	UpdatedAt                 time.Time `json:"updatedAt"`
}

// InstanceReannounceStore manages persistence for InstanceReannounceSettings.
type InstanceReannounceStore struct {
	db dbinterface.Querier
}

// NewInstanceReannounceStore creates a new store.
func NewInstanceReannounceStore(db dbinterface.Querier) *InstanceReannounceStore {
	return &InstanceReannounceStore{db: db}
}

// DefaultInstanceReannounceSettings returns default values for a new instance.
func DefaultInstanceReannounceSettings(instanceID int) *InstanceReannounceSettings {
	return &InstanceReannounceSettings{
		InstanceID:                instanceID,
		Enabled:                   false,
		InitialWaitSeconds:        defaultInitialWaitSeconds,
		ReannounceIntervalSeconds: defaultReannounceIntervalSecs,
		MaxAgeSeconds:             defaultMaxAgeSeconds,
		MaxRetries:                defaultMaxRetries,
		Aggressive:                false,
		MonitorAll:                false,
		ExcludeCategories:         false,
		Categories:                []string{},
		ExcludeTags:               false,
		Tags:                      []string{},
		ExcludeTrackers:           false,
		Trackers:                  []string{},
	}
}

// Get returns settings for an instance, falling back to defaults if missing.
func (s *InstanceReannounceStore) Get(ctx context.Context, instanceID int) (*InstanceReannounceSettings, error) {
	const query = `SELECT instance_id, enabled, initial_wait_seconds, reannounce_interval_seconds,
		max_age_seconds, max_retries, aggressive, monitor_all, categories_json, tags_json, trackers_json, updated_at,
		exclude_categories, exclude_tags, exclude_trackers
		FROM instance_reannounce_settings WHERE instance_id = ?`

	row := s.db.QueryRowContext(ctx, query, instanceID)
	settings, err := scanInstanceReannounceSettings(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DefaultInstanceReannounceSettings(instanceID), nil
		}
		return nil, err
	}
	return settings, nil
}

// List returns settings for all instances that have overrides. Instances without overrides are omitted.
func (s *InstanceReannounceStore) List(ctx context.Context) ([]*InstanceReannounceSettings, error) {
	const query = `SELECT instance_id, enabled, initial_wait_seconds, reannounce_interval_seconds,
		max_age_seconds, max_retries, aggressive, monitor_all, categories_json, tags_json, trackers_json, updated_at,
		exclude_categories, exclude_tags, exclude_trackers
		FROM instance_reannounce_settings`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*InstanceReannounceSettings
	for rows.Next() {
		settings, err := scanInstanceReannounceSettings(rows)
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
func (s *InstanceReannounceStore) Upsert(ctx context.Context, settings *InstanceReannounceSettings) (*InstanceReannounceSettings, error) {
	if settings == nil {
		return nil, fmt.Errorf("settings cannot be nil")
	}

	coerced := sanitizeInstanceReannounceSettings(settings)
	catJSON, err := encodeStringSliceJSON(coerced.Categories)
	if err != nil {
		return nil, err
	}
	tagJSON, err := encodeStringSliceJSON(coerced.Tags)
	if err != nil {
		return nil, err
	}
	trackerJSON, err := encodeStringSliceJSON(coerced.Trackers)
	if err != nil {
		return nil, err
	}

	const stmt = `INSERT INTO instance_reannounce_settings (
		instance_id, enabled, initial_wait_seconds, reannounce_interval_seconds,
		max_age_seconds, max_retries, aggressive, monitor_all, categories_json, tags_json, trackers_json,
		exclude_categories, exclude_tags, exclude_trackers)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(instance_id) DO UPDATE SET
		enabled = excluded.enabled,
		initial_wait_seconds = excluded.initial_wait_seconds,
		reannounce_interval_seconds = excluded.reannounce_interval_seconds,
		max_age_seconds = excluded.max_age_seconds,
		max_retries = excluded.max_retries,
		aggressive = excluded.aggressive,
		monitor_all = excluded.monitor_all,
		categories_json = excluded.categories_json,
		tags_json = excluded.tags_json,
		trackers_json = excluded.trackers_json,
		exclude_categories = excluded.exclude_categories,
		exclude_tags = excluded.exclude_tags,
		exclude_trackers = excluded.exclude_trackers`

	_, err = s.db.ExecContext(ctx, stmt,
		coerced.InstanceID,
		boolToSQLite(coerced.Enabled),
		coerced.InitialWaitSeconds,
		coerced.ReannounceIntervalSeconds,
		coerced.MaxAgeSeconds,
		coerced.MaxRetries,
		boolToSQLite(coerced.Aggressive),
		boolToSQLite(coerced.MonitorAll),
		catJSON,
		tagJSON,
		trackerJSON,
		boolToSQLite(coerced.ExcludeCategories),
		boolToSQLite(coerced.ExcludeTags),
		boolToSQLite(coerced.ExcludeTrackers),
	)
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, coerced.InstanceID)
}

func boolToSQLite(v bool) int {
	if v {
		return 1
	}
	return 0
}

func sanitizeInstanceReannounceSettings(s *InstanceReannounceSettings) *InstanceReannounceSettings {
	clone := *s
	if clone.InitialWaitSeconds <= 0 {
		clone.InitialWaitSeconds = defaultInitialWaitSeconds
	}
	if clone.ReannounceIntervalSeconds <= 0 {
		clone.ReannounceIntervalSeconds = defaultReannounceIntervalSecs
	}
	if clone.MaxAgeSeconds <= 0 {
		clone.MaxAgeSeconds = defaultMaxAgeSeconds
	}
	if clone.MaxRetries <= 0 {
		clone.MaxRetries = defaultMaxRetries
	} else if clone.MaxRetries > 50 {
		clone.MaxRetries = 50
	}
	clone.Categories = sanitizeStringSlice(clone.Categories)
	clone.Tags = sanitizeStringSlice(clone.Tags)
	clone.Trackers = sanitizeStringSlice(clone.Trackers)
	return &clone
}

func sanitizeStringSlice(values []string) []string {
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

func encodeStringSliceJSON(values []string) (string, error) {
	if len(values) == 0 {
		return "[]", nil
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func decodeStringSliceJSON(raw sql.NullString) ([]string, error) {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw.String), &values); err != nil {
		return nil, err
	}
	return sanitizeStringSlice(values), nil
}

func scanInstanceReannounceSettings(scanner interface {
	Scan(dest ...any) error
}) (*InstanceReannounceSettings, error) {
	var (
		instanceID           int
		enabledInt           int
		initialWait          int
		reannounceInterval   int
		maxAge               int
		maxRetries           int
		aggressiveInt        int
		monitorAllInt        int
		catJSON              sql.NullString
		tagJSON              sql.NullString
		trackerJSON          sql.NullString
		updatedAt            sql.NullTime
		excludeCategoriesInt int
		excludeTagsInt       int
		excludeTrackersInt   int
	)

	if err := scanner.Scan(
		&instanceID,
		&enabledInt,
		&initialWait,
		&reannounceInterval,
		&maxAge,
		&maxRetries,
		&aggressiveInt,
		&monitorAllInt,
		&catJSON,
		&tagJSON,
		&trackerJSON,
		&updatedAt,
		&excludeCategoriesInt,
		&excludeTagsInt,
		&excludeTrackersInt,
	); err != nil {
		return nil, err
	}

	categories, err := decodeStringSliceJSON(catJSON)
	if err != nil {
		return nil, fmt.Errorf("decode categories: %w", err)
	}
	tags, err := decodeStringSliceJSON(tagJSON)
	if err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}
	trackers, err := decodeStringSliceJSON(trackerJSON)
	if err != nil {
		return nil, fmt.Errorf("decode trackers: %w", err)
	}

	settings := &InstanceReannounceSettings{
		InstanceID:                instanceID,
		Enabled:                   enabledInt == 1,
		InitialWaitSeconds:        initialWait,
		ReannounceIntervalSeconds: reannounceInterval,
		MaxAgeSeconds:             maxAge,
		MaxRetries:                maxRetries,
		Aggressive:                aggressiveInt == 1,
		MonitorAll:                monitorAllInt == 1,
		ExcludeCategories:         excludeCategoriesInt == 1,
		Categories:                categories,
		ExcludeTags:               excludeTagsInt == 1,
		Tags:                      tags,
		ExcludeTrackers:           excludeTrackersInt == 1,
		Trackers:                  trackers,
	}

	if updatedAt.Valid {
		settings.UpdatedAt = updatedAt.Time
	}

	return sanitizeInstanceReannounceSettings(settings), nil
}
