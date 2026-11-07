// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/testdb"
)

func BenchmarkBatchReads(b *testing.B) {
	const readHashes = 800
	const filesPerTorrent = 3

	for _, cached := range []int{6000, 50000} {
		db := testdb.NewMigratedSQLite(b, "filesmanager-bench")
		ctx := b.Context()
		seedInstanceRow(ctx, b, db, 1)
		repo := NewRepository(db)

		hashes := make([]string, cached)
		files := make([]CachedFile, 0, cached*filesPerTorrent)
		infos := make([]SyncInfo, 0, cached)
		for i := range cached {
			hashes[i] = fmt.Sprintf("%040x", i)
			for idx := range filesPerTorrent {
				f := baseFile()
				f.TorrentHash = hashes[i]
				f.FileIndex = idx
				f.Name = fmt.Sprintf("Release.%d/file%d.mkv", i, idx)
				files = append(files, f)
			}
			infos = append(infos, SyncInfo{InstanceID: 1, TorrentHash: hashes[i], TorrentProgress: 1, FileCount: filesPerTorrent})
		}
		require.NoError(b, repo.UpsertFiles(ctx, files))
		require.NoError(b, repo.UpsertSyncInfoBatch(ctx, infos))

		read := hashes[:readHashes]

		b.Run(fmt.Sprintf("GetFilesBatch/cached=%d", cached), func(b *testing.B) {
			for b.Loop() {
				got, err := repo.GetFilesBatch(ctx, 1, read)
				if err != nil || len(got) != readHashes {
					b.Fatalf("got %d torrents, err %v", len(got), err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*readHashes), "ns/hash")
		})

		b.Run(fmt.Sprintf("GetSyncInfoBatch/cached=%d", cached), func(b *testing.B) {
			for b.Loop() {
				got, err := repo.GetSyncInfoBatch(ctx, 1, read)
				if err != nil || len(got) != readHashes {
					b.Fatalf("got %d torrents, err %v", len(got), err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*readHashes), "ns/hash")
		})
	}
}
