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

	"github.com/autobrr/qui/internal/dbinterface"
)

// Default section order
var DefaultSectionOrder = []string{"server-stats", "tracker-breakdown", "global-stats", "instances"}

// Default section visibility (all visible)
var DefaultSectionVisibility = map[string]bool{
	"server-stats":      true,
	"tracker-breakdown": true,
	"global-stats":      true,
	"instances":         true,
}

type DashboardSettings struct {
	ID                           int             `json:"id"`
	UserID                       int             `json:"userId"`
	SectionVisibility            map[string]bool `json:"sectionVisibility"`
	SectionOrder                 []string        `json:"sectionOrder"`
	SectionCollapsed             map[string]bool `json:"sectionCollapsed"`
	TrackerBreakdownSortColumn   string          `json:"trackerBreakdownSortColumn"`
	TrackerBreakdownSortDir      string          `json:"trackerBreakdownSortDirection"`
	TrackerBreakdownItemsPerPage int             `json:"trackerBreakdownItemsPerPage"`
	CreatedAt                    time.Time       `json:"createdAt"`
	UpdatedAt                    time.Time       `json:"updatedAt"`
}

type DashboardSettingsInput struct {
	SectionVisibility            map[string]bool `json:"sectionVisibility,omitempty"`
	SectionOrder                 []string        `json:"sectionOrder,omitempty"`
	SectionCollapsed             map[string]bool `json:"sectionCollapsed,omitempty"`
	TrackerBreakdownSortColumn   string          `json:"trackerBreakdownSortColumn,omitempty"`
	TrackerBreakdownSortDir      string          `json:"trackerBreakdownSortDirection,omitempty"`
	TrackerBreakdownItemsPerPage int             `json:"trackerBreakdownItemsPerPage,omitempty"`
}

type DashboardSettingsStore struct {
	db dbinterface.Querier
}

func NewDashboardSettingsStore(db dbinterface.Querier) *DashboardSettingsStore {
	return &DashboardSettingsStore{db: db}
}

