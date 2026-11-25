// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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

const needFactor = 2

var (
	ErrUnsupported   = database.ErrShrinkUnsupported
	ErrRestartBusy   = errors.New("an update or restart is already running")
	ErrTooLittleDisk = errors.New("the disk has too little free space for the shrink")
)

type Restarter interface {
	TryLock() bool
	Unlock()
	Request() error
}

type Result struct {
	BeforeBytes int64 `json:"beforeBytes"`
	AfterBytes  int64 `json:"afterBytes"`
}

type Status struct {
	Available        bool    `json:"available"`
	FileBytes        int64   `json:"fileBytes"`
	FreeBytes        int64   `json:"freeBytes"`
	NeedBytes        int64   `json:"needBytes"`
	RestartAvailable bool    `json:"restartAvailable"`
	Pending          bool    `json:"pending"`
	Result           *Result `json:"result,omitempty"`
}

type Service struct {
	db        *database.DB
	notices   *systemnotice.Module
	restarter Restarter
	freeBytes func(ctx context.Context, dir string) (int64, error)

	mu     sync.Mutex
	result *Result
}

func New(db *database.DB, notices *systemnotice.Module, restarter Restarter) *Service {
	return &Service{db: db, notices: notices, restarter: restarter, freeBytes: diskFreeBytes}
}

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

func (s *Service) storeMark(ctx context.Context) error {
	if err := s.db.RequestShrink(ctx); err != nil {
		return err
	}
	s.notices.Close(systemnotice.ShrinkFailed)
	return nil
}

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
