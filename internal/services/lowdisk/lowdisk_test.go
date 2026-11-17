// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package lowdisk

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	mb = 1_000_000
	gb = 1_000_000_000
)

func TestLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		total int64
		want  int64
	}{
		{name: "5% of a small disk", total: 10 * gb, want: 500 * mb},
		{name: "1 GB where 5% is the same", total: 20 * gb, want: 1 * gb},
		{name: "1 GB of a large disk", total: 100 * gb, want: 1 * gb},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, Limit(tt.total))
		})
	}
}

// sent counts the events that the notice module sends.
type sent struct {
	mu     sync.Mutex
	events []notifications.Event
}

func (s *sent) Notify(_ context.Context, event notifications.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *sent) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// fakeDisk is a disk with a free space that the test sets.
type fakeDisk struct {
	mu    sync.Mutex
	free  int64
	total int64
	dirs  []string
}

func (d *fakeDisk) set(free int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.free = free
}

func (d *fakeDisk) read(_ context.Context, dir string) (disk, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dirs = append(d.dirs, dir)
	return disk{freeBytes: d.free, totalBytes: d.total}, nil
}

// The test disk has 10 GB, so its limit is 500 MB, and the notice closes at
// 550 MB.
const (
	lowFree    = 400 * mb
	plentyFree = 600 * mb
)

func newMonitor(t *testing.T, db *database.DB, disk *fakeDisk) (*Monitor, *systemnotice.Module, *sent) {
	t.Helper()
	notifier := &sent{}
	notices := systemnotice.New(notifier)
	monitor := New(db, notices)
	monitor.readDisk = disk.read
	return monitor, notices, notifier
}

func requireLowDisk(t *testing.T, notices *systemnotice.Module, wantOpen bool) {
	t.Helper()
	_, isOpen := notices.Opening(systemnotice.LowDisk)
	require.Equal(t, wantOpen, isOpen)
}

func TestTheNoticeOpensBelowTheLimit(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "lowdisk-open")
	disk := &fakeDisk{free: plentyFree, total: 10 * gb}
	monitor, notices, notifier := newMonitor(t, db, disk)

	require.NoError(t, monitor.Check(t.Context()))
	requireLowDisk(t, notices, false)

	disk.set(lowFree)
	require.NoError(t, monitor.Check(t.Context()))
	require.NoError(t, monitor.Check(t.Context()))
	open := notices.List()
	require.Len(t, open, 1)
	require.Equal(t, systemnotice.LowDisk, open[0].Kind)
	require.Equal(t, systemnotice.SeverityWarning, open[0].Severity)
	require.Equal(t, "400 MB free of 10 GB on the disk of the database file. The limit is 500 MB.", open[0].Cause)
	require.Equal(t, 1, notifier.count(), "the notice sends one notification when it opens")

	dir, err := db.SQLiteFileDir()
	require.NoError(t, err)
	require.Equal(t, dir, disk.dirs[0], "the check reads the disk of the database file")

	disk.set(520 * mb)
	require.NoError(t, monitor.Check(t.Context()))
	disk.set(lowFree)
	require.NoError(t, monitor.Check(t.Context()))
	requireLowDisk(t, notices, true)
	require.Equal(t, 1, notifier.count(), "free space below the limit plus 10% does not close the notice")

	disk.set(plentyFree)
	require.NoError(t, monitor.Check(t.Context()))
	requireLowDisk(t, notices, false)
}

