// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package dbshrink lets the user shrink the SQLite database file: it checks
// the free disk space, stores the "shrink pending" mark of the database
// module, requests a Restart, and reports the outcome of the shrink that the
// database module ran at startup.
package dbshrink

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/services/systemnotice"
)

// The SQLite documentation gives a need of up to two times the size of the
// database file for a VACUUM.
const needFactor = 2

var (
	ErrUnsupported   = database.ErrShrinkUnsupported
	ErrRestartBusy   = errors.New("an update or restart is already running")
	ErrTooLittleDisk = errors.New("the disk has too little free space for the shrink")
)

// Restarter is the Restart module: the lock over each system action, and the
// request that restarts qui.
type Restarter interface {
	TryLock() bool
	Unlock()
	Request() error
}

// Result is the outcome of a shrink that succeeded.
type Result struct {
	BeforeBytes int64 `json:"beforeBytes"`
	AfterBytes  int64 `json:"afterBytes"`
}

type Status struct {
	// Available is false on Postgres, which has no file to shrink.
	Available bool  `json:"available"`
	FileBytes int64 `json:"fileBytes"`
	FreeBytes int64 `json:"freeBytes"`
	NeedBytes int64 `json:"needBytes"`
	// RestartAvailable is false where qui cannot restart itself. The shrink
	// then runs at the next start.
	RestartAvailable bool `json:"restartAvailable"`
	Pending          bool `json:"pending"`
	// Result is the outcome of the shrink at the last start, until the web UI
	// dismisses it.
	Result *Result `json:"result,omitempty"`
}

type Service struct {
	db        *database.DB
	notices   *systemnotice.Module
	restarter Restarter
	freeBytes func(ctx context.Context, dir string) (int64, error)

	mu     sync.Mutex
	result *Result
}

// New returns the service. restarter is nil where Restart is not available.
func New(db *database.DB, notices *systemnotice.Module, restarter Restarter) *Service {
	return &Service{db: db, notices: notices, restarter: restarter, freeBytes: diskFreeBytes}
}

// Report takes the outcome of the shrink that the database module ran at
// startup. A success waits for the web UI, and a failure opens the System
// notice "Shrink failed".
func (s *Service) Report(startup database.ShrinkResult) {
	if startup.Err != nil {
		s.notices.OpenWithReason(systemnotice.ShrinkFailed, string(startup.Failure), startup.Err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result = &Result{BeforeBytes: startup.BeforeBytes, AfterBytes: startup.AfterBytes}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	path := s.db.SQLitePath()
	if path == "" {
		return Status{}, nil
	}
	status := Status{Available: true, RestartAvailable: s.restarter != nil}
	info, err := os.Stat(path)
	if err != nil {
		return Status{}, fmt.Errorf("read database file size: %w", err)
	}
	status.FileBytes = info.Size()
	status.NeedBytes = needFactor * status.FileBytes
	dataDir, err := s.db.SQLiteFileDir()
	if err != nil {
		return Status{}, fmt.Errorf("resolve the database directory: %w", err)
	}
	if status.FreeBytes, err = s.freeBytes(ctx, dataDir); err != nil {
		return Status{}, fmt.Errorf("read free disk space: %w", err)
	}
	if status.Pending, err = s.db.ShrinkPending(ctx); err != nil {
		return Status{}, err
	}
	s.mu.Lock()
	status.Result = s.result
	s.mu.Unlock()
	return status, nil
}

// Request stores the "shrink pending" mark and requests a Restart under the
// lock of the Restart module. It reports false when Restart is not available:
// the shrink then runs at the next start. A stored mark closes the notice
// "Shrink failed". It returns ErrTooLittleDisk when the free disk space is
// below the need, and stores no mark then.
func (s *Service) Request(ctx context.Context) (restarting bool, err error) {
	status, err := s.Status(ctx)
	if err != nil {
		return false, err
	}
	if !status.Available {
		return false, ErrUnsupported
	}
	if status.FreeBytes < status.NeedBytes {
		return false, fmt.Errorf("%w: %d bytes free, %d bytes needed", ErrTooLittleDisk, status.FreeBytes, status.NeedBytes)
	}
	if s.restarter == nil {
		return false, s.storeMark(ctx)
	}
	if !s.restarter.TryLock() {
		return false, ErrRestartBusy
	}
	if err := s.storeMark(ctx); err != nil {
		s.restarter.Unlock()
		return false, err
	}
	if err := s.restarter.Request(); err != nil {
		s.restarter.Unlock()
		return false, fmt.Errorf("the shrink runs at the next start, but the restart was refused: %w", err)
	}
	return true, nil
}

// storeMark stores the "shrink pending" mark. The new request replaces the
// failure of an earlier one.
func (s *Service) storeMark(ctx context.Context) error {
	if err := s.db.RequestShrink(ctx); err != nil {
		return err
	}
	s.notices.Close(systemnotice.ShrinkFailed)
	return nil
}

// DismissResult forgets the outcome of the last shrink, after the web UI
// showed it.
func (s *Service) DismissResult() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result = nil
}

func diskFreeBytes(ctx context.Context, dir string) (int64, error) {
	stat, err := local.NewBackend().Statfs(ctx, dir)
	if err != nil {
		return 0, err
	}
	return stat.BytesAvailable, nil
}
