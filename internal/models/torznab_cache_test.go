// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// expiredCacheAt is before the synctest bubble clock (2000-01-01) and older
// than the cleanup ages below, so an expired read is an expired read on both
// clocks and both engines.
var expiredCacheAt = time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)

func TestTorznabTorrentCacheStoreSQLite(t *testing.T) {
	t.Parallel()
	runTorznabTorrentCacheStoreTests(t, func(t *testing.T) *database.DB {
		return testdb.NewMigratedSQLite(t, "torznab-torrent-cache")
	})
}

func TestTorznabTorrentCacheStorePostgresIntegration(t *testing.T) {
	t.Parallel()
	runTorznabTorrentCacheStoreTests(t, func(t *testing.T) *database.DB {
		return testdb.NewMigratedPostgres(t, "torznab-torrent-cache")
	})
}

func runTorznabTorrentCacheStoreTests(t *testing.T, open func(t *testing.T) *database.DB) {
	t.Run("refreshed entry survives an expired fetch", func(t *testing.T) {
		checkRefreshedEntrySurvivesExpiredFetch(t, open(t))
	})
	t.Run("fetch misses when expired and hits while live", func(t *testing.T) {
		checkFetchMissesExpiredAndHitsLive(t, open(t))
	})
	t.Run("cleanup deletes rows older than the age", func(t *testing.T) {
		checkCleanupDeletesOldRows(t, open(t))
	})
}

func checkRefreshedEntrySurvivesExpiredFetch(t *testing.T, db *database.DB) {
	indexerID := insertTestTorznabIndexer(t, db, "Cache Indexer", "http://indexer.example")
	const cacheKey = "cache-key-refresh"
	stale := []byte("stale-payload")
	fresh := []byte("fresh-payload")

	store := models.NewTorznabTorrentCacheStore(db)
	require.NoError(t, store.Store(context.Background(), torrentCacheEntry(indexerID, cacheKey, stale)))
	backdateTorrentCache(t, db, indexerID, cacheKey)

	synctest.Test(t, func(t *testing.T) {
		// Hold a delete-by-id until the refresh is stored. The old Fetch parked
		// that delete on a goroutine after returning the miss; releasing it
		// afterwards removed the new payload. With the goroutine gone, nothing
		// receives on hold.
		hold := make(chan struct{})
		var releaseOnce sync.Once
		release := func() { releaseOnce.Do(func() { close(hold) }) }
		defer release()

		gated := &evictionGate{db: db, hold: hold}
		gatedStore := models.NewTorznabTorrentCacheStore(gated)
		ctx := t.Context()

		_, ok, err := gatedStore.Fetch(ctx, indexerID, cacheKey, time.Hour)
		require.NoError(t, err)
		require.False(t, ok)

		// Park the old eviction if Fetch started one, then refresh.
		synctest.Wait()
		require.NoError(t, gatedStore.Store(ctx, torrentCacheEntry(indexerID, cacheKey, fresh)))
		release()
		synctest.Wait()

		got, ok, err := gatedStore.Fetch(ctx, indexerID, cacheKey, 0)
		require.NoError(t, err)
		require.True(t, ok, "refreshed payload missing after an expired fetch")
		require.Equal(t, fresh, got)
		synctest.Wait()
	})
}

func checkFetchMissesExpiredAndHitsLive(t *testing.T, db *database.DB) {
	indexerID := insertTestTorznabIndexer(t, db, "Cache Indexer", "http://indexer.example")
	store := models.NewTorznabTorrentCacheStore(db)
	ctx := t.Context()

	stale := []byte("stale-payload")
	live := []byte("live-payload")
	require.NoError(t, store.Store(ctx, torrentCacheEntry(indexerID, "cache-key-stale", stale)))
	require.NoError(t, store.Store(ctx, torrentCacheEntry(indexerID, "cache-key-live", live)))
	backdateTorrentCache(t, db, indexerID, "cache-key-stale")

	_, ok, err := store.Fetch(ctx, indexerID, "cache-key-stale", time.Hour)
	require.NoError(t, err)
	require.False(t, ok)

	// A hit touches last_used_at after it returns. Age that column first so the
	// test can see the write finish before the database is closed.
	ageTorrentCacheLastUsed(t, db, indexerID, "cache-key-live")
	got, ok, err := store.Fetch(ctx, indexerID, "cache-key-live", time.Hour)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, live, got)
	awaitTorrentCacheTouch(t, db, indexerID, "cache-key-live")
}

