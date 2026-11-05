// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/redact"
)

type automationInstanceSnapshot struct {
	instance *models.Instance
	torrents []qbt.Torrent
}

type automationSnapshots struct {
	instances map[int]*automationInstanceSnapshot
}

type automationContext struct {
	snapshots      *automationSnapshots
	candidateCache map[string]*FindCandidatesResponse
}

// AutomationRunOptions configures a manual automation run.
type AutomationRunOptions struct {
	RequestedBy string
	Mode        models.CrossSeedRunMode
	DryRun      bool
}

func (s *Service) ensureIndexersConfigured(ctx context.Context) error {
	if s.jackettService == nil {
		return errors.New("jackett service not configured")
	}
	indexers, err := s.jackettService.GetEnabledIndexersInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to load enabled torznab indexers: %w", err)
	}
	if len(indexers) == 0 {
		return ErrNoIndexersConfigured
	}
	return nil
}

// AutomationStatus summarises scheduler state for the API.
type AutomationStatus struct {
	Settings  *models.CrossSeedAutomationSettings `json:"settings"`
	LastRun   *models.CrossSeedRun                `json:"lastRun,omitempty"`
	NextRunAt *time.Time                          `json:"nextRunAt,omitempty"`
	Running   bool                                `json:"running"`
}

// StartAutomation launches the background scheduler loop.
func (s *Service) StartAutomation(ctx context.Context) {
	s.automationMu.Lock()
	defer s.automationMu.Unlock()

	if s.automationStore == nil || s.jackettService == nil {
		log.Warn().Msg("Cross-seed automation disabled: missing store or Jackett service")
		return
	}

	if s.automationCancel != nil {
		return
	}

	loopCtx, cancel := context.WithCancel(ctx)
	s.automationCancel = cancel

	go s.automationLoop(loopCtx)
}

// StopAutomation stops the background scheduler loop if it is running.
func (s *Service) StopAutomation() {
	s.automationMu.Lock()
	cancel := s.automationCancel
	s.automationCancel = nil
	s.automationMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// CancelAutomationRun cancels the currently running automation run, if any.
// Returns true if a run was canceled, false if no run was active.
func (s *Service) CancelAutomationRun() bool {
	if !s.runActive.Load() {
		return false
	}

	s.automationMu.Lock()
	cancel := s.runCancel
	s.automationMu.Unlock()

	if cancel != nil {
		cancel()
		return true
	}
	return false
}

// RunAutomation executes a cross-seed automation cycle immediately.
func (s *Service) RunAutomation(ctx context.Context, opts AutomationRunOptions) (*models.CrossSeedRun, error) {
	if s.automationStore == nil || s.jackettService == nil {
		return nil, errors.New("cross-seed automation not configured")
	}

	if !s.runActive.CompareAndSwap(false, true) {
		return nil, ErrAutomationRunning
	}

	// Create cancellable context for this run
	runCtx, cancel := context.WithCancel(ctx)
	s.automationMu.Lock()
	s.runCancel = cancel
	s.automationMu.Unlock()

	// Signal connected clients that an automation run started so they switch to the
	// while-running refresh cadence instead of polling. Published after releasing the lock.
	if s.activityPublisher != nil {
		s.activityPublisher.Publish(activity.Event{Kind: activity.KindCrossSeedStatus})
	}

	defer func() {
		s.automationMu.Lock()
		s.runCancel = nil
		s.automationMu.Unlock()
		cancel()
		s.runActive.Store(false)

		// Signal the run finished so clients refetch and drop the while-running cadence.
		if s.activityPublisher != nil {
			s.activityPublisher.Publish(activity.Event{Kind: activity.KindCrossSeedStatus})
		}
	}()

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}

	if err := s.ensureIndexersConfigured(ctx); err != nil {
		return nil, err
	}

	if len(settings.TargetInstanceIDs) == 0 {
		return nil, ErrNoTargetInstancesConfigured
	}

	// Default requested by / mode values
	if opts.Mode == "" {
		opts.Mode = models.CrossSeedRunModeManual
	}
	if opts.RequestedBy == "" {
		if opts.Mode == models.CrossSeedRunModeAuto {
			opts.RequestedBy = "scheduler"
		} else {
			opts.RequestedBy = "manual"
		}
	}

	if opts.Mode != models.CrossSeedRunModeAuto {
		intervalMinutes := settings.RunIntervalMinutes
		if intervalMinutes <= 0 {
			intervalMinutes = 120
		}
		cooldown := max(time.Duration(intervalMinutes)*time.Minute, 30*time.Minute)

		lastRun, err := s.automationStore.GetLatestRun(ctx)
		if err != nil {
			return nil, fmt.Errorf("load latest automation run metadata: %w", err)
		}
		if lastRun != nil {
			nextAllowed := lastRun.StartedAt.Add(cooldown)
			now := time.Now()
			if now.Before(nextAllowed) {
				remaining := max(time.Until(nextAllowed), time.Second)
				remaining = remaining.Round(time.Second)
				return nil, fmt.Errorf("%w: manual runs limited to one every %d minutes. Try again in %s (after %s)",
					ErrAutomationCooldownActive,
					int(cooldown.Minutes()),
					remaining.String(),
					nextAllowed.UTC().Format(time.RFC3339))
			}
		}
	}

	// Guard against auto runs when disabled
	if opts.Mode == models.CrossSeedRunModeAuto && !settings.Enabled {
		return nil, errors.New("cross-seed automation disabled")
	}

	run := &models.CrossSeedRun{
		TriggeredBy: opts.RequestedBy,
		Mode:        opts.Mode,
		Status:      models.CrossSeedRunStatusRunning,
		StartedAt:   time.Now().UTC(),
	}

	storedRun, err := s.automationStore.CreateRun(ctx, run)
	if err != nil {
		return nil, err
	}

	finalRun, execErr := s.executeAutomationRun(runCtx, storedRun, settings, opts)
	if execErr != nil {
		return finalRun, execErr
	}

	return finalRun, nil
}

