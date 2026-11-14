// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestFilesPrintCacheSQLite(t *testing.T) {
	t.Parallel()
	runFilesPrintCacheTests(t, testdb.NewMigratedSQLite)
}

func TestFilesPrintCachePostgresIntegration(t *testing.T) {
	t.Parallel()
	runFilesPrintCacheTests(t, testdb.NewMigratedPostgres)
}

// regression: #3945
func TestFilesPrintStaysValidAfterTimeFormMigration(t *testing.T) {
	t.Parallel()

	withTestDB(t, testdb.NewMigratedSQLite, func(ctx context.Context, t *testing.T, db *database.DB) {
		svc := NewService(db)
		require.NoError(t, svc.CacheFilesBatch(ctx, 1, pairWrite(printA)))

		// An older qui on a host one hour east of UTC stored both sync times in this form.
		const goForm = "2026-11-09 12:41:31.947225 +0100 CET"
		_, err := db.ExecContext(ctx, `UPDATE torrent_files_sync SET last_synced_at = ?, print_synced_at = ?`, goForm, goForm)
		require.NoError(t, err)
		testdb.RunSQLiteMigration(t, db, "113_store_times_in_sqlite_form.sql")

		entry, ok := readEntry(ctx, t, svc, pairHash)
		require.True(t, ok)
		require.NotNil(t, entry.Print, "the cached file list is still valid")
		require.Equal(t, printA, *entry.Print)
		require.True(t, entry.SyncedAt.Equal(time.Date(2026, 11, 9, 11, 41, 31, 947225000, time.UTC)), "synced at %s", entry.SyncedAt)
	})
}

func runFilesPrintCacheTests(t *testing.T, open testDBOpener) {
	t.Helper()

	tests := []struct {
		name string
		run  func(*testing.T, testDBOpener)
	}{
		{"PrintWithHighBitRoundTrips", testPrintWithHighBitRoundTrips},
		{"NilPrintReplacesPrint", testNilPrintReplacesPrint},
		{"FailedWriteKeepsOldPair", testFailedWriteKeepsOldPair},
		{"ConcurrentWritersKeepPairs", testConcurrentWritersKeepPairs},
		{"ReaderSeesOnePair", testReaderSeesOnePair},
		{"PreviousReleaseQueriesRun", testPreviousReleaseQueriesRun},
		{"PreviousReleaseInsertReadsAsNoPrint", testPreviousReleaseInsertReadsAsNoPrint},
		{"PreviousReleaseRewriteReadsAsNoPrint", testPreviousReleaseRewriteReadsAsNoPrint},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, open)
		})
	}
}

const pairHash = "pair-hash"

// Each print belongs to one list. printA has the high bit set, as half of all
// FNV-64a prints do once cast to int64.
const (
	printA int64 = -0x5a5a5a5a5a5a5a5b
	printB int64 = 0x1234
)

func pairList(tag string) qbt.TorrentFiles {
	files := make(qbt.TorrentFiles, 3)
	for i := range files {
		files[i] = qbt.TorrentFile{Index: i, Name: fmt.Sprintf("Show.S01.%s/e%d.mkv", tag, i), Size: int64(i + 1)}
	}
	return files
}

func pairWrite(p int64) map[string]qbittorrent.CachedFilesWrite {
	tag := "A"
	if p == printB {
		tag = "B"
	}
	return map[string]qbittorrent.CachedFilesWrite{pairHash: {Files: pairList(tag), Print: &p}}
}

// requireOnePair fails when the entry holds a print with the list of another write.
func requireOnePair(t *testing.T, entry qbittorrent.CachedFilesEntry) {
	t.Helper()
	require.NotNil(t, entry.Print)
	want := "A"
	if *entry.Print == printB {
		want = "B"
	}
	require.Len(t, entry.Files, 3)
	for _, f := range entry.Files {
		require.Containsf(t, f.Name, "."+want+"/", "print %d stored with a file of another list", *entry.Print)
	}
}

func readEntry(ctx context.Context, t *testing.T, svc *Service, hash string) (qbittorrent.CachedFilesEntry, bool) {
	t.Helper()
	entries, err := svc.GetCachedFilesBatchWithMeta(ctx, 1, []string{hash})
	require.NoError(t, err)
	entry, ok := entries[hash]
	return entry, ok
}

