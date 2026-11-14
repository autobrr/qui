// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory_test

import (
	"context"
	"database/sql"
	"errors"
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
)

// onOwnTracker returns torrent(hash) on the tracker <hash>.example, so that a
// query by tracker answers for each torrent.
func onOwnTracker(hash string, uploaded int64) transferhistory.Torrent {
	return onTracker(torrent(hash, uploaded, 0), hash+".example")
}

// everything is the query of all time by tracker.
var everything = transferhistory.TransferQuery{InstanceID: instanceID, To: at(24 * time.Hour), Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker}

func trackerRow(hash string, uploaded, baseline int64, gapSeconds int64) transferhistory.TransferRow {
	row := transferhistory.TransferRow{Group: hash + ".example", Width: transferhistory.BucketTotal, Uploaded: uploaded, GapSeconds: gapSeconds}
	if baseline > 0 {
		row.Baseline = &transferhistory.Bytes{Uploaded: baseline}
	}
	return row
}

// newSplitModule returns a module on db that stores each torrent in its own part.
func newSplitModule(t *testing.T, db dbinterface.Querier, customizations *models.TrackerCustomizationStore) *transferhistory.Module {
	t.Helper()
	module := transferhistory.New(db, customizations, systemnotice.New(nil))
	transferhistory.SetPartSize(module, 1)
	return module
}

func waitFor(t *testing.T, module *transferhistory.Module) {
	t.Helper()
	_, err := module.Status(t.Context())
	require.NoError(t, err)
}

// otherWriteBetweenParts starts another write to the database when the first
// write transaction begins. Before the third one begins, it waits for that
// write and notes whether it completed, with its error.
type otherWriteBetweenParts struct {
	*database.DB
	mu         sync.Mutex
	writes     int
	otherDone  chan error
	beforeLast bool
	otherErr   error
}

func (o *otherWriteBetweenParts) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.writes++
	if o.writes == 3 {
		select {
		case o.otherErr = <-o.otherDone:
			o.beforeLast = true
		case <-time.After(10 * time.Second):
		}
	}
	tx, err := o.DB.BeginTx(ctx, opts)
	if err == nil && o.writes == 1 {
		go func() {
			_, err := o.ExecContext(context.Background(), `
				INSERT INTO transfer_history_settings (id, recording, detail_retention_days) VALUES (1, ?, 400)
				ON CONFLICT (id) DO UPDATE SET detail_retention_days = excluded.detail_retention_days`, true)
			o.otherDone <- err
		}()
	}
	return tx, err
}

func TestAnotherWriteRunsBetweenThePartsOfAFirstWritePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		other := &otherWriteBetweenParts{DB: db, otherDone: make(chan error, 1)}
		module := newSplitModule(t, other, models.NewTrackerCustomizationStore(db))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

		module.Record(*reading(at(0), onOwnTracker("a", 1000), onOwnTracker("b", 2000), onOwnTracker("c", 3000)).reading)
		waitFor(t, module)

		other.mu.Lock()
		defer other.mu.Unlock()
		require.GreaterOrEqual(t, other.writes, 3, "the first write needs 3 parts")
		require.True(t, other.beforeLast, "the other write completes before the last part")
		require.NoError(t, other.otherErr)
		rows, err := module.Transfer(t.Context(), everything)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{trackerRow("a", 0, 1000, 0), trackerRow("b", 0, 2000, 0), trackerRow("c", 0, 3000, 0)}, rows)
	})
}

type partFault int

const (
	// refused refuses the write transaction.
	refused partFault = iota
	// lostAnswer commits and reports an error.
	lostAnswer
	// landsLate commits and reports an error, and the check after it reads
	// the count from before the commit.
	landsLate
	// lostAnswerAndCheck commits and reports an error, and the check after it
	// fails.
	lostAnswerAndCheck
	// passesLimit fails as a write that passes its time limit.
	passesLimit
)

// faultyPart applies fault to the write transaction with the number at. A
// fold reads beside the worker, so mu guards the counts.
type faultyPart struct {
	*database.DB
	mu     sync.Mutex
	fault  partFault
	at     int
	writes int
	checks int
}

func (f *faultyPart) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.writes == f.at {
		switch f.fault {
		case refused:
			return nil, errors.New("disk I/O error")
		case passesLimit:
			return nil, context.DeadlineExceeded
		case lostAnswer, landsLate, lostAnswerAndCheck:
			// The commit of this transaction fails.
		}
	}
	tx, err := f.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &lostAnswerTx{TxQuerier: tx, lose: f.writes == f.at}, nil
}

