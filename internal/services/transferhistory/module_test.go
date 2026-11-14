// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory_test

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/systemnotice"
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

// step is a reading, a delete note, new settings, a check, or a restart or a
// crash of the module.
type step struct {
	reading  *transferhistory.Reading
	note     *transferhistory.DeleteNote
	settings *transferhistory.Settings
	// foldsDone waits until each fold that the module started ends.
	foldsDone bool
	check     *check
	deletes   *deletesCheck
	library   *libraryCheck
	// historyDelete deletes history, and instances checks the instances of the status query.
	historyDelete *transferhistory.HistoryDelete
	instances     *[]transferhistory.InstanceStatus
	restart       bool
	crash         bool
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

func withGroup(query transferhistory.TransferQuery, group string) transferhistory.TransferQuery {
	query.Group = &group
	return query
}

var (
	restart = step{restart: true}
	// crash drops the module without a write, as when qui crashes.
	crash = step{crash: true}
)

// The hour of t0, by tracker, as one total.
var hourTotal = transferhistory.TransferQuery{From: time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC), Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker}

// The UTC day of t0.
var day10 = time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

func total(group string, uploaded, downloaded, crossSeedUploaded int64) transferhistory.TransferRow {
	return transferhistory.TransferRow{
		Group: group, Start: hourTotal.From, Width: transferhistory.BucketTotal,
		Uploaded: uploaded, Downloaded: downloaded, StatisticsCrossSeedUploaded: crossSeedUploaded,
	}
}

