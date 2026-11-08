// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/services/notifications"
)

type completionLane struct {
	mu       sync.Mutex
	searchMu sync.Mutex
	waits    map[string]*completionWaitState
	polling  bool
}

type completionWaitState struct {
	done           chan struct{}
	attempt        int
	retryAt        time.Time
	deadline       time.Time
	timeout        time.Duration
	eventTorrent   qbt.Torrent
	lastSeen       *qbt.Torrent
	result         *qbt.Torrent
	err            error
	checkingLogged bool
}

type completionWaitSnapshot struct {
	state   *completionWaitState
	retryAt time.Time
}

func (s *Service) getCompletionPollInterval() time.Duration {
	if s != nil && s.completionPollInterval > 0 {
		return s.completionPollInterval
	}

	return defaultCompletionCheckingPollInterval
}

func (s *Service) getCompletionTimeout() time.Duration {
	if s != nil && s.completionTimeout > 0 {
		return s.completionTimeout
	}

	return defaultCompletionCheckingTimeout
}

func (s *Service) getCompletionRetryDelay() time.Duration {
	if s != nil && s.completionRetryDelay > 0 {
		return s.completionRetryDelay
	}

	return defaultCompletionCheckingRetryDelay
}

func (s *Service) getCompletionMaxAttempts() int {
	if s != nil && s.completionMaxAttempts > 0 {
		return s.completionMaxAttempts
	}

	return maxCompletionCheckingAttempts
}

// HandleTorrentCompletion evaluates completed torrents for cross-seed opportunities.
func (s *Service) HandleTorrentCompletion(ctx context.Context, instanceID int, torrent qbt.Torrent) {
	if s == nil || instanceID <= 0 {
		return
	}
	s.partialPool.wake(partialPoolWake{instanceID: instanceID, hash: partialPoolCanonicalTorrentKey(&torrent)})

	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	// Load per-instance completion settings
	if s.completionStore == nil {
		log.Error().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Msg("[CROSSSEED-COMPLETION] Completion store not configured")
		return
	}

	completionSettings, err := s.completionStore.Get(ctx, instanceID)
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Msg("[CROSSSEED-COMPLETION] Failed to load instance completion settings")
		return
	}

	if !completionSettings.Enabled {
		logCompletionSkip(instanceID, &torrent, "[CROSSSEED-COMPLETION] Completion search disabled for this instance")
		return
	}

	if s.shouldSkipCompletionTorrent(instanceID, &torrent, completionSettings) {
		return
	}

	if completionSettings.DelaySeconds > 0 {
		log.Info().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Int("delaySeconds", completionSettings.DelaySeconds).
			Msg("[CROSSSEED-COMPLETION] Delaying completion search")

		time.Sleep(time.Duration(completionSettings.DelaySeconds) * time.Second)

		completionSettings, err = s.completionStore.Get(ctx, instanceID)
		if err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("hash", torrent.Hash).
				Msg("[CROSSSEED-COMPLETION] Failed to reload settings after delay")
			return
		}
		if !completionSettings.Enabled {
			logCompletionSkip(instanceID, &torrent, "[CROSSSEED-COMPLETION] Completion search disabled during delay")
			return
		}
		if s.shouldSkipCompletionTorrent(instanceID, &torrent, completionSettings) {
			return
		}
	}

	readyTorrent, err := s.waitForCompletionTorrentReady(ctx, instanceID, torrent)
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msg("[CROSSSEED-COMPLETION] Failed to execute completion search")
		return
	}

	lane := s.getCompletionLane(instanceID)
	lane.searchMu.Lock()
	defer lane.searchMu.Unlock()

	readyTorrent, err = s.getCompletionTorrent(ctx, instanceID, readyTorrent.Hash)
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msg("[CROSSSEED-COMPLETION] Failed to reload completion torrent")
		return
	}
	// An errored torrent goes on to the refusal in content analysis.
	switch s.sourceGate().check(readyTorrent, sourceFilter{}) {
	case sourceMovingOrChecking:
		logCompletionSkip(instanceID, readyTorrent, "[CROSSSEED-COMPLETION] Torrent resumed checking before completion search")
		return
	case sourceIncomplete:
		logCompletionSkip(instanceID, readyTorrent, "[CROSSSEED-COMPLETION] Torrent is no longer fully downloaded")
		return
	default:
	}

	completionSettings, err = s.completionStore.Get(ctx, instanceID)
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", readyTorrent.Hash).
			Msg("[CROSSSEED-COMPLETION] Failed to reload instance completion settings")
		return
	}
	if !completionSettings.Enabled {
		logCompletionSkip(instanceID, readyTorrent, "[CROSSSEED-COMPLETION] Completion search disabled for this instance")
		return
	}
	if s.shouldSkipCompletionTorrent(instanceID, readyTorrent, completionSettings) {
		return
	}

	// Load global automation settings for cross-seed configuration (indexers, tags, etc.)
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", readyTorrent.Hash).
			Msg("[CROSSSEED-COMPLETION] Failed to load automation settings")
		return
	}
	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}

	err = s.invokeCompletionSearch(ctx, instanceID, readyTorrent, settings, completionSettings)
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", readyTorrent.Hash).
			Str("name", readyTorrent.Name).
			Msg("[CROSSSEED-COMPLETION] Failed to execute completion search")
	}
}

