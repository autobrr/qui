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
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// withUnique returns a reading whose Copy groups build gives the Unique size,
// or no completed build when unique is nil.
func withUnique(s step, unique *int64) step {
	s.reading.UniqueSize = unique
	return s
}

// withServer returns a reading with the free disk space and the highest
// speeds that qBittorrent reported.
func withServer(s step, freeDisk, maxUp, maxDown int64) step {
	s.reading.FreeDisk, s.reading.MaxUploadSpeed, s.reading.MaxDownloadSpeed = freeDisk, maxUp, maxDown
	return s
}

func inState(t transferhistory.Torrent, state string) transferhistory.Torrent {
	t.State = state
	return t
}

func sized(t transferhistory.Torrent, size int64) transferhistory.Torrent {
	t.Size = size
	return t
}

func libraryRow(day time.Time, torrents, totalSize int64, unique *int64) transferhistory.LibraryRow {
	return transferhistory.LibraryRow{Day: day, Torrents: new(torrents), TotalSize: new(totalSize), UniqueSize: unique}
}

func TestLibraryPostgresIntegration(t *testing.T) {
	t.Parallel()

	day11 := day10.Add(24 * time.Hour)
	cases := []struct {
		name  string
		steps []step
	}{
		{
			name: "a day with a completed build has a Unique size, and a day without one has none",
			steps: []step{
				withUnique(reading(at(0), torrent("a", 0, 0), sized(torrent("b", 0, 0), 3000)), new(int64(3000))),
				reading(at(24*time.Hour), torrent("a", 0, 0), sized(torrent("b", 0, 0), 3000)),
				expectLibrary(
					libraryRow(day10, 2, 4000, new(int64(3000))),
					libraryRow(day11, 2, 4000, nil),
				),
			},
		},
		{
			name: "the torrent count and the total size come with the event counts of the day",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), torrent("b", 0, 0), addedAt(torrent("c", 0, 0), at(4*time.Minute))),
				reading(at(24*time.Hour), torrent("a", 0, 0), torrent("b", 0, 0)),
				expectLibrary(
					transferhistory.LibraryRow{Day: day10, Torrents: new(int64(3)), TotalSize: new(int64(3000)), Added: 1},
					transferhistory.LibraryRow{Day: day11, Torrents: new(int64(2)), TotalSize: new(int64(2000)), Deleted: 1},
				),
			},
		},
		{
			name: "the last reading of a day decides, and a reading without a Unique size keeps the size of the day",
			steps: []step{
				withUnique(reading(at(0), torrent("a", 0, 0)), new(int64(1000))),
				// A write of the day without a Unique size.
				reading(at(5*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(4*time.Minute))),
				expectLibrary(transferhistory.LibraryRow{Day: day10, Torrents: new(int64(2)), TotalSize: new(int64(2000)), UniqueSize: new(int64(1000)), Added: 1}),
				// A reading between two writes, then a write without a Unique size.
				withUnique(reading(at(6*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(4*time.Minute))), new(int64(1500))),
				reading(at(11*time.Minute), torrent("a", 0, 0)),
				expectLibrary(transferhistory.LibraryRow{Day: day10, Torrents: new(int64(1)), TotalSize: new(int64(1000)), UniqueSize: new(int64(1500)), Added: 1, Deleted: 1}),
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

type instanceHour struct {
	hour                     int64
	freeDisk, maxUp, maxDown int64
	states                   map[string]int64
}

// No query of the module returns the instance state, so this test reads the
// tables.
func storedInstanceHours(t *testing.T, db *database.DB, instanceID int) []instanceHour {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `
		SELECT hour, free_disk, max_upload_speed, max_download_speed
		FROM transfer_history_instance_hours WHERE instance_id = ? ORDER BY hour`, instanceID)
	require.NoError(t, err)
	var hours []instanceHour
	for rows.Next() {
		h := instanceHour{states: map[string]int64{}}
		require.NoError(t, rows.Scan(&h.hour, &h.freeDisk, &h.maxUp, &h.maxDown))
		hours = append(hours, h)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())

	for i := range hours {
		rows, err := db.QueryContext(t.Context(), `
			SELECT state, torrents FROM transfer_history_state_hours WHERE instance_id = ? AND hour = ?`, instanceID, hours[i].hour)
		require.NoError(t, err)
		for rows.Next() {
			var state string
			var torrents int64
			require.NoError(t, rows.Scan(&state, &torrents))
			hours[i].states[state] = torrents
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
	}
	return hours
}

func TestEachHourHoldsTheInstanceStatePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		runSteps(t, db, []step{
			withServer(reading(at(0), inState(torrent("a", 0, 0), "stalledUP"), inState(torrent("b", 0, 0), "downloading")), 900, 100, 50),
			withServer(reading(at(5*time.Minute), inState(torrent("a", 0, 0), "stalledUP"), inState(torrent("b", 0, 0), "stalledUP")), 800, 300, 20),
			// A reading between two writes.
			withServer(reading(at(7*time.Minute), inState(torrent("a", 0, 0), "stalledUP"), inState(torrent("b", 0, 0), "stalledUP")), 750, 400, 1),
			withServer(reading(at(10*time.Minute), inState(torrent("a", 0, 0), "stalledUP"), inState(torrent("b", 0, 0), "stalledUP")), 700, 200, 10),
			withServer(reading(at(time.Hour), inState(torrent("a", 0, 0), "uploading"), inState(torrent("b", 0, 0), "stalledUP")), 600, 5, 5),
		})

		hour := t0.Unix() / 3600
		require.Equal(t, []instanceHour{
			{hour: hour, freeDisk: 700, maxUp: 400, maxDown: 50, states: map[string]int64{"stalledUP": 2}},
			{hour: hour + 1, freeDisk: 600, maxUp: 5, maxDown: 5, states: map[string]int64{"stalledUP": 1, "uploading": 1}},
		}, storedInstanceHours(t, db, instanceID))
	})
}