func runSteps(t *testing.T, db *database.DB, steps []step) {
	t.Helper()
	customizations := models.NewTrackerCustomizationStore(db)
	module := transferhistory.New(db, customizations, systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
	for i, s := range steps {
		switch {
		case s.reading != nil:
			module.Record(*s.reading)
			// At most 4 readings wait for the module, so each step waits
			// until the module processed its reading.
			_, err := module.Status(t.Context())
			require.NoError(t, err, "step %d", i)
		case s.note != nil:
			module.NoteDelete(*s.note)
		case s.settings != nil:
			require.NoError(t, module.SetSettings(t.Context(), *s.settings), "step %d", i)
		case s.foldsDone:
			transferhistory.WaitForFolds(module)
		case s.deletes != nil:
			answer, err := module.Deletes(t.Context(), s.deletes.query)
			require.NoError(t, err, "step %d", i)
			require.Equal(t, s.deletes.want, answer, "step %d", i)
		case s.library != nil:
			rows, err := module.Library(t.Context(), s.library.query)
			require.NoError(t, err, "step %d", i)
			if s.library.eventsOnly {
				rows = eventCounts(rows)
			}
			if len(s.library.want) == 0 {
				require.Empty(t, rows, "step %d", i)
			} else {
				require.Equal(t, s.library.want, rows, "step %d", i)
			}
		case s.historyDelete != nil:
			require.NoError(t, module.DeleteHistory(t.Context(), *s.historyDelete), "step %d", i)
		case s.instances != nil:
			status, err := module.Status(t.Context())
			require.NoError(t, err, "step %d", i)
			require.Equal(t, *s.instances, status.Instances, "step %d", i)
		case s.restart:
			require.NoError(t, module.Shutdown(t.Context()))
			module = transferhistory.New(db, customizations, systemnotice.New(nil))
		case s.crash:
			// A query waits until the module processed each reading.
			_, err := module.Status(t.Context())
			require.NoError(t, err)
			module = transferhistory.New(db, customizations, systemnotice.New(nil))
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

// engines runs a test on SQLite and on Postgres. The CI Postgres job runs
// only tests whose names contain PostgresIntegration, so each caller has it.
func engines(t *testing.T, run func(t *testing.T, db func(t *testing.T) *database.DB)) {
	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		run(t, func(t *testing.T) *database.DB { return testdb.NewMigratedSQLite(t, "transfer-history") })
	})
	t.Run("postgres", func(t *testing.T) {
		run(t, func(t *testing.T) *database.DB { return testdb.NewMigratedPostgres(t, "transfer-history") })
	})
}

func TestTransferHistoryRecordingPostgresIntegration(t *testing.T) {
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
			name: "a crash of qui loses no bytes",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 100, 0)),
				reading(at(8*time.Minute), torrent("a", 150, 0)),
				crash,
				reading(at(12*time.Minute), torrent("a", 300, 0)),
				expect(hourTotal, transferhistory.TransferRow{
					Group: "tracker.example", Start: hourTotal.From, Width: transferhistory.BucketTotal,
					Uploaded: 300, GapSeconds: int64(7 * time.Minute / time.Second),
				}),
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
					transferhistory.TransferRow{Group: "movies", Start: day10, Width: transferhistory.BucketTotal, Uploaded: 150},
					transferhistory.TransferRow{Group: "tv", Start: day10, Width: transferhistory.BucketTotal, Uploaded: 100}),
			},
		},
		{
			name: "a torrent that moves keeps its Baseline under the category and tracker of its first recording",
			steps: []step{
				reading(at(0), torrent("a", 100, 0)),
				restart,
				reading(at(4*time.Minute), onTracker(inCategory(torrent("a", 150, 0), "movies"), "other.example")),
				expect(transferhistory.TransferQuery{To: hourTotal.To, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByCategory},
					transferhistory.TransferRow{Group: "movies", Width: transferhistory.BucketTotal, Uploaded: 50},
					transferhistory.TransferRow{Group: "tv", Width: transferhistory.BucketTotal, Baseline: &transferhistory.Bytes{Uploaded: 100}}),
				expect(transferhistory.TransferQuery{To: hourTotal.To, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{Group: "other.example", Width: transferhistory.BucketTotal, Uploaded: 50},
					transferhistory.TransferRow{Group: "tracker.example", Width: transferhistory.BucketTotal, Baseline: &transferhistory.Bytes{Uploaded: 100}}),
			},
		},
		{
			name: "a total starts at the first stored bucket that it covers",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 100, 0)),
				expect(transferhistory.TransferQuery{From: hourTotal.From.Add(30 * time.Minute), To: hourTotal.To, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker},
					total("tracker.example", 100, 0, 0)),
				expect(transferhistory.TransferQuery{From: hourTotal.From.Add(30 * time.Minute), To: hourTotal.To, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByCategory},
					transferhistory.TransferRow{Group: "tv", Start: day10, Width: transferhistory.BucketTotal, Uploaded: 100}),
			},
		},
		{
			name: "a query for categories by hour returns day rows",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 100, 0)),
				expect(transferhistory.TransferQuery{From: hourTotal.From, To: hourTotal.To, Bucket: transferhistory.BucketHour, GroupBy: transferhistory.GroupByCategory},
					transferhistory.TransferRow{Group: "tv", Start: day10, Width: transferhistory.BucketDay, Uploaded: 100}),
			},
		},
		{
			name: "a Statistics cross-seed and a torrent without the flag upload to one tracker in one hour",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				// x completes with 0.5% of its size downloaded, y with exactly 1%.
				reading(at(5*time.Minute), torrent("a", 0, 0), complete(torrent("x", 0, 5)), complete(torrent("y", 0, 10))),
				reading(at(10*time.Minute), torrent("a", 0, 0), complete(torrent("x", 200, 5)), complete(torrent("y", 300, 10))),
				expect(transferhistory.TransferQuery{From: hourTotal.From, To: hourTotal.To, Bucket: transferhistory.BucketHour, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{Group: "tracker.example", Start: hourTotal.From, Width: transferhistory.BucketHour, Uploaded: 500, Downloaded: 15, StatisticsCrossSeedUploaded: 200}),
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
		{
			name: "one group limits the answer to the bytes of that group",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute),
					torrent("a", 10, 1),
					onTracker(torrent("b", 20, 2), "b.example", "a.example"),
					inCategory(onTracker(torrent("c", 40, 4), "a.example"), "")),
				expect(withGroup(transferhistory.TransferQuery{From: hourTotal.From, To: hourTotal.To, Bucket: transferhistory.BucketHour, GroupBy: transferhistory.GroupByTracker}, "a.example"),
					transferhistory.TransferRow{Group: "a.example", Start: hourTotal.From, Width: transferhistory.BucketHour, Uploaded: 60, Downloaded: 6}),
				expect(withGroup(transferhistory.TransferQuery{From: hourTotal.From, To: hourTotal.To, Bucket: transferhistory.BucketDay, GroupBy: transferhistory.GroupByCategory}, ""),
					transferhistory.TransferRow{Group: "", Start: day10, Width: transferhistory.BucketDay, Uploaded: 40, Downloaded: 4}),
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
	module := transferhistory.New(blocked, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
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
	module := transferhistory.New(blocked, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
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

func TestTrackerRenameChangesThePastPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		customizations := models.NewTrackerCustomizationStore(db)
		custom, err := customizations.Create(t.Context(), &models.TrackerCustomization{DisplayName: "Old name", Domains: []string{"tracker.example"}})
		require.NoError(t, err)
		query := hourTotal
		query.InstanceID = instanceID

		module := transferhistory.New(db, customizations, systemnotice.New(nil))
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

func TestStatusKeepsTheNameOfARemovedInstancePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
		require.NoError(t, err)
		recorded, err := instances.Create(t.Context(), "Seedbox", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)
		_, err = instances.Create(t.Context(), "Idle", "http://localhost:8081", "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)

		module := transferhistory.New(db, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		module.Record(transferhistory.Reading{InstanceID: recorded.ID, Time: at(0), Torrents: []transferhistory.Torrent{torrent("a", 0, 0)}})
		_, err = module.Status(t.Context())
		require.NoError(t, err)
		require.NoError(t, instances.Delete(t.Context(), recorded.ID))
		module.Record(transferhistory.Reading{InstanceID: recorded.ID, Time: at(5 * time.Minute), Torrents: []transferhistory.Torrent{torrent("a", 10, 0)}})

		status, err := module.Status(t.Context())
		require.NoError(t, err)
		require.Equal(t, []transferhistory.InstanceStatus{{InstanceID: recorded.ID, Name: "Seedbox", FirstRecordedAt: t0}}, status.Instances)
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

func TestACommitWithALostAnswerCountsOncePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		module := transferhistory.New(&lostCommitAnswer{DB: db}, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
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
	module := transferhistory.New(noWrites{db}, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
	module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
	require.ErrorContains(t, module.Shutdown(context.Background()), "disk I/O error")
}

// failingWrites refuses each write transaction while fail is set.
type failingWrites struct {
	*database.DB
	fail atomic.Bool
}

func (f *failingWrites) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	if f.fail.Load() {
		return nil, errors.New("attempt to write a readonly database")
	}
	return f.DB.BeginTx(ctx, opts)
}

// sentNotices counts the notifications that the System notice module sends.
type sentNotices struct{ count atomic.Int32 }

func (s *sentNotices) Notify(context.Context, notifications.Event) { s.count.Add(1) }

func TestAFailedWriteOpensRecordingStoppedUntilAWriteSucceeds(t *testing.T) {
	t.Parallel()
	db := &failingWrites{DB: testdb.NewMigratedSQLite(t, "transfer-history")}
	sent := &sentNotices{}
	notices := systemnotice.New(sent)
	module := transferhistory.New(db, models.NewTrackerCustomizationStore(db.DB), notices)
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

	db.fail.Store(true)
	module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
	module.Record(*reading(at(5*time.Minute), torrent("a", 100, 0)).reading)
	_, err := module.Status(t.Context())
	require.NoError(t, err)

	open := notices.List()
	require.Len(t, open, 1)
	require.Equal(t, systemnotice.RecordingStopped, open[0].Kind)
	require.Contains(t, open[0].Cause, "attempt to write a readonly database")
	require.EqualValues(t, 1, sent.count.Load(), "two failed writes send one notification")

	db.fail.Store(false)
	module.Record(*reading(at(10*time.Minute), torrent("a", 150, 0)).reading)
	query := hourTotal
	query.InstanceID = instanceID
	rows, err := module.Transfer(t.Context(), query)
	require.NoError(t, err)
	require.Equal(t, []transferhistory.TransferRow{total("tracker.example", 150, 0, 0)}, rows)
	require.Empty(t, notices.List())
}

func TestRecordingStoppedStaysOpenWhileOneInstanceCannotWrite(t *testing.T) {
	t.Parallel()
	db := &failingWrites{DB: testdb.NewMigratedSQLite(t, "transfer-history")}
	notices := systemnotice.New(nil)
	module := transferhistory.New(db, models.NewTrackerCustomizationStore(db.DB), notices)
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

	db.fail.Store(true)
	module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
	_, err := module.Status(t.Context())
	require.NoError(t, err)

	db.fail.Store(false)
	other := *reading(at(time.Minute), torrent("b", 0, 0)).reading
	other.InstanceID = instanceID + 1
	module.Record(other)
	_, err = module.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, notices.List(), 1, "the changes of the first instance are not stored yet")

	module.Record(*reading(at(5*time.Minute), torrent("a", 100, 0)).reading)
	_, err = module.Status(t.Context())
	require.NoError(t, err)
	require.Empty(t, notices.List())
}

func TestRecordingStoppedClosesWhenAnInstanceThatIsNoLongerReadCanWrite(t *testing.T) {
	t.Parallel()
	db := &failingWrites{DB: testdb.NewMigratedSQLite(t, "transfer-history")}
	notices := systemnotice.New(nil)
	module := transferhistory.New(db, models.NewTrackerCustomizationStore(db.DB), notices)
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

	gone := *reading(at(0), torrent("a", 100, 0)).reading
	gone.InstanceID = instanceID + 1
	db.fail.Store(true)
	module.Record(gone)
	_, err := module.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, notices.List(), 1)

	// The failed instance gets no more readings, for example because the
	// user removed it. The other instance writes 5 minutes later.
	db.fail.Store(false)
	module.Record(*reading(at(5*time.Minute), torrent("b", 0, 0)).reading)
	_, err = module.Status(t.Context())
	require.NoError(t, err)
	require.Empty(t, notices.List())

	status, err := module.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, status.Instances, 2, "the changes of the failed instance are stored")
}

func TestRecordingStoppedClosesWhenAnInstanceWithAnUnknownWriteIsNoLongerRead(t *testing.T) {
	t.Parallel()
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	notices := systemnotice.New(nil)
	// The third write loses the answer of its commit and of the check after
	// it. It holds transfer of instance 2, so the module drops the changes of
	// instance 2 from memory.
	module := transferhistory.New(&failedCommit{DB: db, fail: 3, failCheck: true}, models.NewTrackerCustomizationStore(db), notices)
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

	module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
	for _, r := range []step{reading(at(time.Minute), torrent("b", 0, 0)), reading(at(6*time.Minute), torrent("b", 100, 0))} {
		gone := *r.reading
		gone.InstanceID = instanceID + 1
		module.Record(gone)
	}
	_, err := module.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, notices.List(), 1)

	// The next write of instance 1 succeeds.
	module.Record(*reading(at(5*time.Minute), torrent("a", 0, 0)).reading)
	_, err = module.Status(t.Context())
	require.NoError(t, err)
	require.Empty(t, notices.List())
}

// lostCommitAndCheck loses the answer of the second commit, as
// lostCommitAnswer does, and also fails the read that follows it.
type lostCommitAndCheck struct {
	lostCommitAnswer
	checks int
}

func (l *lostCommitAndCheck) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, "SELECT writes") && l.writes == 2 && l.checks == 0 {
		l.checks++
		return l.DB.QueryRowContext(ctx, "SELECT no_such_column FROM transfer_history_instances")
	}
	return l.DB.QueryRowContext(ctx, query, args...)
}

