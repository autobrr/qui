// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestSeasonPackRunStore_ListBreaksCreatedAtTiesByID(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewSeasonPackRunStore(db)

	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		INSERT INTO season_pack_runs (torrent_name, phase, status, created_at)
		VALUES (?, ?, ?, ?), (?, ?, ?, ?)
	`,
		"Pack.S01.720p", "check", "ready", "2026-03-31 12:00:00",
		"Pack.S02.1080p", "check", "ready", "2026-03-31 12:00:00",
	)
	require.NoError(t, err)

	runs, err := store.List(ctx, 2)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, "Pack.S02.1080p", runs[0].TorrentName)
	require.Greater(t, runs[0].ID, runs[1].ID)
}
