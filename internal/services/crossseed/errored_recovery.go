// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"slices"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/externalprograms"
)

// RunExternalProgram sends the Execution Request for the External Program
// that After injection selects, if any, for one added cross-seed. hashes are
// the v1 and v2 hashes of that torrent.
func (s *Service) RunExternalProgram(ctx context.Context, instanceID int, hashes []string) {
	if s.postInjectionHook != nil {
		s.postInjectionHook(ctx, instanceID, hashes)
		return
	}

	s.executeExternalProgram(ctx, instanceID, hashes)
}

// findAddedTorrent waits until an added torrent is in the sync cache and returns it.
// qBittorrent lists a torrent that has a v2 hash under the first 40 characters of
// that hash, so the lookup also tries that form.
func (s *Service) findAddedTorrent(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
	lookup := slices.Clone(hashes)
	for _, hash := range hashes {
		if len(hash) == 64 {
			lookup = append(lookup, hash[:40])
		}
	}
	s.waitForTorrentAvailability(ctx, instanceID, lookup, crossSeedRenameWaitTimeout)
	return s.syncManager.HasTorrentByAnyHash(ctx, instanceID, lookup)
}

// executeExternalProgram runs the configured external program for a successfully injected torrent.
//
// WARNING: externalprograms limits how many programs run at the same time, but a launcher run
// (a terminal emulator, or any run on Windows) releases its slot after the start, so rapid
// injections can still start many concurrent processes.
func (s *Service) executeExternalProgram(ctx context.Context, instanceID int, hashes []string) {
	if s.externalProgramService == nil {
		return
	}

	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	// Get current settings to check if external program is configured
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get automation settings for external program execution")
		return
	}

	if settings.RunExternalProgramID == nil {
		return // No external program configured
	}

	programID := *settings.RunExternalProgramID

	// Execute in a separate goroutine to avoid blocking the cross-seed operation
	go func() {
		targetTorrent, found, err := s.findAddedTorrent(ctx, instanceID, hashes)
		if err != nil {
			log.Error().Err(err).Int("instanceId", instanceID).Strs("torrentHashes", hashes).Msg("Failed to get torrent for external program execution")
			return
		}

		if !found || targetTorrent == nil {
			log.Error().Strs("torrentHashes", hashes).Msg("Torrent not found for external program execution")
			return
		}

		log.Debug().
			Int("instanceId", instanceID).
			Strs("torrentHashes", hashes).
			Int("programId", programID).
			Msg("Executing external program for cross-seed injection")

		// Execute using the shared external programs service
		result := s.externalProgramService.Execute(ctx, externalprograms.ExecuteRequest{
			ProgramID:  programID,
			Torrent:    targetTorrent,
			InstanceID: instanceID,
		})

		if !result.Success {
			log.Error().
				Err(result.Error).
				Int("instanceId", instanceID).
				Strs("torrentHashes", hashes).
				Int("programId", programID).
				Msg("External program failed for cross-seed injection")
		} else {
			log.Debug().
				Int("instanceId", instanceID).
				Strs("torrentHashes", hashes).
				Int("programId", programID).
				Str("message", result.Message).
				Msg("External program executed for cross-seed injection")
		}
	}()
}

