// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"io/fs"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

// These tests turn local access off between the snapshot a scan or an
// injection was admitted on and the backend it reads through. The pool then
// routes to the SSH host, which must see none of the reads.

var remoteRow = &models.Instance{ID: 1, SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey"}

// remoteSpy stands in for the SSH host and counts the reads that reached it.
type remoteSpy struct {
	fsops.Backend
	calls atomic.Int32
}

func (s *remoteSpy) notExist(op, p string) error {
	s.calls.Add(1)
	return &fs.PathError{Op: op, Path: p, Err: fs.ErrNotExist}
}

func (s *remoteSpy) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	return nil, s.notExist("stat", p)
}

func (s *remoteSpy) Lstat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	return nil, s.notExist("lstat", p)
}

func (s *remoteSpy) ReadDir(_ context.Context, p string) ([]fsops.DirEntry, error) {
	return nil, s.notExist("readdir", p)
}

func (s *remoteSpy) MkdirAll(_ context.Context, p string, _ fs.FileMode) error {
	return s.notExist("mkdirall", p)
}

func (s *remoteSpy) SameFilesystem(_ context.Context, p, _ string) (bool, error) {
	return false, s.notExist("samefilesystem", p)
}

// remotePool answers every instance read with the remote row.
func remotePool() (*fsops.Pool, *remoteSpy) {
	spy := &remoteSpy{}
	return fsops.NewPoolWithRemote(&fakeInstanceStore{instance: remoteRow}, local.NewBackend(),
		func(*models.Instance) fsops.Backend { return spy }), spy
}

func TestInjectRefusesAnInstanceThatLeftLocalMode(t *testing.T) {
	for _, test := range []struct {
		name         string
		useHardlinks bool
	}{
		{name: "regular"},
		{name: "hardlink", useHardlinks: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fx := newInjectFixture(t)
			fx.instance.UseHardlinks = test.useHardlinks
			pool, spy := remotePool()
			adder := &failingTorrentAdder{err: errors.New("add failed")}
			injector := NewInjector(nil, adder, nil, &fakeInstanceStore{instance: fx.instance}, nil, pool)

			_, err := injector.Inject(t.Context(), fx.req)

			require.ErrorIs(t, err, fsops.ErrNotLocal)
			require.False(t, adder.called, "nothing may be added over paths qui checked on another host")
			require.Zero(t, spy.calls.Load(), "the injection must not read the SSH host")
		})
	}
}

func TestScanFailsForAnInstanceThatLeftLocalMode(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := setupDirScanServiceTestDB(t)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, new(true))
	require.NoError(t, err)
	store := models.NewDirScanStore(db)
	dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path: t.TempDir(), Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)
	runID, err := store.CreateRunIfNoActive(ctx, dir.ID, "scheduled", "")
	require.NoError(t, err)

	pool, spy := remotePool()
	svc := &Service{store: store, backendPool: pool}
	logger := zerolog.Nop()

	_, _, ok := svc.runScanPhase(ctx, dir, dir.Path, runID, &logger)

	require.False(t, ok)
	require.Zero(t, spy.calls.Load(), "the scan must not walk the SSH host")
	run, err := store.GetRun(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, models.DirScanRunStatusFailed, run.Status)
	require.Contains(t, run.ErrorMessage, fsops.ErrNotLocal.Error())
}
