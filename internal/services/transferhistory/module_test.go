// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/transferhistory"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const instanceID = 1

var t0 = time.Date(2026, 11, 10, 10, 0, 30, 0, time.UTC)

func at(d time.Duration) time.Time { return t0.Add(d) }

// torrent returns a private, incomplete torrent on tracker.example in the
// category tv, added at t0.
func torrent(hash string, uploaded, downloaded int64) transferhistory.Torrent {
	return transferhistory.Torrent{
		Hash:           hash,
		Name:           "Some.Show.S01E01.1080p.WEB-DL-GRP",
		Category:       "tv",
		TrackerDomains: []string{"tracker.example"},
		Private:        new(true),
		Size:           1000,
		Uploaded:       uploaded,
		Downloaded:     downloaded,
		AddedAt:        t0,
	}
}

func complete(t transferhistory.Torrent) transferhistory.Torrent {
	t.Complete = true
	return t
}

func addedAt(t transferhistory.Torrent, added time.Time) transferhistory.Torrent {
	t.AddedAt = added
	return t
}

func inCategory(t transferhistory.Torrent, category string) transferhistory.Torrent {
	t.Category = category
	return t
}

func onTracker(t transferhistory.Torrent, domains ...string) transferhistory.Torrent {
	t.TrackerDomains = domains
	return t
}

func public(t transferhistory.Torrent) transferhistory.Torrent {
	t.Private = new(false)
	return t
}

// step is a reading, a check, or a restart of the module.
type step struct {
	reading *transferhistory.Reading
	check   *check
	restart bool
}

type check struct {
	query transferhistory.TransferQuery
	want  []transferhistory.TransferRow
}

func reading(time time.Time, torrents ...transferhistory.Torrent) step {
	return step{reading: &transferhistory.Reading{InstanceID: instanceID, Time: time, Torrents: torrents}}
}

func readingAfterFailure(time, failedSince time.Time, torrents ...transferhistory.Torrent) step {
	s := reading(time, torrents...)
	s.reading.FailedSince = failedSince
	return s
}

func expect(query transferhistory.TransferQuery, want ...transferhistory.TransferRow) step {
	query.InstanceID = instanceID
	return step{check: &check{query: query, want: want}}
}

var restart = step{restart: true}

// The hour of t0, by tracker, as one total.
var hourTotal = transferhistory.TransferQuery{From: time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC), Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker}

func total(group string, uploaded, downloaded, crossSeedUploaded int64) transferhistory.TransferRow {
	return transferhistory.TransferRow{
		Group: group, Start: hourTotal.From, Width: transferhistory.BucketTotal,
		Uploaded: uploaded, Downloaded: downloaded, StatisticsCrossSeedUploaded: crossSeedUploaded,
	}
}

func runSteps(t *testing.T, db *database.DB, steps []step) {
	t.Helper()
	customizations := models.NewTrackerCustomizationStore(db)
	module := transferhistory.New(db, customizations)
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
	for i, s := range steps {
		switch {
		case s.reading != nil:
			module.Record(*s.reading)
		case s.restart:
			require.NoError(t, module.Shutdown(t.Context()))
			module = transferhistory.New(db, customizations)
		case s.check != nil:
			rows, err := module.Transfer(t.Context(), s.check.query)
			require.NoError(t, err, "step %d", i)
			if len(s.check.want) == 0 {
				require.Empty(t, rows, "step %d", i)
			} else {
				require.Equal(t, s.check.want, rows, "step %d", i)
			}
			for _, row := range rows {
				require.LessOrEqual(t, row.StatisticsCrossSeedUploaded, row.Uploaded, "step %d: %s", i, row.Group)
				if row.Baseline != nil {
					require.LessOrEqual(t, row.Baseline.StatisticsCrossSeedUploaded, row.Baseline.Uploaded, "step %d: %s", i, row.Group)
				}
			}
		}
	}
}

func engines(t *testing.T, run func(t *testing.T, db func(t *testing.T) *database.DB)) {
	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		run(t, func(t *testing.T) *database.DB { return testdb.NewMigratedSQLite(t, "transfer-history") })
	})
	t.Run("postgres", func(t *testing.T) {
		run(t, func(t *testing.T) *database.DB { return testdb.NewMigratedPostgres(t, "transfer-history") })
	})
}