// GetByUserID returns settings for a user, creating defaults if none exist
func (s *DashboardSettingsStore) GetByUserID(ctx context.Context, userID int) (*DashboardSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, section_visibility, section_order, section_collapsed,
		       tracker_breakdown_sort_column, tracker_breakdown_sort_direction,
		       tracker_breakdown_items_per_page, created_at, updated_at
		FROM dashboard_settings
		WHERE user_id = ?
	`, userID)

	var ds DashboardSettings
	var visibilityJSON, orderJSON, collapsedJSON string

	err := row.Scan(
		&ds.ID, &ds.UserID, &visibilityJSON, &orderJSON, &collapsedJSON,
		&ds.TrackerBreakdownSortColumn, &ds.TrackerBreakdownSortDir,
		&ds.TrackerBreakdownItemsPerPage, &ds.CreatedAt, &ds.UpdatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		// Create default settings for this user
		return s.createDefault(ctx, userID)
	}
	if err != nil {
		return nil, err
	}

	// Parse JSON fields
	if visibilityJSON != "" && visibilityJSON != "{}" {
		if err := json.Unmarshal([]byte(visibilityJSON), &ds.SectionVisibility); err != nil {
			ds.SectionVisibility = copyVisibilityMap(DefaultSectionVisibility)
		}
	} else {
		ds.SectionVisibility = copyVisibilityMap(DefaultSectionVisibility)
	}

	if orderJSON != "" && orderJSON != "[]" {
		if err := json.Unmarshal([]byte(orderJSON), &ds.SectionOrder); err != nil {
			ds.SectionOrder = copyStringSlice(DefaultSectionOrder)
		}
	} else {
		ds.SectionOrder = copyStringSlice(DefaultSectionOrder)
	}

	if collapsedJSON != "" && collapsedJSON != "{}" {
		if err := json.Unmarshal([]byte(collapsedJSON), &ds.SectionCollapsed); err != nil {
			ds.SectionCollapsed = make(map[string]bool)
		}
	} else {
		ds.SectionCollapsed = make(map[string]bool)
	}

	return &ds, nil
}

// Update updates settings for a user (partial update - merges with existing)
func (s *DashboardSettingsStore) Update(ctx context.Context, userID int, input *DashboardSettingsInput) (*DashboardSettings, error) {
	if input == nil {
		return nil, errors.New("input is nil")
	}

	// Get existing settings (creates if none)
	existing, err := s.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Merge input with existing
	if input.SectionVisibility != nil {
		existing.SectionVisibility = input.SectionVisibility
	}
	if len(input.SectionOrder) > 0 {
		existing.SectionOrder = input.SectionOrder
	}
	if input.SectionCollapsed != nil {
		existing.SectionCollapsed = input.SectionCollapsed
	}
	if input.TrackerBreakdownSortColumn != "" {
		existing.TrackerBreakdownSortColumn = input.TrackerBreakdownSortColumn
	}
	if input.TrackerBreakdownSortDir != "" {
		existing.TrackerBreakdownSortDir = input.TrackerBreakdownSortDir
	}
	if input.TrackerBreakdownItemsPerPage > 0 {
		existing.TrackerBreakdownItemsPerPage = input.TrackerBreakdownItemsPerPage
	}

	// Serialize JSON fields
	visibilityJSON, err := json.Marshal(existing.SectionVisibility)
	if err != nil {
		return nil, err
	}
	orderJSON, err := json.Marshal(existing.SectionOrder)
	if err != nil {
		return nil, err
	}
	collapsedJSON, err := json.Marshal(existing.SectionCollapsed)
	if err != nil {
		return nil, err
	}

	// Update in database
	_, err = s.db.ExecContext(ctx, `
		UPDATE dashboard_settings
		SET section_visibility = ?,
		    section_order = ?,
		    section_collapsed = ?,
		    tracker_breakdown_sort_column = ?,
		    tracker_breakdown_sort_direction = ?,
		    tracker_breakdown_items_per_page = ?
		WHERE user_id = ?
	`,
		string(visibilityJSON),
		string(orderJSON),
		string(collapsedJSON),
		existing.TrackerBreakdownSortColumn,
		existing.TrackerBreakdownSortDir,
		existing.TrackerBreakdownItemsPerPage,
		userID,
	)
	if err != nil {
		return nil, err
	}

	return s.GetByUserID(ctx, userID)
}

// createDefault creates default settings for a user
func (s *DashboardSettingsStore) createDefault(ctx context.Context, userID int) (*DashboardSettings, error) {
	visibilityJSON, err := json.Marshal(DefaultSectionVisibility)
	if err != nil {
		return nil, fmt.Errorf("marshal section visibility: %w", err)
	}
	orderJSON, err := json.Marshal(DefaultSectionOrder)
	if err != nil {
		return nil, fmt.Errorf("marshal section order: %w", err)
	}

	var id int
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO dashboard_settings (user_id, section_visibility, section_order, section_collapsed,
		                                tracker_breakdown_sort_column, tracker_breakdown_sort_direction,
		                                tracker_breakdown_items_per_page)
		VALUES (?, ?, ?, '{}', 'uploaded', 'desc', 15)
		RETURNING id
	`, userID, string(visibilityJSON), string(orderJSON)).Scan(&id)
	if err != nil {
		return nil, err
	}

	return &DashboardSettings{
		ID:                           id,
		UserID:                       userID,
		SectionVisibility:            copyVisibilityMap(DefaultSectionVisibility),
		SectionOrder:                 copyStringSlice(DefaultSectionOrder),
		SectionCollapsed:             make(map[string]bool),
		TrackerBreakdownSortColumn:   "uploaded",
		TrackerBreakdownSortDir:      "desc",
		TrackerBreakdownItemsPerPage: 15,
		CreatedAt:                    time.Now(),
		UpdatedAt:                    time.Now(),
	}, nil
}

func copyVisibilityMap(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyStringSlice(src []string) []string {
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}