func testPrintWithHighBitRoundTrips(t *testing.T, open testDBOpener) {
	t.Parallel()

	withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
		svc := NewService(db)
		before := time.Now()
		require.NoError(t, svc.CacheFilesBatch(ctx, 1, pairWrite(printA)))

		entry, ok := readEntry(ctx, t, svc, pairHash)
		require.True(t, ok)
		require.NotNil(t, entry.Print, "a fresh insert stores print_synced_at equal to last_synced_at")
		require.Equal(t, printA, *entry.Print)
		require.WithinDuration(t, before, entry.SyncedAt, time.Minute)
		requireOnePair(t, entry)

		_, ok = readEntry(ctx, t, svc, "absent")
		require.False(t, ok)
	})
}

func testNilPrintReplacesPrint(t *testing.T, open testDBOpener) {
	t.Parallel()

	withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
		svc := NewService(db)
		require.NoError(t, svc.CacheFilesBatch(ctx, 1, pairWrite(printA)))
		require.NoError(t, svc.CacheFiles(ctx, 1, pairHash, qbittorrent.CachedFilesWrite{Files: pairList("A")}))

		entry, ok := readEntry(ctx, t, svc, pairHash)
		require.True(t, ok)
		require.Nil(t, entry.Print)
	})
}

// failingCommitDB rolls back, and fails the commit of, each transaction that
// wrote sync rows. A combined write fails after its file rows.
type failingCommitDB struct {
	*database.DB
}

func (d failingCommitDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := d.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &failingCommitTx{TxQuerier: tx}, nil
}

type failingCommitTx struct {
	dbinterface.TxQuerier
	wroteSync bool
}

func (t *failingCommitTx) DeferForeignKeyChecks(ctx context.Context) error {
	return dbinterface.DeferForeignKeyChecks(ctx, t.TxQuerier)
}

func (t *failingCommitTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "INSERT INTO torrent_files_sync") {
		t.wroteSync = true
	}
	return t.TxQuerier.ExecContext(ctx, query, args...)
}

func (t *failingCommitTx) Commit() error {
	if t.wroteSync {
		if err := t.Rollback(); err != nil {
			return err
		}
		return errors.New("commit failed")
	}
	return t.TxQuerier.Commit()
}

func testFailedWriteKeepsOldPair(t *testing.T, open testDBOpener) {
	t.Parallel()

	withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
		svc := NewService(db)
		require.NoError(t, svc.CacheFilesBatch(ctx, 1, pairWrite(printA)))

		failing := NewService(failingCommitDB{db})
		require.Error(t, failing.CacheFilesBatch(ctx, 1, pairWrite(printB)))

		entry, ok := readEntry(ctx, t, svc, pairHash)
		require.True(t, ok)
		require.Equal(t, printA, *entry.Print)
		requireOnePair(t, entry)
	})
}

// releaseTogether starts n calls at once and returns their errors.
func releaseTogether(n int, call func(i int) error) []error {
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			errs[i] = call(i)
		})
	}
	close(start)
	wg.Wait()
	return errs
}

func testConcurrentWritersKeepPairs(t *testing.T, open testDBOpener) {
	t.Parallel()

	withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
		svc := NewService(db)
		for round := range 30 {
			errs := releaseTogether(8, func(i int) error {
				if (i+round)%2 == 0 {
					return svc.CacheFilesBatch(ctx, 1, pairWrite(printA))
				}
				return svc.CacheFilesBatch(ctx, 1, pairWrite(printB))
			})
			require.NoError(t, errors.Join(errs...))

			entry, ok := readEntry(ctx, t, svc, pairHash)
			require.True(t, ok)
			requireOnePair(t, entry)
		}
	})
}