// updateAutomationRunWithRetry attempts to update the automation run in the database with retries
func (s *Service) updateAutomationRunWithRetry(ctx context.Context, run *models.CrossSeedRun) (*models.CrossSeedRun, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-ctx.Done():
				return run, ctx.Err()
			case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
			}
		}

		updated, err := s.automationStore.UpdateRun(ctx, run)
		if err == nil {
			return updated, nil
		}

		lastErr = err
		log.Warn().Err(err).Int("attempt", attempt+1).Int64("runID", run.ID).Msg("Failed to update automation run, retrying")
	}

	log.Error().Err(lastErr).Int64("runID", run.ID).Msg("Failed to update automation run after retries")
	return run, lastErr
}

// GetAutomationStatus returns scheduler information for the API.
func (s *Service) GetAutomationStatus(ctx context.Context) (*AutomationStatus, error) {
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, err
	}

	status := &AutomationStatus{
		Settings: settings,
		Running:  s.runActive.Load(),
	}

	if s.automationStore != nil {
		lastRun, err := s.automationStore.GetLatestRun(ctx)
		if err != nil {
			return nil, fmt.Errorf("load latest automation run: %w", err)
		}

		// Check if the last run is stuck in running status (e.g., due to crash)
		// Only mark as stuck if no run is currently active in memory - this prevents
		// marking legitimate long-running automations as failed while they're still executing
		if lastRun != nil && lastRun.Status == models.CrossSeedRunStatusRunning && !s.runActive.Load() {
			// If it's been running for more than 5 minutes, mark it as failed
			if time.Since(lastRun.StartedAt) > 5*time.Minute {
				lastRun.Status = models.CrossSeedRunStatusFailed
				msg := "automation run timed out (stuck for >5 minutes)"
				lastRun.ErrorMessage = &msg
				completed := time.Now().UTC()
				lastRun.CompletedAt = &completed
				if updated, updateErr := s.automationStore.UpdateRun(ctx, lastRun); updateErr != nil {
					log.Warn().Err(updateErr).Int64("runID", lastRun.ID).Msg("failed to update stuck automation run")
				} else {
					lastRun = updated
				}
			}
		}

		status.LastRun = lastRun

		delay, shouldRun := s.computeNextRunDelay(ctx, settings)
		if shouldRun {
			now := time.Now().UTC()
			status.NextRunAt = &now
		} else {
			next := time.Now().Add(delay)
			status.NextRunAt = &next
		}
	}

	return status, nil
}

// ListAutomationRuns returns stored automation run history.
func (s *Service) ListAutomationRuns(ctx context.Context, limit, offset int) ([]*models.CrossSeedRun, error) {
	if s.automationStore == nil {
		return []*models.CrossSeedRun{}, nil
	}
	runs, err := s.automationStore.ListRuns(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list automation runs: %w", err)
	}
	return runs, nil
}

func (s *Service) automationLoop(ctx context.Context) {
	log.Debug().Msg("Starting cross-seed automation loop")
	defer log.Debug().Msg("Cross-seed automation loop stopped")

	timer := time.NewTimer(time.Minute)
	if !timer.Stop() {
		<-timer.C
	}

	for {
		if ctx.Err() != nil {
			return
		}

		settings, err := s.GetAutomationSettings(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to load cross-seed automation settings")
			s.waitTimer(ctx, timer, time.Minute)
			continue
		}

		nextDelay, shouldRun := s.computeNextRunDelay(ctx, settings)
		if shouldRun {
			if _, err := s.RunAutomation(ctx, AutomationRunOptions{
				RequestedBy: "scheduler",
				Mode:        models.CrossSeedRunModeAuto,
			}); err != nil {
				if errors.Is(err, ErrAutomationRunning) {
					// Add a short delay to prevent tight loop when automation is already running
					select {
					case <-ctx.Done():
						return
					case <-time.After(150 * time.Millisecond):
						// Continue after delay
					}
				} else if errors.Is(err, ErrNoIndexersConfigured) {
					log.Info().Msg("Skipping cross-seed automation run: no Torznab indexers configured")
					s.waitTimer(ctx, timer, 5*time.Minute)
				} else if errors.Is(err, ErrNoTargetInstancesConfigured) {
					log.Info().Msg("Skipping cross-seed automation run: no target instances configured")
					s.waitTimer(ctx, timer, 5*time.Minute)
				} else if errors.Is(err, context.Canceled) {
					log.Debug().Msg("Cross-seed automation run canceled (context canceled)")
				} else {
					log.Warn().Err(err).Msg("Cross-seed automation run failed")
					// Prevent tight loop on unexpected errors
					s.waitTimer(ctx, timer, time.Minute)
				}
			}
			continue
		}

		s.waitTimer(ctx, timer, nextDelay)
	}
}

func (s *Service) waitTimer(ctx context.Context, timer *time.Timer, delay time.Duration) {
	if delay <= 0 {
		delay = time.Second
	}
	// Cap delay to 24h to avoid overflow
	if delay > 24*time.Hour {
		delay = 24 * time.Hour
	}

	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}

	timer.Reset(delay)

	select {
	case <-ctx.Done():
	case <-timer.C:
	case <-s.automationWake:
	}
}