// recoverErroredTorrents identifies errored torrents and performs batched recheck to fix their state.
// This function blocks until all recoverable torrents are processed, ensuring they can participate
// in the current automation run. API calls are batched to minimize qBittorrent load.
func (s *Service) recoverErroredTorrents(ctx context.Context, instanceID int, torrents []qbt.Torrent) error {
	// Identify errored torrents
	var erroredTorrents []qbt.Torrent
	for _, torrent := range torrents {
		if torrent.State == qbt.TorrentStateError || torrent.State == qbt.TorrentStateMissingFiles {
			erroredTorrents = append(erroredTorrents, torrent)
		}
	}

	if len(erroredTorrents) == 0 {
		return nil
	}

	log.Info().
		Int("instanceID", instanceID).
		Int("count", len(erroredTorrents)).
		Msg("Found errored torrents, attempting batched recovery")

	minCompletionProgress := 1.0 - defaultSizeMismatchTolerancePercent/100.0

	// Get qBittorrent app preferences once for disk cache TTL
	appPrefs, err := s.syncManager.GetAppPreferences(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get app preferences, using default cache timeout")
		appPrefs.DiskCacheTTL = 60
	}

	cacheTimeout := max(time.Duration(appPrefs.DiskCacheTTL)*time.Second, 1*time.Second)

	// Collect all hashes and build lookup maps
	allHashes := make([]string, len(erroredTorrents))
	torrentByHash := make(map[string]qbt.Torrent, len(erroredTorrents))
	for i, t := range erroredTorrents {
		allHashes[i] = t.Hash
		torrentByHash[t.Hash] = t
	}

	// Track per-torrent state
	type torrentRecoveryState struct {
		errorRetries      int
		progress          float64
		checkingStartTime time.Time // When torrent entered checking state (zero if not yet checking)
	}
	states := make(map[string]*torrentRecoveryState, len(erroredTorrents))
	for _, hash := range allHashes {
		states[hash] = &torrentRecoveryState{}
	}

	const (
		maxErrorRetries = 3
		maxCheckingTime = 25 * time.Minute // Per-torrent timeout once actively checking
		pollInterval    = 2 * time.Second
	)

	// performBatchRecheck triggers recheck on hashes and polls until all complete or timeout.
	// Returns hashes that completed within threshold and those that didn't.
	// Timeout is per-torrent: only starts counting when torrent enters checkingDL/checkingUP state.
	performBatchRecheck := func(hashes []string) (complete, incomplete []string) {
		if len(hashes) == 0 {
			return nil, nil
		}

		// Reset checking start times for this phase
		for _, h := range hashes {
			states[h].checkingStartTime = time.Time{}
		}

		// Trigger recheck on all hashes
		if err := s.syncManager.BulkAction(ctx, instanceID, hashes, "recheck"); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to trigger batch recheck")
			return nil, hashes
		}

		pending := make(map[string]bool, len(hashes))
		for _, h := range hashes {
			pending[h] = true
		}

		for len(pending) > 0 {
			if ctx.Err() != nil {
				for h := range pending {
					incomplete = append(incomplete, h)
				}
				return complete, incomplete
			}

			// Collect pending hashes for batch query
			pendingHashes := make([]string, 0, len(pending))
			for h := range pending {
				pendingHashes = append(pendingHashes, h)
			}

			// Batch query all pending torrents
			currentTorrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Hashes: pendingHashes})
			if err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to check recheck progress")
				time.Sleep(pollInterval)
				continue
			}

			// Build lookup map
			currentByHash := make(map[string]qbt.Torrent, len(currentTorrents))
			for _, t := range currentTorrents {
				currentByHash[t.Hash] = t
			}

			// Process each pending torrent
			for hash := range pending {
				current, exists := currentByHash[hash]
				if !exists {
					log.Warn().Str("hash", hash).Msg("Torrent disappeared during recheck")
					delete(pending, hash)
					continue
				}

				state := current.State

				// Recheck completed to paused/stopped state
				if state == qbt.TorrentStatePausedDl || state == qbt.TorrentStatePausedUp ||
					state == qbt.TorrentStateStoppedDl || state == qbt.TorrentStateStoppedUp {
					states[hash].progress = current.Progress
					if current.Progress >= minCompletionProgress {
						complete = append(complete, hash)
					} else {
						incomplete = append(incomplete, hash)
					}
					delete(pending, hash)
					continue
				}

				// Torrent is actively checking - track start time
				if state == qbt.TorrentStateCheckingDl || state == qbt.TorrentStateCheckingUp ||
					state == qbt.TorrentStateCheckingResumeData {
					if states[hash].checkingStartTime.IsZero() {
						states[hash].checkingStartTime = time.Now()
					} else if time.Since(states[hash].checkingStartTime) > maxCheckingTime {
						// Torrent has been checking for too long
						log.Warn().
							Str("hash", hash).
							Dur("checkingDuration", time.Since(states[hash].checkingStartTime)).
							Msg("Torrent recheck timed out after extended checking")
						states[hash].progress = current.Progress
						incomplete = append(incomplete, hash)
						delete(pending, hash)
					}
					continue
				}

				// Torrent went back to error/missingFiles - retry recheck
				if state == qbt.TorrentStateError || state == qbt.TorrentStateMissingFiles {
					states[hash].errorRetries++
					states[hash].checkingStartTime = time.Time{} // Reset for retry
					if states[hash].errorRetries > maxErrorRetries {
						log.Warn().
							Int("instanceID", instanceID).
							Str("hash", hash).
							Str("state", string(state)).
							Int("retries", states[hash].errorRetries).
							Msg("Torrent still in error state after max retries")
						states[hash].progress = current.Progress
						delete(pending, hash)
						continue
					}

					log.Debug().
						Int("instanceID", instanceID).
						Str("hash", hash).
						Str("state", string(state)).
						Int("retry", states[hash].errorRetries).
						Msg("Torrent in error state, retrying recheck")

					// Pause then recheck again
					if err := s.syncManager.BulkAction(ctx, instanceID, []string{hash}, "pause"); err != nil {
						log.Warn().Err(err).Str("hash", hash).Msg("Failed to pause errored torrent for retry")
					}
					time.Sleep(500 * time.Millisecond)
					if err := s.syncManager.BulkAction(ctx, instanceID, []string{hash}, "recheck"); err != nil {
						log.Warn().Err(err).Str("hash", hash).Msg("Failed to recheck errored torrent for retry")
					}
					continue
				}

				// Still queued or other state - keep waiting (no timeout for queued torrents)
			}

			if len(pending) > 0 {
				time.Sleep(pollInterval)
			}
		}

		return complete, incomplete
	}

	// Phase 1: Pause all and trigger first recheck
	if err := s.syncManager.BulkAction(ctx, instanceID, allHashes, "pause"); err != nil {
		return fmt.Errorf("pause errored torrents: %w", err)
	}

	complete, incomplete := performBatchRecheck(allHashes)

	// Resume all complete torrents from phase 1
	if len(complete) > 0 {
		if err := s.syncManager.BulkAction(ctx, instanceID, complete, "resume"); err != nil {
			log.Warn().Err(err).Int("count", len(complete)).Msg("Failed to resume recovered torrents")
		}
		for _, hash := range complete {
			t := torrentByHash[hash]
			log.Info().
				Int("instanceID", instanceID).
				Str("hash", hash).
				Str("name", t.Name).
				Float64("progress", states[hash].progress).
				Msg("Successfully recovered errored torrent after first recheck")
		}
	}

	// Phase 2: Second recheck for incomplete torrents after cache timeout
	var phase2Complete []string
	if len(incomplete) > 0 {
		log.Info().
			Int("instanceID", instanceID).
			Int("count", len(incomplete)).
			Dur("cacheTimeout", cacheTimeout).
			Msg("Waiting for disk cache timeout before second recheck of incomplete torrents")

		select {
		case <-time.After(cacheTimeout):
		case <-ctx.Done():
			return ctx.Err()
		}

		// Reset error retries for phase 2
		for _, hash := range incomplete {
			states[hash].errorRetries = 0
		}

		var stillIncomplete []string
		phase2Complete, stillIncomplete = performBatchRecheck(incomplete)

		// Resume phase 2 completions
		if len(phase2Complete) > 0 {
			if err := s.syncManager.BulkAction(ctx, instanceID, phase2Complete, "resume"); err != nil {
				log.Warn().Err(err).Int("count", len(phase2Complete)).Msg("Failed to resume torrents recovered in phase 2")
			}
			for _, hash := range phase2Complete {
				t := torrentByHash[hash]
				log.Info().
					Int("instanceID", instanceID).
					Str("hash", hash).
					Str("name", t.Name).
					Float64("progress", states[hash].progress).
					Msg("Successfully recovered errored torrent after second recheck")
			}
		}

		// Log failures
		for _, hash := range stillIncomplete {
			t := torrentByHash[hash]
			log.Warn().
				Int("instanceID", instanceID).
				Str("hash", hash).
				Str("name", t.Name).
				Float64("progress", states[hash].progress).
				Msg("Errored torrent not complete after second recheck, recovery failed")
		}
	}

	recoveredCount := len(complete) + len(phase2Complete)
	if recoveredCount > 0 {
		log.Info().
			Int("instanceID", instanceID).
			Int("recovered", recoveredCount).
			Int("total", len(erroredTorrents)).
			Msg("Errored torrent recovery completed")
	}

	return nil
}
