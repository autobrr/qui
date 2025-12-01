// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupTorznabTorrentCacheStore(t *testing.T) (*TorznabTorrentCacheStore, func()) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	db := newMockQuerier(sqlDB)

	ctx := t.Context()

	// Create table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE torznab_torrent_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			indexer_id INTEGER NOT NULL,
			cache_key TEXT NOT NULL,
			guid TEXT NOT NULL DEFAULT '',
			download_url TEXT NOT NULL DEFAULT '',
			info_hash TEXT,
			title TEXT NOT NULL DEFAULT '',
			size_bytes INTEGER NOT NULL DEFAULT 0,
			torrent_data BLOB NOT NULL,
			cached_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_used_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(indexer_id, cache_key)
		)
	`)
	require.NoError(t, err)

	store := NewTorznabTorrentCacheStore(db)
	cleanup := func() { _ = sqlDB.Close() }

	return store, cleanup
}

func TestNewTorznabTorrentCacheStore(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer sqlDB.Close()

	db := newMockQuerier(sqlDB)
	store := NewTorznabTorrentCacheStore(db)

	require.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestTorznabTorrentCacheStore_Store(t *testing.T) {
	t.Parallel()

	t.Run("successful store", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()
		entry := &TorznabTorrentCacheEntry{
			IndexerID:   1,
			CacheKey:    "test-cache-key",
			GUID:        "test-guid-123",
			DownloadURL: "http://example.com/download/123",
			InfoHash:    "abc123def456",
			Title:       "Test Torrent",
			SizeBytes:   1024 * 1024 * 100,
			TorrentData: []byte("fake torrent data"),
		}

		err := store.Store(ctx, entry)
		require.NoError(t, err)
	})

	t.Run("nil entry error", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()
		err := store.Store(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "entry cannot be nil")
	})

	t.Run("invalid indexer id", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()
		entry := &TorznabTorrentCacheEntry{
			IndexerID:   0,
			CacheKey:    "test-key",
			TorrentData: []byte("data"),
		}

		err := store.Store(ctx, entry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "indexer id must be positive")
	})

	t.Run("empty cache key", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()
		entry := &TorznabTorrentCacheEntry{
			IndexerID:   1,
			CacheKey:    "",
			TorrentData: []byte("data"),
		}

		err := store.Store(ctx, entry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cache key required")
	})

	t.Run("empty torrent data", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()
		entry := &TorznabTorrentCacheEntry{
			IndexerID:   1,
			CacheKey:    "test-key",
			TorrentData: nil,
		}

		err := store.Store(ctx, entry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "torrent data required")
	})

	t.Run("upsert on conflict", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()

		// Store initial entry
		entry1 := &TorznabTorrentCacheEntry{
			IndexerID:   1,
			CacheKey:    "same-key",
			GUID:        "guid-1",
			Title:       "Original Title",
			TorrentData: []byte("original data"),
		}
		err := store.Store(ctx, entry1)
		require.NoError(t, err)

		// Store updated entry with same key
		entry2 := &TorznabTorrentCacheEntry{
			IndexerID:   1,
			CacheKey:    "same-key",
			GUID:        "guid-2",
			Title:       "Updated Title",
			TorrentData: []byte("updated data"),
		}
		err = store.Store(ctx, entry2)
		require.NoError(t, err)

		// Fetch should return updated data
		data, found, err := store.Fetch(ctx, 1, "same-key", 0)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, []byte("updated data"), data)
	})
}

func TestTorznabTorrentCacheStore_Fetch(t *testing.T) {
	t.Parallel()

	t.Run("cache hit", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()

		// Store entry
		entry := &TorznabTorrentCacheEntry{
			IndexerID:   1,
			CacheKey:    "test-key",
			TorrentData: []byte("cached torrent data"),
		}
		err := store.Store(ctx, entry)
		require.NoError(t, err)

		// Fetch
		data, found, err := store.Fetch(ctx, 1, "test-key", 0)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, []byte("cached torrent data"), data)
	})

	t.Run("cache miss", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()

		data, found, err := store.Fetch(ctx, 1, "nonexistent-key", 0)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, data)
	})

	t.Run("invalid parameters", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()

		// Invalid indexer ID
		_, _, err := store.Fetch(ctx, 0, "test-key", 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid cache lookup parameters")

		// Empty cache key
		_, _, err = store.Fetch(ctx, 1, "", 0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid cache lookup parameters")
	})

	t.Run("wrong indexer id returns miss", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()

		// Store for indexer 1
		entry := &TorznabTorrentCacheEntry{
			IndexerID:   1,
			CacheKey:    "test-key",
			TorrentData: []byte("data"),
		}
		err := store.Store(ctx, entry)
		require.NoError(t, err)

		// Fetch for indexer 2
		data, found, err := store.Fetch(ctx, 2, "test-key", 0)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, data)
	})

	t.Run("maxAge zero disables expiration", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()

		// Store entry
		entry := &TorznabTorrentCacheEntry{
			IndexerID:   1,
			CacheKey:    "test-key",
			TorrentData: []byte("data"),
		}
		err := store.Store(ctx, entry)
		require.NoError(t, err)

		// Fetch with maxAge 0 should always return data
		data, found, err := store.Fetch(ctx, 1, "test-key", 0)
		require.NoError(t, err)
		assert.True(t, found)
		assert.NotNil(t, data)
	})

	t.Run("negative maxAge disables expiration", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()

		// Store entry
		entry := &TorznabTorrentCacheEntry{
			IndexerID:   1,
			CacheKey:    "test-key",
			TorrentData: []byte("data"),
		}
		err := store.Store(ctx, entry)
		require.NoError(t, err)

		// Fetch with negative maxAge should always return data
		data, found, err := store.Fetch(ctx, 1, "test-key", -1*time.Hour)
		require.NoError(t, err)
		assert.True(t, found)
		assert.NotNil(t, data)
	})
}

func TestTorznabTorrentCacheStore_Cleanup(t *testing.T) {
	t.Parallel()

	t.Run("cleanup old entries", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()

		// Store entries
		for i := 1; i <= 3; i++ {
			entry := &TorznabTorrentCacheEntry{
				IndexerID:   1,
				CacheKey:    "key-" + string(rune('0'+i)),
				TorrentData: []byte("data"),
			}
			err := store.Store(ctx, entry)
			require.NoError(t, err)
		}

		// Cleanup with 0 duration should return 0 (no deletions)
		deleted, err := store.Cleanup(ctx, 0)
		require.NoError(t, err)
		assert.Equal(t, int64(0), deleted)

		// Cleanup with negative duration should return 0
		deleted, err = store.Cleanup(ctx, -1*time.Hour)
		require.NoError(t, err)
		assert.Equal(t, int64(0), deleted)
	})

	t.Run("cleanup returns count of deleted", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupTorznabTorrentCacheStore(t)
		defer cleanup()

		ctx := t.Context()

		// Store an entry
		entry := &TorznabTorrentCacheEntry{
			IndexerID:   1,
			CacheKey:    "old-key",
			TorrentData: []byte("old data"),
		}
		err := store.Store(ctx, entry)
		require.NoError(t, err)

		// Immediate cleanup won't delete anything since last_used_at is now
		deleted, err := store.Cleanup(ctx, 1*time.Second)
		require.NoError(t, err)
		// Entry was just stored, so it shouldn't be deleted
		assert.Equal(t, int64(0), deleted)
	})
}

func TestTorznabTorrentCacheEntry(t *testing.T) {
	t.Parallel()

	t.Run("struct fields", func(t *testing.T) {
		t.Parallel()

		entry := TorznabTorrentCacheEntry{
			IndexerID:   5,
			CacheKey:    "cache-key-123",
			GUID:        "guid-456",
			DownloadURL: "http://example.com/download",
			InfoHash:    "abcdef123456",
			Title:       "My Torrent",
			SizeBytes:   1073741824, // 1 GB
			TorrentData: []byte("torrent content"),
		}

		assert.Equal(t, 5, entry.IndexerID)
		assert.Equal(t, "cache-key-123", entry.CacheKey)
		assert.Equal(t, "guid-456", entry.GUID)
		assert.Equal(t, "http://example.com/download", entry.DownloadURL)
		assert.Equal(t, "abcdef123456", entry.InfoHash)
		assert.Equal(t, "My Torrent", entry.Title)
		assert.Equal(t, int64(1073741824), entry.SizeBytes)
		assert.Equal(t, []byte("torrent content"), entry.TorrentData)
	})
}