func (s *Service) shouldSkipCompletionTorrent(instanceID int, torrent *qbt.Torrent, completionSettings *models.InstanceCrossSeedCompletionSettings) bool {
	if torrent == nil {
		return true
	}

	if torrent.CompletionOn <= 0 || torrent.Hash == "" {
		// Safety check – the qbittorrent completion hook should only fire for completed torrents.
		return true
	}

	if hasCrossSeedTag(torrent.Tags) {
		logCompletionSkip(instanceID, torrent, "[CROSSSEED-COMPLETION] Skipping already tagged cross-seed torrent")
		return true
	}

	if s.sourceGate().check(torrent, completionSourceFilter(completionSettings)) == sourceOutsideFilter {
		logCompletionSkip(instanceID, torrent, "[CROSSSEED-COMPLETION] Torrent does not match completion filters")
		return true
	}

	return false
}

func completionSourceFilter(settings *models.InstanceCrossSeedCompletionSettings) sourceFilter {
	return sourceFilter{settings.Categories, settings.Tags, settings.ExcludeCategories, settings.ExcludeTags}
}

func logCompletionSkip(instanceID int, torrent *qbt.Torrent, message string) {
	event := log.Debug().Int("instanceID", instanceID)
	if torrent != nil {
		event = event.Str("hash", torrent.Hash).Str("name", torrent.Name)
	}
	event.Msg(message)
}

func (s *Service) getCompletionLane(instanceID int) *completionLane {
	s.completionLaneMu.Lock()
	defer s.completionLaneMu.Unlock()

	if s.completionLanes == nil {
		s.completionLanes = make(map[int]*completionLane)
	}

	lane, ok := s.completionLanes[instanceID]
	if !ok {
		lane = &completionLane{}
		s.completionLanes[instanceID] = lane
	}
	return lane
}

func (s *Service) waitForCompletionTorrentReady(ctx context.Context, instanceID int, eventTorrent qbt.Torrent) (*qbt.Torrent, error) {
	lane := s.getCompletionLane(instanceID)
	lane.mu.Lock()
	defer lane.mu.Unlock()

	return s.waitForCompletionTorrentReadyLocked(ctx, instanceID, lane, eventTorrent)
}

func (s *Service) waitForCompletionTorrentReadyLocked(
	_ context.Context,
	instanceID int,
	lane *completionLane,
	eventTorrent qbt.Torrent,
) (*qbt.Torrent, error) {
	wait := s.registerCompletionWaitLocked(instanceID, lane, eventTorrent)
	done := wait.done

	lane.mu.Unlock()
	<-done
	lane.mu.Lock()

	if wait.err != nil {
		return nil, wait.err
	}
	if wait.result == nil {
		return nil, nil
	}
	torrent := *wait.result
	return &torrent, nil
}

func (s *Service) registerCompletionWaitLocked(
	instanceID int,
	lane *completionLane,
	eventTorrent qbt.Torrent,
) *completionWaitState {
	if lane.waits == nil {
		lane.waits = make(map[string]*completionWaitState)
	}

	hash := normalizeHash(eventTorrent.Hash)
	timeout := s.getCompletionTimeout()
	now := time.Now()
	deadline := now.Add(timeout)

	wait, ok := lane.waits[hash]
	if ok {
		base := now
		if wait.retryAt.After(base) {
			base = wait.retryAt
		}
		deadline = base.Add(timeout)
		if deadline.After(wait.deadline) {
			wait.deadline = deadline
			wait.timeout = timeout
		}
		s.startCompletionLanePollerLocked(instanceID, lane)
		return wait
	}

	wait = &completionWaitState{
		done:         make(chan struct{}),
		attempt:      1,
		deadline:     deadline,
		timeout:      timeout,
		eventTorrent: eventTorrent,
	}
	lane.waits[hash] = wait

	s.startCompletionLanePollerLocked(instanceID, lane)

	return wait
}

