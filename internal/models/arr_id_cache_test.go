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

// forceNonUTCLocal points time.Local at a fixed non-UTC zone for the duration of
// the test, reproducing a home server whose system clock is not UTC. CI runners
// default to UTC, which is the exact configuration that hid #1961, so without this
// the regression would stay invisible. These tests must not call t.Parallel():
// time.Local is process-global, but Go runs non-parallel tests sequentially before
// any parallel ones resume, so a non-parallel test owns time.Local while it runs.
func forceNonUTCLocal(t *testing.T) {
	t.Helper()
	original := time.Local
	// -06:00, no DST; any non-UTC offset triggers the original lexical-comparison flip.
	time.Local = time.FixedZone("test-non-utc", -6*60*60)
	t.Cleanup(func() { time.Local = original })
}

// TestArrIDCacheStore_GetReturnsLiveEntryInNonUTCTimezone is the #1961 guard: Set
// stored expires_at in local time while Get compared it against CURRENT_TIMESTAMP
// (UTC). In any non-UTC zone the lexical comparison of the two string formats
// flipped, so Get returned sql.ErrNoRows for a freshly written, unexpired entry —
// silently disabling the cache. After the fix both sides use a UTC time.Time.
func TestArrIDCacheStore_GetReturnsLiveEntryInNonUTCTimezone(t *testing.T) {
	forceNonUTCLocal(t)

	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "arr-id-cache-nonutc")
	store := models.NewArrIDCacheStore(db)

	ids := &models.ExternalIDs{IMDbID: "tt1234567", TMDbID: 42}
	require.NoError(t, store.Set(ctx, "title-hash", "tv", nil, ids, false, time.Hour))

	entry, err := store.Get(ctx, "title-hash", "tv")
	require.NoError(t, err) // pre-fix in non-UTC: "sql: no rows in result set"
	require.NotNil(t, entry)
	require.Equal(t, *ids, entry.ExternalIDs)
	require.False(t, entry.IsNegative)
}

// TestArrIDCacheStore_ExpiryHelpersRespectNonUTCTimezone confirms the same UTC
// normalization holds for the expiry-counting and cleanup helpers: a live entry
// counts as valid and survives cleanup, while an already-expired entry is excluded
// and removed — regardless of the process timezone.
func TestArrIDCacheStore_ExpiryHelpersRespectNonUTCTimezone(t *testing.T) {
	forceNonUTCLocal(t)

	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "arr-id-cache-nonutc-expiry")
	store := models.NewArrIDCacheStore(db)

	require.NoError(t, store.Set(ctx, "live", "tv", nil, nil, false, time.Hour))
	require.NoError(t, store.Set(ctx, "stale", "movie", nil, nil, false, -time.Hour))

	// The already-expired entry must be unretrievable via Get even before cleanup runs.
	_, err := store.Get(ctx, "stale", "movie")
	require.ErrorIs(t, err, sql.ErrNoRows)

	valid, err := store.CountValid(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), valid)

	removed, err := store.CleanupExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)

	total, err := store.Count(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)

	// The live entry is the survivor and is still retrievable.
	entry, err := store.Get(ctx, "live", "tv")
	require.NoError(t, err)
	require.NotNil(t, entry)
}

// TestArrIDCacheStore_GetReturnsNegativeEntryInNonUTCTimezone confirms negative
// cache entries (a remembered "not found", with no external IDs) round-trip in a
// non-UTC zone too — they take the same UTC-normalized expiry path as positive
// entries, and a negative hit is what spares the upstream ARR a repeat lookup.
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

// TestArrIDCacheStore_NegativeWriteDoesNotClobberLivePositive is the #2300 guard:
// concurrent lookups of the same title can race, and the slower one may resolve
// nothing. Its negative write must not replace freshly cached IDs, or every
// search for that title goes ID-less until the negative TTL expires.
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

		// Get filters expired rows, so read the row state via CountValid semantics:
		// the negative entry must be the live one.
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