// computeNextRunDelay calculates the delay until the next RSS automation run.
// Returns (delay, shouldRunNow) where shouldRunNow=true means run immediately.
// This is for RSS Automation only, not Seeded Torrent Search.
func (s *Service) computeNextRunDelay(ctx context.Context, settings *models.CrossSeedAutomationSettings) (time.Duration, bool) {
	// When RSS automation is disabled, return 5 minutes for internal polling loop
	// (this value is NOT shown to users - it's just for checking if automation got re-enabled)
	if settings == nil || !settings.Enabled {
		return 5 * time.Minute, false
	}

	if s.automationStore == nil {
		return time.Hour, false
	}

	// RSS Automation: enforce minimum 30 minutes between runs
	intervalMinutes := settings.RunIntervalMinutes
	if intervalMinutes <= 0 {
		intervalMinutes = 120
	}
	interval := max(time.Duration(intervalMinutes)*time.Minute, 30*time.Minute)

	lastRun, err := s.automationStore.GetLatestRun(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get latest cross-seed run metadata")
		return time.Minute, false
	}

	if lastRun == nil {
		return 0, true
	}

	elapsed := time.Since(lastRun.StartedAt)
	if elapsed >= interval {
		return 0, true
	}

	remaining := max(interval-elapsed, time.Second)

	return remaining, false
}

func (s *Service) signalAutomationWake() {
	if s.automationWake == nil {
		return
	}

	select {
	case s.automationWake <- struct{}{}:
	default:
	}
}

func (s *Service) executeAutomationRun(ctx context.Context, run *models.CrossSeedRun, settings *models.CrossSeedAutomationSettings, opts AutomationRunOptions) (*models.CrossSeedRun, error) {
	ctx = withRouteRun(ctx)
	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}
	var runErr error
	defer func() {
		s.notifyAutomationRun(context.WithoutCancel(ctx), run, runErr)
	}()

	searchCtx := jackett.WithSearchPriority(ctx, jackett.RateLimitPriorityRSS)

	var searchResp *jackett.SearchResponse
	respCh := make(chan *jackett.SearchResponse, 1)
	errCh := make(chan error, 1)

	// RSS reads the Torznab feeds, so it keeps the RED/OPS Torznab indexers.
	resolvedIndexerIDs, resolveErr := s.resolveTorznabIndexerIDs(ctx, settings.TargetIndexerIDs) //nolint:forbidigo // RSS automation expands an empty list without the Search scope
	if resolveErr != nil {
		msg := resolveErr.Error()
		run.ErrorMessage = &msg
		run.Status = models.CrossSeedRunStatusFailed
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		if updated, updateErr := s.updateAutomationRunWithRetry(ctx, run); updateErr == nil {
			run = updated
		}
		return run, resolveErr
	}
	if len(resolvedIndexerIDs) == 0 {
		msg := "no enabled Torznab indexers configured"
		run.ErrorMessage = &msg
		run.Status = models.CrossSeedRunStatusFailed
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		if updated, updateErr := s.updateAutomationRunWithRetry(ctx, run); updateErr == nil {
			run = updated
		}
		return run, ErrNoIndexersConfigured
	}

	err := s.jackettService.Recent(searchCtx, rssFeedPageSize, 0, resolvedIndexerIDs, func(resp *jackett.SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	})
	if err != nil {
		msg := err.Error()
		run.ErrorMessage = &msg
		run.Status = models.CrossSeedRunStatusFailed
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		if updated, updateErr := s.updateAutomationRunWithRetry(ctx, run); updateErr == nil {
			run = updated
		}
		runErr = err
		return run, err
	}

	select {
	case searchResp = <-respCh:
		// continue
	case err := <-errCh:
		msg := err.Error()
		run.ErrorMessage = &msg
		run.Status = models.CrossSeedRunStatusFailed
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		if updated, updateErr := s.updateAutomationRunWithRetry(ctx, run); updateErr == nil {
			run = updated
		}
		runErr = err
		return run, err
	case <-time.After(10 * time.Minute): // generous timeout for RSS automation
		msg := "Recent search timed out"
		run.ErrorMessage = &msg
		run.Status = models.CrossSeedRunStatusFailed
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		if updated, updateErr := s.updateAutomationRunWithRetry(ctx, run); updateErr == nil {
			run = updated
		}
		timeoutErr := errors.New("recent search timed out")
		runErr = timeoutErr
		return run, timeoutErr
	case <-ctx.Done():
		msg := "canceled by user"
		run.ErrorMessage = &msg
		run.Status = models.CrossSeedRunStatusFailed
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		if updated, updateErr := s.updateAutomationRunWithRetry(context.WithoutCancel(ctx), run); updateErr == nil {
			run = updated
		}
		runErr = ctx.Err()
		return run, ctx.Err()
	}

	// One page misses announces that scrolled past the feed head between
	// runs, so walk deeper pages per indexer until a page is fully known.
	// The very first run has no handled boundary to walk back to, so it
	// stays on one page instead of paging blindly into history.
	feedPageCap := rssFeedMaxPages
	if runs, listErr := s.automationStore.ListRuns(ctx, 2, 0); listErr != nil || len(runs) <= 1 {
		feedPageCap = 1
	}
	s.pageAutomationFeed(searchCtx, searchResp, feedPageCap)

	// Pre-fetch all indexer info (names and domains) for performance
	indexerInfo, err := s.jackettService.GetEnabledIndexersInfo(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to fetch indexer info, will use fallback lookups")
		indexerInfo = make(map[int]jackett.EnabledIndexerInfo) // Empty map as fallback
	}

	autoCtx := &automationContext{
		snapshots:      s.buildAutomationSnapshots(ctx, settings.TargetInstanceIDs, settings),
		candidateCache: make(map[string]*FindCandidatesResponse),
	}

	log.Debug().
		Int("feedItems", len(searchResp.Results)).
		Bool("contextCancelled", ctx.Err() != nil).
		Msg("[RSS] Starting feed item processing")

	processed := 0
	for _, result := range searchResp.Results {
		if ctx.Err() != nil {
			runErr = ctx.Err()
			break
		}

		if strings.TrimSpace(result.GUID) == "" || result.IndexerID == 0 {
			continue
		}

		// Only processed items are skipped. A skipped or size-rejected item is evaluated
		// again each run until it leaves the feed, on purpose: the local candidate changes
		// over time (a copy finishes, another instance gets it, reflinks are turned on).
		alreadyHandled, lastStatus, err := s.automationStore.HasProcessedFeedItem(ctx, result.GUID, result.IndexerID)
		if err != nil {
			log.Warn().Err(err).Str("guid", redact.URLString(result.GUID)).Msg("Failed to check feed item cache")
		}

		run.TotalFeedItems++

		if alreadyHandled && lastStatus == models.CrossSeedFeedItemStatusProcessed {
			s.markFeedItem(ctx, result, lastStatus, lastStatus, run.ID, nil)
			continue
		}

		status, infoHash, procErr := s.processAutomationCandidate(ctx, run, settings, autoCtx, result, opts, indexerInfo)
		if procErr != nil {
			if runErr == nil {
				runErr = procErr
			}
			log.Debug().Err(procErr).Str("title", result.Title).Msg("Cross-seed automation candidate failed")
		}

		processed++
		s.markFeedItem(ctx, result, lastStatus, status, run.ID, infoHash)
	}

	completed := time.Now().UTC()
	run.CompletedAt = &completed

	switch {
	case run.CandidatesFailed > 0 && run.CrossSeedsAdded > 0:
		run.Status = models.CrossSeedRunStatusPartial
	case run.CandidatesFailed > 0 && run.CrossSeedsAdded == 0:
		run.Status = models.CrossSeedRunStatusFailed
	default:
		run.Status = models.CrossSeedRunStatusSuccess
	}

	summary := fmt.Sprintf("processed=%d candidates=%d added=%d skipped=%d failed=%d", processed, run.CandidatesFound, run.CrossSeedsAdded, run.CandidatesSkipped, run.CandidatesFailed)
	run.Message = &summary

	// Use context.WithoutCancel to ensure DB update succeeds even on cancellation
	updateCtx := ctx
	if ctx.Err() != nil {
		run.Status = models.CrossSeedRunStatusFailed
		cancelMsg := "canceled by user"
		run.ErrorMessage = &cancelMsg
		updateCtx = context.WithoutCancel(ctx)
	}

	if updated, updateErr := s.updateAutomationRunWithRetry(updateCtx, run); updateErr == nil {
		run = updated
	} else {
		log.Warn().Err(updateErr).Msg("Failed to persist automation run update")
	}

	// Opportunistic cleanup of stale feed items (older than 30 days)
	if s.automationStore != nil {
		cutoff := time.Now().UTC().Truncate(24 * time.Hour).Add(-30 * 24 * time.Hour)
		if _, pruneErr := s.automationStore.PruneFeedItems(ctx, cutoff); pruneErr != nil {
			log.Debug().Err(pruneErr).Msg("Failed to prune cross-seed feed cache")
		}
	}

	return run, runErr
}

