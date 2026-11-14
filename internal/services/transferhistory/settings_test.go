// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/services/transferhistory"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func settings(recording bool, retentionDays int) step {
	return step{settings: &transferhistory.Settings{Recording: recording, DetailRetentionDays: retentionDays}}
}

var (
	recordingOff = settings(false, 400)
	recordingOn  = settings(true, 400)
)

func row(group string, start time.Time, width transferhistory.Bucket, uploaded, downloaded, crossSeedUploaded, gapSeconds int64) transferhistory.TransferRow {
	return transferhistory.TransferRow{
		Group: group, Start: start, Width: width, GapSeconds: gapSeconds,
		Uploaded: uploaded, Downloaded: downloaded, StatisticsCrossSeedUploaded: crossSeedUploaded,
	}
}

func period(from, to time.Time, bucket transferhistory.Bucket, groupBy transferhistory.Grouping) transferhistory.TransferQuery {
	return transferhistory.TransferQuery{From: from, To: to, Bucket: bucket, GroupBy: groupBy}
}

var (
	day11 = day10.Add(24 * time.Hour)
	day12 = day10.Add(48 * time.Hour)
)

// crossSeed is a complete torrent that downloaded nothing, so its upload is
// upload from Statistics cross-seeds.
func crossSeed(uploaded int64) transferhistory.Torrent {
	return complete(torrent("c", uploaded, 0))
}

// twoDaysOfUpload records upload on day 10 and day 11, with a Recording gap that
// ends at 12:00 on day 10. Its last reading is on day 11.
var twoDaysOfUpload = []step{
	reading(at(0), torrent("a", 0, 0), crossSeed(0)),
	reading(at(5*time.Minute), torrent("a", 100, 50), crossSeed(30)),
	readingAfterFailure(at(2*time.Hour), at(30*time.Minute), torrent("a", 300, 50), crossSeed(60)),
	reading(at(25*time.Hour), torrent("a", 400, 50), crossSeed(60)),
}

// twoDaysChecks are the answers of each grouping for twoDaysOfUpload when no fold ran.
var twoDaysChecks = []step{
	expect(period(day10, day12, transferhistory.BucketHour, transferhistory.GroupByTracker),
		row("tracker.example", day10.Add(10*time.Hour), transferhistory.BucketHour, 130, 50, 30, 0),
		row("tracker.example", day10.Add(12*time.Hour), transferhistory.BucketHour, 230, 0, 30, 5400),
		row("tracker.example", day11.Add(11*time.Hour), transferhistory.BucketHour, 100, 0, 0, 0),
	),
	expect(period(day10, day12, transferhistory.BucketDay, transferhistory.GroupByTracker),
		row("tracker.example", day10, transferhistory.BucketDay, 360, 50, 60, 5400),
		row("tracker.example", day11, transferhistory.BucketDay, 100, 0, 0, 0),
	),
	expect(period(day10, day12, transferhistory.BucketTotal, transferhistory.GroupByTracker),
		row("tracker.example", day10, transferhistory.BucketTotal, 460, 50, 60, 5400),
	),
	expect(period(day10, day12, transferhistory.BucketDay, transferhistory.GroupByCategory),
		row("tv", day10, transferhistory.BucketDay, 360, 50, 60, 5400),
		row("tv", day11, transferhistory.BucketDay, 100, 0, 0, 0),
	),
	expect(period(day10, day12, transferhistory.BucketTotal, transferhistory.GroupByCategory),
		row("tv", day10, transferhistory.BucketTotal, 460, 50, 60, 5400),
	),
	// A period with no start.
	expect(transferhistory.TransferQuery{To: day12, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker},
		row("tracker.example", time.Time{}, transferhistory.BucketTotal, 460, 50, 60, 5400),
	),
	expect(transferhistory.TransferQuery{To: day12, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByCategory},
		row("tv", time.Time{}, transferhistory.BucketTotal, 460, 50, 60, 5400),
	),
}