func TestACommitWithALostAnswerAndAFailedCheckCountsOncePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		module := transferhistory.New(&lostCommitAndCheck{DB: db}, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

		module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
		module.Record(*reading(at(5*time.Minute), torrent("a", 100, 0)).reading)
		// Readings come each minute, so the reload stores no Recording gap.
		module.Record(*reading(at(6*time.Minute), torrent("a", 150, 0)).reading)

		query := hourTotal
		query.InstanceID = instanceID
		rows, err := module.Transfer(t.Context(), query)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{total("tracker.example", 150, 0, 0)}, rows)
	})
}

// failedCommit rolls back the write transaction with the number fail at its
// commit, and reports an error. With failCheck, the read that follows fails
// too.
type failedCommit struct {
	*database.DB
	fail      int
	failCheck bool
	writes    int
	checks    int
}

type failedCommitTx struct {
	dbinterface.TxQuerier
	fail bool
}

func (tx *failedCommitTx) Commit() error {
	if tx.fail {
		_ = tx.Rollback()
		return errors.New("disk I/O error")
	}
	return tx.TxQuerier.Commit()
}

func (f *failedCommit) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := f.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	f.writes++
	return &failedCommitTx{TxQuerier: tx, fail: f.writes == f.fail}, nil
}

func (f *failedCommit) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if f.failCheck && strings.Contains(query, "SELECT writes") && f.writes == f.fail && f.checks == 0 {
		f.checks++
		return f.DB.QueryRowContext(ctx, "SELECT no_such_column FROM transfer_history_instances")
	}
	return f.DB.QueryRowContext(ctx, query, args...)
}

