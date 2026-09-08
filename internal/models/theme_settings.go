// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"

	"github.com/autobrr/qui/internal/dbinterface"
)

// ThemeSettings is the single persisted theme selection (one row, id = 1).
type ThemeSettings struct {
	ThemeID   string `json:"themeId"`
	Mode      string `json:"mode"`
	Variation string `json:"variation,omitempty"`
}

type ThemeSettingsStore struct {
	db dbinterface.Querier
}

func NewThemeSettingsStore(db dbinterface.Querier) *ThemeSettingsStore {
	return &ThemeSettingsStore{db: db}
}

// Get returns the stored theme settings, or nil when none have been saved.
func (s *ThemeSettingsStore) Get(ctx context.Context) (*ThemeSettings, error) {
	var ts ThemeSettings

	err := s.db.QueryRowContext(ctx, `
		SELECT theme_id, mode, variation FROM theme_settings WHERE id = 1
	`).Scan(&ts.ThemeID, &ts.Mode, &ts.Variation)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &ts, nil
}

// Set stores the theme settings, replacing any previous selection.
func (s *ThemeSettingsStore) Set(ctx context.Context, ts *ThemeSettings) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO theme_settings (id, theme_id, mode, variation)
		VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			theme_id = excluded.theme_id,
			mode = excluded.mode,
			variation = excluded.variation,
			updated_at = CURRENT_TIMESTAMP
	`, ts.ThemeID, ts.Mode, ts.Variation)
	return err
}