func TestDetailRetentionPostgresIntegration(t *testing.T) {
	t.Parallel()

	// A reading on day 101 starts a new UTC day. A retention of 90 days then
	// folds day 10, and keeps day 11.
	day101 := reading(day10.Add(91*24*time.Hour+10*time.Hour), torrent("a", 400, 50), crossSeed(60))
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	inNewYork := func(bucket transferhistory.Bucket) transferhistory.TransferQuery {
		q := period(day10, day12, bucket, transferhistory.GroupByTracker)
		q.Location = newYork
		return q
	}
	cases := []struct {
		name  string
		steps []step
	}{
		{
			name: "a fold changes no period total of any grouping",
			steps: concat(twoDaysOfUpload, twoDaysChecks, []step{settings(true, 90), day101, {foldsDone: true}},
				[]step{
					// The hours of day 10 are one day row now.
					expect(period(day10, day12, transferhistory.BucketHour, transferhistory.GroupByTracker),
						row("tracker.example", day10, transferhistory.BucketDay, 360, 50, 60, 5400),
						row("tracker.example", day11.Add(11*time.Hour), transferhistory.BucketHour, 100, 0, 0, 0),
					),
					// The day row is wider than the period.
					expect(period(day10.Add(12*time.Hour), day10.Add(13*time.Hour), transferhistory.BucketHour, transferhistory.GroupByTracker),
						row("tracker.example", day10, transferhistory.BucketDay, 360, 50, 60, 5400),
					),
					expect(period(day10.Add(12*time.Hour), day12, transferhistory.BucketTotal, transferhistory.GroupByTracker),
						row("tracker.example", day10, transferhistory.BucketTotal, 460, 50, 60, 5400),
					),
				},
				twoDaysChecks[1:]),
		},
		{
			// 02:00 UTC on day 11 is 21:00 on November 10 in New York.
			name: "a folded day stays a UTC day beside the local days of the hours",
			steps: concat(twoDaysOfUpload[:3], []step{
				reading(at(16*time.Hour), torrent("a", 350, 50), crossSeed(60)),
				reading(at(25*time.Hour), torrent("a", 400, 50), crossSeed(60)),
				settings(true, 90),
				day101,
				{foldsDone: true},
				expect(inNewYork(transferhistory.BucketDay),
					row("tracker.example", day10, transferhistory.BucketDay, 360, 50, 60, 5400),
					row("tracker.example", day10.Add(5*time.Hour), transferhistory.BucketDay, 50, 0, 0, 0),
					row("tracker.example", day11.Add(5*time.Hour), transferhistory.BucketDay, 50, 0, 0, 0),
				),
				expect(inNewYork(transferhistory.BucketTotal),
					row("tracker.example", day10, transferhistory.BucketTotal, 460, 50, 60, 5400),
				),
			}),
		},
		{
			name:  "the default retention of 400 days does not fold day 10 on day 101",
			steps: concat(twoDaysOfUpload, []step{day101}, twoDaysChecks),
		},
		{
			name: "with the retention set to forever, no fold runs",
			steps: concat(twoDaysOfUpload, []step{
				settings(true, 0),
				reading(day10.Add(900*24*time.Hour), torrent("a", 400, 50), crossSeed(60)),
			}, twoDaysChecks),
		},
		{
			name: "a new retention folds at the next reading of the same day",
			steps: concat(twoDaysOfUpload, []step{
				// The first reading of day 101 folds nothing with 400 days.
				reading(day10.Add(91*24*time.Hour+9*time.Hour), torrent("a", 400, 50), crossSeed(60)),
				twoDaysChecks[0],
				settings(true, 90),
				day101,
				{foldsDone: true},
				expect(period(day10, day12, transferhistory.BucketHour, transferhistory.GroupByTracker),
					row("tracker.example", day10, transferhistory.BucketDay, 360, 50, 60, 5400),
					row("tracker.example", day11.Add(11*time.Hour), transferhistory.BucketHour, 100, 0, 0, 0),
				),
			}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engines(t, func(t *testing.T, db func(t *testing.T) *database.DB) {
				runSteps(t, db(t), tc.steps)
			})
		})
	}
}