func (s *Service) processAutomationCandidate(ctx context.Context, run *models.CrossSeedRun, settings *models.CrossSeedAutomationSettings, autoCtx *automationContext, result jackett.SearchResult, opts AutomationRunOptions, indexerInfo map[int]jackett.EnabledIndexerInfo) (models.CrossSeedFeedItemStatus, *string, error) {
	sourceIndexer := result.Indexer
	if resolved := jackett.GetIndexerNameFromInfo(indexerInfo, result.IndexerID); resolved != "" {
		sourceIndexer = resolved
	}

	findReq := &FindCandidatesRequest{
		TorrentName:            result.Title,
		SourceIndexer:          sourceIndexer,
		FindIndividualEpisodes: settings.FindIndividualEpisodes,
	}
	if len(settings.TargetInstanceIDs) > 0 {
		findReq.TargetInstanceIDs = append([]int(nil), settings.TargetInstanceIDs...)
	}

	candidatesResp, err := s.findCandidatesWithAutomationContext(ctx, findReq, autoCtx)
	if err != nil {
		run.CandidatesFailed++
		return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("find candidates: %w", err)
	}

	candidateCount := len(candidatesResp.Candidates)
	var boundMatches []boundAnnouncementMatch
	// Season packs with same-title episodes in the library have no direct
	// candidates but can still be assembled by the diversion inside CrossSeed,
	// so they must proceed to download instead of skipping.
	seasonPackDivertible := candidatesResp.seasonPackEpisodeCandidates && settings.SeasonPackAutomationEnabled
	// When diversion is the only reason to download, a recent diversion failure
	// for the same release name makes the download a guaranteed waste until the
	// cooldown lapses. Feed items marked skipped are re-evaluated every run, so
	// the item retries once the cooldown expires.
	if candidateCount == 0 && seasonPackDivertible && s.seasonPackFailCooldownActive(ctx, result.Title, minSearchCooldownMinutes*time.Minute) {
		run.CandidatesSkipped++
		run.Results = append(run.Results, models.CrossSeedRunResult{
			InstanceName: result.Indexer,
			IndexerName:  result.Indexer,
			Success:      false,
			Status:       "skipped",
			Message:      "Season pack diversion recently failed for " + result.Title + "; waiting out cooldown",
		})
		return models.CrossSeedFeedItemStatusSkipped, nil, nil
	}
	if candidateCount == 0 && !seasonPackDivertible {
		boundMatches, err = s.findRSSAnnouncementMatches(ctx, result, settings, autoCtx)
		if err != nil {
			run.CandidatesFailed++
			return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("find RSS announcement matches: %w", err)
		}
		candidateCount = len(boundMatches)
	}
	if candidateCount == 0 && !seasonPackDivertible {
		run.CandidatesSkipped++
		run.Results = append(run.Results, models.CrossSeedRunResult{
			InstanceName: result.Indexer,
			IndexerName:  result.Indexer,
			Success:      false,
			Status:       "no_match",
			Message:      "No matching torrents for " + result.Title,
		})
		return models.CrossSeedFeedItemStatusSkipped, nil, nil
	}
	if settings.SkipRecheck && len(boundMatches) > 0 {
		allowed := boundMatches[:0]
		for _, match := range boundMatches {
			if !searchDecisionRequiresVerification(match.decision) {
				allowed = append(allowed, match)
			}
		}
		boundMatches = allowed
		candidateCount = len(boundMatches)
		if candidateCount == 0 {
			run.CandidatesSkipped++
			run.Results = append(run.Results, models.CrossSeedRunResult{
				InstanceName: result.Indexer,
				IndexerName:  result.Indexer,
				Success:      false,
				Status:       "skipped_recheck",
				Message:      "RSS exact-size match requires a recheck but SkipRecheck is enabled",
			})
			return models.CrossSeedFeedItemStatusSkipped, nil, nil
		}
	}

	run.CandidatesFound++

	if opts.DryRun {
		run.CandidatesSkipped++
		dryRunMessage := fmt.Sprintf("Dry run: %d viable candidates", candidateCount)
		if candidateCount == 0 && seasonPackDivertible {
			dryRunMessage = "Dry run: season pack could be assembled from local episodes"
		}
		run.Results = append(run.Results, models.CrossSeedRunResult{
			InstanceName: result.Indexer,
			IndexerName:  result.Indexer,
			Success:      true,
			Status:       "dry-run",
			Message:      dryRunMessage,
		})
		return models.CrossSeedFeedItemStatusSkipped, nil, nil
	}

	precheckCandidates := candidatesResp.Candidates
	if len(boundMatches) > 0 {
		precheckCandidates = boundRSSPrecheckCandidates(boundMatches)
	}

	// Optimization: If RSS feed provides infohash, check if torrent already exists
	// on ALL candidate instances before downloading. This avoids unnecessary downloads
	// when the exact torrent (by hash) is already present - commonly happens when
	// autobrr grabs via IRC before RSS automation processes the same release.
	if result.InfoHashV1 != "" {
		allExist := true
		var existingResults []models.CrossSeedRunResult

		for _, candidate := range precheckCandidates {
			existing, exists, hashErr := s.syncManager.HasTorrentByAnyHash(ctx, candidate.InstanceID, []string{result.InfoHashV1})
			if hashErr != nil {
				// Context cancellation should propagate, not trigger fallback
				if errors.Is(hashErr, context.Canceled) || errors.Is(hashErr, context.DeadlineExceeded) {
					run.CandidatesFailed++
					return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("hash check canceled: %w", hashErr)
				}
				log.Warn().
					Err(hashErr).
					Int("instanceID", candidate.InstanceID).
					Str("instanceName", candidate.InstanceName).
					Str("hash", result.InfoHashV1).
					Str("title", result.Title).
					Msg("[RSS] Failed to check existing hash on instance, will proceed with download")
				allExist = false
				break
			}

			if exists && existing != nil {
				existingResults = append(existingResults, models.CrossSeedRunResult{
					InstanceID:         candidate.InstanceID,
					InstanceName:       candidate.InstanceName,
					IndexerName:        result.Indexer,
					Success:            false,
					Status:             "exists",
					Message:            "Torrent already exists (infohash pre-check)",
					MatchedTorrentHash: func() *string { h := existing.Hash; return &h }(),
					MatchedTorrentName: func() *string { n := existing.Name; return &n }(),
				})
			} else {
				allExist = false
				break
			}
		}

		if allExist && len(existingResults) > 0 {
			run.CandidatesSkipped++
			run.Results = append(run.Results, existingResults...)

			log.Debug().
				Str("title", result.Title).
				Str("hash", result.InfoHashV1).
				Int("instances", len(existingResults)).
				Msg("[RSS] Skipped download - torrent already exists on all candidate instances (infohash pre-check)")

			return models.CrossSeedFeedItemStatusProcessed, &result.InfoHashV1, nil
		}
	}

	// Fallback for UNIT3D and similar trackers: check if torrent URL exists in comments.
	// UNIT3D torrents include the source URL in the comment field (e.g., "https://seedpool.org/torrents/607803").
	// The GUID from RSS is typically this same URL, so we can match without needing infohash.
	if commentURL := extractTorrentURLForCommentMatch(result.GUID, result.InfoURL); commentURL != "" {
		allExist := true
		var existingResults []models.CrossSeedRunResult

		for _, candidate := range precheckCandidates {
			// Check for context cancellation before processing each candidate
			if ctx.Err() != nil {
				run.CandidatesFailed++
				return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("comment URL pre-check canceled: %w", ctx.Err())
			}

			found := false
			var matchedTorrent *qbt.Torrent

			for i := range candidate.Torrents {
				t := &candidate.Torrents[i]
				if t.Comment != "" && strings.Contains(t.Comment, commentURL) {
					found = true
					matchedTorrent = t
					break
				}
			}

			if found && matchedTorrent != nil {
				existingResults = append(existingResults, models.CrossSeedRunResult{
					InstanceID:         candidate.InstanceID,
					InstanceName:       candidate.InstanceName,
					IndexerName:        result.Indexer,
					Success:            false,
					Status:             "exists",
					Message:            "Torrent already exists (comment URL pre-check)",
					MatchedTorrentHash: func() *string { h := matchedTorrent.Hash; return &h }(),
					MatchedTorrentName: func() *string { n := matchedTorrent.Name; return &n }(),
				})
			} else {
				allExist = false
				break
			}
		}

		if allExist && len(existingResults) > 0 {
			run.CandidatesSkipped++
			run.Results = append(run.Results, existingResults...)

			log.Debug().
				Str("title", result.Title).
				Str("commentURL", redact.URLString(commentURL)).
				Int("instances", len(existingResults)).
				Msg("[RSS] Skipped download - torrent already exists on all candidate instances (comment URL pre-check)")

			return models.CrossSeedFeedItemStatusProcessed, nil, nil
		}
	}

	torrentBytes, err := s.downloadTorrent(ctx, jackett.TorrentDownloadRequest{
		IndexerID:   result.IndexerID,
		DownloadURL: result.DownloadURL,
		GUID:        result.GUID,
		Title:       result.Title,
		Size:        result.Size,
	})
	if err != nil {
		run.CandidatesFailed++
		return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("download torrent: %w", err)
	}

	encodedTorrent := base64.StdEncoding.EncodeToString(torrentBytes)
	var (
		resp      *CrossSeedResponse
		invokeErr error
	)
	if len(boundMatches) == 0 {
		resp, invokeErr = s.invokeCrossSeed(ctx, s.newAutomationCrossSeedRequest(encodedTorrent, sourceIndexer, settings))
		if invokeErr != nil {
			run.CandidatesFailed++
			return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("cross-seed request: %w", invokeErr)
		}
	} else {
		resp, invokeErr = s.invokeBoundAnnouncementMatches(ctx, boundMatches, func(source boundAnnouncementMatch) *CrossSeedRequest {
			req := s.newAutomationCrossSeedRequest(encodedTorrent, sourceIndexer, settings)
			req.TargetInstanceIDs = []int{source.instanceID}
			req.SearchDecision = source.decision
			return req
		})
	}

	var infoHash *string
	if resp.TorrentInfo != nil && resp.TorrentInfo.Hash != "" {
		hash := resp.TorrentInfo.Hash
		infoHash = &hash
	}

	itemStatus := models.CrossSeedFeedItemStatusSkipped
	itemHasSuccess := false
	itemHasFailure := false
	itemHadExisting := false

	if len(resp.Results) == 0 {
		run.CandidatesSkipped++
		run.Results = append(run.Results, models.CrossSeedRunResult{
			InstanceName: result.Indexer,
			IndexerName:  result.Indexer,
			Success:      false,
			Status:       "no_result",
			Message:      "Cross-seed returned no actionable instances for " + result.Title,
		})
		return itemStatus, infoHash, nil
	}

	for _, instanceResult := range resp.Results {
		mapped := models.CrossSeedRunResult{
			InstanceID:            instanceResult.InstanceID,
			InstanceName:          instanceResult.InstanceName,
			IndexerName:           result.Indexer,
			Success:               instanceResult.Success,
			Status:                instanceResult.Status,
			Message:               instanceResult.Message,
			CrossSeedInstanceID:   instanceResult.CrossSeedInstanceID,
			CrossSeedInstanceName: instanceResult.CrossSeedInstanceName,
		}
		if instanceResult.MatchedTorrent != nil {
			hash := instanceResult.MatchedTorrent.Hash
			name := instanceResult.MatchedTorrent.Name
			mapped.MatchedTorrentHash = &hash
			mapped.MatchedTorrentName = &name
		}

		run.Results = append(run.Results, mapped)

		switch instanceResult.class() {
		case classAdded:
			itemHasSuccess = true
			run.CrossSeedsAdded++
		case classExists:
			itemHadExisting = true
		case classSkipped:
		default:
			itemHasFailure = true
		}
	}

	// One feed item is one candidate: it lands in one bucket no matter how
	// many instances it was applied to. A candidate with one add and one
	// failed instance counts as added; the failure stays in run.Results.
	switch {
	case itemHasSuccess:
		itemStatus = models.CrossSeedFeedItemStatusProcessed
	case itemHasFailure:
		itemStatus = models.CrossSeedFeedItemStatusFailed
		run.CandidatesFailed++
	case itemHadExisting:
		itemStatus = models.CrossSeedFeedItemStatusProcessed
		run.CandidatesSkipped++
	default:
		itemStatus = models.CrossSeedFeedItemStatusSkipped
		run.CandidatesSkipped++
	}

	return itemStatus, infoHash, invokeErr
}