func TestTheDismissRule(t *testing.T) {
	tests := []struct {
		name string
		// after runs after the user dismissed the notice of a low disk.
		after    func(t *testing.T, monitor *Monitor, notices *systemnotice.Module, disk *fakeDisk)
		wantOpen bool
	}{
		{
			name: "stays hidden while the free space stays low",
			after: func(t *testing.T, monitor *Monitor, _ *systemnotice.Module, disk *fakeDisk) {
				disk.set(lowFree - 100*mb)
				require.NoError(t, monitor.Check(t.Context()))
			},
			wantOpen: false,
		},
		{
			name: "opens again after the space recovers and drops",
			after: func(t *testing.T, monitor *Monitor, _ *systemnotice.Module, disk *fakeDisk) {
				disk.set(plentyFree)
				require.NoError(t, monitor.Check(t.Context()))
				disk.set(lowFree)
				require.NoError(t, monitor.Check(t.Context()))
			},
			wantOpen: true,
		},
		{
			name: "opens again after a failed write",
			after: func(t *testing.T, monitor *Monitor, notices *systemnotice.Module, _ *fakeDisk) {
				notices.Open(systemnotice.RecordingStopped, "database or disk is full")
				require.NoError(t, monitor.Check(t.Context()))
			},
			wantOpen: true,
		},
		{
			name: "stays hidden while the free space stays below the limit plus 10%",
			after: func(t *testing.T, monitor *Monitor, _ *systemnotice.Module, disk *fakeDisk) {
				disk.set(520 * mb)
				require.NoError(t, monitor.Check(t.Context()))
				disk.set(lowFree)
				require.NoError(t, monitor.Check(t.Context()))
			},
			wantOpen: false,
		},
		{
			name: "opens again after a failed write when the dismiss cannot be stored",
			after: func(t *testing.T, monitor *Monitor, notices *systemnotice.Module, _ *fakeDisk) {
				_, err := monitor.db.ExecContext(t.Context(), "DROP TABLE system_notice_dismissals")
				require.NoError(t, err)
				notices.Open(systemnotice.RecordingStopped, "database or disk is full")
				require.Error(t, monitor.Check(t.Context()))
			},
			wantOpen: true,
		},
		{
			name: "stays hidden while the failed write that it saw stays open",
			after: func(t *testing.T, monitor *Monitor, notices *systemnotice.Module, _ *fakeDisk) {
				notices.Open(systemnotice.RecordingStopped, "database or disk is full")
				require.NoError(t, monitor.Check(t.Context()))
				require.NoError(t, notices.Dismiss(t.Context(), systemnotice.LowDisk))
				require.NoError(t, monitor.Check(t.Context()))
			},
			wantOpen: false,
		},
		{
			name: "stays hidden after a dismiss while a failed write is open",
			after: func(t *testing.T, monitor *Monitor, notices *systemnotice.Module, _ *fakeDisk) {
				notices.Open(systemnotice.RecordingStopped, "database or disk is full")
				require.NoError(t, monitor.Check(t.Context()))
				notices.Close(systemnotice.RecordingStopped)
				require.NoError(t, monitor.Check(t.Context()))
				notices.Open(systemnotice.RecordingStopped, "database or disk is full")
				require.NoError(t, notices.Dismiss(t.Context(), systemnotice.LowDisk))
				require.NoError(t, monitor.Check(t.Context()))
			},
			wantOpen: false,
		},
		{
			name: "opens again after a failed write that closed and opened again between two checks",
			after: func(t *testing.T, monitor *Monitor, notices *systemnotice.Module, _ *fakeDisk) {
				notices.Open(systemnotice.RecordingStopped, "database or disk is full")
				require.NoError(t, monitor.Check(t.Context()))
				require.NoError(t, notices.Dismiss(t.Context(), systemnotice.LowDisk))
				notices.Close(systemnotice.RecordingStopped)
				notices.Open(systemnotice.RecordingStopped, "database or disk is full")
				require.NoError(t, monitor.Check(t.Context()))
			},
			wantOpen: true,
		},
		{
			name: "opens after a dismiss that came after the notice closed",
			after: func(t *testing.T, monitor *Monitor, notices *systemnotice.Module, disk *fakeDisk) {
				disk.set(plentyFree)
				require.NoError(t, monitor.Check(t.Context()))
				disk.set(lowFree)
				require.NoError(t, monitor.Check(t.Context()))
				disk.set(plentyFree)
				require.NoError(t, monitor.Check(t.Context()))
				require.NoError(t, notices.Dismiss(t.Context(), systemnotice.LowDisk))
				disk.set(lowFree)
				require.NoError(t, monitor.Check(t.Context()))
			},
			wantOpen: true,
		},
		{
			name: "opens again after a failed write that opened and closed between two checks",
			after: func(t *testing.T, monitor *Monitor, notices *systemnotice.Module, _ *fakeDisk) {
				notices.Open(systemnotice.RecordingStopped, "database or disk is full")
				notices.Close(systemnotice.RecordingStopped)
				require.NoError(t, monitor.Check(t.Context()))
			},
			wantOpen: true,
		},
		{
			name: "opens again after the next failed write",
			after: func(t *testing.T, monitor *Monitor, notices *systemnotice.Module, _ *fakeDisk) {
				notices.Open(systemnotice.RecordingStopped, "database or disk is full")
				require.NoError(t, monitor.Check(t.Context()))
				require.NoError(t, notices.Dismiss(t.Context(), systemnotice.LowDisk))
				notices.Close(systemnotice.RecordingStopped)
				require.NoError(t, monitor.Check(t.Context()))
				requireLowDisk(t, notices, false)
				notices.Open(systemnotice.RecordingStopped, "database or disk is full")
				require.NoError(t, monitor.Check(t.Context()))
			},
			wantOpen: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testdb.NewMigratedSQLite(t, "lowdisk-dismiss")
			disk := &fakeDisk{free: lowFree, total: 10 * gb}
			monitor, notices, _ := newMonitor(t, db, disk)
			require.NoError(t, monitor.Check(t.Context()))
			requireLowDisk(t, notices, true)

			require.NoError(t, notices.Dismiss(t.Context(), systemnotice.LowDisk))
			requireLowDisk(t, notices, false)

			tt.after(t, monitor, notices, disk)
			requireLowDisk(t, notices, tt.wantOpen)
		})
	}
}