func TestTheHourlyStateFollowsTheDetailRetentionPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		// A reading on day 101 starts a new UTC day. A retention of 90 days
		// then deletes the hours of day 10, and keeps those of day 11.
		day101 := day10.Add(91*24*time.Hour + 10*time.Hour)
		runSteps(t, db, []step{
			settings(true, 90),
			withServer(reading(at(0), inState(torrent("a", 0, 0), "stalledUP")), 900, 1, 1),
			withServer(reading(at(24*time.Hour), inState(torrent("a", 0, 0), "stalledUP")), 800, 2, 2),
			withServer(reading(day101, inState(torrent("a", 0, 0), "uploading")), 700, 3, 3),
			{foldsDone: true},
		})

		hour := t0.Unix() / 3600
		require.Equal(t, []instanceHour{
			{hour: hour + 24, freeDisk: 800, maxUp: 2, maxDown: 2, states: map[string]int64{"stalledUP": 1}},
			{hour: day101.Unix() / 3600, freeDisk: 700, maxUp: 3, maxDown: 3, states: map[string]int64{"uploading": 1}},
		}, storedInstanceHours(t, db, instanceID))
		var orphans int
		require.NoError(t, db.QueryRowContext(t.Context(), `
			SELECT COUNT(*) FROM transfer_history_state_hours WHERE instance_id = ? AND hour < ?`, instanceID, hour+24).Scan(&orphans))
		require.Zero(t, orphans)
	})
}

// regression: #4096
func TestADroppedReadingKeepsItsPeakSpeeds(t *testing.T) {
	t.Parallel()
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	blocked := &blockingWrites{DB: db, writing: make(chan struct{}, 1), release: make(chan struct{})}
	module := transferhistory.New(blocked, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

	module.Record(*reading(at(0), torrent("a", 0, 0)).reading)
	<-blocked.writing
	// Five readings wait while the worker writes, so the oldest one, which
	// holds the peak, drops out of the queue.
	module.Record(*withServer(reading(at(30*time.Minute), torrent("a", 0, 0)), 0, 900, 90).reading)
	for i := range 4 {
		module.Record(*withServer(reading(at(time.Duration(31+i)*time.Minute), torrent("a", 0, 0)), 0, 1, 1).reading)
	}
	close(blocked.release)
	_, err := module.Status(t.Context())
	require.NoError(t, err)

	hours := storedInstanceHours(t, db, instanceID)
	require.Len(t, hours, 1)
	require.Equal(t, [2]int64{900, 90}, [2]int64{hours[0].maxUp, hours[0].maxDown})
}

// regression: #4096
func TestAFoldWritesTheHourlyStateInMemoryFirstPostgresIntegration(t *testing.T) {
	t.Parallel()
	const offline = 2
	onInstance := func(s step, id int) step {
		s.reading.InstanceID = id
		return s
	}
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		runSteps(t, db, []step{
			settings(true, 90),
			onInstance(reading(at(0), torrent("b", 0, 0)), offline),
			// A reading with no transfer that waits for the next write.
			onInstance(reading(at(time.Minute), inState(torrent("b", 0, 0), "uploading")), offline),
			// A reading of the other instance on day 101 deletes the hours of day 10.
			reading(day10.Add(91*24*time.Hour+10*time.Hour), torrent("a", 0, 0)),
			{foldsDone: true},
			// The shutdown writes the changes that are still in memory.
			restart,
		})
		require.Empty(t, storedInstanceHours(t, db, offline))
	})
}

func TestDeleteHistoryDeletesTheHourlyStatePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		runSteps(t, db, []step{
			withServer(reading(at(0), inState(torrent("a", 0, 0), "stalledUP")), 900, 1, 1),
			withServer(reading(at(25*time.Hour), inState(torrent("a", 0, 0), "uploading")), 800, 2, 2),
			// The UTC day of Before is day 11.
			deleteHistory(instanceID, at(24*time.Hour)),
		})
		require.Equal(t, []instanceHour{
			{hour: at(25*time.Hour).Unix() / 3600, freeDisk: 800, maxUp: 2, maxDown: 2, states: map[string]int64{"uploading": 1}},
		}, storedInstanceHours(t, db, instanceID))
		var states int
		require.NoError(t, db.QueryRowContext(t.Context(), `
			SELECT COUNT(*) FROM transfer_history_state_hours WHERE instance_id = ?`, instanceID).Scan(&states))
		require.Equal(t, 1, states)
	})
}