// markFeedItem records a feed item's status. previous is the stored status,
// or pending when the item has no row. Skipped and failed items are retried
// on every poll, so an unchanged outcome only refreshes last_seen_at: the
// full upsert would rewrite and lock every feed row on every poll.
func (s *Service) markFeedItem(ctx context.Context, result jackett.SearchResult, previous, status models.CrossSeedFeedItemStatus, runID int64, infoHash *string) {
	if s.automationStore == nil {
		return
	}

	if status == previous && infoHash == nil {
		if err := s.automationStore.TouchFeedItem(ctx, result.GUID, result.IndexerID, time.Now()); err != nil {
			log.Debug().Err(err).Str("guid", redact.URLString(result.GUID)).Msg("Failed to refresh cross-seed feed item last seen")
		}
		return
	}

	var runPtr *int64
	if runID > 0 {
		runPtr = &runID
	}

	item := &models.CrossSeedFeedItem{
		GUID:       result.GUID,
		IndexerID:  result.IndexerID,
		Title:      result.Title,
		LastStatus: status,
		LastRunID:  runPtr,
		InfoHash:   infoHash,
	}

	if err := s.automationStore.MarkFeedItem(ctx, item); err != nil {
		log.Debug().Err(err).Str("guid", redact.URLString(result.GUID)).Msg("Failed to persist cross-seed feed item state")
	}
}