func TestADismissSurvivesARestart(t *testing.T) {
	path := testdb.CloneMigratedSQLite(t, "lowdisk-restart")
	disk := &fakeDisk{free: lowFree, total: 10 * gb}

	db, err := database.Open(database.OpenOptions{Engine: string(database.DialectSQLite), SQLitePath: path})
	require.NoError(t, err)
	monitor, notices, _ := newMonitor(t, db, disk)
	require.NoError(t, monitor.Check(t.Context()))
	require.NoError(t, notices.Dismiss(t.Context(), systemnotice.LowDisk))
	require.NoError(t, db.Close())

	db, err = database.Open(database.OpenOptions{Engine: string(database.DialectSQLite), SQLitePath: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	monitor, notices, _ = newMonitor(t, db, disk)
	disk.set(520 * mb)
	require.NoError(t, monitor.Check(t.Context()))
	disk.set(lowFree)
	require.NoError(t, monitor.Check(t.Context()))
	requireLowDisk(t, notices, false)

	disk.set(plentyFree)
	require.NoError(t, monitor.Check(t.Context()))
	disk.set(lowFree)
	require.NoError(t, monitor.Check(t.Context()))
	requireLowDisk(t, notices, true)
}

func TestTheNoticeNeverOpensOnPostgresIntegration(t *testing.T) {
	db := testdb.NewMigratedPostgres(t, "lowdisk")
	disk := &fakeDisk{free: lowFree, total: 10 * gb}
	monitor, notices, _ := newMonitor(t, db, disk)

	require.NoError(t, monitor.Check(t.Context()))
	requireLowDisk(t, notices, false)
	require.Empty(t, disk.dirs, "on Postgres, qui reads no disk")
}

func TestReadDisk(t *testing.T) {
	t.Parallel()
	disk, err := readDisk(t.Context(), t.TempDir())
	require.NoError(t, err)
	require.Positive(t, disk.freeBytes)
	require.Positive(t, disk.totalBytes)
	require.LessOrEqual(t, disk.freeBytes, disk.totalBytes)
}
