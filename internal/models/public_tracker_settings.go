// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

type PublicTrackerSettings struct {
	ID             int        `json:"id"`
	TrackerListURL string     `json:"trackerListUrl"`
	CachedTrackers []string   `json:"cachedTrackers"`
	LastFetchedAt  *time.Time `json:"lastFetchedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type PublicTrackerSettingsInput struct {
	TrackerListURL *string  `json:"trackerListUrl,omitempty"`
	CachedTrackers []string `json:"cachedTrackers,omitempty"`
}

type PublicTrackerSettingsStore struct {
	db dbinterface.Querier
}

func NewPublicTrackerSettingsStore(db dbinterface.Querier) *PublicTrackerSettingsStore {
	return &PublicTrackerSettingsStore{db: db}
}

// Get returns the singleton public tracker settings
func (s *PublicTrackerSettingsStore) Get(ctx context.Context) (*PublicTrackerSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tracker_list_url, cached_trackers, last_fetched_at, created_at, updated_at
		FROM public_tracker_settings
		WHERE id = 1
	`)

	var pts PublicTrackerSettings
	var trackersJSON string
	var lastFetchedAt sql.NullTime

	err := row.Scan(
		&pts.ID, &pts.TrackerListURL, &trackersJSON, &lastFetchedAt, &pts.CreatedAt, &pts.UpdatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		// This shouldn't happen since migration inserts default row, but handle gracefully
		return s.createDefault(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("scan public tracker settings: %w", err)
	}

	// Parse cached trackers JSON
	if trackersJSON != "" && trackersJSON != "[]" {
		if err := json.Unmarshal([]byte(trackersJSON), &pts.CachedTrackers); err != nil {
			pts.CachedTrackers = []string{}
		}
	} else {
		pts.CachedTrackers = []string{}
	}

	if lastFetchedAt.Valid {
		pts.LastFetchedAt = &lastFetchedAt.Time
	}

	return &pts, nil
}

// Update updates the public tracker settings (partial update)
func (s *PublicTrackerSettingsStore) Update(ctx context.Context, input *PublicTrackerSettingsInput) (*PublicTrackerSettings, error) {
	if input == nil {
		return nil, errors.New("input is nil")
	}

	// Get existing settings
	existing, err := s.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Merge input with existing
	if input.TrackerListURL != nil {
		existing.TrackerListURL = *input.TrackerListURL
	}
	if input.CachedTrackers != nil {
		existing.CachedTrackers = input.CachedTrackers
	}

	// Serialize cached trackers
	trackersJSON, err := json.Marshal(existing.CachedTrackers)
	if err != nil {
		return nil, fmt.Errorf("marshal cached trackers: %w", err)
	}

	// Update in database
	_, err = s.db.ExecContext(ctx, `
		UPDATE public_tracker_settings
		SET tracker_list_url = ?,
		    cached_trackers = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`, existing.TrackerListURL, string(trackersJSON))
	if err != nil {
		return nil, fmt.Errorf("update public tracker settings: %w", err)
	}

	return s.Get(ctx)
}

// UpdateCachedTrackers updates just the cached trackers and last_fetched_at timestamp
func (s *PublicTrackerSettingsStore) UpdateCachedTrackers(ctx context.Context, trackers []string) error {
	trackersJSON, err := json.Marshal(trackers)
	if err != nil {
		return fmt.Errorf("marshal trackers: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE public_tracker_settings
		SET cached_trackers = ?,
		    last_fetched_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`, string(trackersJSON))
	if err != nil {
		return fmt.Errorf("update cached trackers: %w", err)
	}
	return nil
}

// createDefault creates the default settings row (should only be called if migration didn't run)
func (s *PublicTrackerSettingsStore) createDefault(ctx context.Context) (*PublicTrackerSettings, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO public_tracker_settings (id, tracker_list_url, cached_trackers)
		VALUES (1, '', '[]')
	`)
	if err != nil {
		return nil, fmt.Errorf("create default public tracker settings: %w", err)
	}

	return &PublicTrackerSettings{
		ID:             1,
		TrackerListURL: "",
		CachedTrackers: []string{},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}, nil
}