func concat(parts ...[]step) []step {
	var out []step
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// A fold moves the days of a torrent before the Detail retention into one row
// for each year. A query for the days of a torrent in a folded year returns
// that row, and a period after the fold does not.
func TestAFoldMovesTheDaysOfATorrentIntoItsYearPostgresIntegration(t *testing.T) {
	t.Parallel()
	name := "Some.Show.S01E01.1080p.WEB-DL-GRP"
	a := &transferhistory.TorrentFacts{Name: name, TrackerGroup: "tracker.example", UploadFirst24h: new(int64(300))}
	c := &transferhistory.TorrentFacts{Name: name, TrackerGroup: "tracker.example", StatisticsCrossSeed: true, UploadFirst24h: new(int64(60))}
	year := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	days := period(day10, day12, transferhistory.BucketDay, transferhistory.GroupByTorrent)
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		runSteps(t, newDB(t), concat(twoDaysOfUpload, []step{
			settings(true, 90),
			reading(day10.Add(91*24*time.Hour+10*time.Hour), torrent("a", 400, 50), crossSeed(60)),
			{foldsDone: true},
			expect(days,
				torrentRow("a", year, transferhistory.BucketYear, 300, 50, 0, a),
				torrentRow("c", year, transferhistory.BucketYear, 60, 0, 0, c),
				torrentRow("a", day11, transferhistory.BucketDay, 100, 0, 0, a),
			),
			expect(withGroup(days, "c"),
				torrentRow("c", year, transferhistory.BucketYear, 60, 0, 0, c),
			),
			// A total starts at the first year that it covers.
			expect(period(day10, day12, transferhistory.BucketTotal, transferhistory.GroupByTorrent),
				torrentRow("a", year, transferhistory.BucketTotal, 400, 50, 5400, a),
				torrentRow("c", year, transferhistory.BucketTotal, 60, 0, 5400, c),
			),
			expect(limited(period(day10, day12, transferhistory.BucketTotal, transferhistory.GroupByTorrent), 1),
				torrentRow("a", year, transferhistory.BucketTotal, 400, 50, 5400, a),
			),
			expect(period(day11, day12, transferhistory.BucketDay, transferhistory.GroupByTorrent),
				torrentRow("a", day11, transferhistory.BucketDay, 100, 0, 0, a),
			),
		}))
	})
}

// An instance that moved no bytes after the fold has no hourly rows, and a
// recent period of it still gets no folded year.
// regression: #4113
func TestARecentPeriodOfAnIdleInstanceGetsNoFoldedYearPostgresIntegration(t *testing.T) {
	t.Parallel()
	later := day10.Add(100 * 24 * time.Hour)
	afterFold := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		runSteps(t, newDB(t), concat(twoDaysOfUpload, []step{
			settings(true, 90),
			reading(later.Add(10*time.Hour), torrent("a", 400, 50), crossSeed(60)),
			{foldsDone: true},
			// The fold moved the days before 2026-11-20 into the year 2026.
			expect(period(afterFold, afterFold.Add(24*time.Hour), transferhistory.BucketTotal, transferhistory.GroupByTorrent)),
			expect(period(afterFold, afterFold.Add(24*time.Hour), transferhistory.BucketDay, transferhistory.GroupByTorrent)),
		}))
	})
}