func (s *Service) buildAutomationSnapshots(ctx context.Context, targetInstanceIDs []int, settings *models.CrossSeedAutomationSettings) *automationSnapshots {
	if s == nil || s.instanceStore == nil || s.syncManager == nil {
		return nil
	}

	snapshots := &automationSnapshots{
		instances: make(map[int]*automationInstanceSnapshot),
	}

	instanceIDs := normalizeInstanceIDs(targetInstanceIDs)
	if len(instanceIDs) == 0 {
		instances, err := s.instanceStore.List(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to list instances for automation snapshot")
			return nil
		}
		for _, inst := range instances {
			if inst == nil {
				continue
			}
			instanceIDs = append(instanceIDs, inst.ID)
			snapshots.instances[inst.ID] = &automationInstanceSnapshot{
				instance: inst,
			}
		}
	}

	// Check if RSS source filters are configured
	var rssFilter sourceFilter
	if settings != nil {
		rssFilter = defaultsFor(triggerRSS, settings).sourceFilter
	}
	hasRSSSourceFilters := rssFilter.hasRules()

	if settings != nil {
		log.Debug().
			Strs("includeCategories", settings.RSSSourceCategories).
			Strs("excludeCategories", settings.RSSSourceExcludeCategories).
			Strs("includeTags", settings.RSSSourceTags).
			Strs("excludeTags", settings.RSSSourceExcludeTags).
			Bool("hasFilters", hasRSSSourceFilters).
			Msg("[RSS] Source filter settings loaded for automation snapshot")
	}

	for _, instanceID := range instanceIDs {
		snap := snapshots.instances[instanceID]
		if snap == nil {
			instance, err := s.instanceStore.Get(ctx, instanceID)
			if err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get instance for automation snapshot")
				continue
			}
			snap = &automationInstanceSnapshot{instance: instance}
			snapshots.instances[instanceID] = snap
		}

		torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterAll})
		if err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("instanceName", snap.instance.Name).
				Msg("Failed to get torrents for automation snapshot, skipping")
			continue
		}

		// Filter torrents inline without creating intermediate copies
		if hasRSSSourceFilters {
			var counts sourceFilterCounts
			originalCount := len(torrents)
			filtered := make([]qbt.Torrent, 0, len(torrents))
			for i := range torrents {
				kept := rssFilter.match(&torrents[i])
				counts.add(torrents[i].Category, kept)
				if kept {
					filtered = append(filtered, torrents[i])
				}
			}
			torrents = filtered

			// Log filter results
			if !counts.empty() {
				log.Debug().
					Int("instanceID", instanceID).
					Str("instanceName", snap.instance.Name).
					Int("original", originalCount).
					Int("filtered", len(torrents)).
					Strs("excludedCategories", categoryCountSummary(counts.excluded)).
					Strs("includedCategories", categoryCountSummary(counts.included)).
					Msg("[RSS] Source filter results")
			}
		}

		snap.torrents = torrents
	}

	if len(snapshots.instances) == 0 {
		return nil
	}

	return snapshots
}

