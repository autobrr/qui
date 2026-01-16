// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// worker processes transfers from the queue
func (s *Service) worker(id int) {
	log.Debug().Int("workerID", id).Msg("[TRANSFER] Worker started")

	for {
		select {
		case <-s.workerCtx.Done():
			log.Debug().Int("workerID", id).Msg("[TRANSFER] Worker stopping")
			return
		case transferID := <-s.queue:
			s.processTransfer(transferID)
		}
	}
}

// processTransfer executes the state machine for a single transfer
func (s *Service) processTransfer(id int64) {
	ctx := s.workerCtx

	t, err := s.store.Get(ctx, id)
	if err != nil {
		log.Error().Err(err).Int64("transferID", id).Msg("[TRANSFER] Failed to load transfer")
		return
	}

	// Skip if already in terminal state
	if t.State.IsTerminal() {
		log.Debug().
			Int64("id", t.ID).
			Str("state", string(t.State)).
			Msg("[TRANSFER] Skipping terminal transfer")
		return
	}

	log.Debug().
		Int64("id", t.ID).
		Str("hash", t.TorrentHash).
		Str("state", string(t.State)).
		Msg("[TRANSFER] Processing transfer")

	// State machine
	switch t.State {
	case models.TransferStatePending:
		s.doPrepare(ctx, t)

	case models.TransferStatePreparing:
		// If we're still in preparing state, it means prepare was interrupted
		// Restart from pending
		s.updateState(ctx, t, models.TransferStatePending, "")
		s.doPrepare(ctx, t)

	case models.TransferStateLinksCreating:
		// Links may be partial - attempt rollback and restart
		s.rollbackLinks(ctx, t)
		s.updateState(ctx, t, models.TransferStatePending, "")
		s.doPrepare(ctx, t)

	case models.TransferStateLinksCreated:
		s.doAddTorrent(ctx, t)

	case models.TransferStateAddingTorrent:
		// Check if torrent was actually added
		exists := s.checkTorrentExists(ctx, t.TargetInstanceID, t.TorrentHash)
		if exists {
			s.updateState(ctx, t, models.TransferStateTorrentAdded, "")
			s.continueAfterAdd(ctx, t)
		} else {
			// Torrent wasn't added - rollback and fail
			s.rollbackLinks(ctx, t)
			s.fail(ctx, t, "interrupted during add - rolled back")
		}

	case models.TransferStateTorrentAdded:
		s.continueAfterAdd(ctx, t)

	case models.TransferStateDeletingSource:
		s.doDeleteSource(ctx, t)
	}
}

// continueAfterAdd handles post-add logic
func (s *Service) continueAfterAdd(ctx context.Context, t *models.Transfer) {
	if t.DeleteFromSource {
		s.doDeleteSource(ctx, t)
	} else {
		s.markCompleted(ctx, t)
	}
}

// requeueTransfer puts a transfer back in the queue for processing
func (s *Service) requeueTransfer(t *models.Transfer) {
	select {
	case s.queue <- t.ID:
		log.Debug().Int64("id", t.ID).Msg("[TRANSFER] Requeued transfer")
	default:
		log.Warn().Int64("id", t.ID).Msg("[TRANSFER] Queue full, transfer will be picked up on next recovery")
	}
}
