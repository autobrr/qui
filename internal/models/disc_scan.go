// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// Disc scan statuses.
const (
	DiscScanStatusPending   = "pending"
	DiscScanStatusScanning  = "scanning"
	DiscScanStatusCompleted = "completed"
	DiscScanStatusFailed    = "failed"
	DiscScanStatusCanceled  = "canceled"
)

// DiscScanRun is one queued or running BDInfo job on one Disc, and after
// completion its cached Disc report.
type DiscScanRun struct {
	ID             int64      `json:"id"`
	InstanceID     int        `json:"instanceId"`
	TorrentHash    string     `json:"torrentHash"`
	DiscPath       string     `json:"discPath"`
	ResolvedPath   string     `json:"resolvedPath"`
	Status         string     `json:"status"`
	ErrorMessage   string     `json:"errorMessage,omitempty"`
	ProcessedBytes int64      `json:"processedBytes"`
	TotalBytes     int64      `json:"totalBytes"`
	QueuePosition  int        `json:"queuePosition,omitempty"`
	Report         string     `json:"report,omitempty"`
	QuickSummary   string     `json:"quickSummary,omitempty"`
	ForumsBlock    string     `json:"forumsBlock,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
}

// Active reports whether the run is still queued or running.
func (r *DiscScanRun) Active() bool {
	return r.Status == DiscScanStatusPending || r.Status == DiscScanStatusScanning
}

// DiscScanStore handles database operations for Disc scans.
type DiscScanStore struct {
	db dbinterface.Querier
}

// NewDiscScanStore creates a new DiscScanStore.
func NewDiscScanStore(db dbinterface.Querier) *DiscScanStore {
	return &DiscScanStore{db: db}
}

// discScanColumns is the shared projection. queue_position counts the pending
// rows at or before this one, so a queued run knows where it stands.
const discScanColumns = `
	id, instance_id, torrent_hash, disc_path, resolved_path, status, error_message,
	processed_bytes, total_bytes,
	CASE WHEN status = 'pending'
	     THEN (SELECT COUNT(*) FROM disc_scan_runs p WHERE p.status = 'pending' AND p.id <= d.id)
	     ELSE 0 END,
	report, quick_summary, forums_block, created_at, started_at, completed_at`

// Create inserts a pending run and returns its id.
func (s *DiscScanStore) Create(ctx context.Context, instanceID int, torrentHash, discPath, resolvedPath string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO disc_scan_runs (instance_id, torrent_hash, disc_path, resolved_path)
		VALUES (?, ?, ?, ?)
		RETURNING id
	`, instanceID, torrentHash, discPath, resolvedPath).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert disc scan run: %w", err)
	}
	return id, nil
}

// GetByInstance returns one run scoped to an instance, or nil.
func (s *DiscScanStore) GetByInstance(ctx context.Context, instanceID int, runID int64) (*DiscScanRun, error) {
	return s.one(ctx, `SELECT `+discScanColumns+` FROM disc_scan_runs d WHERE id = ? AND instance_id = ?`, runID, instanceID)
}

// Latest returns the newest run for the cache key (instance, resolved path), or nil.
func (s *DiscScanStore) Latest(ctx context.Context, instanceID int, resolvedPath string) (*DiscScanRun, error) {
	return s.one(ctx, `
		SELECT `+discScanColumns+` FROM disc_scan_runs d
		WHERE instance_id = ? AND resolved_path = ?
		ORDER BY id DESC LIMIT 1
	`, instanceID, resolvedPath)
}

// NextPending returns the oldest pending run, or nil when the queue is empty.
func (s *DiscScanStore) NextPending(ctx context.Context) (*DiscScanRun, error) {
	return s.one(ctx, `
		SELECT `+discScanColumns+` FROM disc_scan_runs d
		WHERE status = 'pending'
		ORDER BY id ASC LIMIT 1
	`)
}

