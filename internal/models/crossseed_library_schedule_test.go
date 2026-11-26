// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestCrossSeedStore_LibrarySchedule(t *testing.T) {
	checkLibrarySchedule(t, testdb.NewMigratedSQLite(t, "crossseed-library-schedule"))
}

func TestCrossSeedStore_LibrarySchedulePostgresIntegration(t *testing.T) {
	checkLibrarySchedule(t, testdb.NewMigratedPostgres(t, "crossseed-library-schedule"))
}

func checkLibrarySchedule(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := t.Context()
	store, err := models.NewCrossSeedStore(db, make([]byte, 32))
	require.NoError(t, err)
	instances, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instances.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	defaults, err := store.GetSearchSettings(ctx)
	require.NoError(t, err)
	require.False(t, defaults.ScheduleEnabled)
	require.True(t, defaults.TorznabEnabled)
	_, found, err := store.LastScheduledSearchRun(ctx)
	require.NoError(t, err)
	require.False(t, found)

	started := time.Date(2026, 11, 17, 8, 30, 0, 0, time.UTC)
	require.NoError(t, store.SetLastScheduledSearchRun(ctx, started))

	want := &models.CrossSeedSearchSettings{
		InstanceID:             &instance.ID,
		Categories:             []string{"movies"},
		Tags:                   []string{"keep"},
		ExcludeCategories:      []string{"no-cross-seed"},
		ExcludeTags:            []string{"no-cs"},
		IndexerIDs:             []int{3},
		IntervalSeconds:        5,
		CooldownMinutes:        720,
		MaxAddedAgeDays:        30,
		SkipIndividualEpisodes: true,
		TorznabEnabled:         false,
		ScheduleEnabled:        true,
		ScheduleIntervalHours:  6,
		ScheduleMaxSearches:    50,
		ScheduleMaxAgeDays:     7,
	}
	saved, err := store.UpsertSearchSettings(ctx, want)
	require.NoError(t, err)
	saved.CreatedAt, saved.UpdatedAt = time.Time{}, time.Time{}
	require.Equal(t, want, saved)

	last, found, err := store.LastScheduledSearchRun(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, started.Equal(last), "got %s", last)

	require.NoError(t, store.ClearLastScheduledSearchRun(ctx))
	_, found, err = store.LastScheduledSearchRun(ctx)
	require.NoError(t, err)
	require.False(t, found)

	run, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{
		InstanceID:  instance.ID,
		Kind:        models.CrossSeedSearchRunKindScheduled,
		Status:      models.CrossSeedSearchRunStatusRunning,
		Filters:     models.CrossSeedSearchFilters{Categories: []string{"movies"}, ExcludeCategories: []string{"no-cross-seed"}, ExcludeTags: []string{"no-cs"}},
		SearchLimit: 400,
	})
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedSearchRunKindScheduled, run.Kind)
	require.Equal(t, []string{"no-cross-seed"}, run.Filters.ExcludeCategories)
	require.Equal(t, []string{"no-cs"}, run.Filters.ExcludeTags)
	require.Equal(t, 400, run.SearchLimit)

	run.WaitingTorrents = 200
	require.NoError(t, store.UpdateSearchRun(ctx, run))
	manual, err := store.CreateSearchRun(ctx, &models.CrossSeedSearchRun{InstanceID: instance.ID, Status: models.CrossSeedSearchRunStatusRunning})
	require.NoError(t, err)

	runs, err := store.ListSearchRuns(ctx, instance.ID, 10, 0)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	byID := map[int64]*models.CrossSeedSearchRun{runs[0].ID: runs[0], runs[1].ID: runs[1]}
	require.Equal(t, 200, byID[run.ID].WaitingTorrents)
	require.Equal(t, models.CrossSeedSearchRunKindManual, byID[manual.ID].Kind, "a run without a kind is manual")
}