func (s *Service) startCompletionLanePollerLocked(instanceID int, lane *completionLane) {
	if lane.polling {
		return
	}

	lane.polling = true

	go s.runCompletionLanePoller(instanceID, lane)
}

func (s *Service) runCompletionLanePoller(instanceID int, lane *completionLane) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		<-timer.C
		nextDelay, ok := s.pollCompletionLane(instanceID, lane)
		if !ok {
			return
		}
		timer.Reset(nextDelay)
	}
}

func (s *Service) pollCompletionLane(instanceID int, lane *completionLane) (time.Duration, bool) {
	waits := s.snapshotCompletionWaits(lane)
	if len(waits) == 0 {
		return 0, false
	}

	now := time.Now()
	activeWaits := make(map[string]*completionWaitState, len(waits))
	hashes := make([]string, 0, len(waits))
	for hash, wait := range waits {
		if wait.retryAt.After(now) {
			continue
		}
		activeWaits[hash] = wait.state
		hashes = append(hashes, hash)
	}

	if len(activeWaits) == 0 {
		lane.mu.Lock()
		defer lane.mu.Unlock()

		return s.nextCompletionPollDelayLocked(lane, now)
	}

	torrents, err := s.getCompletionTorrents(context.Background(), instanceID, hashes)
	now = time.Now()

	lane.mu.Lock()
	defer lane.mu.Unlock()

	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Int("torrents", len(hashes)).
			Msg("[CROSSSEED-COMPLETION] Failed to refresh completion torrents while waiting for checking to finish")
		s.expireCompletionWaitsLocked(instanceID, lane, now)
		return s.nextCompletionPollDelayLocked(lane, now)
	}

	s.applyCompletionPollResultsLocked(instanceID, lane, activeWaits, torrents, now)

	return s.nextCompletionPollDelayLocked(lane, now)
}

func (s *Service) snapshotCompletionWaits(lane *completionLane) map[string]completionWaitSnapshot {
	lane.mu.Lock()
	defer lane.mu.Unlock()

	if len(lane.waits) == 0 {
		lane.polling = false
		return nil
	}

	waits := make(map[string]completionWaitSnapshot, len(lane.waits))
	for hash, wait := range lane.waits {
		waits[hash] = completionWaitSnapshot{
			state:   wait,
			retryAt: wait.retryAt,
		}
	}

	return waits
}

func (s *Service) applyCompletionPollResultsLocked(
	instanceID int,
	lane *completionLane,
	waits map[string]*completionWaitState,
	torrents map[string]qbt.Torrent,
	now time.Time,
) {
	for hash, wait := range waits {
		currentWait, ok := lane.waits[hash]
		if !ok || currentWait != wait {
			continue
		}

		torrent, ok := torrents[hash]
		if !ok {
			s.failMissingCompletionWaitLocked(instanceID, lane, hash, wait)
			continue
		}

		current := torrent
		wait.lastSeen = &current

		if s.keepWaitingForCompletion(instanceID, lane, hash, wait, current, now) {
			continue
		}

		if s.sourceGate().check(&current, sourceFilter{}) == sourceIncomplete {
			log.Warn().
				Int("instanceID", instanceID).
				Str("hash", current.Hash).
				Str("name", current.Name).
				Str("state", string(current.State)).
				Float64("progress", current.Progress).
				Msg("[CROSSSEED-COMPLETION] Torrent finished checking but is still incomplete")
			s.completeCompletionWaitLocked(
				lane,
				hash,
				wait,
				nil,
				fmt.Errorf("%w: torrent %s is not fully downloaded (progress %.2f)", ErrTorrentNotComplete, current.Name, current.Progress),
			)
			continue
		}

		s.completeCompletionWaitLocked(lane, hash, wait, &current, nil)
	}
}

