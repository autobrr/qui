// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func forceNonUTCLocal(t *testing.T) {
	t.Helper()
	original := time.Local
	time.Local = time.FixedZone("test-non-utc", -6*60*60)
	t.Cleanup(func() { time.Local = original })
}

func TestArrIDCacheStore_GetReturnsLiveEntryInNonUTCTimezone(t *testing.T) {
	forceNonUTCLocal(t)

	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "arr-id-cache-nonutc")
	store := models.NewArrIDCacheStore(db)

	ids := &models.ExternalIDs{IMDbID: "tt1234567", TMDbID: 42}
	require.NoError(t, store.Set(ctx, "title-hash", "tv", nil, ids, false, time.Hour))

	entry, err := store.Get(ctx, "title-hash", "tv")
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.Equal(t, *ids, entry.ExternalIDs)
	require.False(t, entry.IsNegative)
}

func TestArrIDCacheStore_ExpiryHelpersRespectNonUTCTimezone(t *testing.T) {
	forceNonUTCLocal(t)

	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "arr-id-cache-nonutc-expiry")
	store := models.NewArrIDCacheStore(db)

	require.NoError(t, store.Set(ctx, "live", "tv", nil, nil, false, time.Hour))
	require.NoError(t, store.Set(ctx, "stale", "movie", nil, nil, false, -time.Hour))

	_, err := store.Get(ctx, "stale", "movie")
	require.ErrorIs(t, err, sql.ErrNoRows)

	removed, err := store.CleanupExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)

	removed, err = store.CleanupExpired(ctx)
	require.NoError(t, err)
	require.Zero(t, removed)

	entry, err := store.Get(ctx, "live", "tv")
	require.NoError(t, err)
	require.NotNil(t, entry)
}

func TestArrIDCacheStore_GetReturnsNegativeEntryInNonUTCTimezone(t *testing.T) {
	forceNonUTCLocal(t)

	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "arr-id-cache-nonutc-negative")
	store := models.NewArrIDCacheStore(db)

	require.NoError(t, store.Set(ctx, "missing-title", "movie", nil, nil, true, time.Hour))

	entry, err := store.Get(ctx, "missing-title", "movie")
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.True(t, entry.IsNegative)
	require.True(t, entry.ExternalIDs.IsEmpty())
}

func TestArrIDCacheStore_NegativeWriteDoesNotClobberLivePositive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "arr-id-cache-negative-clobber")
	store := models.NewArrIDCacheStore(db)
	ids := &models.ExternalIDs{IMDbID: "tt1234567", TMDbID: 42}

	t.Run("negative loses to live positive", func(t *testing.T) {
		require.NoError(t, store.Set(ctx, "race", "movie", nil, ids, false, time.Hour))
		require.NoError(t, store.Set(ctx, "race", "movie", nil, nil, true, time.Hour))

		entry, err := store.Get(ctx, "race", "movie")
		require.NoError(t, err)
		require.False(t, entry.IsNegative)
		require.Equal(t, *ids, entry.ExternalIDs)
	})

	t.Run("positive replaces negative", func(t *testing.T) {
		require.NoError(t, store.Set(ctx, "neg-first", "movie", nil, nil, true, time.Hour))
		require.NoError(t, store.Set(ctx, "neg-first", "movie", nil, ids, false, time.Hour))

		entry, err := store.Get(ctx, "neg-first", "movie")
		require.NoError(t, err)
		require.False(t, entry.IsNegative)
		require.Equal(t, *ids, entry.ExternalIDs)
	})

	t.Run("positive refreshes positive", func(t *testing.T) {
		newer := &models.ExternalIDs{IMDbID: "tt7654321", TMDbID: 7}
		require.NoError(t, store.Set(ctx, "refresh", "movie", nil, ids, false, time.Hour))
		require.NoError(t, store.Set(ctx, "refresh", "movie", nil, newer, false, time.Hour))

		entry, err := store.Get(ctx, "refresh", "movie")
		require.NoError(t, err)
		require.Equal(t, *newer, entry.ExternalIDs)
	})

	t.Run("negative replaces expired positive", func(t *testing.T) {
		require.NoError(t, store.Set(ctx, "expired", "movie", nil, ids, false, -time.Minute))
		require.NoError(t, store.Set(ctx, "expired", "movie", nil, nil, true, time.Hour))

		entry, err := store.Get(ctx, "expired", "movie")
		require.NoError(t, err)
		require.True(t, entry.IsNegative)
	})

	t.Run("negative refreshes negative", func(t *testing.T) {
		require.NoError(t, store.Set(ctx, "neg-refresh", "movie", nil, nil, true, time.Minute))
		require.NoError(t, store.Set(ctx, "neg-refresh", "movie", nil, nil, true, time.Hour))

		entry, err := store.Get(ctx, "neg-refresh", "movie")
		require.NoError(t, err)
		require.True(t, entry.IsNegative)
	})
}

func TestArrIDCacheStore_EpisodeMapRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "arr-id-cache-episode-map")
	store := models.NewArrIDCacheStore(db)
	ids := &models.ExternalIDs{TVDbID: 471000}
	episodeMap := &models.EpisodeMap{Season: 4, Episode: 15, Absolute: 81}

	t.Run("map stored and read back", func(t *testing.T) {
		require.NoError(t, store.SetWithTitles(ctx, "mapped", "tv", nil, ids, []string{"Solitude"}, episodeMap, true, false, time.Hour))
		entry, err := store.Get(ctx, "mapped", "tv")
		require.NoError(t, err)
		require.True(t, entry.HasEpisodeMap)
		require.Equal(t, episodeMap, entry.EpisodeMap)
	})

	t.Run("looked and found none sets the flag with nulls", func(t *testing.T) {
		require.NoError(t, store.SetWithTitles(ctx, "unmapped", "tv", nil, ids, []string{"Solitude"}, nil, true, false, time.Hour))
		entry, err := store.Get(ctx, "unmapped", "tv")
		require.NoError(t, err)
		require.True(t, entry.HasEpisodeMap)
		require.Nil(t, entry.EpisodeMap)
	})

	t.Run("legacy positive write leaves the flag unset", func(t *testing.T) {
		require.NoError(t, store.Set(ctx, "legacy", "tv", nil, ids, false, time.Hour))
		entry, err := store.Get(ctx, "legacy", "tv")
		require.NoError(t, err)
		require.False(t, entry.HasEpisodeMap)
		require.Nil(t, entry.EpisodeMap)
	})

	t.Run("negative row carries no map", func(t *testing.T) {
		require.NoError(t, store.Set(ctx, "negative", "tv", nil, nil, true, time.Hour))
		entry, err := store.Get(ctx, "negative", "tv")
		require.NoError(t, err)
		require.True(t, entry.IsNegative)
		require.False(t, entry.HasEpisodeMap)
		require.Nil(t, entry.EpisodeMap)
	})
}
