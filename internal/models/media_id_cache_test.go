// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestMediaIDCacheStore(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "media-id-cache")
	store := models.NewMediaIDCacheStore(db)

	t.Run("missing row returns nil without error", func(t *testing.T) {
		entry, err := store.Get(ctx, "v1:missing", "movie")
		require.NoError(t, err)
		require.Nil(t, entry)
	})

	t.Run("positive entry round-trips", func(t *testing.T) {
		require.NoError(t, store.Set(ctx, &models.MediaIDCacheEntry{
			TorrentKey: "v1:abc", ContentType: "movie",
			ExtractorVersion: 1, IDType: "imdb", IDValue: "tt0111161",
		}))
		entry, err := store.Get(ctx, "v1:abc", "movie")
		require.NoError(t, err)
		require.NotNil(t, entry)
		require.Equal(t, "imdb", entry.IDType)
		require.Equal(t, "tt0111161", entry.IDValue)
		require.Equal(t, 1, entry.ExtractorVersion)
		require.False(t, entry.CachedAt.IsZero())
	})

	t.Run("negative entry stores NULL ids and reads back empty", func(t *testing.T) {
		require.NoError(t, store.Set(ctx, &models.MediaIDCacheEntry{
			TorrentKey: "v1:neg", ContentType: "tv", ExtractorVersion: 1,
		}))
		entry, err := store.Get(ctx, "v1:neg", "tv")
		require.NoError(t, err)
		require.NotNil(t, entry)
		require.Empty(t, entry.IDType)
		require.Empty(t, entry.IDValue)
	})

	t.Run("upsert replaces the row including a downgrade to negative", func(t *testing.T) {
		require.NoError(t, store.Set(ctx, &models.MediaIDCacheEntry{
			TorrentKey: "v1:abc", ContentType: "movie", ExtractorVersion: 2,
		}))
		entry, err := store.Get(ctx, "v1:abc", "movie")
		require.NoError(t, err)
		require.NotNil(t, entry)
		require.Equal(t, 2, entry.ExtractorVersion)
		require.Empty(t, entry.IDType)
	})

	t.Run("content types are separate rows", func(t *testing.T) {
		require.NoError(t, store.Set(ctx, &models.MediaIDCacheEntry{
			TorrentKey: "v1:both", ContentType: "movie", ExtractorVersion: 1, IDType: "imdb", IDValue: "tt1",
		}))
		require.NoError(t, store.Set(ctx, &models.MediaIDCacheEntry{
			TorrentKey: "v1:both", ContentType: "tv", ExtractorVersion: 1, IDType: "tvdb", IDValue: "42",
		}))
		movie, err := store.Get(ctx, "v1:both", "movie")
		require.NoError(t, err)
		require.Equal(t, "imdb", movie.IDType)
		tv, err := store.Get(ctx, "v1:both", "tv")
		require.NoError(t, err)
		require.Equal(t, "tvdb", tv.IDType)
	})
}
