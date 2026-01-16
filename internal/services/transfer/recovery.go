// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// recoverInterrupted handles transfers that were in progress when the app stopped
func (s *Service) recoverInterrupted() {
	ctx := context.Background()

	// Get all in-progress transfers
	states := []models.TransferState{
		models.TransferStatePending,
		models.TransferStatePreparing,
		models.TransferStateLinksCreating,
		models.TransferStateLinksCreated,
		models.TransferStateAddingTorrent,
		models.TransferStateTorrentAdded,
		models.TransferStateDeletingSource,
	}

	transfers, err := s.store.ListByStates(ctx, states, 100, 0)
	if err != nil {
		log.Error().Err(err).Msg("[TRANSFER] Failed to list interrupted transfers")
		return
	}

	if len(transfers) == 0 {
		return
	}

	log.Info().Int("count", len(transfers)).Msg("[TRANSFER] Recovering interrupted transfers")

	for _, t := range transfers {
		s.recoverTransfer(ctx, t)
	}
}

// tryEnqueue attempts a non-blocking enqueue; logs a warning if the queue is full.
func (s *Service) tryEnqueue(id int64) {
	select {
	case s.queue <- id:
	default:
		log.Warn().
			Int64("id", id).
			Msg("[TRANSFER] Queue full, transfer will be picked up later")
	}
}

// recoverTransfer handles recovery for a single interrupted transfer
func (s *Service) recoverTransfer(ctx context.Context, t *models.Transfer) {
	log.Debug().
		Int64("id", t.ID).
		Str("state", string(t.State)).
		Str("hash", t.TorrentHash).
		Msg("[TRANSFER] Recovering transfer")

	switch t.State {
	case models.TransferStatePending:
		// Safe to restart
		s.tryEnqueue(t.ID)

	case models.TransferStatePreparing:
		// Safe to restart from beginning
		s.updateState(ctx, t, models.TransferStatePending, "")
		s.tryEnqueue(t.ID)

	case models.TransferStateLinksCreating:
		// Links may be partial - attempt rollback and restart
		s.rollbackLinks(ctx, t)
		s.updateState(ctx, t, models.TransferStatePending, "")
		s.tryEnqueue(t.ID)

	case models.TransferStateLinksCreated:
		// Links are done, continue with add
		s.tryEnqueue(t.ID)

	case models.TransferStateAddingTorrent:
		// Check if torrent was actually added
		exists := s.checkTorrentExists(ctx, t.TargetInstanceID, t.TorrentHash)
		if exists {
			// Torrent was added - continue
			s.updateState(ctx, t, models.TransferStateTorrentAdded, "")
		} else {
			// Torrent wasn't added - rollback links and restart
			s.rollbackLinks(ctx, t)
			s.updateState(ctx, t, models.TransferStatePending, "")
		}
		s.tryEnqueue(t.ID)

	case models.TransferStateTorrentAdded:
		// Torrent is on target - continue to delete source if needed
		s.tryEnqueue(t.ID)

	case models.TransferStateDeletingSource:
		// Check if source still has torrent
		exists := s.checkTorrentExists(ctx, t.SourceInstanceID, t.TorrentHash)
		if exists {
			// Still there - continue deletion
			s.tryEnqueue(t.ID)
		} else {
			// Already deleted - mark complete
			s.markCompleted(ctx, t)
		}
	}
}

// ReconcileInterruptedTransfers marks stale in-progress transfers as failed
// Called during startup to clean up any truly stuck transfers
func (s *Service) ReconcileInterruptedTransfers(ctx context.Context) (int64, error) {
	// Mark very old in-progress transfers as failed
	// This is a safety net for transfers that got stuck
	stuckStates := []models.TransferState{
		models.TransferStateLinksCreating,
		models.TransferStateAddingTorrent,
	}

	count, err := s.store.MarkInterrupted(ctx, stuckStates, "transfer interrupted (application restarted)")
	if err != nil {
		return 0, err
	}

	if count > 0 {
		log.Info().Int64("count", count).Msg("[TRANSFER] Reconciled stuck transfers")
	}

	return count, nil
}

// periodicRecovery runs in the background and re-enqueues pending transfers
// that may have been missed due to a full queue.
func (s *Service) periodicRecovery() {
	ticker := time.NewTicker(s.recoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.workerCtx.Done():
			return
		case <-ticker.C:
			s.requeuePending()
		}
	}
}

// requeuePending queries pending transfers and attempts to enqueue them.
func (s *Service) requeuePending() {
	ctx := context.Background()

	transfers, err := s.store.ListByStates(ctx, []models.TransferState{models.TransferStatePending}, 100, 0)
	if err != nil {
		log.Error().Err(err).Msg("[TRANSFER] Failed to list pending transfers for requeue")
		return
	}

	if len(transfers) == 0 {
		return
	}

	log.Debug().Int("count", len(transfers)).Msg("[TRANSFER] Re-enqueuing pending transfers")

	for _, t := range transfers {
		s.tryEnqueue(t.ID)
	}
}
