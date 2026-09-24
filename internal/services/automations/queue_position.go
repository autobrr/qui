// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"cmp"
	"context"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

type queueMoveInput struct {
	Hash            string
	TargetPosition  string // models.QueuePositionTop or models.QueuePositionBottom
	CurrentPriority int64  // qBittorrent queue position, 1-based
}

// planQueueMoves returns the topPriority and bottomPriority batches to send, in send order.
// qBittorrent keeps the relative order inside one call, but every topPrio call lands above
// the previous one (and every bottomPrio call below it), so split sets are sent from the far
// end inwards to keep the moved torrents in their current relative order.
func planQueueMoves(inputs []queueMoveInput, queueingEnabled bool, totalDownloading int, batchSize int) (topBatches, bottomBatches [][]string) {
	if !queueingEnabled {
		return nil, nil
	}

	var topSet, bottomSet []queueMoveInput
	for _, in := range inputs {
		switch in.TargetPosition {
		case models.QueuePositionTop:
			topSet = append(topSet, in)
		case models.QueuePositionBottom:
			bottomSet = append(bottomSet, in)
		}
	}

	if batches := sortedQueueBatches(topSet, 1, batchSize); batches != nil {
		slices.Reverse(batches)
		topBatches = batches
	}
	bottomBatches = sortedQueueBatches(bottomSet, int64(totalDownloading-len(bottomSet)+1), batchSize)
	return topBatches, bottomBatches
}

// sortedQueueBatches sorts set by current position and chunks it, or returns nil when the
// set already sits at the contiguous positions starting at firstPosition.
func sortedQueueBatches(set []queueMoveInput, firstPosition int64, batchSize int) [][]string {
	if len(set) == 0 {
		return nil
	}
	slices.SortFunc(set, func(a, b queueMoveInput) int { return cmp.Compare(a.CurrentPriority, b.CurrentPriority) })

	inPlace := true
	hashes := make([]string, len(set))
	for i, in := range set {
		hashes[i] = in.Hash
		if in.CurrentPriority != firstPosition+int64(i) {
			inPlace = false
		}
	}
	if inPlace {
		return nil
	}
	return limitHashBatch(hashes, batchSize)
}

// planQueuePositionMoves reads the instance's queueing preference and plans the moves.
// Queueing turned off after the rule was saved skips only this action for the pass.
func (s *Service) planQueuePositionMoves(ctx context.Context, instanceID int, inputs []queueMoveInput, torrents []qbt.Torrent) (topBatches, bottomBatches [][]string) {
	prefs, err := s.syncManager.GetAppPreferences(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to read queueing preference, skipping queue position moves")
		return nil, nil
	}
	if !prefs.QueueingEnabled {
		log.Debug().Int("instanceID", instanceID).Int("count", len(inputs)).Msg("automations: queueing disabled, skipping queue position moves")
	}

	totalDownloading := 0
	for _, t := range torrents {
		if t.Priority > 0 {
			totalDownloading++
		}
	}
	return planQueueMoves(inputs, prefs.QueueingEnabled, totalDownloading, s.cfg.MaxBatchHashes)
}