func TestRecordingSwitchPostgresIntegration(t *testing.T) {
	t.Parallel()

	threeHours := period(day10.Add(10*time.Hour), day10.Add(13*time.Hour), transferhistory.BucketHour, transferhistory.GroupByTracker)
	allTime := transferhistory.TransferQuery{To: day12, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker}
	allTimeRow := func(uploaded int64) transferhistory.TransferRow {
		r := row("tracker.example", time.Time{}, transferhistory.BucketTotal, uploaded, 0, 0, 0)
		r.Baseline = &transferhistory.Bytes{Uploaded: 10}
		return r
	}
	cases := []struct {
		name  string
		steps []step
	}{
		{
			name: "with the switch off, a reading stores nothing, and the next reading after the switch goes on records the gap",
			steps: []step{
				reading(at(0), torrent("a", 10, 0)),
				reading(at(5*time.Minute), torrent("a", 110, 0)),
				recordingOff,
				// Without the switch, this reading would write.
				reading(at(10*time.Minute), torrent("a", 210, 0)),
				reading(at(20*time.Minute), torrent("a", 310, 0)),
				expect(threeHours, row("tracker.example", day10.Add(10*time.Hour), transferhistory.BucketHour, 100, 0, 0, 0)),
				recordingOn,
				reading(at(2*time.Hour), torrent("a", 510, 0)),
				// The gap starts at the last reading before the switch went off.
				expect(threeHours,
					row("tracker.example", day10.Add(10*time.Hour), transferhistory.BucketHour, 100, 0, 0, 0),
					row("tracker.example", day10.Add(12*time.Hour), transferhistory.BucketHour, 400, 0, 0, int64((2*time.Hour-5*time.Minute)/time.Second)),
				),
			},
		},
		{
			name: "the stored data is the same before and after the switch goes off and on",
			steps: []step{
				reading(at(0), torrent("a", 10, 0)),
				reading(at(5*time.Minute), torrent("a", 110, 0)),
				expect(allTime, allTimeRow(100)),
				expect(threeHours, row("tracker.example", day10.Add(10*time.Hour), transferhistory.BucketHour, 100, 0, 0, 0)),
				recordingOff,
				recordingOn,
				expect(allTime, allTimeRow(100)),
				expect(threeHours, row("tracker.example", day10.Add(10*time.Hour), transferhistory.BucketHour, 100, 0, 0, 0)),
			},
		},
		{
			name: "the switch going off writes the changes in memory",
			steps: []step{
				reading(at(0), torrent("a", 10, 0)),
				reading(at(5*time.Minute), torrent("a", 110, 0)),
				// No write is due yet.
				reading(at(7*time.Minute), torrent("a", 160, 0)),
				expect(allTime, allTimeRow(100)),
				recordingOff,
				expect(allTime, allTimeRow(150)),
			},
		},
		{
			name: "a short time off with no reading gives no gap",
			steps: []step{
				reading(at(0), torrent("a", 10, 0)),
				reading(at(5*time.Minute), torrent("a", 110, 0)),
				recordingOff,
				recordingOn,
				reading(at(6*time.Minute), torrent("a", 210, 0)),
				reading(at(10*time.Minute), torrent("a", 210, 0)),
				expect(threeHours, row("tracker.example", day10.Add(10*time.Hour), transferhistory.BucketHour, 200, 0, 0, 0)),
			},
		},
		{
			name: "a delete note while the switch is off is dropped",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), torrent("b", 0, 0)),
				recordingOff,
				note(at(6*time.Minute), byUser, "a"),
				recordingOn,
				reading(at(10*time.Minute), torrent("b", 0, 0)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Other: 1}, deleted("a", at(10*time.Minute), 0, 0, outside)),
			},
		},
		// regression: #4085
		{
			name: "a delete note before the switch goes off keeps its cause",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), torrent("b", 0, 0)),
				note(at(6*time.Minute), byUser, "a"),
				recordingOff,
				recordingOn,
				reading(at(10*time.Minute), torrent("b", 0, 0)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{User: 1}, deleted("a", at(10*time.Minute), 0, 0, byUser)),
			},
		},
		{
			name: "a delete note before the stored switch is read after a restart is dropped",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), torrent("b", 0, 0)),
				recordingOff,
				restart,
				note(at(11*time.Minute), byUser, "a"),
				reading(at(12*time.Minute), torrent("b", 0, 0)),
				recordingOn,
				reading(at(15*time.Minute), torrent("b", 0, 0)),
				// No reading held the torrent since the restart.
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Other: 1}, transferhistory.DeleteEvent{
					Time: at(15 * time.Minute).Truncate(time.Second), Hash: "a", Name: "Some.Show.S01E01.1080p.WEB-DL-GRP",
					TrackerGroup: "tracker.example", Cause: outside,
				}),
			},
		},
		{
			name: "the switch stays off after a restart",
			steps: []step{
				reading(at(0), torrent("a", 10, 0)),
				reading(at(5*time.Minute), torrent("a", 110, 0)),
				recordingOff,
				restart,
				reading(at(10*time.Minute), torrent("a", 210, 0)),
				reading(at(20*time.Minute), torrent("a", 310, 0)),
				expect(threeHours, row("tracker.example", day10.Add(10*time.Hour), transferhistory.BucketHour, 100, 0, 0, 0)),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engines(t, func(t *testing.T, db func(t *testing.T) *database.DB) {
				runSteps(t, db(t), tc.steps)
			})
		})
	}
}

func TestStatusGivesTheSizeAndTheSettingsPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		module := transferhistory.New(db, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

		status, err := module.Status(t.Context())
		require.NoError(t, err)
		require.Equal(t, transferhistory.Settings{Recording: true, DetailRetentionDays: 400}, status.Settings)
		require.Positive(t, status.HistoryBytes)

		require.NoError(t, module.SetSettings(t.Context(), transferhistory.Settings{Recording: false, DetailRetentionDays: 90}))
		status, err = module.Status(t.Context())
		require.NoError(t, err)
		require.Equal(t, transferhistory.Settings{Recording: false, DetailRetentionDays: 90}, status.Settings)

		restarted := transferhistory.New(db, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, restarted.Shutdown(context.Background())) })
		status, err = restarted.Status(t.Context())
		require.NoError(t, err)
		require.Equal(t, transferhistory.Settings{Recording: false, DetailRetentionDays: 90}, status.Settings)
	})
}

func TestSetSettingsRefusesAnotherRetention(t *testing.T) {
	module := transferhistory.New(nil, nil, systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
	err := module.SetSettings(t.Context(), transferhistory.Settings{Recording: true, DetailRetentionDays: 30})
	require.ErrorIs(t, err, transferhistory.ErrInvalidSettings)
}

// unreadableSettings fails each read of the Statistics settings.
type unreadableSettings struct{ *database.DB }

func (u unreadableSettings) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, "transfer_history_settings") {
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		return u.DB.QueryRowContext(ctx, query, args...)
	}
	return u.DB.QueryRowContext(ctx, query, args...)
}