// ListNewestForTorrent returns the newest run per Disc path for one torrent.
func (s *DiscScanStore) ListNewestForTorrent(ctx context.Context, instanceID int, torrentHash string) ([]*DiscScanRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+discScanColumns+` FROM disc_scan_runs d
		WHERE id IN (
			SELECT MAX(id) FROM disc_scan_runs
			WHERE instance_id = ? AND torrent_hash = ?
			GROUP BY disc_path
		)
		ORDER BY disc_path
	`, instanceID, torrentHash)
	if err != nil {
		return nil, fmt.Errorf("list disc scan runs: %w", err)
	}
	defer rows.Close()

	runs := make([]*DiscScanRun, 0)
	for rows.Next() {
		run, err := scanDiscScanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// MarkScanning moves a pending run to scanning. Returns false when the run
// was no longer pending, for example because it was canceled meanwhile.
func (s *DiscScanStore) MarkScanning(ctx context.Context, runID int64) (bool, error) {
	return s.transition(ctx, `
		UPDATE disc_scan_runs
		SET status = 'scanning', started_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'pending'
	`, runID)
}

// UpdateProgress stores the byte counters of a running scan.
func (s *DiscScanStore) UpdateProgress(ctx context.Context, runID int64, processed, total int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE disc_scan_runs SET processed_bytes = ?, total_bytes = ?
		WHERE id = ? AND status = 'scanning'
	`, processed, total, runID)
	return err
}

// MarkCompleted stores the report on a scanning run.
func (s *DiscScanStore) MarkCompleted(ctx context.Context, runID int64, report, quickSummary, forumsBlock string) (bool, error) {
	return s.transition(ctx, `
		UPDATE disc_scan_runs
		SET status = 'completed', report = ?, quick_summary = ?, forums_block = ?,
		    processed_bytes = total_bytes, completed_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'scanning'
	`, report, quickSummary, forumsBlock, runID)
}

// MarkFailed fails a scanning run with a message.
func (s *DiscScanStore) MarkFailed(ctx context.Context, runID int64, errorMessage string) (bool, error) {
	return s.transition(ctx, `
		UPDATE disc_scan_runs
		SET status = 'failed', error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'scanning'
	`, errorMessage, runID)
}

// MarkCanceled cancels a pending or scanning run. Returns false when the run
// had already finished.
func (s *DiscScanStore) MarkCanceled(ctx context.Context, runID int64) (bool, error) {
	return s.transition(ctx, `
		UPDATE disc_scan_runs
		SET status = 'canceled', completed_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN ('pending', 'scanning')
	`, runID)
}

// MarkInterrupted fails every scanning run. Called once at startup, because a
// scan does not survive a process restart.
func (s *DiscScanStore) MarkInterrupted(ctx context.Context, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE disc_scan_runs
		SET status = 'failed', error_message = ?, completed_at = CURRENT_TIMESTAMP
		WHERE status = 'scanning'
	`, errorMessage)
	return err
}

func (s *DiscScanStore) transition(ctx context.Context, query string, args ...any) (bool, error) {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *DiscScanStore) one(ctx context.Context, query string, args ...any) (*DiscScanRun, error) {
	run, err := scanDiscScanRun(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return run, err
}

func scanDiscScanRun(row rowScanner) (*DiscScanRun, error) {
	var run DiscScanRun
	var errorMessage, report, quickSummary, forumsBlock sql.NullString
	var startedAt, completedAt sql.NullTime

	err := row.Scan(
		&run.ID, &run.InstanceID, &run.TorrentHash, &run.DiscPath, &run.ResolvedPath, &run.Status, &errorMessage,
		&run.ProcessedBytes, &run.TotalBytes, &run.QueuePosition,
		&report, &quickSummary, &forumsBlock, &run.CreatedAt, &startedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}
	run.ErrorMessage = errorMessage.String
	run.Report = report.String
	run.QuickSummary = quickSummary.String
	run.ForumsBlock = forumsBlock.String
	if startedAt.Valid {
		run.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	return &run, nil
}