func TestAFailedCommitInTheSecondOfTheLastWriteKeepsItsChangesPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		customizations := models.NewTrackerCustomizationStore(db)
		first := transferhistory.New(db, customizations, systemnotice.New(nil))
		first.Record(*reading(at(0), torrent("a", 0, 0)).reading)
		require.NoError(t, first.Shutdown(t.Context()))

		module := transferhistory.New(&failedCommit{DB: db, fail: 1}, customizations, systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		// The first write after a load is due at once, in the second of the
		// last stored write.
		module.Record(*reading(at(500*time.Millisecond), torrent("a", 100, 0)).reading)
		module.Record(*reading(at(6*time.Minute), torrent("a", 150, 0)).reading)

		query := hourTotal
		query.InstanceID = instanceID
		rows, err := module.Transfer(t.Context(), query)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{total("tracker.example", 150, 0, 0)}, rows)
	})
}

func TestAFailedCommitAndAFailedCheckStoreNoRecordingGapPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		module := transferhistory.New(&failedCommit{DB: db, fail: 2, failCheck: true}, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

		module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
		module.Record(*reading(at(5*time.Minute), torrent("a", 100, 0)).reading)
		module.Record(*reading(at(6*time.Minute), torrent("a", 150, 0)).reading)

		query := hourTotal
		query.InstanceID = instanceID
		rows, err := module.Transfer(t.Context(), query)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{total("tracker.example", 150, 0, 0)}, rows)
	})
}