func (s *Service) findRSSAnnouncementMatches(ctx context.Context, result jackett.SearchResult, settings *models.CrossSeedAutomationSettings, autoCtx *automationContext) ([]boundAnnouncementMatch, error) {
	if result.Size <= 0 || settings == nil {
		return nil, nil
	}

	candidate := namedRelease{release: s.releaseCache.Parse(result.Title), rawName: result.Title}
	// Resolve ARR aliases only when a torrent survives the exact-size gate
	// below, so feed items with no byte-size collision never touch ARR.
	aliasLookup := sync.OnceValues(func() ([]string, *models.EpisodeMap) {
		return s.announceAliasTitles(ctx, result.Title, DetermineContentType(candidate.release).ContentType)
	})
	snapshots := (*automationSnapshots)(nil)
	if autoCtx != nil {
		snapshots = autoCtx.snapshots
	}
	if snapshots == nil {
		snapshots = s.buildAutomationSnapshots(ctx, settings.TargetInstanceIDs, settings)
	}
	if snapshots == nil {
		return nil, nil
	}

	instanceIDs := make([]int, 0, len(snapshots.instances))
	for instanceID := range snapshots.instances {
		instanceIDs = append(instanceIDs, instanceID)
	}
	sort.Ints(instanceIDs)

	gate := s.sourceGate()
	rssFilter := defaultsFor(triggerRSS, settings).sourceFilter
	matches := make([]boundAnnouncementMatch, 0, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		snapshot := snapshots.instances[instanceID]
		if snapshot == nil || snapshot.instance == nil {
			continue
		}
		for i := range snapshot.torrents {
			torrent := &snapshot.torrents[i]
			if gate.check(torrent, rssFilter) != sourcePass {
				continue
			}
			if sourceSize := searchSourceSize(torrent); sourceSize <= 0 || sourceSize != result.Size {
				continue
			}

			aliasTitles, episodeMap := aliasLookup()
			decision := s.classifyAnnouncementSource(ctx, snapshot.instance.ID, torrent, candidate, result.Size, announcementMatchPolicy{
				findIndividualEpisodes: settings.FindIndividualEpisodes,
				rescueTitleMismatches:  settings.RescueTitleMismatches && !settings.SkipRecheck,
				allowUnknownSize:       false,
				candidateTitles:        aliasTitles,
				episodeMap:             episodeMap,
			})
			if !decision.replayable || !decision.decision.Accepted {
				continue
			}

			match := boundAnnouncementMatch{
				instanceID:   snapshot.instance.ID,
				instanceName: snapshot.instance.Name,
				torrent:      *torrent,
				decision:     decision.decision.provenance().bindSource(snapshot.instance.ID, torrent.Hash),
			}
			matches = append(matches, match)
		}
	}
	sortBoundAnnouncementMatches(matches)

	return matches, nil
}