func (s *Service) keepWaitingForCompletion(
	instanceID int,
	lane *completionLane,
	hash string,
	wait *completionWaitState,
	current qbt.Torrent,
	now time.Time,
) bool {
	if s.sourceGate().check(&current, sourceFilter{}) != sourceMovingOrChecking {
		return false
	}

	if !wait.checkingLogged {
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", current.Hash).
			Str("name", current.Name).
			Str("state", string(current.State)).
			Float64("progress", current.Progress).
			Msg("[CROSSSEED-COMPLETION] Deferring completion search while torrent is checking")
		wait.checkingLogged = true
	}

	if now.Before(wait.deadline) {
		return true
	}

	if wait.attempt < s.getCompletionMaxAttempts() {
		s.retryCompletionWaitLocked(instanceID, wait, current, now)
		return true
	}

	log.Warn().
		Int("instanceID", instanceID).
		Str("hash", current.Hash).
		Str("name", current.Name).
		Str("state", string(current.State)).
		Float64("progress", current.Progress).
		Dur("timeout", wait.timeout).
		Msg("[CROSSSEED-COMPLETION] Timed out waiting for torrent checking to finish")
	s.completeCompletionWaitLocked(
		lane,
		hash,
		wait,
		nil,
		fmt.Errorf("completion torrent %s still checking after %s", current.Name, wait.timeout),
	)

	return true
}

func (s *Service) retryCompletionWaitLocked(instanceID int, wait *completionWaitState, current qbt.Torrent, now time.Time) {
	retryAfter := s.getCompletionRetryDelay()
	retryAt := now.Add(retryAfter)
	nextAttempt := wait.attempt + 1

	log.Warn().
		Int("instanceID", instanceID).
		Str("hash", current.Hash).
		Str("name", current.Name).
		Str("state", string(current.State)).
		Float64("progress", current.Progress).
		Int("attempt", wait.attempt).
		Int("nextAttempt", nextAttempt).
		Int("maxAttempts", s.getCompletionMaxAttempts()).
		Dur("timeout", wait.timeout).
		Dur("retryAfter", retryAfter).
		Msg("[CROSSSEED-COMPLETION] Timed out waiting for torrent checking to finish, retrying")

	wait.attempt = nextAttempt
	wait.retryAt = retryAt
	wait.deadline = retryAt.Add(wait.timeout)
	wait.lastSeen = &current
	wait.checkingLogged = false
}

func (s *Service) failMissingCompletionWaitLocked(
	instanceID int,
	lane *completionLane,
	hash string,
	wait *completionWaitState,
) {
	err := fmt.Errorf("%w: torrent %s not found in instance %d", ErrTorrentNotFound, wait.eventTorrent.Hash, instanceID)

	log.Warn().
		Int("instanceID", instanceID).
		Str("hash", wait.eventTorrent.Hash).
		Str("name", wait.eventTorrent.Name).
		Err(err).
		Msg("[CROSSSEED-COMPLETION] Completion torrent disappeared while waiting for checking to finish")

	s.completeCompletionWaitLocked(lane, hash, wait, nil, err)
}

func (s *Service) expireCompletionWaitsLocked(instanceID int, lane *completionLane, now time.Time) {
	for hash, wait := range lane.waits {
		if now.Before(wait.deadline) {
			continue
		}

		s.failTimedOutCompletionWaitLocked(instanceID, lane, hash, wait)
	}
}

func (s *Service) failTimedOutCompletionWaitLocked(
	instanceID int,
	lane *completionLane,
	hash string,
	wait *completionWaitState,
) {
	name := wait.eventTorrent.Name
	state := qbt.TorrentState("")
	progress := 0.0

	if wait.lastSeen != nil {
		name = wait.lastSeen.Name
		state = wait.lastSeen.State
		progress = wait.lastSeen.Progress
	}

	log.Warn().
		Int("instanceID", instanceID).
		Str("hash", wait.eventTorrent.Hash).
		Str("name", name).
		Str("state", string(state)).
		Float64("progress", progress).
		Dur("timeout", wait.timeout).
		Msg("[CROSSSEED-COMPLETION] Timed out waiting for torrent checking to finish")
	s.completeCompletionWaitLocked(
		lane,
		hash,
		wait,
		nil,
		fmt.Errorf("completion torrent %s still checking after %s", name, wait.timeout),
	)
}

func (s *Service) completeCompletionWaitLocked(
	lane *completionLane,
	hash string,
	wait *completionWaitState,
	result *qbt.Torrent,
	err error,
) {
	delete(lane.waits, hash)

	if result != nil {
		torrent := *result
		wait.result = &torrent
	}
	wait.err = err

	close(wait.done)
}