// regression: #4085
func TestShutdownReportsReadingsThatWaitForUnreadableSettings(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	module := transferhistory.New(unreadableSettings{db}, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
	module.Record(transferhistory.Reading{InstanceID: instanceID, Time: at(0), Torrents: []transferhistory.Torrent{torrent("a", 10, 0)}})
	require.Error(t, module.Shutdown(t.Context()))
}

// An instance that goes offline keeps its last changes in memory. A fold
// while it is away must not leave those changes to recreate a folded day.
// regression: #4085
func TestAFoldWritesTheChangesInMemoryFirstPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		t.Run("the write succeeds", func(t *testing.T) {
			db := newDB(t)
			foldWhileAway(t, db, db)
		})
		// The fourth write is the one before the fold. The next reading folds again.
		t.Run("the write before the fold fails", func(t *testing.T) {
			db := newDB(t)
			foldWhileAway(t, &failedCommit{DB: db, fail: 4}, db)
		})
	})
}

func foldWhileAway(t *testing.T, q dbinterface.Querier, db *database.DB) {
	module := transferhistory.New(q, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
	require.NoError(t, module.SetSettings(t.Context(), transferhistory.Settings{Recording: true, DetailRetentionDays: 90}))
	const away = 2
	record := func(instanceID int, at time.Time, uploaded int64) {
		module.Record(transferhistory.Reading{InstanceID: instanceID, Time: at, Torrents: []transferhistory.Torrent{torrent("a", uploaded, 0)}})
		_, err := module.Status(t.Context())
		require.NoError(t, err)
		transferhistory.WaitForFolds(module)
	}
	record(away, at(0), 0)
	record(away, at(5*time.Minute), 100)
	// No write is due yet, and then the instance goes offline.
	record(away, at(7*time.Minute), 150)
	// Another instance starts a fold that covers day 10.
	day101 := day10.Add(91*24*time.Hour + 10*time.Hour)
	record(instanceID, day101, 0)
	record(away, day101.Add(time.Hour), 150)

	rows, err := module.Transfer(t.Context(), transferhistory.TransferQuery{
		InstanceID: away, From: day10, To: day11, Bucket: transferhistory.BucketHour, GroupBy: transferhistory.GroupByTracker,
	})
	require.NoError(t, err)
	require.Equal(t, []transferhistory.TransferRow{row("tracker.example", day10, transferhistory.BucketDay, 150, 0, 0, 0)}, rows)
}

// blockedFold holds the first fold in the move of the torrent days until
// release is called.
type blockedFold struct {
	*database.DB
	hold, release func()
}

func newBlockedFold(db *database.DB) (q *blockedFold, folding <-chan struct{}) {
	started, held := make(chan struct{}), make(chan struct{})
	return &blockedFold{
		DB:      db,
		hold:    sync.OnceFunc(func() { close(started); <-held }),
		release: sync.OnceFunc(func() { close(held) }),
	}, started
}

func (b *blockedFold) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "transfer_history_torrent_days d WHERE d.torrent_id") {
		b.hold()
	}
	return b.DB.QueryContext(ctx, query, args...)
}

// A new Detail retention while a fold moves the days of the torrents starts
// no fold that keeps the transfer queries waiting for that move.
// regression: #4085
func TestATransferQueryDoesNotWaitForTheTorrentDaysOfAnEarlierFold(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	q, folding := newBlockedFold(db)
	module := transferhistory.New(q, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
	t.Cleanup(q.release)

	// The first reading after a start folds.
	module.Record(transferhistory.Reading{InstanceID: instanceID, Time: at(0), Torrents: []transferhistory.Torrent{torrent("a", 0, 0)}})
	<-folding
	require.NoError(t, module.SetSettings(t.Context(), transferhistory.Settings{Recording: true, DetailRetentionDays: 90}))
	module.Record(transferhistory.Reading{InstanceID: instanceID, Time: at(5 * time.Minute), Torrents: []transferhistory.Torrent{torrent("a", 100, 0)}})
	_, err := module.Status(t.Context())
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, err := module.Transfer(t.Context(), period(day10, day11, transferhistory.BucketHour, transferhistory.GroupByTracker))
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("the transfer query waits for the torrent days of the earlier fold")
	}
	q.release()
	transferhistory.WaitForFolds(module)
}
