// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestDefaultCrossSeedAutomationSettings_SeasonPackDefaults(t *testing.T) {
	settings := models.DefaultCrossSeedAutomationSettings()
	require.False(t, settings.SeasonPackEnabled)
	require.InDelta(t, 0.75, settings.SeasonPackCoverageThreshold, 0.0001)
}

func TestSeasonPackRunStore_CreateAndList(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewSeasonPackRunStore(db)

	ctx := context.Background()

	instanceID := 1
	created, err := store.Create(ctx, &models.SeasonPackRun{
		TorrentName:     "Show.S01.1080p.WEB-DL-GRP",
		Phase:           "check",
		Status:          "ready",
		InstanceID:      &instanceID,
		MatchedEpisodes: 10,
		TotalEpisodes:   12,
		Coverage:        0.8333,
	})
	require.NoError(t, err)
	require.NotZero(t, created.ID)

	runs, err := store.List(ctx, 20)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, created.ID, runs[0].ID)
	require.Equal(t, "Show.S01.1080p.WEB-DL-GRP", runs[0].TorrentName)
	require.Equal(t, "check", runs[0].Phase)
	require.Equal(t, "ready", runs[0].Status)
	require.NotNil(t, runs[0].InstanceID)
	require.Equal(t, 1, *runs[0].InstanceID)
	require.Equal(t, 10, runs[0].MatchedEpisodes)
	require.Equal(t, 12, runs[0].TotalEpisodes)
	require.InDelta(t, 0.8333, runs[0].Coverage, 0.0001)
}

func TestSeasonPackRunStore_CreateWithoutInstance(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewSeasonPackRunStore(db)

	ctx := context.Background()

	created, err := store.Create(ctx, &models.SeasonPackRun{
		TorrentName:     "Show.S02.720p.HDTV-GRP",
		Phase:           "check",
		Status:          "skipped",
		Reason:          "below threshold",
		MatchedEpisodes: 3,
		TotalEpisodes:   10,
		Coverage:        0.3,
	})
	require.NoError(t, err)
	require.NotZero(t, created.ID)
	require.Nil(t, created.InstanceID)
	require.Equal(t, "below threshold", created.Reason)
}

func TestSeasonPackRunStore_ListLimit(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewSeasonPackRunStore(db)

	ctx := context.Background()

	for i := range 5 {
		_, err := store.Create(ctx, &models.SeasonPackRun{
			TorrentName:   "Show.S01.1080p.WEB-DL-GRP",
			Phase:         "check",
			Status:        "ready",
			TotalEpisodes: i + 1,
		})
		require.NoError(t, err)
	}

	runs, err := store.List(ctx, 3)
	require.NoError(t, err)
	require.Len(t, runs, 3)
}

func TestSeasonPackRunStore_SettingsRoundTrip(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)

	ctx := context.Background()

	// Defaults should include season pack fields
	defaults, err := store.GetSettings(ctx)
	require.NoError(t, err)
	require.False(t, defaults.SeasonPackEnabled)
	require.InDelta(t, 0.75, defaults.SeasonPackCoverageThreshold, 0.0001)

	// Upsert with season pack fields
	updated, err := store.UpsertSettings(ctx, &models.CrossSeedAutomationSettings{
		Enabled:                     true,
		RunIntervalMinutes:          60,
		StartPaused:                 true,
		SeasonPackEnabled:           true,
		SeasonPackCoverageThreshold: 0.85,
		RSSAutomationTags:           []string{"cross-seed"},
		SeededSearchTags:            []string{"cross-seed"},
		CompletionSearchTags:        []string{"cross-seed"},
		WebhookTags:                 []string{"cross-seed"},
	})
	require.NoError(t, err)
	require.True(t, updated.SeasonPackEnabled)
	require.InDelta(t, 0.85, updated.SeasonPackCoverageThreshold, 0.0001)

	// Reload should match
	reloaded, err := store.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, reloaded.SeasonPackEnabled)
	require.InDelta(t, 0.85, reloaded.SeasonPackCoverageThreshold, 0.0001)
}