func (s *Service) updateCompletionPollerStateLocked(lane *completionLane) bool {
	if len(lane.waits) > 0 {
		return true
	}

	lane.polling = false
	return false
}

func (s *Service) nextCompletionPollDelayLocked(lane *completionLane, now time.Time) (time.Duration, bool) {
	if !s.updateCompletionPollerStateLocked(lane) {
		return 0, false
	}

	pollInterval := s.getCompletionPollInterval()
	nextDelay := pollInterval

	for _, wait := range lane.waits {
		if !wait.retryAt.After(now) {
			return pollInterval, true
		}

		delay := wait.retryAt.Sub(now)
		if delay < nextDelay {
			nextDelay = delay
		}
	}

	return nextDelay, true
}

func (s *Service) getCompletionTorrent(ctx context.Context, instanceID int, hash string) (*qbt.Torrent, error) {
	torrents, err := s.getCompletionTorrents(ctx, instanceID, []string{hash})
	if err != nil {
		return nil, err
	}

	torrent, ok := torrents[normalizeHash(hash)]
	if !ok {
		return nil, fmt.Errorf("%w: torrent %s not found in instance %d", ErrTorrentNotFound, hash, instanceID)
	}

	current := torrent
	return &current, nil
}

func (s *Service) getCompletionTorrents(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.Torrent, error) {
	apiCtx, cancel := context.WithTimeout(ctx, recheckAPITimeout)
	defer cancel()

	torrents, err := s.syncManager.GetTorrents(apiCtx, instanceID, qbt.TorrentFilterOptions{
		Hashes: hashes,
	})
	if err != nil {
		return nil, fmt.Errorf("load torrents: %w", err)
	}

	result := make(map[string]qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		result[normalizeHash(torrent.Hash)] = torrent
	}

	return result, nil
}

func (s *Service) invokeCompletionSearch(
	ctx context.Context,
	instanceID int,
	torrent *qbt.Torrent,
	settings *models.CrossSeedAutomationSettings,
	completionSettings *models.InstanceCrossSeedCompletionSettings,
) error {
	if s.completionSearchInvoker != nil {
		return s.completionSearchInvoker(ctx, instanceID, torrent, settings, completionSettings)
	}
	return s.executeCompletionSearch(ctx, instanceID, torrent, settings, completionSettings)
}

