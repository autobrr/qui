// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func setupSearchCacheDB(t *testing.T) (*database.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "search-cache")
	return db, ctx
}

func TestTorznabSearchCacheStore_RebaseTTL_ExtendsExistingEntries(t *testing.T) {
	t.Parallel()

	db, ctx := setupSearchCacheDB(t)
	store := models.NewTorznabSearchCacheStore(db)

	cachedAt := time.Now().Add(-2 * time.Hour).UTC()
	entry := &models.TorznabSearchCacheEntry{
		CacheKey:           "cache-key-extend",
		Scope:              "general",
		Query:              "Example Query",
		Categories:         []int{1000},
		IndexerIDs:         []int{1},
		RequestFingerprint: "fp-extend",
		ResponseData:       []byte(`{"Results":[],"Total":0}`),
		TotalResults:       0,
		CachedAt:           cachedAt,
		LastUsedAt:         cachedAt,
		ExpiresAt:          cachedAt.Add(30 * time.Minute),
	}

	require.NoError(t, store.Store(ctx, entry))

	rebased, err := store.RebaseTTL(ctx, 7*24*60) // 7 days
	require.NoError(t, err)
	require.Equal(t, int64(1), rebased)

	fetched, found, err := store.Fetch(ctx, entry.CacheKey)
	require.NoError(t, err)
	require.True(t, found, "entry should remain after TTL rebase")
	require.True(t, fetched.ExpiresAt.After(time.Now().UTC()))
}

func TestTorznabSearchCacheStore_RebaseTTL_CanExpireEntries(t *testing.T) {
	t.Parallel()

	db, ctx := setupSearchCacheDB(t)
	store := models.NewTorznabSearchCacheStore(db)

	cachedAt := time.Now().Add(-90 * time.Minute).UTC()
	entry := &models.TorznabSearchCacheEntry{
		CacheKey:           "cache-key-expire",
		Scope:              "general",
		Query:              "Old Query",
		Categories:         []int{5000},
		IndexerIDs:         []int{2},
		RequestFingerprint: "fp-expire",
		ResponseData:       []byte(`{"Results":[],"Total":0}`),
		TotalResults:       0,
		CachedAt:           cachedAt,
		LastUsedAt:         cachedAt,
		ExpiresAt:          cachedAt.Add(3 * time.Hour),
	}

	require.NoError(t, store.Store(ctx, entry))

	rebased, err := store.RebaseTTL(ctx, 30) // 30 minutes
	require.NoError(t, err)
	require.Equal(t, int64(1), rebased)

	// After rebasing to 30 minutes, this entry is now expired relative to now.
	_, found, err := store.Fetch(ctx, entry.CacheKey)
	require.NoError(t, err)
	require.False(t, found, "expired entry should be pruned after TTL shrink")
}

func TestTorznabSearchCacheStore_StatsReadsTheCachedTimeRange(t *testing.T) {
	t.Parallel()
	checkSearchCacheStatsTimeRange(t, testdb.NewMigratedSQLite(t, "search-cache-stats"))
}

func TestTorznabSearchCacheStore_StatsReadsTheCachedTimeRangePostgresIntegration(t *testing.T) {
	t.Parallel()
	checkSearchCacheStatsTimeRange(t, testdb.NewMigratedPostgres(t, "search-cache-stats"))
}

func checkSearchCacheStatsTimeRange(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := t.Context()
	store := models.NewTorznabSearchCacheStore(db)

	newest := time.Now().UTC().Add(-time.Hour).Truncate(time.Second).Add(250 * time.Millisecond)
	oldest := newest.Add(-2 * time.Hour)
	for key, cachedAt := range map[string]time.Time{"oldest": oldest, "newest": newest} {
		require.NoError(t, store.Store(ctx, &models.TorznabSearchCacheEntry{
			CacheKey:           key,
			Scope:              "general",
			Query:              key,
			Categories:         []int{1000},
			IndexerIDs:         []int{1},
			RequestFingerprint: key,
			ResponseData:       []byte(`{"Results":[],"Total":0}`),
			CachedAt:           cachedAt,
			LastUsedAt:         cachedAt.Add(time.Minute),
			ExpiresAt:          cachedAt.Add(24 * time.Hour),
		}))
	}

	stats, err := store.Stats(ctx)
	require.NoError(t, err)
	require.NotNil(t, stats.OldestCachedAt, "bound times")
	require.NotNil(t, stats.NewestCachedAt, "bound times")
	require.NotNil(t, stats.LastUsedAt, "bound times")
	require.True(t, stats.OldestCachedAt.Equal(oldest), "oldest %s, want %s", stats.OldestCachedAt, oldest)
	require.True(t, stats.NewestCachedAt.Equal(newest), "newest %s, want %s", stats.NewestCachedAt, newest)
	require.True(t, stats.LastUsedAt.Equal(newest.Add(time.Minute)), "last used %s", stats.LastUsedAt)

	// Rows that the database clock wrote hold whole seconds in this form.
	for key, cachedAt := range map[string]time.Time{"oldest": oldest, "newest": newest} {
		_, err := db.ExecContext(ctx, `UPDATE torznab_search_cache SET cached_at = ?, last_used_at = ? WHERE cache_key = ?`,
			cachedAt.Format(time.DateTime), cachedAt.Add(time.Minute).Format(time.DateTime), key)
		require.NoError(t, err)
	}

	stats, err = store.Stats(ctx)
	require.NoError(t, err)
	require.NotNil(t, stats.OldestCachedAt, "database clock times")
	require.NotNil(t, stats.NewestCachedAt, "database clock times")
	require.NotNil(t, stats.LastUsedAt, "database clock times")
	require.True(t, stats.OldestCachedAt.Equal(oldest.Truncate(time.Second)), "oldest %s", stats.OldestCachedAt)
	require.True(t, stats.NewestCachedAt.Equal(newest.Truncate(time.Second)), "newest %s", stats.NewestCachedAt)
	require.True(t, stats.LastUsedAt.Equal(newest.Add(time.Minute).Truncate(time.Second)), "last used %s", stats.LastUsedAt)
}