func (f *faultyPart) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writes == f.at && f.checks == 0 && strings.Contains(query, "SELECT writes") {
		switch f.fault {
		case landsLate:
			f.checks++
			return f.DB.QueryRowContext(ctx, "SELECT writes - 1 FROM transfer_history_instances WHERE instance_id = ?", args...)
		case lostAnswerAndCheck:
			f.checks++
			return f.DB.QueryRowContext(ctx, "SELECT no_such_column FROM transfer_history_instances")
		case refused, lostAnswer, passesLimit:
		}
	}
	return f.DB.QueryRowContext(ctx, query, args...)
}

func TestAFailedPartCountsEachByteOncePostgresIntegration(t *testing.T) {
	t.Parallel()
	// Each write has 3 parts: a, b, and c. The first write is transactions 1
	// to 3, and the second write is transactions 4 to 6.
	type row struct {
		name  string
		fault partFault
		at    int
	}
	rows := make([]row, 0, 30)
	for _, fault := range []struct {
		name  string
		fault partFault
	}{
		{"an error", refused},
		{"a commit that loses its answer", lostAnswer},
		{"a commit that lands late", landsLate},
		{"a commit that loses its answer before a failed check", lostAnswerAndCheck},
		{"a write that passes its time limit", passesLimit},
	} {
		for write, first := range map[string]int{"the first write": 1, "the second write": 4} {
			for part, offset := range map[string]int{"first": 0, "middle": 1, "last": 2} {
				rows = append(rows, row{name: fault.name + " in the " + part + " part of " + write, fault: fault.fault, at: first + offset})
			}
		}
	}

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
				db := newDB(t)
				module := newSplitModule(t, &faultyPart{DB: db, fault: tc.fault, at: tc.at}, models.NewTrackerCustomizationStore(db))
				t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

				for _, r := range []struct {
					at       time.Duration
					uploaded int64
				}{{0, 0}, {5 * time.Minute, 100}, {10 * time.Minute, 150}, {11 * time.Minute, 180}} {
					module.Record(*reading(at(r.at), onOwnTracker("a", 1000+r.uploaded), onOwnTracker("b", 2000+r.uploaded), onOwnTracker("c", 3000+r.uploaded)).reading)
					waitFor(t, module)
				}
				// The write after the last reading stores the changes that a
				// failed part kept in memory.
				module.Record(*reading(at(16*time.Minute), onOwnTracker("a", 1180), onOwnTracker("b", 2180), onOwnTracker("c", 3180)).reading)

				answer, err := module.Transfer(t.Context(), everything)
				require.NoError(t, err)
				require.Equal(t, []transferhistory.TransferRow{trackerRow("a", 180, 1000, 0), trackerRow("b", 180, 2000, 0), trackerRow("c", 180, 3000, 0)}, answer)
			})
		})
	}
}

// stopAfter stores the first stop write transactions. It refuses each later
// one, as when qui stops between two parts.
type stopAfter struct {
	*database.DB
	stop   int
	mu     sync.Mutex
	writes int
}

func (s *stopAfter) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	s.mu.Lock()
	s.writes++
	stopped := s.writes > s.stop
	s.mu.Unlock()
	if stopped {
		return nil, errors.New("qui stopped")
	}
	return s.DB.BeginTx(ctx, opts)
}

func TestAStopAfterTheFirstPartKeepsTheBaselinePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		customizations := models.NewTrackerCustomizationStore(db)
		stopped := newSplitModule(t, &stopAfter{DB: db, stop: 1}, customizations)
		t.Cleanup(func() { _ = stopped.Shutdown(context.Background()) })
		// a is in the stored part. b and c are in parts that qui did not
		// store, and the user added c 30 minutes before the first recording.
		young := addedAt(onOwnTracker("c", 3000), at(-30*time.Minute))
		stopped.Record(*reading(at(0), onOwnTracker("a", 1000), complete(onOwnTracker("b", 2000)), young).reading)
		waitFor(t, stopped)

		module := newSplitModule(t, db, customizations)
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		young.Uploaded = 3100
		// The user added d after the first recording, while qui was stopped.
		added := addedAt(onOwnTracker("d", 500), at(2*time.Hour))
		module.Record(*reading(at(3*time.Hour), onOwnTracker("a", 1100), complete(onOwnTracker("b", 2100)), young, added).reading)

		answer, err := module.Transfer(t.Context(), everything)
		require.NoError(t, err)
		gap := int64(3 * time.Hour / time.Second)
		// b downloaded nothing, so it is a Statistics cross-seed.
		crossSeedB := trackerRow("b", 0, 2100, gap)
		crossSeedB.Baseline.StatisticsCrossSeedUploaded = 2100
		require.Equal(t, []transferhistory.TransferRow{
			trackerRow("a", 100, 1000, gap), crossSeedB, trackerRow("c", 0, 3100, gap), trackerRow("d", 500, 0, gap),
		}, answer)
		// Only d is an add. b completed before the first recording.
		library, err := module.Library(t.Context(), twoDays)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.LibraryRow{{Day: day10, Added: 1}}, eventCounts(library))
	})
}

