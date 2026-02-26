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
	"github.com/autobrr/qui/internal/testdb"
)

func setupSearchCacheDB(t *testing.T) (*database.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	dbPath := testdb.PathFromTemplate(t, "models", "search-cache.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
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
