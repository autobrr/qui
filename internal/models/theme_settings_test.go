// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestThemeSettingsStore_GetEmpty(t *testing.T) {
	store := models.NewThemeSettingsStore(testdb.NewMigratedSQLite(t, "theme-settings"))

	settings, err := store.Get(context.Background())
	require.NoError(t, err)
	require.Nil(t, settings)
}

func TestThemeSettingsStore_SetAndOverwrite(t *testing.T) {
	store := models.NewThemeSettingsStore(testdb.NewMigratedSQLite(t, "theme-settings"))
	ctx := context.Background()

	require.NoError(t, store.Set(ctx, &models.ThemeSettings{ThemeID: "minimal", Mode: "dark", Variation: "blue"}))

	settings, err := store.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, &models.ThemeSettings{ThemeID: "minimal", Mode: "dark", Variation: "blue"}, settings)

	// Overwrite: the single row is replaced, variation cleared.
	require.NoError(t, store.Set(ctx, &models.ThemeSettings{ThemeID: "catppuccin", Mode: "auto"}))

	settings, err = store.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, &models.ThemeSettings{ThemeID: "catppuccin", Mode: "auto"}, settings)
}
