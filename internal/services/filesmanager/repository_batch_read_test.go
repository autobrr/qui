// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	viewFilesBatchQuery = `
		SELECT id, instance_id, torrent_hash, file_index, name, size, progress,
		       priority, is_seed, piece_range_start, piece_range_end, availability, cached_at
		FROM torrent_files_cache_view
		WHERE instance_id = ? AND torrent_hash IN (%s)
		ORDER BY torrent_hash, file_index ASC
	`
	viewSyncInfoBatchQuery = `
		SELECT instance_id, torrent_hash, last_synced_at, torrent_progress, file_count
		FROM torrent_files_sync_view
		WHERE instance_id = ? AND torrent_hash IN (%s)
	`
)

func TestBatchReadsMatchViewsSQLite(t *testing.T) {
	t.Parallel()
	testBatchReadsMatchViews(t, testdb.NewMigratedSQLite)
}

func TestBatchReadsMatchViewsPostgresIntegration(t *testing.T) {
	t.Parallel()
	testBatchReadsMatchViews(t, testdb.NewMigratedPostgres)
}

func testBatchReadsMatchViews(t *testing.T, open testDBOpener) {
	withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
		repo := NewRepository(db)
		seedInstanceRow(ctx, t, db, 2)

		var files []CachedFile
		var infos []SyncInfo
		for instanceID := 1; instanceID <= 2; instanceID++ {
			for _, hash := range []string{"aaa111", "bbb222", "ccc333"} {
				for idx := range 3 {
					f := baseFile()
					f.InstanceID = instanceID
					f.TorrentHash = hash
					f.FileIndex = 2 - idx
					f.Name = fmt.Sprintf("%s/%d/file%d.mkv", hash, instanceID, idx)
					f.Size = int64(instanceID*100 + idx)
					files = append(files, f)
				}
				infos = append(infos, SyncInfo{InstanceID: instanceID, TorrentHash: hash, TorrentProgress: 1, FileCount: 3})
			}
		}
		other := baseFile()
		other.InstanceID = 2
		other.TorrentHash = "only-on-2"
		files = append(files, other)
		infos = append(infos, SyncInfo{InstanceID: 2, TorrentHash: "only-on-2", FileCount: 1})

		require.NoError(t, repo.UpsertFiles(ctx, files))
		require.NoError(t, repo.UpsertSyncInfoBatch(ctx, infos))

		input := []string{"ccc333", "aaa111", "missing", "only-on-2", "AAA111", "ccc333", "bbb222"}

		for instanceID := 1; instanceID <= 2; instanceID++ {
			args := make([]any, 0, 1+len(input))
			args = append(args, instanceID)
			for _, h := range input {
				args = append(args, h)
			}
			placeholders := buildPlaceholders(len(input))

			wantFiles := queryRows(ctx, t, db, fmt.Sprintf(viewFilesBatchQuery, placeholders), args)
			gotFiles := queryRows(ctx, t, db, fmt.Sprintf(filesBatchQuery, placeholders), args)
			require.NotEmpty(t, wantFiles)
			require.Equal(t, wantFiles, gotFiles, "files rows, instance %d", instanceID)

			wantSync := queryRows(ctx, t, db, fmt.Sprintf(viewSyncInfoBatchQuery, placeholders), args)
			gotSync := queryRows(ctx, t, db, fmt.Sprintf(syncInfoBatchQuery, placeholders), args)
			require.NotEmpty(t, wantSync)
			require.ElementsMatch(t, wantSync, gotSync, "sync rows, instance %d", instanceID)
		}
	})
}

func seedInstanceRow(ctx context.Context, tb testing.TB, db *database.DB, id int) {
	tb.Helper()

	ids := make([]int64, 3)
	for i, field := range []string{"name", "host", "username"} {
		value := fmt.Sprintf("instance-%d-%s", id, field)
		require.NoError(tb, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", value).Scan(&ids[i]))
	}

	_, err := db.ExecContext(ctx, "INSERT INTO instances (id, name_id, host_id, username_id, password_encrypted) VALUES (?, ?, ?, ?, ?)", id, ids[0], ids[1], ids[2], "enc")
	require.NoError(tb, err)
}

func queryRows(ctx context.Context, t *testing.T, db *database.DB, query string, args []any) [][]any {
	t.Helper()

	rows, err := db.QueryContext(ctx, query, args...)
	require.NoError(t, err)
	defer rows.Close()

	cols, err := rows.Columns()
	require.NoError(t, err)

	var out [][]any
	for rows.Next() {
		row := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range row {
			ptrs[i] = &row[i]
		}
		require.NoError(t, rows.Scan(ptrs...))
		out = append(out, row)
	}
	require.NoError(t, rows.Err())
	return out
}
