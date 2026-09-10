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

type testDBOpener func(testing.TB, string) *database.DB

func TestFilesmanagerSQLite(t *testing.T) {
	t.Parallel()
	runFilesmanagerTests(t, testdb.NewMigratedSQLite)
}

func TestFilesmanagerPostgresIntegration(t *testing.T) {
	t.Parallel()
	runFilesmanagerTests(t, testdb.NewMigratedPostgres)
}

func runFilesmanagerTests(t *testing.T, open testDBOpener) {
	t.Helper()

	tests := []struct {
		name string
		run  func(*testing.T, testDBOpener)
	}{
		{"UpsertFilesSkipsUnchangedRows", testUpsertFilesSkipsUnchangedRows},
		{"UpsertFilesWritesChangedRows", testUpsertFilesWritesChangedRows},
		{"UpsertFilesGuardIsNullSafe", testUpsertFilesGuardIsNullSafe},
		{"UpsertFilesGuardIsPerRowAcrossBatches", testUpsertFilesGuardIsPerRowAcrossBatches},
		{"CacheFilesBatchAcrossQueryBatches", testCacheFilesBatchAcrossQueryBatches},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, open)
		})
	}
}

func withTestDB(t *testing.T, open testDBOpener, run func(context.Context, *testing.T, *database.DB)) {
	t.Helper()

	db := open(t, "filesmanager")
	ctx := t.Context()
	seedInstance(ctx, t, db)
	run(ctx, t, db)
}

// seedInstance creates the instance row the cache rows point at.
func seedInstance(ctx context.Context, t *testing.T, db *database.DB) {
	t.Helper()

	var nameID, hostID, usernameID int64
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-name").Scan(&nameID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-host").Scan(&hostID))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES (?) RETURNING id", "instance-username").Scan(&usernameID))

	_, err := db.ExecContext(ctx, "INSERT INTO instances (id, name_id, host_id, username_id, password_encrypted) VALUES (?, ?, ?, ?, ?)", 1, nameID, hostID, usernameID, "enc")
	require.NoError(t, err)
}

// sentinel is far enough in the past that a real write can never land on it.
var sentinel = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// markCachedAt stamps every cached file row with the sentinel so a later upsert
// that actually writes is visible: the row moves off the sentinel.
func markCachedAt(ctx context.Context, t *testing.T, db *database.DB) {
	t.Helper()
	_, err := db.ExecContext(ctx, `UPDATE torrent_files_cache SET cached_at = ?`, sentinel)
	require.NoError(t, err)
}

