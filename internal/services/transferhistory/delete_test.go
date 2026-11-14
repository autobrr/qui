// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/services/transferhistory"
)

const otherInstanceID = 2

func readingOf(id int, time time.Time, torrents ...transferhistory.Torrent) step {
	s := reading(time, torrents...)
	s.reading.InstanceID = id
	return s
}

func expectFor(id int, query transferhistory.TransferQuery, want ...transferhistory.TransferRow) step {
	s := expect(query, want...)
	s.check.query.InstanceID = id
	return s
}

func deleteHistory(id int, before time.Time) step {
	return step{historyDelete: &transferhistory.HistoryDelete{InstanceID: id, Before: before}}
}

func deleteAllHistory(before time.Time) step {
	return step{historyDelete: &transferhistory.HistoryDelete{AllInstances: true, Before: before}}
}

func expectInstances(want ...transferhistory.InstanceStatus) step {
	if want == nil {
		want = []transferhistory.InstanceStatus{}
	}
	return step{instances: &want}
}

func expectLibrary(want ...transferhistory.LibraryRow) step {
	return step{library: &libraryCheck{query: twoDays, want: want}}
}

// allTime is each byte of the instance up to day 12, by tracker, as one total.
var allTime = transferhistory.TransferQuery{To: day12, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker}

func allTimeRow(uploaded int64, baseline *transferhistory.Bytes) transferhistory.TransferRow {
	return transferhistory.TransferRow{Group: "tracker.example", Width: transferhistory.BucketTotal, Uploaded: uploaded, Baseline: baseline}
}

// twoDaysWithEvents records upload, an add, and a delete on day 10 and on day
// 11. Torrent "a" is in the Baseline with 10. On day 10, "a" uploads 100 and
// "gone" uploads 20 and goes. On day 11, "a" uploads 300, "new" comes, and
// "late" goes.
var twoDaysWithEvents = []step{
	reading(at(0), torrent("a", 10, 0), torrent("gone", 0, 0), torrent("late", 0, 0)),
	reading(at(5*time.Minute), torrent("a", 110, 0), torrent("gone", 20, 0), torrent("late", 0, 0), addedAt(torrent("new1", 0, 0), at(5*time.Minute))),
	reading(at(10*time.Minute), torrent("a", 110, 0), torrent("late", 0, 0), torrent("new1", 0, 0)),
	reading(at(25*time.Hour), torrent("a", 410, 0), torrent("late", 0, 0), torrent("new1", 0, 0), addedAt(torrent("new2", 0, 0), at(25*time.Hour))),
	reading(at(26*time.Hour), torrent("a", 410, 0), torrent("new1", 0, 0), torrent("new2", 0, 0)),
}