func (s *Service) executeCompletionSearch(ctx context.Context, instanceID int, torrent *qbt.Torrent, settings *models.CrossSeedAutomationSettings, completionSettings *models.InstanceCrossSeedCompletionSettings) error {
	if torrent == nil {
		return nil
	}
	ctx = withRouteRun(ctx)
	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}
	if s.syncManager == nil {
		return errors.New("qbittorrent sync manager not configured")
	}

	startedAt := time.Now().UTC()

	gazelleClients, gazelleErr := s.buildGazelleClientSet(ctx, settings)
	if gazelleErr != nil {
		log.Warn().Err(gazelleErr).Msg("[CROSSSEED-COMPLETION] Failed to initialize Gazelle clients; continuing without Gazelle")
	}
	var requestedIndexerIDs []int
	cacheMode := ""
	if completionSettings != nil {
		requestedIndexerIDs = completionSettings.IndexerIDs
		if completionSettings.BypassTorznabCache {
			cacheMode = jackett.CacheModeBypass
		}
	}
	scope := s.collectSearchScope(ctx, instanceID, torrent, searchScopeFacts{
		requestedIndexerIDs: requestedIndexerIDs,
		picked:              len(uniquePositiveInts(requestedIndexerIDs)) > 0,
		gazelleKeys:         gazelleClients.hosts(),
	}, true)
	if scope.err != nil {
		return scope.err
	}
	if len(scope.torznabIndexerIDs) == 0 && len(scope.gazelleHosts) == 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msgf("[CROSSSEED-COMPLETION] Skipping completion search: %s", scope.skipReason)
		return nil
	}

	searchCtx := jackett.WithSearchPriority(ctx, jackett.RateLimitPriorityCompletion)
	searchResp, _, _, err := s.searchTorrentMatches(searchCtx, instanceID, torrent.Hash, TorrentSearchOptions{
		FindIndividualEpisodes: settings.FindIndividualEpisodes,
		RescueTitleMismatches:  settings.RescueTitleMismatches && !settings.SkipRecheck,
		CacheMode:              cacheMode,
	}, scope, gazelleClients)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Warn().
				Int("instanceID", instanceID).
				Str("hash", torrent.Hash).
				Str("name", torrent.Name).
				Msg("[CROSSSEED-COMPLETION] Search timed out, no cross-seeds found")
			return nil
		}
		return err
	}
	if searchResp.torznabErr != nil {
		log.Warn().
			Err(searchResp.torznabErr).
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msg("[CROSSSEED-COMPLETION] Torznab failed; searched only RED/OPS")
	}
	if len(searchResp.Results) == 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("name", torrent.Name).
			Msg("[CROSSSEED-COMPLETION] No matches returned for completed torrent")
		return nil
	}

	defaults := defaultsFor(triggerCompletion, settings)
	// Pass completion source filters to ensure CrossSeed respects them when finding candidates
	if completionSettings != nil {
		defaults.sourceFilter = completionSourceFilter(completionSettings)
	}
	entries, err := s.applySourceSearchResults(ctx, instanceID, torrent, searchResp, defaults, time.Now().UTC())
	if err != nil {
		return err
	}
	results := make([]models.CrossSeedSearchResult, 0, len(entries))
	for _, entry := range entries {
		results = append(results, entry.result)
		if entry.err != nil {
			log.Debug().
				Err(entry.err).
				Int("instanceID", instanceID).
				Str("hash", torrent.Hash).
				Str("matchIndexer", entry.result.IndexerName).
				Msg("[CROSSSEED-COMPLETION] Cross-seed apply attempt failed")
		}
	}
	counts := countSourceApplyEntries(entries)

	logCompletionSearchSummary(instanceID, torrent, counts.added, counts.failed, counts.skipped)

	if s.notifier != nil && (counts.added > 0 || counts.failed > 0) {
		completedAt := time.Now().UTC()
		eventType := notifications.EventCrossSeedCompletionSucceeded
		if counts.failed > 0 {
			eventType = notifications.EventCrossSeedCompletionFailed
		}
		var errorMessage string
		if eventType == notifications.EventCrossSeedCompletionFailed {
			if len(counts.errorSamples) > 0 {
				errorMessage = counts.errorSamples[0]
			} else {
				errorMessage = "cross-seed completion failed"
			}
		}
		lines := []string{
			"Torrent: " + torrent.Name,
			fmt.Sprintf("Matches: %d", len(searchResp.Results)),
			fmt.Sprintf("Added: %d", counts.added),
			fmt.Sprintf("Failed: %d", counts.failed),
			fmt.Sprintf("Skipped: %d", counts.skipped),
		}
		samples := collectSearchResultSamples(results, 0)
		if sampleText := formatSamplesForMessage(samples, 3); sampleText != "" {
			lines = append(lines, "Samples: "+sampleText)
		}
		notifyCtx := context.WithoutCancel(ctx)
		s.notifier.Notify(notifyCtx, notifications.Event{
			Type:        eventType,
			InstanceID:  instanceID,
			TorrentName: torrent.Name,
			Message:     strings.Join(lines, "\n"),
			CrossSeed: &notifications.CrossSeedEventData{
				Matches: len(searchResp.Results),
				Added:   counts.added,
				Failed:  counts.failed,
				Skipped: counts.skipped,
				Samples: samples,
			},
			ErrorMessage:  errorMessage,
			ErrorMessages: counts.errorSamples,
			StartedAt:     &startedAt,
			CompletedAt:   &completedAt,
		})
	}

	return nil
}

// logCompletionSearchSummary emits every outcome count and promotes runs with
// at least one addition from debug to info level.
func logCompletionSearchSummary(instanceID int, torrent *qbt.Torrent, added, failed, skipped int) {
	event := log.Debug()
	message := "[CROSSSEED-COMPLETION] Completion search executed with no additions"
	if added > 0 {
		event = log.Info()
		message = "[CROSSSEED-COMPLETION] Added cross-seed from completion search"
	}
	event.
		Int("instanceID", instanceID).
		Str("hash", torrent.Hash).
		Str("name", torrent.Name).
		Int("added", added).
		Int("failed", failed).
		Int("skipped", skipped).
		Msg(message)
}

func hasCrossSeedTag(rawTags string) bool {
	return slices.Contains(splitTags(rawTags), "cross-seed")
}