func TestAReloadKeepsTheTransferOfATorrentAddedBeforeItPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		module := transferhistory.New(&failedCommit{DB: db, fail: 2, failCheck: true}, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

		added := addedAt(torrent("b", 500, 0), at(30*time.Minute))
		module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
		// qui first sees b in this reading, whose write has an unknown outcome.
		module.Record(*reading(at(2*time.Hour), torrent("a", 0, 0), added).reading)
		module.Record(*reading(at(2*time.Hour+time.Minute), torrent("a", 0, 0), added).reading)

		rows, err := module.Transfer(t.Context(), transferhistory.TransferQuery{
			InstanceID: instanceID, To: at(3 * time.Hour), Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker,
		})
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{{Group: "tracker.example", Width: transferhistory.BucketTotal, Uploaded: 500}}, rows)
	})
}

// lateCommit commits the second write but reports an error, as
// lostCommitAnswer does. The check that follows still reads the count from
// before that commit, as when the commit lands after the check.
type lateCommit struct {
	lostCommitAnswer
	checks int
}

func (l *lateCommit) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, "SELECT writes") && l.writes == 2 && l.checks == 0 {
		l.checks++
		return l.DB.QueryRowContext(ctx, "SELECT writes - 1 FROM transfer_history_instances WHERE instance_id = ?", args...)
	}
	return l.DB.QueryRowContext(ctx, query, args...)
}