func checkCleanupDeletesOldRows(t *testing.T, db *database.DB) {
	indexerID := insertTestTorznabIndexer(t, db, "Cache Indexer", "http://indexer.example")
	store := models.NewTorznabTorrentCacheStore(db)
	ctx := t.Context()

	oldPayload := []byte("old-payload")
	newPayload := []byte("new-payload")
	require.NoError(t, store.Store(ctx, torrentCacheEntry(indexerID, "cache-key-old", oldPayload)))
	require.NoError(t, store.Store(ctx, torrentCacheEntry(indexerID, "cache-key-new", newPayload)))
	backdateTorrentCache(t, db, indexerID, "cache-key-old")

	deleted, err := store.Cleanup(ctx, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	deleted, err = store.Cleanup(ctx, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, int64(0), deleted)

	_, ok, err := store.Fetch(ctx, indexerID, "cache-key-old", 0)
	require.NoError(t, err)
	require.False(t, ok)

	ageTorrentCacheLastUsed(t, db, indexerID, "cache-key-new")
	got, ok, err := store.Fetch(ctx, indexerID, "cache-key-new", 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, newPayload, got)
	awaitTorrentCacheTouch(t, db, indexerID, "cache-key-new")
}

func torrentCacheEntry(indexerID int, cacheKey string, payload []byte) *models.TorznabTorrentCacheEntry {
	return &models.TorznabTorrentCacheEntry{
		IndexerID:   indexerID,
		CacheKey:    cacheKey,
		GUID:        cacheKey,
		Title:       "Synthetic Cache Payload",
		SizeBytes:   int64(len(payload)),
		TorrentData: payload,
	}
}

func backdateTorrentCache(t *testing.T, db *database.DB, indexerID int, cacheKey string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		UPDATE torznab_torrent_cache
		SET cached_at = ?, last_used_at = ?
		WHERE indexer_id = ? AND cache_key = ?`,
		expiredCacheAt, expiredCacheAt, indexerID, cacheKey)
	require.NoError(t, err)
}

func ageTorrentCacheLastUsed(t *testing.T, db *database.DB, indexerID int, cacheKey string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		UPDATE torznab_torrent_cache
		SET last_used_at = ?
		WHERE indexer_id = ? AND cache_key = ?`,
		expiredCacheAt, indexerID, cacheKey)
	require.NoError(t, err)
}

func awaitTorrentCacheTouch(t *testing.T, db *database.DB, indexerID int, cacheKey string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var lastUsed time.Time
		err := db.QueryRowContext(context.Background(), `
			SELECT last_used_at
			FROM torznab_torrent_cache
			WHERE indexer_id = ? AND cache_key = ?`,
			indexerID, cacheKey).Scan(&lastUsed)
		return err == nil && lastUsed.After(expiredCacheAt)
	}, 5*time.Second, 10*time.Millisecond)
}

// evictionGate holds a delete-by-id until hold is closed.
type evictionGate struct {
	db   dbinterface.Querier
	hold chan struct{}
}

func (g *evictionGate) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "DELETE FROM torznab_torrent_cache WHERE id") {
		<-g.hold
	}
	return g.db.ExecContext(ctx, query, args...)
}

func (g *evictionGate) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return g.db.QueryContext(ctx, query, args...)
}

func (g *evictionGate) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return g.db.QueryRowContext(ctx, query, args...)
}

func (g *evictionGate) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	return g.db.BeginTx(ctx, opts)
}
