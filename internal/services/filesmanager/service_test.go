// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func setupFilesManagerDB(t *testing.T) (*database.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db := testdb.NewMigratedSQLite(t, "filesmanager-service")

	// Seed instance row to satisfy foreign key constraints for cache writes
	var (
		instanceNameID int64
		hostID         int64
		usernameID     int64
	)

	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-name").Scan(&instanceNameID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-host").Scan(&hostID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-username").Scan(&usernameID))

	_, err := db.ExecContext(ctx, "INSERT INTO instances (id, name_id, host_id, username_id, password_encrypted) VALUES (?, ?, ?, ?, ?)", 1, instanceNameID, hostID, usernameID, "enc")
	require.NoError(t, err)

	return db, ctx
}

func TestCacheFilesAndGetCachedFiles(t *testing.T) {
	t.Parallel()

	db, ctx := setupFilesManagerDB(t)
	svc := NewService(db)

	files := qbt.TorrentFiles{
		{
			Index:      0,
			Name:       "example.mkv",
			Size:       1 << 20,
			Progress:   0.5,
			Priority:   1,
			PieceRange: []int{0, 1},
		},
	}

	require.NoError(t, svc.CacheFiles(ctx, 1, "hash", files))

	cached, err := svc.GetCachedFiles(ctx, 1, "hash")
	require.NoError(t, err)
	require.NotNil(t, cached, "cache should be available")
	require.Len(t, cached, 1)
	require.Equal(t, "example.mkv", cached[0].Name)
}

func TestCacheFilesBatch_MaintainsHashAlignment(t *testing.T) {
	t.Parallel()

	db, ctx := setupFilesManagerDB(t)
	svc := NewService(db)

	hashes := []string{"hash-a", "hash-b", "hash-c"}
	names := map[string]string{
		"hash-a": "alpha.mkv",
		"hash-b": "bravo.mkv",
		"hash-c": "charlie.mkv",
	}

	for attempt := range 3 {
		// Reset cache tables to isolate each attempt.
		_, err := db.ExecContext(ctx, "DELETE FROM torrent_files_cache; DELETE FROM torrent_files_sync;")
		require.NoError(t, err)

		files := make(map[string]qbt.TorrentFiles, len(hashes))
		for _, hash := range hashes {
			files[hash] = qbt.TorrentFiles{
				{
					Index: 0,
					Name:  names[hash],
					Size:  int64(attempt + 1),
				},
			}
		}

		require.NoError(t, svc.CacheFilesBatch(ctx, 1, files))

		for _, hash := range hashes {
			cached, err := svc.GetCachedFiles(ctx, 1, hash)
			require.NoError(t, err)
			require.Len(t, cached, 1, "attempt %d hash %s", attempt, hash)
			require.Equalf(t, names[hash], cached[0].Name, "attempt %d hash %s", attempt, hash)
		}
	}
}

func TestCacheFilesBatchAcrossQueryBatches(t *testing.T) {
	t.Parallel()

	forEachBackend(t, func(ctx context.Context, t *testing.T, db *database.DB) {
		svc := NewService(db)
		hashes := make([]string, 801)
		files := make(map[string]qbt.TorrentFiles, len(hashes))
		for i := range hashes {
			hashes[i] = fmt.Sprintf("batch-hash-%d", i)
			files[hashes[i]] = qbt.TorrentFiles{{Name: fmt.Sprintf("example-%d.mkv", i), Size: int64(i + 1)}}
		}
		require.NoError(t, svc.CacheFilesBatch(ctx, 1, files))

		for _, empty := range [][]string{nil, {}, {"", " "}} {
			cached, err := svc.repo.GetFilesBatch(ctx, 1, empty)
			require.NoError(t, err)
			require.Empty(t, cached)
			info, err := svc.repo.GetSyncInfoBatch(ctx, 1, empty)
			require.NoError(t, err)
			require.Empty(t, info)
			require.NoError(t, svc.repo.DeleteTorrentCache(ctx, 1, empty))
		}

		cached, missing, err := svc.GetCachedFilesBatch(ctx, 1, append(hashes, hashes[0], " ", "absent"), 0)
		require.NoError(t, err)
		require.Equal(t, []string{"absent"}, missing)
		require.Len(t, cached, len(hashes))
		for _, hash := range hashes {
			require.Len(t, cached[hash], 1)
			require.Equal(t, files[hash][0].Name, cached[hash][0].Name)
			require.Equal(t, files[hash][0].Size, cached[hash][0].Size)
		}

		require.NoError(t, svc.CacheFiles(ctx, 1, "keep", qbt.TorrentFiles{{Name: "keep.mkv", Size: 1}}))
		require.NoError(t, svc.repo.DeleteTorrentCache(ctx, 1, hashes))
		cachedFiles, err := svc.repo.GetFilesBatch(ctx, 1, hashes)
		require.NoError(t, err)
		require.Empty(t, cachedFiles)
		info, err := svc.repo.GetSyncInfoBatch(ctx, 1, hashes)
		require.NoError(t, err)
		require.Empty(t, info)
		kept, err := svc.GetCachedFiles(ctx, 1, "keep")
		require.NoError(t, err)
		require.Len(t, kept, 1)
		require.Equal(t, "keep.mkv", kept[0].Name)
	})
}

func TestGetCachedFilesBatch_MaxAgeServesAgedRows(t *testing.T) {
	t.Parallel()

	db, ctx := setupFilesManagerDB(t)
	svc := NewService(db)

	files := qbt.TorrentFiles{{Name: "aged.mkv", Size: 1, Priority: 1}}
	require.NoError(t, svc.CacheFiles(ctx, 1, "aged", files))

	_, err := db.ExecContext(ctx, "UPDATE torrent_files_sync SET last_synced_at = ?", time.Now().Add(-time.Hour))
	require.NoError(t, err)

	cached, missing, err := svc.GetCachedFilesBatch(ctx, 1, []string{"aged", "absent"}, 0)
	require.NoError(t, err)
	require.Empty(t, cached)
	require.ElementsMatch(t, []string{"aged", "absent"}, missing)

	cached, missing, err = svc.GetCachedFilesBatch(ctx, 1, []string{"aged", "absent"}, 2*time.Hour)
	require.NoError(t, err)
	require.Len(t, cached, 1)
	require.Equal(t, "aged.mkv", cached["aged"][0].Name)
	require.Equal(t, []string{"absent"}, missing)
}