func TestACommitThatLandsAfterTheCheckCountsOncePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		module := transferhistory.New(&lateCommit{DB: db}, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

		module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
		module.Record(*reading(at(5*time.Minute), torrent("a", 100, 0)).reading)
		// The write of this reading has the number of the write that landed.
		module.Record(*reading(at(10*time.Minute), torrent("a", 150, 0)).reading)
		module.Record(*reading(at(11*time.Minute), torrent("a", 180, 0)).reading)

		query := hourTotal
		query.InstanceID = instanceID
		rows, err := module.Transfer(t.Context(), query)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{total("tracker.example", 180, 0, 0)}, rows)
	})
}

// slowWrites refuses each write transaction whose time limit is under need,
// as a write that takes need does, until one write is stored. limits holds
// the time limit of each write, in seconds.
type slowWrites struct {
	*database.DB
	need   time.Duration
	stored bool
	limits []int
}

func (s *slowWrites) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	deadline, _ := ctx.Deadline()
	limit := time.Until(deadline)
	s.limits = append(s.limits, int(limit.Round(time.Second)/time.Second))
	if !s.stored && limit < s.need {
		return nil, context.DeadlineExceeded
	}
	s.stored = true
	return s.DB.BeginTx(ctx, opts)
}

func TestAWriteThatPassesItsTimeLimitGetsMoreTimePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		slow := &slowWrites{DB: db, need: 590 * time.Second}
		module := transferhistory.New(slow, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

		// A write is due with each reading, 5 minutes apart.
		for k := range 7 {
			module.Record(*reading(at(time.Duration(k)*5*time.Minute), torrent("a", int64(100+10*k), 0)).reading)
			// A query waits until the module processed the reading.
			_, err := module.Status(t.Context())
			require.NoError(t, err)
		}

		rows, err := module.Transfer(t.Context(), transferhistory.TransferQuery{
			InstanceID: instanceID, To: hourTotal.To, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker,
		})
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{{
			Group: "tracker.example", Width: transferhistory.BucketTotal,
			Uploaded: 60, Baseline: &transferhistory.Bytes{Uploaded: 100},
		}}, rows)
		// The limit doubles up to 10 minutes, and a stored write sets it back.
		require.Equal(t, []int{30, 60, 120, 240, 480, 600, 30}, slow.limits)
	})
}

type localDay struct {
	start    time.Time
	uploaded int64
}