func TestAStopBetweenTwoPartsStoresNoRecordingGapForReadTimePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		customizations := models.NewTrackerCustomizationStore(db)
		// The first write stores its 3 parts. The second write stores its
		// first part, and then qui stops.
		stopped := newSplitModule(t, &stopAfter{DB: db, stop: 4}, customizations)
		t.Cleanup(func() { _ = stopped.Shutdown(context.Background()) })
		stopped.Record(*reading(at(0), onOwnTracker("a", 1000), onOwnTracker("b", 2000), onOwnTracker("c", 3000)).reading)
		stopped.Record(*reading(at(5*time.Minute), onOwnTracker("a", 1100), onOwnTracker("b", 2100), onOwnTracker("c", 3100)).reading)
		waitFor(t, stopped)

		// qui starts again 1 minute after its last reading.
		module := newSplitModule(t, db, customizations)
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		module.Record(*reading(at(6*time.Minute), onOwnTracker("a", 1150), onOwnTracker("b", 2150), onOwnTracker("c", 3150)).reading)

		answer, err := module.Transfer(t.Context(), everything)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{trackerRow("a", 150, 1000, 0), trackerRow("b", 150, 2000, 0), trackerRow("c", 150, 3000, 0)}, answer)
	})
}

// regression: #4120
func TestAStopBetweenTwoPartsCountsTheBytesOfAMagnetThatGotItsHashBackOncePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		customizations := models.NewTrackerCustomizationStore(db)
		// The writes at 0, 5, and 10 minutes take 4 transactions. The write
		// at 15 minutes stores its first part, and then qui stops.
		stopped := newSplitModule(t, &stopAfter{DB: db, stop: 5}, customizations)
		t.Cleanup(func() { _ = stopped.Shutdown(context.Background()) })
		magnet := addedAt(torrent("v1", 100, 0), at(9*time.Minute))
		for _, s := range []step{
			reading(at(0), torrent("a", 0, 0), torrent("v2", 1000, 0)),
			reading(at(5*time.Minute), torrent("a", 0, 0)),
			reading(at(10*time.Minute), torrent("a", 0, 0), magnet),
			// The magnet gets the hash of the deleted record v2.
			reading(at(15*time.Minute), torrent("a", 0, 0), withV1(addedAt(torrent("v2", 150, 0), at(9*time.Minute)), "v1")),
		} {
			stopped.Record(*s.reading)
			waitFor(t, stopped)
		}

		module := newSplitModule(t, db, customizations)
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		module.Record(*reading(at(16*time.Minute), torrent("a", 0, 0), withV1(addedAt(torrent("v2", 180, 0), at(9*time.Minute)), "v1")).reading)

		query := hourTotal
		query.InstanceID = instanceID
		answer, err := module.Transfer(t.Context(), query)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{total("tracker.example", 180, 0, 0)}, answer)
	})
}

