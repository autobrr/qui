// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// expiredCacheAt is older than the max age and the cleanup age below, so an
// expired read is an expired read on both engines.
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

// regression: #3100
func checkRefreshedEntrySurvivesExpiredFetch(t *testing.T, db *database.DB) {
	indexerID := insertTestTorznabIndexer(t, db, "Cache Indexer", "http://indexer.example")
	const cacheKey = "cache-key-refresh"
	stale := []byte("stale-payload")
	fresh := []byte("fresh-payload")

	// The old Fetch deleted the expired row by id on a goroutine after it
	// returned the miss. When that delete ran after the refresh, it removed the
	// new payload. Fetch must leave the row to Cleanup.
	signals := newCacheSignals(db)
	store := models.NewTorznabTorrentCacheStore(signals)
	ctx := t.Context()
	require.NoError(t, store.Store(ctx, torrentCacheEntry(indexerID, cacheKey, stale)))
	backdateTorrentCache(t, db, indexerID, cacheKey)

	_, ok, err := store.Fetch(ctx, indexerID, cacheKey, time.Hour)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, store.Store(ctx, torrentCacheEntry(indexerID, cacheKey, fresh)))
	got, ok, err := store.Fetch(ctx, indexerID, cacheKey, 0)
	require.NoError(t, err)
	require.True(t, ok, "refreshed payload missing after an expired fetch")
	require.Equal(t, fresh, got)

	// The touch from the hit runs on a goroutine that Fetch started after an
	// eviction goroutine would have started. Go does not run them in that
	// order, so with one CPU this check on SQLite can miss a delete that has
	// not run yet. The Postgres run catches it.
	awaitTorrentCacheTouch(t, db, signals, indexerID, cacheKey)
	select {
	case <-signals.evicted:
		t.Fatal("an expired fetch deleted the cache row")
	default:
	}
}

func checkFetchMissesExpiredAndHitsLive(t *testing.T, db *database.DB) {
	indexerID := insertTestTorznabIndexer(t, db, "Cache Indexer", "http://indexer.example")
	signals := newCacheSignals(db)
	store := models.NewTorznabTorrentCacheStore(signals)
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
	// test can see that the touch wrote it.
	ageTorrentCacheLastUsed(t, db, indexerID, "cache-key-live")
	got, ok, err := store.Fetch(ctx, indexerID, "cache-key-live", time.Hour)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, live, got)
	awaitTorrentCacheTouch(t, db, signals, indexerID, "cache-key-live")
}

func checkCleanupDeletesOldRows(t *testing.T, db *database.DB) {
	indexerID := insertTestTorznabIndexer(t, db, "Cache Indexer", "http://indexer.example")
	signals := newCacheSignals(db)
	store := models.NewTorznabTorrentCacheStore(signals)
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
	awaitTorrentCacheTouch(t, db, signals, indexerID, "cache-key-new")
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

func awaitTorrentCacheTouch(t *testing.T, db *database.DB, signals *cacheSignals, indexerID int, cacheKey string) {
	t.Helper()
	select {
	case <-signals.touched:
	case <-time.After(5 * time.Second):
		t.Fatal("cache hit did not touch last_used_at")
	}

	var lastUsed time.Time
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT last_used_at
		FROM torznab_torrent_cache
		WHERE indexer_id = ? AND cache_key = ?`,
		indexerID, cacheKey).Scan(&lastUsed))
	require.True(t, lastUsed.After(expiredCacheAt), "touch did not update last_used_at")
}

// cacheSignals sends on evicted when a delete on torznab_torrent_cache
// arrives, whatever shape its SQL takes, and on touched after the async cache
// touch has written last_used_at.
type cacheSignals struct {
	dbinterface.Querier
	evicted chan struct{}
	touched chan struct{}
}

func newCacheSignals(db dbinterface.Querier) *cacheSignals {
	return &cacheSignals{Querier: db, evicted: make(chan struct{}, 1), touched: make(chan struct{}, 1)}
}

func (s *cacheSignals) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if isTorznabTorrentCacheDelete(query) {
		notify(s.evicted)
	}
	result, err := s.Querier.ExecContext(ctx, query, args...)
	if strings.HasPrefix(normalizedSQL(query), "UPDATE TORZNAB_TORRENT_CACHE SET LAST_USED_AT") {
		notify(s.touched)
	}
	return result, err
}

func notify(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

const torznabTorrentCacheDeletePrefix = "DELETE FROM TORZNAB_TORRENT_CACHE"

func normalizedSQL(query string) string {
	return strings.ToUpper(strings.Join(strings.Fields(query), " "))
}

func isTorznabTorrentCacheDelete(query string) bool {
	return strings.Contains(normalizedSQL(query), torznabTorrentCacheDeletePrefix)
}