// recordDays records readings of the torrent a, which qui first sees at the
// first reading, and returns the days of the queries.
func recordDays(t *testing.T, db *database.DB, uploads []transferhistory.Reading, queries []transferhistory.TransferQuery) [][]localDay {
	t.Helper()
	module := transferhistory.New(db, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
	for _, r := range uploads {
		r.InstanceID = instanceID
		module.Record(r)
		_, err := module.Status(t.Context())
		require.NoError(t, err)
	}
	answers := make([][]localDay, 0, len(queries))
	for _, query := range queries {
		query.InstanceID, query.Bucket, query.GroupBy = instanceID, transferhistory.BucketDay, transferhistory.GroupByTracker
		rows, err := module.Transfer(t.Context(), query)
		require.NoError(t, err)
		days := make([]localDay, 0, len(rows))
		for _, row := range rows {
			require.Equal(t, transferhistory.BucketDay, row.Width)
			days = append(days, localDay{row.Start, row.Uploaded})
		}
		answers = append(answers, days)
	}
	return answers
}

// uploads returns a reading of the torrent a at each time, with the upload
// counter at each value.
func uploads(first time.Time, readings map[time.Time]int64) []transferhistory.Reading {
	a := func(uploaded int64) transferhistory.Torrent { return addedAt(torrent("a", uploaded, 0), first) }
	times := slices.SortedFunc(maps.Keys(readings), time.Time.Compare)
	out := make([]transferhistory.Reading, 0, 1+len(times))
	out = append(out, transferhistory.Reading{Time: first, Torrents: []transferhistory.Torrent{a(0)}})
	for _, at := range times {
		out = append(out, transferhistory.Reading{Time: at, Torrents: []transferhistory.Torrent{a(readings[at])}})
	}
	return out
}

func TestDayBucketsStartAtTheMidnightsOfTheTimeZonePostgresIntegration(t *testing.T) {
	t.Parallel()

	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	// Daylight saving time in New York ends on 2026-11-01 at 06:00 UTC.
	readings := uploads(time.Date(2026, 10, 31, 12, 0, 30, 0, time.UTC), map[time.Time]int64{
		time.Date(2026, 11, 1, 3, 30, 30, 0, time.UTC): 100,  // Oct 31, 23:30 EDT
		time.Date(2026, 11, 1, 4, 30, 30, 0, time.UTC): 300,  // Nov 1, 00:30 EDT
		time.Date(2026, 11, 2, 4, 30, 30, 0, time.UTC): 700,  // Nov 1, 23:30 EST
		time.Date(2026, 11, 2, 5, 30, 30, 0, time.UTC): 1500, // Nov 2, 00:30 EST
	})
	to := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	newYorkDays := []localDay{
		{time.Date(2026, 10, 31, 4, 0, 0, 0, time.UTC), 100},
		{time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC), 600},
		{time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC), 800},
	}

	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		got := recordDays(t, newDB(t), readings, []transferhistory.TransferQuery{
			{From: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), To: to},
			{From: time.Date(2026, 10, 31, 4, 0, 0, 0, time.UTC), To: to, Location: newYork},
			{To: to, Location: newYork},
		})
		require.Equal(t, [][]localDay{
			{
				{time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), 300},
				{time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), 1200},
			},
			newYorkDays,
			newYorkDays,
		}, got, "UTC days, New York days, and New York days of a period with no start")
	})
}

func TestADayWithoutAMidnightStartsAtTheChangeToSummerTimePostgresIntegration(t *testing.T) {
	t.Parallel()

	santiago, err := time.LoadLocation("America/Santiago")
	require.NoError(t, err)
	// Summer time in Santiago starts on 2026-09-06 at 04:00 UTC, so that day has no midnight.
	readings := uploads(time.Date(2026, 9, 5, 12, 0, 30, 0, time.UTC), map[time.Time]int64{
		time.Date(2026, 9, 6, 3, 30, 30, 0, time.UTC): 100, // Sep 5, 23:30 -04
		time.Date(2026, 9, 6, 4, 30, 30, 0, time.UTC): 300, // Sep 6, 01:30 -03
	})

	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		got := recordDays(t, newDB(t), readings, []transferhistory.TransferQuery{
			{From: time.Date(2026, 9, 5, 4, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC), Location: santiago},
		})
		require.Equal(t, [][]localDay{{
			{time.Date(2026, 9, 5, 4, 0, 0, 0, time.UTC), 100},
			{time.Date(2026, 9, 6, 4, 0, 0, 0, time.UTC), 200},
		}}, got)
	})
}

// regression: #4084
func TestDayBucketsPastTheZoneDataPostgresIntegration(t *testing.T) {
	t.Parallel()

	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	// Past the zone data, Go gives New York a zone period that ends where it starts, on 2040-12-31.
	readings := uploads(time.Date(2040, 12, 30, 12, 0, 30, 0, time.UTC), map[time.Time]int64{
		time.Date(2040, 12, 31, 12, 0, 30, 0, time.UTC): 100,
		time.Date(2041, 1, 1, 12, 0, 30, 0, time.UTC):   300,
	})

	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		got := recordDays(t, newDB(t), readings, []transferhistory.TransferQuery{
			{From: time.Date(2040, 12, 1, 5, 0, 0, 0, time.UTC), To: time.Date(2041, 2, 1, 5, 0, 0, 0, time.UTC), Location: newYork},
		})
		require.Equal(t, [][]localDay{{
			{time.Date(2040, 12, 31, 5, 0, 0, 0, time.UTC), 100},
			{time.Date(2041, 1, 1, 5, 0, 0, 0, time.UTC), 200},
		}}, got)
	})
}
