// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestThemeSettingsStore_SlotsAreIndependent(t *testing.T) {
	store := models.NewThemeSettingsStore(testdb.NewMigratedSQLite(t, "theme-settings"))
	ctx := t.Context()

	slots, err := store.GetAll(ctx)
	require.NoError(t, err)
	require.Equal(t, models.ThemeSlots{}, slots)

	require.NoError(t, store.Set(ctx, models.ThemeSlotDefault, &models.ThemeSettings{ThemeID: "minimal", Mode: "dark", Variation: "blue"}))
	require.NoError(t, store.Set(ctx, models.ThemeSlotMobile, &models.ThemeSettings{ThemeID: "catppuccin", Mode: "dark"}))
	// Overwrite: the slot's row is replaced, variation cleared.
	require.NoError(t, store.Set(ctx, models.ThemeSlotDefault, &models.ThemeSettings{ThemeID: "catppuccin", Mode: "auto"}))

	slots, err = store.GetAll(ctx)
	require.NoError(t, err)
	require.Equal(t, models.ThemeSlots{
		Default: &models.ThemeSettings{ThemeID: "catppuccin", Mode: "auto"},
		Mobile:  &models.ThemeSettings{ThemeID: "catppuccin", Mode: "dark"},
	}, slots)

	require.NoError(t, store.Delete(ctx, models.ThemeSlotMobile))
	slots, err = store.GetAll(ctx)
	require.NoError(t, err)
	require.Equal(t, models.ThemeSlots{Default: &models.ThemeSettings{ThemeID: "catppuccin", Mode: "auto"}}, slots)
}

func TestThemeSettingsMigration_KeepsSingleRowAsDefaultSlot(t *testing.T) {
	// Put the database back at the single-row schema with a stored selection,
	// then let the slot migration run again on open.
	dbPath := testdb.CloneMigratedSQLite(t, "theme-settings-migration")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	ctx := t.Context()
	for _, stmt := range []string{
		`DROP TABLE theme_settings`,
		`CREATE TABLE theme_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			theme_id TEXT NOT NULL,
			mode TEXT NOT NULL DEFAULT 'auto',
			variation TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO theme_settings (id, theme_id, mode, variation) VALUES (1, 'minimal', 'dark', 'blue')`,
		`DELETE FROM migrations WHERE filename = '104_theme_settings_slots.sql'`,
	} {
		_, err := db.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	require.NoError(t, db.Close())

	db, err = database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	slots, err := models.NewThemeSettingsStore(db).GetAll(ctx)
	require.NoError(t, err)
	require.Equal(t, models.ThemeSlots{Default: &models.ThemeSettings{ThemeID: "minimal", Mode: "dark", Variation: "blue"}}, slots)
}