func TestTransferHistoryRecording(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		steps []step
	}{
		{
			name: "a torrent added after the first recording counts all its bytes",
			steps: []step{
				reading(at(0), torrent("a", 100, 100)),
				reading(at(5*time.Minute), torrent("a", 100, 100), torrent("b", 500, 1000)),
				expect(hourTotal, total("tracker.example", 500, 1000, 0)),
			},
		},
		{
			name: "a torrent at the first recording puts its bytes into the Baseline",
			steps: []step{
				reading(at(0), torrent("a", 100, 200)),
				reading(at(5*time.Minute), torrent("a", 150, 200)),
				expect(transferhistory.TransferQuery{To: hourTotal.To, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{
						Group: "tracker.example", Width: transferhistory.BucketTotal,
						Uploaded: 50,
						Baseline: &transferhistory.Bytes{Uploaded: 100, Downloaded: 200},
					}),
			},
		},
		{
			name: "a torrent that qBittorrent had before the last reading but left out of it goes into the Baseline",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 8000, 100), at(-30*24*time.Hour))),
				expect(hourTotal),
				expect(transferhistory.TransferQuery{To: hourTotal.To, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{
						Group: "tracker.example", Width: transferhistory.BucketTotal,
						Baseline: &transferhistory.Bytes{Uploaded: 8000, Downloaded: 100},
					}),
			},
		},
		{
			name: "an all-time total holds the Recording gap of a period with no transfer",
			steps: []step{
				reading(at(0), torrent("a", 100, 0)),
				restart,
				reading(at(3*time.Hour), torrent("a", 100, 0)),
				expect(transferhistory.TransferQuery{To: at(4 * time.Hour), Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{
						Group: "tracker.example", Width: transferhistory.BucketTotal,
						GapSeconds: int64(3 * time.Hour / time.Second),
						Baseline:   &transferhistory.Bytes{Uploaded: 100},
					}),
			},
		},
		{
			name: "a period that ends a fraction of a second into an hour includes that hour",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 100, 0)),
				expect(transferhistory.TransferQuery{From: hourTotal.From.Add(-time.Hour), To: hourTotal.From.Add(time.Millisecond), Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{Group: "tracker.example", Start: hourTotal.From.Add(-time.Hour), Width: transferhistory.BucketTotal, Uploaded: 100}),
			},
		},
		{
			name: "a lower reading starts a new count and adds nothing negative",
			steps: []step{
				reading(at(0), torrent("a", 100, 0)),
				reading(at(5*time.Minute), torrent("a", 300, 0)),
				reading(at(10*time.Minute), torrent("a", 50, 0)),
				reading(at(15*time.Minute), torrent("a", 80, 0)),
				expect(hourTotal, total("tracker.example", 230, 0, 0)),
			},
		},
		{
			name: "the changes reach the database 5 minutes after the last write",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(4*time.Minute), torrent("a", 100, 0)),
				expect(hourTotal),
				reading(at(5*time.Minute), torrent("a", 150, 0)),
				expect(hourTotal, total("tracker.example", 150, 0, 0)),
			},
		},
		{
			name: "a restart of qui loses no bytes and stores the Recording gap",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 100, 0)),
				reading(at(7*time.Minute), torrent("a", 120, 0)),
				restart,
				reading(at(3*time.Hour), torrent("a", 400, 0)),
				expect(transferhistory.TransferQuery{From: hourTotal.From, To: at(4 * time.Hour), Bucket: transferhistory.BucketHour, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{Group: "tracker.example", Start: hourTotal.From, Width: transferhistory.BucketHour, Uploaded: 120},
					transferhistory.TransferRow{Group: "tracker.example", Start: hourTotal.From.Add(3 * time.Hour), Width: transferhistory.BucketHour, Uploaded: 280, GapSeconds: int64((3*time.Hour - 7*time.Minute) / time.Second)}),
			},
		},
		{
			name: "failed reads store the Recording gap from the first failure",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 100, 0)),
				readingAfterFailure(at(40*time.Minute), at(10*time.Minute), torrent("a", 400, 0)),
				expect(transferhistory.TransferQuery{From: hourTotal.From, To: hourTotal.To, Bucket: transferhistory.BucketHour, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{Group: "tracker.example", Start: hourTotal.From, Width: transferhistory.BucketHour, Uploaded: 400, GapSeconds: int64(30 * time.Minute / time.Second)}),
			},
		},
		{
			name: "a torrent that moves to another category keeps its old bytes under the old category",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 100, 0)),
				reading(at(10*time.Minute), inCategory(torrent("a", 250, 0), "movies")),
				expect(transferhistory.TransferQuery{From: hourTotal.From, To: hourTotal.To, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByCategory},
					transferhistory.TransferRow{Group: "movies", Start: hourTotal.From, Width: transferhistory.BucketTotal, Uploaded: 150},
					transferhistory.TransferRow{Group: "tv", Start: hourTotal.From, Width: transferhistory.BucketTotal, Uploaded: 100}),
			},
		},
		{
			name: "a query for categories by hour returns day rows",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 100, 0)),
				expect(transferhistory.TransferQuery{From: hourTotal.From, To: hourTotal.To, Bucket: transferhistory.BucketHour, GroupBy: transferhistory.GroupByCategory},
					transferhistory.TransferRow{Group: "tv", Start: time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), Width: transferhistory.BucketDay, Uploaded: 100}),
			},
		},
		{
			name: "a Statistics cross-seed and a torrent without the flag upload to one tracker in one hour",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				// x completes with 0.5% of its size downloaded, y with 50%.
				reading(at(5*time.Minute), torrent("a", 0, 0), complete(torrent("x", 0, 5)), complete(torrent("y", 0, 500))),
				reading(at(10*time.Minute), torrent("a", 0, 0), complete(torrent("x", 200, 5)), complete(torrent("y", 300, 500))),
				expect(transferhistory.TransferQuery{From: hourTotal.From, To: hourTotal.To, Bucket: transferhistory.BucketHour, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{Group: "tracker.example", Start: hourTotal.From, Width: transferhistory.BucketHour, Uploaded: 500, Downloaded: 505, StatisticsCrossSeedUploaded: 200}),
			},
		},
		{
			name: "only the upload after the reading that shows the completion is upload from Statistics cross-seeds",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), torrent("x", 0, 5)),
				reading(at(10*time.Minute), torrent("a", 0, 0), complete(torrent("x", 100, 5))),
				reading(at(15*time.Minute), torrent("a", 0, 0), complete(torrent("x", 160, 5))),
				expect(hourTotal, total("tracker.example", 160, 5, 60)),
			},
		},
		{
			name: "the Baseline of a torrent with the flag counts as upload from Statistics cross-seeds",
			steps: []step{
				reading(at(0), complete(torrent("x", 400, 0)), complete(torrent("y", 100, 1000))),
				expect(transferhistory.TransferQuery{To: hourTotal.To, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{
						Group: "tracker.example", Width: transferhistory.BucketTotal,
						Baseline: &transferhistory.Bytes{Uploaded: 500, Downloaded: 1000, StatisticsCrossSeedUploaded: 400},
					}),
			},
		},
		{
			name: "the tracker rows of a period add up to the period total",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute),
					torrent("a", 10, 1),
					onTracker(torrent("b", 20, 2), "b.example", "a.example"),
					onTracker(torrent("c", 40, 4), "a.example"),
					public(onTracker(torrent("d", 80, 8), "open.example")),
					onTracker(torrent("e", 160, 16))),
				expect(hourTotal,
					total("No tracker", 160, 16, 0),
					total("Public", 80, 8, 0),
					total("a.example", 60, 6, 0),
					total("tracker.example", 10, 1, 0)),
			},
		},
	}

	engines(t, func(t *testing.T, db func(t *testing.T) *database.DB) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				runSteps(t, db(t), tc.steps)
			})
		}
	})
}

