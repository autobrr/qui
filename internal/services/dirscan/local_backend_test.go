// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
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
			injector := NewInjector(nil, crossseed.NewAligner(adder), testAdder(adder, nil, nil), nil, nil, pool)

			_, err := inject(t.Context(), t, injector, fx.req)

			require.ErrorIs(t, err, errLeftLocalMode)
			require.EqualError(t, err, "get filesystem backend: instance 1 is in remote filesystem mode: instance does not have local filesystem access")
			require.False(t, adder.called, "nothing may be added over paths qui checked on another host")
			require.Zero(t, spy.calls.Load(), "the injection must not read the SSH host")
		})
	}
}

func TestValidateDirectoryAdmitsARemoteInstance(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, dir, runID := newLocalModeRun(t)
	pool, spy := remotePool()
	svc := &Service{store: store, backendPool: pool}
	logger := zerolog.Nop()

	_, target, ok := svc.validateDirectory(ctx, dir.ID, runID, &logger)

	require.True(t, ok)
	require.Same(t, remoteRow, target.instance)
	require.Same(t, spy, target.backend, "the walk reads the instance's own backend")
	require.Zero(t, spy.calls.Load(), "admitting the instance reads nothing yet")
}

// failingInstanceStore cannot read any instance.
type failingInstanceStore struct{}

func (failingInstanceStore) Get(context.Context, int) (*models.Instance, error) {
	return nil, errors.New("database is locked")
}

func TestValidateDirectoryFailsTheRun(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		store interface {
			Get(context.Context, int) (*models.Instance, error)
		}
		want []string
	}{
		{
			name:  "no filesystem access",
			store: &fakeInstanceStore{instance: &models.Instance{ID: 1}},
			want:  []string{"target instance does not have local filesystem access"},
		},
		{
			name:  "instance cannot be read",
			store: failingInstanceStore{},
			want:  []string{"failed to get target instance: ", "database is locked"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			store, dir, runID := newLocalModeRun(t)
			svc := &Service{store: store, backendPool: fsops.NewPool(tt.store, local.NewBackend())}
			logger := zerolog.Nop()

			_, target, ok := svc.validateDirectory(ctx, dir.ID, runID, &logger)

			require.False(t, ok)
			require.Nil(t, target)
			run, err := store.GetRun(ctx, runID)
			require.NoError(t, err)
			require.Equal(t, models.DirScanRunStatusFailed, run.Status)
			for _, want := range tt.want {
				require.Contains(t, run.ErrorMessage, want)
			}
		})
	}
}

// flippingStore answers local on the first read and remote after, like local access turned off mid-run.
type flippingStore struct{ reads atomic.Int32 }

func (s *flippingStore) Get(context.Context, int) (*models.Instance, error) {
	if s.reads.Add(1) == 1 {
		return &models.Instance{ID: remoteRow.ID, HasLocalFilesystemAccess: true}, nil
	}
	return remoteRow, nil
}

// A run keeps the backend of the read that admitted it, so a later flip cannot walk the SSH host.
func TestScanWalksTheBackendOfTheReadThatAdmittedIt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, dir, runID := newLocalModeRun(t)
	movie := filepath.Join(dir.Path, "Synthetic.Film.2024.1080p.WEB.x264-GRP.mkv")
	require.NoError(t, os.WriteFile(movie, []byte("synthetic media"), 0o600))

	instances := &flippingStore{}
	spy := &remoteSpy{}
	pool := fsops.NewPoolWithRemote(instances, local.NewBackend(), func(*models.Instance) fsops.Backend { return spy })
	svc := &Service{store: store, backendPool: pool, torrentFiles: stubTorrentFiles{empty: true}}
	logger := zerolog.Nop()

	validated, target, ok := svc.validateDirectory(ctx, dir.ID, runID, &logger)
	require.True(t, ok)
	require.True(t, target.instance.HasLocalFilesystemAccess)

	result, _, ok := svc.runScanPhase(ctx, validated, target, dir.Path, runID, &logger)

	require.True(t, ok)
	require.Len(t, result.Searchees, 1)
	require.Equal(t, movie, result.Searchees[0].Path)
	require.Zero(t, spy.calls.Load(), "the walk must stay on the backend of the admitting read")
}

func newLocalModeRun(t *testing.T) (*models.DirScanStore, *models.DirScanDirectory, int64) {
	t.Helper()
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
	return store, dir, runID
}
