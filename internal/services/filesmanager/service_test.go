// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
)

func setupFilesManagerDB(t *testing.T) (*database.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	// Seed instance row to satisfy foreign key constraints for cache writes
	var (
		instanceNameID int64
		hostID         int64
		usernameID     int64
	)

	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-name").Scan(&instanceNameID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-host").Scan(&hostID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-username").Scan(&usernameID))

	_, err = db.ExecContext(ctx, "INSERT INTO instances (id, name_id, host_id, username_id, password_encrypted) VALUES (?, ?, ?, ?, ?)", 1, instanceNameID, hostID, usernameID, "enc")
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