func (s *Service) newAutomationCrossSeedRequest(encodedTorrent, sourceIndexer string, settings *models.CrossSeedAutomationSettings) *CrossSeedRequest {
	req := defaultsFor(triggerRSS, settings).request(encodedTorrent, sourceIndexer)
	req.TargetInstanceIDs = append([]int(nil), settings.TargetInstanceIDs...)
	return req
}

func boundRSSPrecheckCandidates(matches []boundAnnouncementMatch) []CrossSeedCandidate {
	candidates := make([]CrossSeedCandidate, 0, len(matches))
	for _, match := range matches {
		if len(candidates) == 0 || candidates[len(candidates)-1].InstanceID != match.instanceID {
			candidates = append(candidates, CrossSeedCandidate{
				InstanceID:   match.instanceID,
				InstanceName: match.instanceName,
			})
		}
		last := &candidates[len(candidates)-1]
		last.Torrents = append(last.Torrents, match.torrent)
	}
	return candidates
}

func (s *Service) automationCacheKey(name string, findIndividual bool) string {
	if s == nil || s.releaseCache == nil || s.stringNormalizer == nil {
		return ""
	}

	release := s.releaseCache.Parse(name)
	title := s.stringNormalizer.Normalize(release.Title)
	group := s.stringNormalizer.Normalize(release.Group)
	source := s.stringNormalizer.Normalize(release.Source)
	resolution := s.stringNormalizer.Normalize(release.Resolution)
	collection := s.stringNormalizer.Normalize(release.Collection)

	return fmt.Sprintf("%s|%d|%d|%d|%d|%s|%s|%s|%s|%t",
		title,
		release.Type,
		release.Series,
		release.Episode,
		release.Year,
		group,
		source,
		resolution,
		collection,
		findIndividual)
}

func (s *Service) findCandidatesWithAutomationContext(ctx context.Context, req *FindCandidatesRequest, autoCtx *automationContext) (*FindCandidatesResponse, error) {
	if autoCtx == nil {
		return s.findCandidates(ctx, req, nil)
	}

	if autoCtx.candidateCache == nil {
		autoCtx.candidateCache = make(map[string]*FindCandidatesResponse)
	}

	cacheKey := s.automationCacheKey(req.TorrentName, req.FindIndividualEpisodes)
	if cacheKey != "" {
		if cached, ok := autoCtx.candidateCache[cacheKey]; ok {
			return cached, nil
		}
	}

	resp, err := s.findCandidates(ctx, req, autoCtx.snapshots)
	if err != nil {
		return nil, err
	}

	if cacheKey != "" {
		autoCtx.candidateCache[cacheKey] = resp
	}
	return resp, nil
}

// extractTorrentURLForCommentMatch extracts a torrent URL suitable for matching against
// torrent comments. UNIT3D and similar trackers embed the source URL in the torrent's
// comment field. Returns empty string if no suitable URL pattern is found.
//
// Matches any HTTPS URL containing these path patterns:
//   - /torrents/ (UNIT3D style: seedpool.org, aither.cc, blutopia.cc, etc.)
//   - /details/ (BHD style: beyond-hd.me)
func extractTorrentURLForCommentMatch(guid, infoURL string) string {
	// Try GUID first (typically the details URL for UNIT3D)
	if url := parseTorrentDetailsURL(guid); url != "" {
		return url
	}

	// Fall back to InfoURL
	if url := parseTorrentDetailsURL(infoURL); url != "" {
		return url
	}

	return ""
}

// parseTorrentDetailsURL checks if the URL looks like a tracker torrent details page
// and returns it normalized for comment matching.
func parseTorrentDetailsURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	// Must be HTTPS URL
	if !strings.HasPrefix(rawURL, "https://") {
		return ""
	}

	// Check for common torrent details URL patterns:
	// - /torrents/ID (UNIT3D style)
	// - /details/ID (BHD style)
	if strings.Contains(rawURL, "/torrents/") || strings.Contains(rawURL, "/details/") {
		return rawURL
	}

	return ""
}