func testReaderSeesOnePair(t *testing.T, open testDBOpener) {
	t.Parallel()

	withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
		svc := NewService(db)
		require.NoError(t, svc.CacheFilesBatch(ctx, 1, pairWrite(printA)))

		const readers, writers, rounds = 8, 8, 30
		var mu sync.Mutex
		var reads []qbittorrent.CachedFilesEntry
		errs := releaseTogether(readers+writers, func(i int) error {
			for r := range rounds {
				if i >= readers {
					if err := cacheRetryingBusy(ctx, svc, pairWrite(printB)); err != nil {
						return err
					}
					if err := cacheRetryingBusy(ctx, svc, pairWrite(printA)); err != nil {
						return err
					}
					continue
				}
				entries, err := svc.GetCachedFilesBatchWithMeta(ctx, 1, []string{pairHash})
				if err != nil {
					return fmt.Errorf("read %d: %w", r, err)
				}
				mu.Lock()
				reads = append(reads, entries[pairHash])
				mu.Unlock()
			}
			return nil
		})
		require.NoError(t, errors.Join(errs...))

		require.Len(t, reads, readers*rounds)
		for _, entry := range reads {
			requireOnePair(t, entry)
		}
	})
}

// cacheRetryingBusy writes again when SQLite fails the write with
// SQLITE_BUSY. On the Windows CI runner, some writes in testReaderSeesOnePair
// failed so at once, while readers ran. qui logs such a failed write and
// fetches the list again, so the test checks only what readers see.
func cacheRetryingBusy(ctx context.Context, svc *Service, writes map[string]qbittorrent.CachedFilesWrite) error {
	var err error
	for range 20 {
		err = svc.CacheFilesBatch(ctx, 1, writes)
		sqliteErr, ok := errors.AsType[*sqlite.Error](err)
		if !ok || sqliteErr.Code()&0xff != sqlite3.SQLITE_BUSY {
			return err
		}
	}
	return err
}

// previousReleaseSyncUpsert is the sync row write of the release before the
// print columns.
const previousReleaseSyncUpsert = `
	INSERT INTO torrent_files_sync
	(instance_id, torrent_hash_id, last_synced_at, torrent_progress, file_count)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(instance_id, torrent_hash_id) DO UPDATE SET
		last_synced_at = excluded.last_synced_at,
		torrent_progress = excluded.torrent_progress,
		file_count = excluded.file_count
`

// previousReleaseWrite writes one torrent as the release before the print
// columns did.
func previousReleaseWrite(ctx context.Context, t *testing.T, db *database.DB, hash string, syncedAt time.Time) {
	t.Helper()
	repo := NewRepository(db)
	files := pairList("A")
	cached := make([]CachedFile, len(files))
	for i, f := range files {
		cached[i] = CachedFile{InstanceID: 1, TorrentHash: hash, FileIndex: f.Index, Name: f.Name, Size: f.Size, IsSeed: new(false)}
	}
	require.NoError(t, repo.UpsertCache(ctx, cached, nil))

	var hashID int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", hash).Scan(&hashID))
	_, err := db.ExecContext(ctx, previousReleaseSyncUpsert, 1, hashID, syncedAt, 0.0, len(files))
	require.NoError(t, err)
}

func testPreviousReleaseQueriesRun(t *testing.T, open testDBOpener) {
	t.Parallel()

	withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
		previousReleaseWrite(ctx, t, db, "old-hash", time.Now())

		repo := NewRepository(db)
		infos, err := repo.GetSyncInfoBatch(ctx, 1, []string{"old-hash"})
		require.NoError(t, err)
		require.Equal(t, 3, infos["old-hash"].FileCount)
		files, err := repo.GetFilesBatch(ctx, 1, []string{"old-hash"})
		require.NoError(t, err)
		require.Len(t, files["old-hash"], 3)
		_, err = repo.GetCacheStats(ctx, 1)
		require.NoError(t, err)
		require.NoError(t, repo.DeleteTorrentCache(ctx, 1, []string{"old-hash"}))
	})
}

func testPreviousReleaseInsertReadsAsNoPrint(t *testing.T, open testDBOpener) {
	t.Parallel()

	withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
		previousReleaseWrite(ctx, t, db, pairHash, time.Now())

		entry, ok := readEntry(ctx, t, NewService(db), pairHash)
		require.True(t, ok)
		require.Nil(t, entry.Print)
	})
}

