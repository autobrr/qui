// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// ErrNotRetryable refuses a Retry of a Transfer that did not fail and was
// not cancelled.
var ErrNotRetryable = errors.New("only a failed or cancelled Transfer runs again")

// Retry runs a failed or cancelled Transfer or Export run again, and returns
// the id of the run. A Transfer that waits for its retry runs at once as the
// same Transfer, with the data that it kept. Any other starts again as a new
// Transfer or Export run with the same target and choices, with the checks
// of Start, and the old row stays.
func (s *Service) Retry(ctx context.Context, id int64) (int64, error) {
	if s.ctx.Err() != nil {
		return 0, errShutdown
	}
	record, err := s.deps.Store.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	switch {
	case record.State == StateFailed && record.FinishedAt.IsZero():
		if s.holds(id) {
			return id, nil
		}
		if err := s.deps.Store.clearRetry(ctx, id); err != nil {
			return 0, err
		}
		s.enqueue(record) //nolint:contextcheck // the Transfer outlives the request and runs in the context of the Service
		return id, nil
	case record.FinishedAt.IsZero() || (record.State != StateFailed && record.State != StateCancelled):
		return 0, fmt.Errorf("%w: the Transfer is %s", ErrNotRetryable, record.State)
	case record.Kind == KindExport:
		m := record.Members[0]
		newID, _, err := s.Export(ctx, ExportRequest{
			SourceInstanceID: record.SourceInstanceID,
			TargetInstanceID: record.TargetInstanceID,
			Hash:             m.Hash,
			Name:             m.Name,
			Size:             record.Size,
			Placement:        m.Placement,
			Paused:           record.Options.Paused,
			SkipChecking:     record.Options.SkipChecking == nil || *record.Options.SkipChecking,
			RuleID:           *record.RuleID,
			RuleName:         record.Steps[0].Detail,
		})
		return newID, err
	}
	placement, err := s.groupPlacement(ctx, record)
	if err != nil {
		return 0, err
	}
	return s.Start(ctx, Request{
		SourceInstanceID: record.SourceInstanceID,
		Hash:             record.Members[0].Hash,
		TargetInstanceID: record.TargetInstanceID,
		Placement:        placement,
		Options:          Options{After: record.Options.After, LeaveStopped: record.Options.LeaveStopped, ForcedWay: record.Options.ForcedWay},
		StartedBy:        StartedByUser,
	})
}

// clearRetry lets Transfer id, which waits for its retry, run now. It
// changes nothing once the Transfer runs again or ended.
func (s *Store) clearRetry(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE torrent_transfers SET retry_at = NULL WHERE id = ? AND state = ? AND finished_at IS NULL`,
		id, string(StateFailed)); err != nil {
		return fmt.Errorf("clear the retry of transfer %d: %w", id, err)
	}
	return nil
}

// groupPlacement returns the placement that the request of record named: the
// target folder of the top folder of the group, with the category and tags
// of the torrents on the target itself.
func (s *Service) groupPlacement(ctx context.Context, record *Record) (Placement, error) {
	target, err := s.instance(ctx, record.TargetInstanceID)
	if err != nil {
		return Placement{}, err
	}
	targetFiles, err := s.deps.Files.GetBackend(ctx, target.ID)
	if err != nil {
		return Placement{}, fmt.Errorf("read the files of %s: %w", target.Name, err)
	}
	saves := make([]string, len(record.Members))
	for i, m := range record.Members {
		saves[i] = m.Placement.SavePath
	}
	// A torrent on the Cross-seed instance of the target has no category
	// and no tags.
	onTarget := record.Members[max(0, slices.IndexFunc(record.Members, func(m Member) bool {
		return m.TargetInstanceID == record.TargetInstanceID
	}))].Placement
	placement := Placement{Category: onTarget.Category, Tags: onTarget.Tags}
	// A request that named no save path names none again, so the new
	// Transfer goes to the folder that the target has for the category by then.
	if !record.Members[0].Placement.SavePathOfTarget {
		placement.SavePath = topFolder(pathsOf(target, targetFiles, saves[0]), saves)
	}
	return placement, nil
}
