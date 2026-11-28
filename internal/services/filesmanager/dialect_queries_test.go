// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestDialectQueriesSQLite(t *testing.T) {
	t.Parallel()
	runDialectQueryTests(t, testdb.NewMigratedSQLite)
}

func TestDialectQueriesPostgresIntegration(t *testing.T) {
	t.Parallel()
	runDialectQueryTests(t, testdb.NewMigratedPostgres)
}

func runDialectQueryTests(t *testing.T, open testDBOpener) {
	t.Helper()

	tests := []struct {
		name string
		run  func(context.Context, *testing.T, *Repository)
	}{
		{"cache stats read the age of the oldest and the newest sync", checkCacheStatsAges},
		{"removing gone torrents keeps the current ones", checkDeleteCacheForRemovedTorrents},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
				tt.run(ctx, t, NewRepository(db))
			})
		})
	}
}

func cacheTorrent(ctx context.Context, t *testing.T, repo *Repository, hash string, syncedAt time.Time) {
	t.Helper()
	file := CachedFile{InstanceID: 1, TorrentHash: hash, FileIndex: 0, Name: "Show.S01E01.1080p.WEB-DL-GRP.mkv", Size: 1 << 20, IsSeed: new(true)}
	info := SyncInfo{InstanceID: 1, TorrentHash: hash, LastSyncedAt: syncedAt, TorrentProgress: 1, FileCount: 1}
	require.NoError(t, repo.UpsertCache(ctx, []CachedFile{file}, []SyncInfo{info}))
}

func checkCacheStatsAges(ctx context.Context, t *testing.T, repo *Repository) {
	now := time.Now().UTC()
	cacheTorrent(ctx, t, repo, testHash("older"), now.Add(-2*time.Hour))
	cacheTorrent(ctx, t, repo, testHash("newer"), now.Add(-time.Hour))

	stats, err := repo.GetCacheStats(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 2, stats.CachedTorrents)
	require.NotNil(t, stats.OldestCacheAge)
	require.NotNil(t, stats.NewestCacheAge)
	require.NotNil(t, stats.AverageCacheAge)
	require.InDelta(t, (2 * time.Hour).Seconds(), stats.OldestCacheAge.Seconds(), time.Minute.Seconds())
	require.InDelta(t, time.Hour.Seconds(), stats.NewestCacheAge.Seconds(), time.Minute.Seconds())
	require.InDelta(t, (90 * time.Minute).Seconds(), stats.AverageCacheAge.Seconds(), time.Minute.Seconds())
}

func checkDeleteCacheForRemovedTorrents(ctx context.Context, t *testing.T, repo *Repository) {
	now := time.Now().UTC()
	cacheTorrent(ctx, t, repo, testHash("kept"), now)
	cacheTorrent(ctx, t, repo, testHash("gone"), now)

	removed, err := repo.DeleteCacheForRemovedTorrents(ctx, 1, []string{testHash("kept")})
	require.NoError(t, err)
	require.Equal(t, 1, removed)

	files, err := repo.GetFilesBatch(ctx, 1, []string{testHash("kept"), testHash("gone")})
	require.NoError(t, err)
	require.Len(t, files[testHash("kept")], 1)
	require.Empty(t, files[testHash("gone")])

	infos, err := repo.GetSyncInfoBatch(ctx, 1, []string{testHash("kept"), testHash("gone")})
	require.NoError(t, err)
	require.Contains(t, infos, testHash("kept"))
	require.NotContains(t, infos, testHash("gone"))
}
