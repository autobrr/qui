// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"

	"github.com/autobrr/qui/internal/dbinterface"
)

const (
	ThemeSlotDefault = "default"
	ThemeSlotMobile  = "mobile"
)

// ThemeSettings is one stored theme selection (a theme slot).
type ThemeSettings struct {
	ThemeID   string `json:"themeId"`
	Mode      string `json:"mode"`
	Variation string `json:"variation,omitempty"`
}

// ThemeSlots holds every stored theme slot. A nil Mobile means the mobile
// layout uses the default slot.
type ThemeSlots struct {
	Default *ThemeSettings `json:"default,omitempty"`
	Mobile  *ThemeSettings `json:"mobile,omitempty"`
}

type ThemeSettingsStore struct {
	db dbinterface.Querier
}

func NewThemeSettingsStore(db dbinterface.Querier) *ThemeSettingsStore {
	return &ThemeSettingsStore{db: db}
}

// GetAll returns every stored theme slot.
func (s *ThemeSettingsStore) GetAll(ctx context.Context) (ThemeSlots, error) {
	var slots ThemeSlots

	rows, err := s.db.QueryContext(ctx, `SELECT slot, theme_id, mode, variation FROM theme_settings`)
	if err != nil {
		return slots, err
	}
	defer rows.Close()

	for rows.Next() {
		var slot string
		var ts ThemeSettings
		if err := rows.Scan(&slot, &ts.ThemeID, &ts.Mode, &ts.Variation); err != nil {
			return slots, err
		}
		switch slot {
		case ThemeSlotDefault:
			slots.Default = &ts
		case ThemeSlotMobile:
			slots.Mobile = &ts
		}
	}

	return slots, rows.Err()
}

// Set stores the theme selection of one slot, replacing any previous one.
func (s *ThemeSettingsStore) Set(ctx context.Context, slot string, ts *ThemeSettings) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO theme_settings (slot, theme_id, mode, variation)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (slot) DO UPDATE SET
			theme_id = excluded.theme_id,
			mode = excluded.mode,
			variation = excluded.variation,
			updated_at = CURRENT_TIMESTAMP
	`, slot, ts.ThemeID, ts.Mode, ts.Variation)
	return err
}

// Delete removes one slot.
func (s *ThemeSettingsStore) Delete(ctx context.Context, slot string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM theme_settings WHERE slot = ?`, slot)
	return err
}
