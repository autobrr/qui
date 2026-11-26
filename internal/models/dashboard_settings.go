// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

var DefaultSectionOrder = []string{"server-stats", "global-stats", "instances"}

var DefaultSectionVisibility = map[string]bool{
	"server-stats": true,
	"global-stats": true,
	"instances":    true,
}

type DashboardSettings struct {
	ID                int             `json:"id"`
	UserID            int             `json:"userId"`
	SectionVisibility map[string]bool `json:"sectionVisibility"`
	SectionOrder      []string        `json:"sectionOrder"`
	SectionCollapsed  map[string]bool `json:"sectionCollapsed"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

type DashboardSettingsInput struct {
	SectionVisibility map[string]bool `json:"sectionVisibility,omitempty"`
	SectionOrder      []string        `json:"sectionOrder,omitempty"`
	SectionCollapsed  map[string]bool `json:"sectionCollapsed,omitempty"`
}

type DashboardSettingsStore struct {
	db dbinterface.Querier
}

func NewDashboardSettingsStore(db dbinterface.Querier) *DashboardSettingsStore {
	return &DashboardSettingsStore{db: db}
}

func (s *DashboardSettingsStore) GetByUserID(ctx context.Context, userID int) (*DashboardSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, section_visibility, section_order, section_collapsed,
		       created_at, updated_at
		FROM dashboard_settings
		WHERE user_id = ?
	`, userID)

	var ds DashboardSettings
	var visibilityJSON, orderJSON, collapsedJSON string

	err := row.Scan(
		&ds.ID, &ds.UserID, &visibilityJSON, &orderJSON, &collapsedJSON,
		&ds.CreatedAt, &ds.UpdatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return s.createDefault(ctx, userID)
	}
	if err != nil {
		return nil, err
	}

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

func (s *DashboardSettingsStore) Update(ctx context.Context, userID int, input *DashboardSettingsInput) (*DashboardSettings, error) {
	if input == nil {
		return nil, errors.New("input is nil")
	}

	existing, err := s.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if input.SectionVisibility != nil {
		existing.SectionVisibility = input.SectionVisibility
	}
	if len(input.SectionOrder) > 0 {
		existing.SectionOrder = input.SectionOrder
	}
	if input.SectionCollapsed != nil {
		existing.SectionCollapsed = input.SectionCollapsed
	}

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

	_, err = s.db.ExecContext(ctx, `
		UPDATE dashboard_settings
		SET section_visibility = ?,
		    section_order = ?,
		    section_collapsed = ?
		WHERE user_id = ?
	`,
		string(visibilityJSON),
		string(orderJSON),
		string(collapsedJSON),
		userID,
	)
	if err != nil {
		return nil, err
	}

	return s.GetByUserID(ctx, userID)
}

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
		INSERT INTO dashboard_settings (user_id, section_visibility, section_order, section_collapsed)
		VALUES (?, ?, ?, '{}')
		RETURNING id
	`, userID, string(visibilityJSON), string(orderJSON)).Scan(&id)
	if err != nil {
		return nil, err
	}

	return &DashboardSettings{
		ID:                id,
		UserID:            userID,
		SectionVisibility: copyVisibilityMap(DefaultSectionVisibility),
		SectionOrder:      copyStringSlice(DefaultSectionOrder),
		SectionCollapsed:  make(map[string]bool),
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}, nil
}

func copyVisibilityMap(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src))
	maps.Copy(dst, src)
	return dst
}

func copyStringSlice(src []string) []string {
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}