// countAtSentinel reports how many rows still carry the sentinel, i.e. how many
// rows the last upsert left untouched.
func countAtSentinel(ctx context.Context, t *testing.T, db *database.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM torrent_files_cache WHERE cached_at = ?`, sentinel).Scan(&n))
	return n
}

func baseFile() CachedFile {
	return CachedFile{
		InstanceID:      1,
		TorrentHash:     "guard-hash",
		FileIndex:       0,
		Name:            "season.pack/episode.mkv",
		Size:            1 << 30,
		Progress:        1.0,
		Priority:        1,
		IsSeed:          new(true),
		PieceRangeStart: 0,
		PieceRangeEnd:   511,
		Availability:    1.0,
	}
}

// A complete, seeding torrent re-synced with identical data must produce no row
// writes at all. This is the case that dominates a steady-state library.
func testUpsertFilesSkipsUnchangedRows(t *testing.T, open testDBOpener) {
	t.Parallel()

	withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
		repo := NewRepository(db)

		files := []CachedFile{baseFile()}
		require.NoError(t, repo.UpsertFiles(ctx, files))

		markCachedAt(ctx, t, db)
		require.NoError(t, repo.UpsertFiles(ctx, files))
		require.Equal(t, 1, countAtSentinel(ctx, t, db), "unchanged row should not have been rewritten")
	})
}

// Each guarded column must still let a real change through on its own.
func testUpsertFilesWritesChangedRows(t *testing.T, open testDBOpener) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*CachedFile)
	}{
		{"name", func(f *CachedFile) { f.Name = "season.pack/episode.renamed.mkv" }},
		{"size", func(f *CachedFile) { f.Size = 1<<30 + 1 }},
		{"progress", func(f *CachedFile) { f.Progress = 0.5 }},
		{"priority", func(f *CachedFile) { f.Priority = 7 }},
		{"is_seed", func(f *CachedFile) { f.IsSeed = new(false) }},
		{"piece_range_start", func(f *CachedFile) { f.PieceRangeStart = 1 }},
		{"piece_range_end", func(f *CachedFile) { f.PieceRangeEnd = 512 }},
		{"availability", func(f *CachedFile) { f.Availability = 0.25 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
				repo := NewRepository(db)

				require.NoError(t, repo.UpsertFiles(ctx, []CachedFile{baseFile()}))
				markCachedAt(ctx, t, db)

				changed := baseFile()
				tt.mutate(&changed)
				require.NoError(t, repo.UpsertFiles(ctx, []CachedFile{changed}))

				require.Zero(t, countAtSentinel(ctx, t, db), "changed %s should have been written", tt.name)

				batch, err := repo.GetFilesBatch(ctx, 1, []string{"guard-hash"})
				require.NoError(t, err)
				stored := batch["guard-hash"]
				require.Len(t, stored, 1)
				require.Equal(t, changed.Name, stored[0].Name)
				require.Equal(t, changed.Size, stored[0].Size)
				require.InDelta(t, changed.Progress, stored[0].Progress, 1e-9)
				require.Equal(t, changed.Priority, stored[0].Priority)
				require.Equal(t, changed.IsSeed, stored[0].IsSeed)
				require.Equal(t, changed.PieceRangeStart, stored[0].PieceRangeStart)
				require.Equal(t, changed.PieceRangeEnd, stored[0].PieceRangeEnd)
				require.InDelta(t, changed.Availability, stored[0].Availability, 1e-9)
			})
		})
	}
}

// is_seed is nullable, so the guard must be null-safe. A plain `<>` would yield
// NULL for these comparisons and silently drop the change.
func testUpsertFilesGuardIsNullSafe(t *testing.T, open testDBOpener) {
	t.Parallel()

	t.Run("null stays null", func(t *testing.T) {
		t.Parallel()

		withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
			repo := NewRepository(db)

			nullSeed := baseFile()
			nullSeed.IsSeed = nil
			require.NoError(t, repo.UpsertFiles(ctx, []CachedFile{nullSeed}))

			markCachedAt(ctx, t, db)
			require.NoError(t, repo.UpsertFiles(ctx, []CachedFile{nullSeed}))
			require.Equal(t, 1, countAtSentinel(ctx, t, db), "null-to-null should not have been rewritten")
		})
	})

	t.Run("null to value", func(t *testing.T) {
		t.Parallel()

		withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
			repo := NewRepository(db)

			nullSeed := baseFile()
			nullSeed.IsSeed = nil
			require.NoError(t, repo.UpsertFiles(ctx, []CachedFile{nullSeed}))
			markCachedAt(ctx, t, db)

			require.NoError(t, repo.UpsertFiles(ctx, []CachedFile{baseFile()}))
			require.Zero(t, countAtSentinel(ctx, t, db), "null-to-value should have been written")

			batch, err := repo.GetFilesBatch(ctx, 1, []string{"guard-hash"})
			require.NoError(t, err)
			stored := batch["guard-hash"]
			require.Len(t, stored, 1)
			require.Equal(t, new(true), stored[0].IsSeed)
		})
	})

	t.Run("value to null", func(t *testing.T) {
		t.Parallel()

		withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
			repo := NewRepository(db)

			require.NoError(t, repo.UpsertFiles(ctx, []CachedFile{baseFile()}))
			markCachedAt(ctx, t, db)

			nullSeed := baseFile()
			nullSeed.IsSeed = nil
			require.NoError(t, repo.UpsertFiles(ctx, []CachedFile{nullSeed}))
			require.Zero(t, countAtSentinel(ctx, t, db), "value-to-null should have been written")

			batch, err := repo.GetFilesBatch(ctx, 1, []string{"guard-hash"})
			require.NoError(t, err)
			stored := batch["guard-hash"]
			require.Len(t, stored, 1)
			require.Nil(t, stored[0].IsSeed)
		})
	})
}

// A sync sweep touches many torrents at once; only the rows that actually moved
// should be written, and only within the batch that contains them.
func testUpsertFilesGuardIsPerRowAcrossBatches(t *testing.T, open testDBOpener) {
	t.Parallel()

	withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
		repo := NewRepository(db)

		// Span more than one batch so the guard is exercised on a full-batch query
		// and on the partial trailing one.
		const total = fileBatchSize*2 + 5
		files := make([]CachedFile, total)
		for i := range files {
			f := baseFile()
			f.FileIndex = i
			files[i] = f
		}
		require.NoError(t, repo.UpsertFiles(ctx, files))

		markCachedAt(ctx, t, db)

		// Move one file in each batch: first, one past the batch boundary, and last.
		changed := make([]CachedFile, total)
		copy(changed, files)
		for _, i := range []int{0, fileBatchSize, total - 1} {
			changed[i].Progress = 0.5
		}
		require.NoError(t, repo.UpsertFiles(ctx, changed))

		require.Equal(t, total-3, countAtSentinel(ctx, t, db), "only the three changed rows should have been written")
	})
}