// blockingWrites holds each write transaction until release closes.
type blockingWrites struct {
	*database.DB
	writing chan struct{}
	release chan struct{}
}

func (b *blockingWrites) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	select {
	case b.writing <- struct{}{}:
	default:
	}
	<-b.release
	return b.DB.BeginTx(ctx, opts)
}

func TestADroppedReadingKeepsItsFailure(t *testing.T) {
	t.Parallel()
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	blocked := &blockingWrites{DB: db, writing: make(chan struct{}, 1), release: make(chan struct{})}
	module := transferhistory.New(blocked, models.NewTrackerCustomizationStore(db))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

	module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
	<-blocked.writing
	// Five readings wait while the worker writes, so the oldest one, which
	// follows the failed reads, drops out of the queue.
	module.Record(*readingAfterFailure(at(30*time.Minute), at(time.Minute), torrent("a", 0, 0)).reading)
	for i := range 4 {
		module.Record(*reading(at(time.Duration(31+i)*time.Minute), torrent("a", 100, 0)).reading)
	}
	close(blocked.release)

	rows, err := module.Transfer(t.Context(), transferhistory.TransferQuery{InstanceID: instanceID, From: hourTotal.From, To: hourTotal.To, Bucket: transferhistory.BucketHour, GroupBy: transferhistory.GroupByTracker})
	require.NoError(t, err)
	require.Equal(t, []transferhistory.TransferRow{{
		Group: "tracker.example", Start: hourTotal.From, Width: transferhistory.BucketHour,
		GapSeconds: int64(30 * time.Minute / time.Second), Uploaded: 100,
	}}, rows)
}