func TestDeleteHistoryPostgresIntegration(t *testing.T) {
	t.Parallel()

	days := period(day10, day12, transferhistory.BucketDay, transferhistory.GroupByTracker)
	cases := []struct {
		name  string
		steps []step
	}{
		{
			name: "a delete of everything of one instance keeps the other instances",
			steps: []step{
				reading(at(0), torrent("a", 100, 0)),
				readingOf(otherInstanceID, at(0), torrent("b", 50, 0)),
				reading(at(5*time.Minute), torrent("a", 300, 0)),
				readingOf(otherInstanceID, at(5*time.Minute), torrent("b", 80, 0)),
				deleteHistory(instanceID, time.Time{}),
				expect(allTime),
				expect(days),
				expectLibrary(),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{}),
				expectFor(otherInstanceID, allTime, allTimeRow(30, &transferhistory.Bytes{Uploaded: 50})),
				expectInstances(transferhistory.InstanceStatus{InstanceID: otherInstanceID, FirstRecordedAt: t0}),
			},
		},
		{
			name: "a delete of everything of each instance",
			steps: []step{
				reading(at(0), torrent("a", 100, 0)),
				readingOf(otherInstanceID, at(0), torrent("b", 50, 0)),
				reading(at(5*time.Minute), torrent("a", 300, 0)),
				readingOf(otherInstanceID, at(5*time.Minute), torrent("b", 80, 0)),
				deleteAllHistory(time.Time{}),
				expect(allTime),
				expectFor(otherInstanceID, allTime),
				expectInstances(),
			},
		},
		{
			name: "a delete before a date keeps the rows from that date on",
			steps: concat(twoDaysWithEvents, []step{
				// The UTC day of Before is day 11.
				deleteHistory(instanceID, at(24*time.Hour)),
				expect(days, row("tracker.example", day11, transferhistory.BucketDay, 300, 0, 0, 0)),
				expect(period(day10, day12, transferhistory.BucketDay, transferhistory.GroupByCategory),
					row("tv", day11, transferhistory.BucketDay, 300, 0, 0, 0)),
				// The Baseline is from before day 11, so it goes too.
				expect(allTime, allTimeRow(300, nil)),
				expectLibrary(transferhistory.LibraryRow{Day: day11, Torrents: new(int64(3)), TotalSize: new(int64(3000)), Added: 1, Deleted: 1}),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Other: 1}, deleted("late", at(26*time.Hour), 0, 0, outside)),
				expectInstances(transferhistory.InstanceStatus{InstanceID: instanceID, FirstRecordedAt: day11}),
				// The record of "gone" went with its day. When the user adds it
				// again, it is new, and "a" counts from its stored counters.
				reading(at(27*time.Hour), torrent("a", 420, 0), torrent("new1", 0, 0), torrent("new2", 0, 0), addedAt(torrent("gone", 20, 0), at(27*time.Hour))),
				reading(at(28*time.Hour), torrent("a", 420, 0), torrent("new1", 0, 0), torrent("new2", 0, 0), torrent("gone", 20, 0)),
				expect(allTime, allTimeRow(330, nil)),
			}),
		},
		{
			name: "a delete before the first recording keeps the Baseline",
			steps: concat(twoDaysWithEvents, []step{
				deleteHistory(instanceID, day10),
				expect(allTime, allTimeRow(420, &transferhistory.Bytes{Uploaded: 10})),
				expectInstances(transferhistory.InstanceStatus{InstanceID: instanceID, FirstRecordedAt: t0}),
			}),
		},
		{
			// regression: #4101
			name: "a delete before a date while recording is off keeps the Recording gap",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 10, 0)),
				recordingOff,
				deleteHistory(instanceID, day10),
				recordingOn,
				reading(at(8*time.Hour), torrent("a", 110, 0)),
				expect(period(at(8*time.Hour).Truncate(time.Hour), at(9*time.Hour).Truncate(time.Hour), transferhistory.BucketTotal, transferhistory.GroupByTracker),
					row("tracker.example", at(8*time.Hour).Truncate(time.Hour), transferhistory.BucketTotal, 100, 0, 0, int64((8*time.Hour-5*time.Minute)/time.Second))),
			},
		},
		{
			name: "a delete before a date of each instance",
			steps: concat(twoDaysWithEvents, []step{
				readingOf(otherInstanceID, at(0), torrent("b", 50, 0)),
				readingOf(otherInstanceID, at(25*time.Hour), torrent("b", 80, 0)),
				deleteAllHistory(at(24 * time.Hour)),
				expect(allTime, allTimeRow(300, nil)),
				expectFor(otherInstanceID, allTime, allTimeRow(30, nil)),
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

func TestDeleteHistoryOfARemovedInstancePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
		require.NoError(t, err)
		removed, err := instances.Create(t.Context(), "Seedbox", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)

		module := transferhistory.New(db, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		module.Record(transferhistory.Reading{InstanceID: removed.ID, Time: at(0), Torrents: []transferhistory.Torrent{torrent("a", 0, 0)}})
		require.NoError(t, instances.Delete(t.Context(), removed.ID))
		require.NoError(t, module.DeleteHistory(t.Context(), transferhistory.HistoryDelete{InstanceID: removed.ID}))
		// A sync that ran during the removal gives one more reading.
		module.Record(transferhistory.Reading{InstanceID: removed.ID, Time: at(time.Minute), Torrents: []transferhistory.Torrent{torrent("a", 10, 0)}})

		status, err := module.Status(t.Context())
		require.NoError(t, err)
		require.Empty(t, status.Instances)
	})
}

func TestADeleteOfEverythingStartsANewBaselinePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
		require.NoError(t, err)
		inst, err := instances.Create(t.Context(), "Seedbox", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)
		require.Equal(t, instanceID, inst.ID)

		runSteps(t, db, []step{
			reading(at(0), torrent("a", 100, 0)),
			reading(at(5*time.Minute), torrent("a", 150, 0)),
			deleteHistory(instanceID, time.Time{}),
			reading(at(10*time.Minute), torrent("a", 200, 0), addedAt(torrent("b", 40, 0), at(10*time.Minute))),
			reading(at(15*time.Minute), torrent("a", 260, 0), torrent("b", 40, 0)),
			expect(allTime, allTimeRow(60, &transferhistory.Bytes{Uploaded: 240})),
			// The new Baseline gives no event, and the library of the day starts again.
			expectLibrary(libraryRow(day10, 2, 2000, nil)),
			expectInstances(transferhistory.InstanceStatus{InstanceID: instanceID, Name: "Seedbox", FirstRecordedAt: at(10 * time.Minute)}),
		})
	})
}
