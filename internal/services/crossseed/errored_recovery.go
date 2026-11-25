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

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/externalprograms"
)

func (s *Service) RunExternalProgram(ctx context.Context, instanceID int, hashes []string) {
	if s.postInjectionHook != nil {
		s.postInjectionHook(ctx, instanceID, hashes)
		return
	}

	s.executeExternalProgram(ctx, instanceID, hashes)
}

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

func (s *Service) executeExternalProgram(ctx context.Context, instanceID int, hashes []string) {
	if s.externalProgramService == nil {
		return
	}

	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get automation settings for external program execution")
		return
	}

	if settings.RunExternalProgramID == nil {
		return
	}

	programID := *settings.RunExternalProgramID

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

func (s *Service) recoverErroredTorrents(ctx context.Context, instanceID int, torrents []*qbt.Torrent) error {
	var erroredTorrents []*qbt.Torrent
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

	appPrefs, err := s.syncManager.GetAppPreferences(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get app preferences, using default cache timeout")
		appPrefs.DiskCacheTTL = 60
	}

	cacheTimeout := max(time.Duration(appPrefs.DiskCacheTTL)*time.Second, 1*time.Second)

	allHashes := make([]string, len(erroredTorrents))
	torrentByHash := make(map[string]*qbt.Torrent, len(erroredTorrents))
	for i, t := range erroredTorrents {
		allHashes[i] = t.Hash
		torrentByHash[t.Hash] = t
	}

	type torrentRecoveryState struct {
		errorRetries      int
		progress          float64
		checkingStartTime time.Time
	}
	states := make(map[string]*torrentRecoveryState, len(erroredTorrents))
	for _, hash := range allHashes {
		states[hash] = &torrentRecoveryState{}
	}

	const (
		maxErrorRetries = 3
		maxCheckingTime = 25 * time.Minute
		pollInterval    = 2 * time.Second
	)

	performBatchRecheck := func(hashes []string) (complete, incomplete []string) {
		if len(hashes) == 0 {
			return nil, nil
		}

		for _, h := range hashes {
			states[h].checkingStartTime = time.Time{}
		}

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

			pendingHashes := make([]string, 0, len(pending))
			for h := range pending {
				pendingHashes = append(pendingHashes, h)
			}

			currentTorrents, err := s.syncManager.GetTorrentRefs(ctx, instanceID, qbittorrent.TorrentRefFilter{Hashes: pendingHashes})
			if err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to check recheck progress")
				time.Sleep(pollInterval)
				continue
			}

			currentByHash := make(map[string]*qbt.Torrent, len(currentTorrents))
			for _, t := range currentTorrents {
				currentByHash[t.Hash] = t
			}

			for hash := range pending {
				current, exists := currentByHash[hash]
				if !exists {
					log.Warn().Str("hash", hash).Msg("Torrent disappeared during recheck")
					delete(pending, hash)
					continue
				}

				state := current.State

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

				if state == qbt.TorrentStateCheckingDl || state == qbt.TorrentStateCheckingUp ||
					state == qbt.TorrentStateCheckingResumeData {
					if states[hash].checkingStartTime.IsZero() {
						states[hash].checkingStartTime = time.Now()
					} else if time.Since(states[hash].checkingStartTime) > maxCheckingTime {
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

				if state == qbt.TorrentStateError || state == qbt.TorrentStateMissingFiles {
					states[hash].errorRetries++
					states[hash].checkingStartTime = time.Time{}
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

					if err := s.syncManager.BulkAction(ctx, instanceID, []string{hash}, "pause"); err != nil {
						log.Warn().Err(err).Str("hash", hash).Msg("Failed to pause errored torrent for retry")
					}
					time.Sleep(500 * time.Millisecond)
					if err := s.syncManager.BulkAction(ctx, instanceID, []string{hash}, "recheck"); err != nil {
						log.Warn().Err(err).Str("hash", hash).Msg("Failed to recheck errored torrent for retry")
					}
					continue
				}
			}

			if len(pending) > 0 {
				time.Sleep(pollInterval)
			}
		}

		return complete, incomplete
	}

	if err := s.syncManager.BulkAction(ctx, instanceID, allHashes, "pause"); err != nil {
		return fmt.Errorf("pause errored torrents: %w", err)
	}

	complete, incomplete := performBatchRecheck(allHashes)

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

		for _, hash := range incomplete {
			states[hash].errorRetries = 0
		}

		var stillIncomplete []string
		phase2Complete, stillIncomplete = performBatchRecheck(incomplete)

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