// regression: #4120
func TestAStopBetweenTwoPartsAfterAnOutageKeepsTheTransferOfTorrentsAddedInItPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		customizations := models.NewTrackerCustomizationStore(db)
		// The writes at 0 and 5 minutes take 3 transactions. The write after
		// the outage stores its first part, a, and then qui stops.
		stopped := newSplitModule(t, &stopAfter{DB: db, stop: 4}, customizations)
		t.Cleanup(func() { _ = stopped.Shutdown(context.Background()) })
		// During the outage, the user adds x and adds y again.
		x := addedAt(onOwnTracker("x", 500), at(30*time.Minute))
		y := addedAt(onOwnTracker("y", 400), at(40*time.Minute))
		for _, s := range []step{
			reading(at(0), onOwnTracker("a", 1000), onOwnTracker("y", 1000)),
			reading(at(5*time.Minute), onOwnTracker("a", 1000)),
			reading(at(2*time.Hour), onOwnTracker("a", 1100), x, y),
		} {
			stopped.Record(*s.reading)
			waitFor(t, stopped)
		}

		module := newSplitModule(t, db, customizations)
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		x.Uploaded, y.Uploaded = 600, 450
		module.Record(*reading(at(2*time.Hour+time.Minute), onOwnTracker("a", 1150), x, y).reading)

		answer, err := module.Transfer(t.Context(), everything)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{trackerRow("a", 150, 1000, 0), trackerRow("x", 600, 0, 0), trackerRow("y", 450, 1000, 0)}, answer)
	})
}

// regression: #4120
func TestAStopBetweenTwoPartsCountsTheBytesOfATorrentLeftOutOfOneReadingOncePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		customizations := models.NewTrackerCustomizationStore(db)
		// The first write takes 2 transactions. The write at 2 hours stores
		// the part of a, which marks a deleted, and then qui stops.
		stopped := newSplitModule(t, &stopAfter{DB: db, stop: 3}, customizations)
		t.Cleanup(func() { _ = stopped.Shutdown(context.Background()) })
		a := addedAt(onOwnTracker("a", 1000), at(-30*time.Minute))
		for _, s := range []step{
			reading(at(0), a, onOwnTracker("b", 2000)),
			// qBittorrent leaves a out of this reading.
			reading(at(2*time.Hour), onOwnTracker("b", 2050)),
		} {
			stopped.Record(*s.reading)
			waitFor(t, stopped)
		}

		module := newSplitModule(t, db, customizations)
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		a.Uploaded = 1100
		module.Record(*reading(at(2*time.Hour+time.Minute), a, onOwnTracker("b", 2050)).reading)

		answer, err := module.Transfer(t.Context(), everything)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{trackerRow("a", 100, 1000, 0), trackerRow("b", 50, 2000, 0)}, answer)
	})
}

// refuseWrites refuses the write transactions whose numbers refuse holds.
type refuseWrites struct {
	*database.DB
	refuse map[int]bool
	mu     sync.Mutex
	writes int
}

func (r *refuseWrites) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	r.mu.Lock()
	r.writes++
	refused := r.refuse[r.writes]
	r.mu.Unlock()
	if refused {
		return nil, errors.New("disk I/O error")
	}
	return r.DB.BeginTx(ctx, opts)
}

// regression: #4120
func TestAStopAfterTwoUnfinishedWritesCountsATorrentAddedAgainPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		customizations := models.NewTrackerCustomizationStore(db)
		// The first write takes transactions 1 to 3. The writes at 1 and 3
		// hours each store their first parts, and then the part of z or of
		// c fails.
		stopped := newSplitModule(t, &refuseWrites{DB: db, refuse: map[int]bool{6: true, 8: true}}, customizations)
		t.Cleanup(func() { _ = stopped.Shutdown(context.Background()) })
		c := addedAt(onOwnTracker("c", 1000), at(-30*time.Minute))
		again := addedAt(onOwnTracker("c", 300), at(90*time.Minute))
		for _, s := range []step{
			reading(at(0), onOwnTracker("a", 1000), c, onOwnTracker("z", 1000)),
			// qBittorrent leaves c out, so a stored part marks it deleted.
			reading(at(time.Hour), onOwnTracker("a", 1010), onOwnTracker("z", 1010)),
			// The user adds c again at 1:30.
			reading(at(3*time.Hour), onOwnTracker("a", 1020), again, onOwnTracker("z", 1020)),
		} {
			stopped.Record(*s.reading)
			waitFor(t, stopped)
		}

		module := newSplitModule(t, db, customizations)
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		again.Uploaded = 310
		module.Record(*reading(at(3*time.Hour+time.Minute), onOwnTracker("a", 1030), again, onOwnTracker("z", 1030)).reading)

		answer, err := module.Transfer(t.Context(), everything)
		require.NoError(t, err)
		require.Equal(t, []transferhistory.TransferRow{trackerRow("a", 30, 1000, 0), trackerRow("c", 310, 1000, 0), trackerRow("z", 30, 1000, 0)}, answer)
	})
}