func testPreviousReleaseRewriteReadsAsNoPrint(t *testing.T, open testDBOpener) {
	t.Parallel()

	withTestDB(t, open, func(ctx context.Context, t *testing.T, db *database.DB) {
		svc := NewService(db)
		require.NoError(t, svc.CacheFilesBatch(ctx, 1, pairWrite(printA)))
		previousReleaseWrite(ctx, t, db, pairHash, time.Now().Add(time.Second))

		var stored sql.NullInt64
		require.NoError(t, db.QueryRowContext(ctx, "SELECT maindata_print FROM torrent_files_sync").Scan(&stored))
		require.True(t, stored.Valid, "the previous release keeps the print column")

		entry, ok := readEntry(ctx, t, svc, pairHash)
		require.True(t, ok)
		require.Nil(t, entry.Print)
	})
}

// The lock order of the cache writers, on Postgres, where row locks can
// deadlock (SQLSTATE 40P01). SQLite has one writer.
func TestFilesCacheLockOrderPostgresIntegration(t *testing.T) {
	t.Parallel()

	twoHashes := func(p int64) map[string]qbittorrent.CachedFilesWrite {
		return map[string]qbittorrent.CachedFilesWrite{
			"lock-hash-1": {Files: pairList(strconv.FormatInt(p, 10)), Print: &p},
			"lock-hash-2": {Files: pairList(strconv.FormatInt(p, 10)), Print: &p},
		}
	}

	// Each case runs calls 0..n-1 released together.
	tests := []struct {
		name          string
		n             int
		deletesTarget bool
		call          func(ctx context.Context, db *database.DB, svc *Service, i int) error
	}{
		{"WritersAndInvalidations", 16, false, func(ctx context.Context, _ *database.DB, svc *Service, i int) error {
			if i%2 == 0 {
				return svc.InvalidateCache(ctx, 1, "lock-hash-1")
			}
			return svc.CacheFilesBatch(ctx, 1, twoHashes(int64(i)))
		}},
		{"WritersAndOrphanCleanup", 9, false, func(ctx context.Context, _ *database.DB, svc *Service, i int) error {
			if i == 0 {
				_, err := svc.CleanupRemovedTorrentsCache(ctx, 1, []string{"other-hash"})
				return err
			}
			return svc.CacheFilesBatch(ctx, 1, twoHashes(int64(i)))
		}},
		{"WritersInvalidationsAndInstanceDelete", 17, true, func(ctx context.Context, db *database.DB, svc *Service, i int) error {
			switch {
			case i == 0:
				store, err := models.NewInstanceStore(db, make([]byte, 32))
				if err != nil {
					return err
				}
				return store.Delete(ctx, 1)
			case i%2 == 0:
				return svc.InvalidateCache(ctx, 1, "lock-hash-1")
			default:
				return svc.CacheFilesBatch(ctx, 1, twoHashes(int64(i)))
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			withTestDB(t, testdb.NewMigratedPostgres, func(ctx context.Context, t *testing.T, db *database.DB) {
				svc := NewService(db)
				// Intern lock-hash-1 before lock-hash-2, so it has the lower id.
				require.NoError(t, svc.CacheFilesBatch(ctx, 1, twoHashes(0)))
				for round := range 5 {
					// Write the sync rows against hash order, so a delete that
					// scans the table meets them in the reverse of the writers' order.
					_, err := svc.CleanupRemovedTorrentsCache(ctx, 1, []string{"other-hash"})
					require.NoError(t, err)
					for _, hash := range []string{"lock-hash-2", "lock-hash-1"} {
						require.NoError(t, svc.CacheFiles(ctx, 1, hash, twoHashes(0)[hash]))
					}
					errs := releaseTogether(tt.n, func(i int) error {
						return tt.call(ctx, db, svc, i)
					})
					for _, err := range errs {
						if err != nil {
							require.NotContainsf(t, err.Error(), "40P01", "round %d", round)
						}
					}
					// Writes after the delete fail on the missing instance.
					if tt.deletesTarget {
						return
					}
					require.NoError(t, errors.Join(errs...), "round %d", round)
				}
			})
		})
	}
}
