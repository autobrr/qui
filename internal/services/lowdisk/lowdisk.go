// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package lowdisk

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/services/systemnotice"
)

const (
	checkInterval = time.Minute
	maxLimitBytes = 1_000_000_000
)

func Limit(totalBytes int64) int64 {
	return min(maxLimitBytes, totalBytes/20)
}

type disk struct {
	freeBytes  int64
	totalBytes int64
}

type Monitor struct {
	db       *database.DB
	notices  *systemnotice.Module
	readDisk func(ctx context.Context, dir string) (disk, error)

	mu          sync.Mutex
	loaded      bool
	low         bool
	dismissed   bool
	stored      bool
	failureSeen uint64
	lastErr     string
}

func New(db *database.DB, notices *systemnotice.Module) *Monitor {
	m := &Monitor{db: db, notices: notices, readDisk: readDisk}
	notices.OnDismiss(systemnotice.LowDisk, m.dismiss)
	return m
}

func (m *Monitor) Run(ctx context.Context) {
	if m.db.SQLitePath() == "" {
		return
	}
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		m.logCheck(m.Check(ctx))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Monitor) logCheck(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case err == nil:
		m.lastErr = ""
	case err.Error() != m.lastErr:
		m.lastErr = err.Error()
		log.Warn().Err(err).Msg("low disk check failed")
	}
}

func (m *Monitor) Check(ctx context.Context) error {
	if m.db.SQLitePath() == "" {
		return nil
	}
	dir, err := m.db.SQLiteFileDir()
	if err != nil {
		return fmt.Errorf("resolve the database directory: %w", err)
	}
	d, err := m.readDisk(ctx, dir)
	if err != nil {
		return fmt.Errorf("read free disk space: %w", err)
	}
	limit := Limit(d.totalBytes)

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadLocked(ctx); err != nil {
		return err
	}
	switch {
	case d.freeBytes < limit:
		m.low = true
	case d.freeBytes >= limit+limit/10:
		m.low = false
	}
	failure, _ := m.notices.Opening(systemnotice.RecordingStopped)
	if !m.low || failure != m.failureSeen {
		m.dismissed = false
	}
	m.failureSeen = failure
	switch {
	case !m.low:
		m.notices.Close(systemnotice.LowDisk)
	case !m.dismissed:
		cause := fmt.Sprintf("%s free of %s on the disk of the database file. The limit is %s.",
			formatBytes(d.freeBytes), formatBytes(d.totalBytes), formatBytes(limit))
		m.notices.Open(systemnotice.LowDisk, cause) //nolint:contextcheck // the notification has its own timeout, as each System notice does
	}
	return m.storeLocked(ctx, m.dismissed)
}

func (m *Monitor) dismiss(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadLocked(ctx); err != nil {
		return err
	}
	if _, isOpen := m.notices.Opening(systemnotice.LowDisk); !isOpen {
		return nil
	}
	if err := m.storeLocked(ctx, true); err != nil {
		return err
	}
	m.dismissed = true
	m.failureSeen, _ = m.notices.Opening(systemnotice.RecordingStopped)
	m.notices.Close(systemnotice.LowDisk)
	return nil
}

func (m *Monitor) loadLocked(ctx context.Context) error {
	if m.loaded {
		return nil
	}
	var count int
	if err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_notice_dismissals WHERE kind = ?", string(systemnotice.LowDisk)).Scan(&count); err != nil {
		return fmt.Errorf("read the dismiss of the low disk notice: %w", err)
	}
	m.stored = count > 0
	m.dismissed = m.stored
	m.low = m.stored
	m.loaded = true
	return nil
}

func (m *Monitor) storeLocked(ctx context.Context, dismissed bool) error {
	if m.stored == dismissed {
		return nil
	}
	query := "DELETE FROM system_notice_dismissals WHERE kind = ?"
	if dismissed {
		query = "INSERT INTO system_notice_dismissals (kind) VALUES (?) ON CONFLICT (kind) DO NOTHING"
	}
	if _, err := m.db.ExecContext(ctx, query, string(systemnotice.LowDisk)); err != nil {
		return fmt.Errorf("store the dismiss of the low disk notice: %w", err)
	}
	m.stored = dismissed
	return nil
}

func readDisk(ctx context.Context, dir string) (disk, error) {
	stat, err := local.NewBackend().Statfs(ctx, dir)
	if err != nil {
		return disk{}, err
	}
	return disk{freeBytes: stat.BytesAvailable, totalBytes: stat.BytesTotal}, nil
}

func formatBytes(n int64) string {
	if n >= 1_000_000_000 {
		return strconv.FormatFloat(float64(n/100_000_000)/10, 'f', -1, 64) + " GB"
	}
	return strconv.FormatInt(n/1_000_000, 10) + " MB"
}