func TestRecordDoesNotWaitForTheDatabase(t *testing.T) {
	t.Parallel()
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	blocked := &blockingWrites{DB: db, writing: make(chan struct{}, 1), release: make(chan struct{})}
	module := transferhistory.New(blocked, models.NewTrackerCustomizationStore(db))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

	module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
	<-blocked.writing
	// The worker waits in the first write. A Record that waited for it would
	// never return.
	module.Record(*reading(at(5*time.Minute), torrent("a", 100, 0)).reading)
	module.Record(*reading(at(10*time.Minute), torrent("a", 250, 0)).reading)
	close(blocked.release)

	rows, err := module.Transfer(t.Context(), transferhistory.TransferQuery{InstanceID: instanceID, From: hourTotal.From, To: hourTotal.To, Bucket: hourTotal.Bucket, GroupBy: hourTotal.GroupBy})
	require.NoError(t, err)
	require.Equal(t, []transferhistory.TransferRow{total("tracker.example", 250, 0, 0)}, rows)
}

func TestTrackerRenameChangesThePast(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		customizations := models.NewTrackerCustomizationStore(db)
		custom, err := customizations.Create(t.Context(), &models.TrackerCustomization{DisplayName: "Old name", Domains: []string{"tracker.example"}})
		require.NoError(t, err)
		query := hourTotal
		query.InstanceID = instanceID

		module := transferhistory.New(db, customizations)
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
		module.Record(*reading(at(5*time.Minute), torrent("a", 100, 0)).reading)
		rows, err := module.Transfer(t.Context(), query)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{total("Old name", 100, 0, 0)}, rows)

		custom.DisplayName = "New name"
		_, err = customizations.Update(t.Context(), custom)
		require.NoError(t, err)
		rows, err = module.Transfer(t.Context(), query)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{total("New name", 100, 0, 0)}, rows)
	})
}

func TestStatusKeepsTheNameOfARemovedInstance(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
		require.NoError(t, err)
		recorded, err := instances.Create(t.Context(), "Seedbox", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)
		_, err = instances.Create(t.Context(), "Idle", "http://localhost:8081", "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)

		module := transferhistory.New(db, models.NewTrackerCustomizationStore(db))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		module.Record(transferhistory.Reading{InstanceID: recorded.ID, Time: at(0), Torrents: []transferhistory.Torrent{torrent("a", 0, 0)}})
		_, err = module.Status(t.Context())
		require.NoError(t, err)
		require.NoError(t, instances.Delete(t.Context(), recorded.ID))
		module.Record(transferhistory.Reading{InstanceID: recorded.ID, Time: at(5 * time.Minute), Torrents: []transferhistory.Torrent{torrent("a", 10, 0)}})

		status, err := module.Status(t.Context())
		require.NoError(t, err)
		require.Equal(t, []transferhistory.InstanceStatus{{InstanceID: recorded.ID, Name: "Seedbox", FirstRecordedAt: t0}}, status)
	})
}

// lostCommitAnswer commits the second write transaction but reports an error,
// as when the connection drops before the answer of the commit.
type lostCommitAnswer struct {
	*database.DB
	writes int
}

type lostAnswerTx struct {
	dbinterface.TxQuerier
	lose bool
}

func (tx *lostAnswerTx) Commit() error {
	if err := tx.TxQuerier.Commit(); err != nil {
		return err
	}
	if tx.lose {
		return errors.New("connection reset")
	}
	return nil
}

func (l *lostCommitAnswer) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := l.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	l.writes++
	return &lostAnswerTx{TxQuerier: tx, lose: l.writes == 2}, nil
}

func TestACommitWithALostAnswerCountsOnce(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		module := transferhistory.New(&lostCommitAnswer{DB: db}, models.NewTrackerCustomizationStore(db))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

		module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
		module.Record(*reading(at(5*time.Minute), torrent("a", 100, 0)).reading)
		module.Record(*reading(at(10*time.Minute), torrent("a", 150, 0)).reading)

		query := hourTotal
		query.InstanceID = instanceID
		rows, err := module.Transfer(t.Context(), query)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{total("tracker.example", 150, 0, 0)}, rows)
	})
}

// noWrites refuses each write transaction.
type noWrites struct{ *database.DB }

func (noWrites) BeginTx(context.Context, *sql.TxOptions) (dbinterface.TxQuerier, error) {
	return nil, errors.New("disk I/O error")
}

func TestShutdownReportsAFailedWrite(t *testing.T) {
	t.Parallel()
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	module := transferhistory.New(noWrites{db}, models.NewTrackerCustomizationStore(db))
	module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
	require.ErrorContains(t, module.Shutdown(context.Background()), "disk I/O error")
}
