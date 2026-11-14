// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dbshrink

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type stubRestarter struct {
	sync.Mutex
	requests int
	err      error
}

func (r *stubRestarter) Request() error {
	r.requests++
	return r.err
}

func newService(t *testing.T, restarter Restarter, free int64) (*Service, *database.DB, *systemnotice.Module) {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "dbshrink")
	notices := systemnotice.New(nil)
	service := New(db, notices, restarter)
	service.freeBytes = func(context.Context, string) (int64, error) { return free, nil }
	return service, db, notices
}

func requirePending(t *testing.T, db *database.DB, want bool) {
	t.Helper()
	pending, err := db.ShrinkPending(t.Context())
	require.NoError(t, err)
	require.Equal(t, want, pending)
}

const plenty = 1 << 40

func TestReport(t *testing.T) {
	t.Run("a failed shrink opens the notice Shrink failed", func(t *testing.T) {
		service, _, notices := newService(t, nil, plenty)

		service.Report(database.ShrinkResult{BeforeBytes: 100, Err: errors.New("database or disk is full"), Failure: database.ShrinkDiskFull})

		open := notices.List()
		require.Len(t, open, 1)
		require.Equal(t, systemnotice.ShrinkFailed, open[0].Kind)
		require.Equal(t, "disk_full", open[0].Reason)
		require.Equal(t, "database or disk is full", open[0].Cause)
		status, err := service.Status(t.Context())
		require.NoError(t, err)
		require.Nil(t, status.Result)
	})

	t.Run("the web UI gets the sizes of a shrink until it dismisses them", func(t *testing.T) {
		service, _, notices := newService(t, nil, plenty)

		service.Report(database.ShrinkResult{BeforeBytes: 300, AfterBytes: 100})

		require.Empty(t, notices.List())
		status, err := service.Status(t.Context())
		require.NoError(t, err)
		require.Equal(t, &Result{BeforeBytes: 300, AfterBytes: 100}, status.Result)
		service.DismissResult()
		status, err = service.Status(t.Context())
		require.NoError(t, err)
		require.Nil(t, status.Result)
	})
}

// regression: #4109
func TestAShrinkThatAnotherProgramBlocksOpensShrinkFailed(t *testing.T) {
	service, db, notices := newService(t, nil, plenty)
	require.NoError(t, db.RequestShrink(t.Context()))
	other, err := sql.Open("sqlite", db.SQLitePath())
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	snapshot, err := other.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = snapshot.Rollback() })
	var count int
	require.NoError(t, snapshot.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM instances").Scan(&count))

	result, ran := db.RunPendingShrink(t.Context())
	require.True(t, ran)
	service.Report(result)

	open := notices.List()
	require.Len(t, open, 1)
	require.Equal(t, systemnotice.ShrinkFailed, open[0].Kind)
	require.Equal(t, string(database.ShrinkLocked), open[0].Reason)
	requirePending(t, db, false)
	status, err := service.Status(t.Context())
	require.NoError(t, err)
	require.Nil(t, status.Result)
}

// regression: #4109
func TestStatusReadsTheFreeSpaceOfTheDiskThatHoldsTheData(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	linkDir := filepath.Join(root, "link")
	require.NoError(t, os.Mkdir(dataDir, 0o750))
	require.NoError(t, os.Mkdir(linkDir, 0o750))
	db, err := database.New(filepath.Join(dataDir, "qui.db"))
	require.NoError(t, err)
	require.NoError(t, db.Close())
	linkPath := filepath.Join(linkDir, "qui.db")
	err = os.Symlink(filepath.Join(dataDir, "qui.db"), linkPath)
	if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
		t.Skipf("symlink creation needs Developer Mode or SeCreateSymbolicLinkPrivilege: %v", err)
	}
	require.NoError(t, err)
	db, err = database.New(linkPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	service := New(db, systemnotice.New(nil), nil)
	var checked string
	service.freeBytes = func(_ context.Context, dir string) (int64, error) {
		checked = dir
		return plenty, nil
	}

	_, err = service.Status(t.Context())

	require.NoError(t, err)
	wantDir, err := filepath.EvalSymlinks(dataDir)
	require.NoError(t, err)
	require.Equal(t, wantDir, checked)
}

func TestRequest(t *testing.T) {
	t.Run("with too little free disk space no mark is stored", func(t *testing.T) {
		restarter := &stubRestarter{}
		service, db, _ := newService(t, restarter, 0)
		status, err := service.Status(t.Context())
		require.NoError(t, err)
		require.Equal(t, 2*status.FileBytes, status.NeedBytes)

		_, err = service.Request(t.Context())

		require.ErrorIs(t, err, ErrTooLittleDisk)
		requirePending(t, db, false)
		require.Zero(t, restarter.requests)
	})

	t.Run("with Restart the mark is stored and qui restarts", func(t *testing.T) {
		restarter := &stubRestarter{}
		service, db, _ := newService(t, restarter, plenty)

		restarting, err := service.Request(t.Context())

		require.NoError(t, err)
		require.True(t, restarting)
		requirePending(t, db, true)
		require.Equal(t, 1, restarter.requests)
		require.False(t, restarter.TryLock(), "a successful request keeps the lock until the restart")
	})

	t.Run("without Restart the mark waits for the next start", func(t *testing.T) {
		service, db, notices := newService(t, nil, plenty)
		service.Report(database.ShrinkResult{Err: errors.New("database is locked"), Failure: database.ShrinkLocked})

		restarting, err := service.Request(t.Context())

		require.NoError(t, err)
		require.False(t, restarting)
		requirePending(t, db, true)
		require.Empty(t, notices.List(), "a new request closes the notice of the old failure")
		status, err := service.Status(t.Context())
		require.NoError(t, err)
		require.False(t, status.RestartAvailable)
	})

	t.Run("a running update stores no mark", func(t *testing.T) {
		restarter := &stubRestarter{}
		restarter.Lock()
		service, db, _ := newService(t, restarter, plenty)

		_, err := service.Request(t.Context())

		require.ErrorIs(t, err, ErrRestartBusy)
		requirePending(t, db, false)
	})

	t.Run("a refused restart unlocks and keeps the mark for the next start", func(t *testing.T) {
		restarter := &stubRestarter{err: errors.New("/opt/qui/qui is not executable")}
		service, db, _ := newService(t, restarter, plenty)

		restarting, err := service.Request(t.Context())

		require.ErrorContains(t, err, "not executable")
		require.False(t, restarting)
		requirePending(t, db, true)
		require.True(t, restarter.TryLock())
	})
}
