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

func TestClientSettingsStore_GetAllEmpty(t *testing.T) {
	store := models.NewClientSettingsStore(testdb.NewMigratedSQLite(t, "client-settings"))

	settings, err := store.GetAll(context.Background())
	require.NoError(t, err)
	require.Empty(t, settings)
}

func TestClientSettingsStore_SetManyAndOverwrite(t *testing.T) {
	store := models.NewClientSettingsStore(testdb.NewMigratedSQLite(t, "client-settings"))
	ctx := context.Background()

	require.NoError(t, store.SetMany(ctx, map[string]string{
		"qui-speed-units":       `"bits"`,
		"qui-column-sorting:1":  `[{"id":"name","desc":false}]`,
		"qui-torrent-view-mode": "compact",
	}))

	settings, err := store.GetAll(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"qui-speed-units":       `"bits"`,
		"qui-column-sorting:1":  `[{"id":"name","desc":false}]`,
		"qui-torrent-view-mode": "compact",
	}, settings)

	// Partial update overwrites one key and leaves the others untouched.
	require.NoError(t, store.SetMany(ctx, map[string]string{
		"qui-speed-units": `"bytes"`,
	}))

	settings, err = store.GetAll(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"qui-speed-units":       `"bytes"`,
		"qui-column-sorting:1":  `[{"id":"name","desc":false}]`,
		"qui-torrent-view-mode": "compact",
	}, settings)
}
