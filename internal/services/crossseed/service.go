// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package crossseed provides intelligent cross-seeding functionality for torrents.
//
// Key features:
// - Uses moistari/rls parser for robust release name parsing on both torrent names and file names
// - TTL-based caching (5 minutes) of rls parsing results for performance (rls parsing is slow)
// - Fuzzy matching for finding related content (single episodes, season packs, etc.)
// - Metadata enrichment: fills missing group, resolution, codec, source, etc. from season pack torrent names
// - Season pack support: matches individual episodes with season packs and vice versa
// - Partial matching: detects when single episode files are contained within season packs
package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/filesmanager"
	"github.com/autobrr/qui/internal/services/jackett"
)

// Service provides cross-seed functionality
type Service struct {
	instanceStore *models.InstanceStore
	syncManager   *qbittorrent.SyncManager
	filesManager  *filesmanager.Service
	releaseCache  *ReleaseCache
	// searchResultCache stores the most recent search results per torrent hash so that
	// apply requests can be validated without trusting client-provided URLs.
	searchResultCache *ttlcache.Cache[string, []TorrentSearchResult]

	automationStore *models.CrossSeedStore
	jackettService  *jackett.Service

	automationMu     sync.Mutex
	automationCancel context.CancelFunc
	automationWake   chan struct{}
	runActive        atomic.Bool
}

const searchResultCacheTTL = 5 * time.Minute

// NewService creates a new cross-seed service
func NewService(
	instanceStore *models.InstanceStore,
	syncManager *qbittorrent.SyncManager,
	filesManager *filesmanager.Service,
	automationStore *models.CrossSeedStore,
	jackettService *jackett.Service,
) *Service {
	searchCache := ttlcache.New(ttlcache.Options[string, []TorrentSearchResult]{}.
		SetDefaultTTL(searchResultCacheTTL))

	return &Service{
		instanceStore:     instanceStore,
		syncManager:       syncManager,
		filesManager:      filesManager,
		releaseCache:      NewReleaseCache(),
		searchResultCache: searchCache,
		automationStore:   automationStore,
		jackettService:    jackettService,
		automationWake:    make(chan struct{}, 1),
	}
}

// ErrAutomationRunning indicates a cross-seed automation run is already in progress.
var ErrAutomationRunning = errors.New("cross-seed automation already running")

// AutomationRunOptions configures a manual automation run.
type AutomationRunOptions struct {
	RequestedBy string
	Mode        models.CrossSeedRunMode
	DryRun      bool
	Limit       int
}

// AutomationStatus summarises scheduler state for the API.
type AutomationStatus struct {
	Settings  *models.CrossSeedAutomationSettings `json:"settings"`
	LastRun   *models.CrossSeedRun                `json:"lastRun,omitempty"`
	NextRunAt *time.Time                          `json:"nextRunAt,omitempty"`
	Running   bool                                `json:"running"`
}

// GetAutomationSettings returns the persisted automation configuration.
func (s *Service) GetAutomationSettings(ctx context.Context) (*models.CrossSeedAutomationSettings, error) {
	if s.automationStore == nil {
		return models.DefaultCrossSeedAutomationSettings(), nil
	}

	settings, err := s.automationStore.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load automation settings: %w", err)
	}

	return settings, nil
}

// UpdateAutomationSettings persists automation configuration and wakes the scheduler.
func (s *Service) UpdateAutomationSettings(ctx context.Context, settings *models.CrossSeedAutomationSettings) (*models.CrossSeedAutomationSettings, error) {
	if settings == nil {
		return nil, errors.New("settings cannot be nil")
	}

	// Validate and normalize settings before checking store
	s.validateAndNormalizeSettings(settings)

	if s.automationStore == nil {
		return nil, errors.New("automation storage not configured")
	}

	updated, err := s.automationStore.UpsertSettings(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("persist automation settings: %w", err)
	}

	s.signalAutomationWake()

	return updated, nil
}

// validateAndNormalizeSettings validates and normalizes automation settings
func (s *Service) validateAndNormalizeSettings(settings *models.CrossSeedAutomationSettings) {
	if settings.RunIntervalMinutes <= 0 {
		settings.RunIntervalMinutes = 120
	}
	if settings.MaxResultsPerRun <= 0 {
		settings.MaxResultsPerRun = 50
	}
	if settings.SizeMismatchTolerancePercent < 0 {
		settings.SizeMismatchTolerancePercent = 5.0 // Default to 5% if negative
	}
	// Cap at 100% to prevent unreasonable tolerances
	if settings.SizeMismatchTolerancePercent > 100.0 {
		settings.SizeMismatchTolerancePercent = 100.0
	}
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

// RunAutomation executes a cross-seed automation cycle immediately.
func (s *Service) RunAutomation(ctx context.Context, opts AutomationRunOptions) (*models.CrossSeedRun, error) {
	if s.automationStore == nil || s.jackettService == nil {
		return nil, errors.New("cross-seed automation not configured")
	}

	if !s.runActive.CompareAndSwap(false, true) {
		return nil, ErrAutomationRunning
	}
	defer s.runActive.Store(false)

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
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

	// Guard against auto runs when disabled
	if opts.Mode == models.CrossSeedRunModeAuto && !settings.Enabled {
		return nil, fmt.Errorf("cross-seed automation disabled")
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

	finalRun, execErr := s.executeAutomationRun(ctx, storedRun, settings, opts)
	if execErr != nil {
		return finalRun, execErr
	}

	return finalRun, nil
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
	log.Info().Msg("Starting cross-seed automation loop")
	defer log.Info().Msg("Cross-seed automation loop stopped")

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
				if !errors.Is(err, ErrAutomationRunning) {
					log.Warn().Err(err).Msg("Cross-seed automation run failed")
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

func (s *Service) computeNextRunDelay(ctx context.Context, settings *models.CrossSeedAutomationSettings) (time.Duration, bool) {
	if settings == nil || !settings.Enabled {
		return 5 * time.Minute, false
	}

	if s.automationStore == nil {
		return time.Hour, false
	}

	intervalMinutes := settings.RunIntervalMinutes
	if intervalMinutes <= 0 {
		intervalMinutes = 120
	}
	interval := time.Duration(intervalMinutes) * time.Minute

	if interval < time.Minute {
		interval = time.Minute
	}

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

	remaining := interval - elapsed
	if remaining < time.Second {
		remaining = time.Second
	}

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
	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = settings.MaxResultsPerRun
	}
	if limit <= 0 {
		limit = 50
	}

	releaseFetchLimit := int(math.Ceil(float64(limit) * 1.5))
	if releaseFetchLimit < limit {
		releaseFetchLimit = limit
	}

	searchResp, err := s.jackettService.Recent(ctx, releaseFetchLimit, settings.TargetIndexerIDs)
	if err != nil {
		msg := err.Error()
		run.ErrorMessage = &msg
		run.Status = models.CrossSeedRunStatusFailed
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		if updated, updateErr := s.automationStore.UpdateRun(ctx, run); updateErr == nil {
			run = updated
		}
		return run, err
	}

	processed := 0
	var runErr error

	for _, result := range searchResp.Results {
		if ctx.Err() != nil {
			runErr = ctx.Err()
			break
		}

		if limit > 0 && processed >= limit {
			break
		}

		if strings.TrimSpace(result.GUID) == "" || result.IndexerID == 0 {
			continue
		}

		alreadyHandled, lastStatus, err := s.automationStore.HasProcessedFeedItem(ctx, result.GUID, result.IndexerID)
		if err != nil {
			log.Warn().Err(err).Str("guid", result.GUID).Msg("Failed to check feed item cache")
		}

		run.TotalFeedItems++

		if alreadyHandled && lastStatus == models.CrossSeedFeedItemStatusProcessed {
			s.markFeedItem(ctx, result, lastStatus, run.ID, nil)
			continue
		}

		status, infoHash, procErr := s.processAutomationCandidate(ctx, run, settings, result, opts)
		if procErr != nil {
			if runErr == nil {
				runErr = procErr
			}
			log.Debug().Err(procErr).Str("title", result.Title).Msg("Cross-seed automation candidate failed")
		}

		processed++
		s.markFeedItem(ctx, result, status, run.ID, infoHash)
	}

	completed := time.Now().UTC()
	run.CompletedAt = &completed

	switch {
	case run.TorrentsFailed > 0 && run.TorrentsAdded > 0:
		run.Status = models.CrossSeedRunStatusPartial
	case run.TorrentsFailed > 0 && run.TorrentsAdded == 0:
		run.Status = models.CrossSeedRunStatusFailed
	default:
		run.Status = models.CrossSeedRunStatusSuccess
	}

	summary := fmt.Sprintf("processed=%d candidates=%d added=%d skipped=%d failed=%d", processed, run.CandidatesFound, run.TorrentsAdded, run.TorrentsSkipped, run.TorrentsFailed)
	run.Message = &summary

	if ctx.Err() != nil {
		run.Status = models.CrossSeedRunStatusPartial
		cancelMsg := "automation cancelled"
		run.ErrorMessage = &cancelMsg
	}

	if updated, updateErr := s.automationStore.UpdateRun(ctx, run); updateErr == nil {
		run = updated
	} else {
		log.Warn().Err(updateErr).Msg("Failed to persist automation run update")
	}

	// Opportunistic cleanup of stale feed items (older than 30 days)
	if s.automationStore != nil {
		cutoff := time.Now().Add(-30 * 24 * time.Hour)
		if _, pruneErr := s.automationStore.PruneFeedItems(ctx, cutoff); pruneErr != nil {
			log.Debug().Err(pruneErr).Msg("Failed to prune cross-seed feed cache")
		}
	}

	return run, runErr
}

func (s *Service) processAutomationCandidate(ctx context.Context, run *models.CrossSeedRun, settings *models.CrossSeedAutomationSettings, result jackett.SearchResult, opts AutomationRunOptions) (models.CrossSeedFeedItemStatus, *string, error) {
	sourceIndexer := result.Indexer
	if resolved := s.jackettService.GetIndexerName(ctx, result.IndexerID); resolved != "" {
		sourceIndexer = resolved
	}

	findReq := &FindCandidatesRequest{
		TorrentName:            result.Title,
		IgnorePatterns:         append([]string(nil), settings.IgnorePatterns...),
		SourceIndexer:          sourceIndexer,
		FindIndividualEpisodes: settings.FindIndividualEpisodes,
	}
	if len(settings.TargetInstanceIDs) > 0 {
		findReq.TargetInstanceIDs = append([]int(nil), settings.TargetInstanceIDs...)
	}

	candidatesResp, err := s.FindCandidates(ctx, findReq)
	if err != nil {
		run.TorrentsFailed++
		return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("find candidates: %w", err)
	}

	candidateCount := len(candidatesResp.Candidates)
	if candidateCount == 0 {
		run.TorrentsSkipped++
		run.Results = append(run.Results, models.CrossSeedRunResult{
			InstanceName: result.Indexer,
			Success:      false,
			Status:       "no_match",
			Message:      fmt.Sprintf("No matching torrents for %s", result.Title),
		})
		return models.CrossSeedFeedItemStatusSkipped, nil, nil
	}

	run.CandidatesFound++

	if opts.DryRun {
		run.TorrentsSkipped++
		run.Results = append(run.Results, models.CrossSeedRunResult{
			InstanceName: result.Indexer,
			Success:      true,
			Status:       "dry-run",
			Message:      fmt.Sprintf("Dry run: %d viable candidates", candidateCount),
		})
		return models.CrossSeedFeedItemStatusSkipped, nil, nil
	}

	torrentBytes, err := s.jackettService.DownloadTorrent(ctx, result.IndexerID, result.DownloadURL)
	if err != nil {
		run.TorrentsFailed++
		return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("download torrent: %w", err)
	}

	encodedTorrent := base64.StdEncoding.EncodeToString(torrentBytes)
	startPaused := settings.StartPaused

	req := &CrossSeedRequest{
		TorrentData:       encodedTorrent,
		TargetInstanceIDs: append([]int(nil), settings.TargetInstanceIDs...),
		Tags:              append([]string(nil), settings.Tags...),
		IgnorePatterns:    append([]string(nil), settings.IgnorePatterns...),
		SkipIfExists:      true,
	}
	if settings.Category != nil {
		req.Category = *settings.Category
	}
	req.StartPaused = &startPaused

	resp, err := s.CrossSeed(ctx, req)
	if err != nil {
		run.TorrentsFailed++
		return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("cross-seed request: %w", err)
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
		run.TorrentsSkipped++
		run.Results = append(run.Results, models.CrossSeedRunResult{
			InstanceName: result.Indexer,
			Success:      false,
			Status:       "no_result",
			Message:      fmt.Sprintf("Cross-seed returned no actionable instances for %s", result.Title),
		})
		return itemStatus, infoHash, nil
	}

	for _, instanceResult := range resp.Results {
		mapped := models.CrossSeedRunResult{
			InstanceID:   instanceResult.InstanceID,
			InstanceName: instanceResult.InstanceName,
			Success:      instanceResult.Success,
			Status:       instanceResult.Status,
			Message:      instanceResult.Message,
		}
		if instanceResult.MatchedTorrent != nil {
			hash := instanceResult.MatchedTorrent.Hash
			name := instanceResult.MatchedTorrent.Name
			mapped.MatchedTorrentHash = &hash
			mapped.MatchedTorrentName = &name
		}

		run.Results = append(run.Results, mapped)

		if instanceResult.Success {
			itemHasSuccess = true
			run.TorrentsAdded++
			continue
		}

		switch instanceResult.Status {
		case "exists":
			itemHadExisting = true
			run.TorrentsSkipped++
		case "no_match", "skipped":
			run.TorrentsSkipped++
		default:
			itemHasFailure = true
			run.TorrentsFailed++
		}
	}

	switch {
	case itemHasSuccess:
		itemStatus = models.CrossSeedFeedItemStatusProcessed
	case itemHasFailure:
		itemStatus = models.CrossSeedFeedItemStatusFailed
	case itemHadExisting:
		itemStatus = models.CrossSeedFeedItemStatusProcessed
	default:
		itemStatus = models.CrossSeedFeedItemStatusSkipped
	}

	return itemStatus, infoHash, nil
}

func (s *Service) markFeedItem(ctx context.Context, result jackett.SearchResult, status models.CrossSeedFeedItemStatus, runID int64, infoHash *string) {
	if s.automationStore == nil {
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
		log.Debug().Err(err).Str("guid", result.GUID).Msg("Failed to persist cross-seed feed item state")
	}
}

// FindCandidates finds ALL existing torrents across instances that match a title string
// Input: Just a torrent NAME (string) - the torrent doesn't exist yet
// Output: All existing torrents that have related content based on release name parsing
func (s *Service) FindCandidates(ctx context.Context, req *FindCandidatesRequest) (*FindCandidatesResponse, error) {
	if req.TorrentName == "" {
		return nil, fmt.Errorf("torrent_name is required")
	}

	// Parse the title string to understand what we're looking for
	targetRelease := s.releaseCache.Parse(req.TorrentName)

	// Build basic info for response
	sourceTorrentInfo := &TorrentInfo{
		Name: req.TorrentName,
	}

	// Determine which instances to search
	var searchInstanceIDs []int
	if len(req.TargetInstanceIDs) > 0 {
		searchInstanceIDs = req.TargetInstanceIDs
	} else {
		// Search all instances
		allInstances, err := s.instanceStore.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list instances: %w", err)
		}
		for _, inst := range allInstances {
			searchInstanceIDs = append(searchInstanceIDs, inst.ID)
		}
	}

	response := &FindCandidatesResponse{
		SourceTorrent: sourceTorrentInfo,
		Candidates:    make([]CrossSeedCandidate, 0),
	}

	totalCandidates := 0

	// Search ALL instances for torrents that match the title
	for _, instanceID := range searchInstanceIDs {
		instance, err := s.instanceStore.Get(ctx, instanceID)
		if err != nil {
			log.Warn().
				Int("instanceID", instanceID).
				Err(err).
				Msg("Failed to get instance info, skipping")
			continue
		}

		// Get all torrents from this instance
		torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
		if err != nil {
			log.Warn().
				Int("instanceID", instanceID).
				Str("instanceName", instance.Name).
				Err(err).
				Msg("Failed to get torrents from instance, skipping")
			continue
		}

		var matchedTorrents []qbt.Torrent
		matchTypeCounts := make(map[string]int)

		// Check EVERY torrent to see if it has the files we need
		for _, torrent := range torrents {
			// Only complete torrents can provide data
			if torrent.Progress < 1.0 {
				continue
			}

			candidateRelease := s.releaseCache.Parse(torrent.Name)

			// Check if releases are related (quick filter)
			if !s.releasesMatch(targetRelease, candidateRelease, req.FindIndividualEpisodes) {
				continue
			}

			// Get the candidate torrent's files to check if it has what we need
			candidateFilesPtr, err := s.syncManager.GetTorrentFiles(ctx, instanceID, torrent.Hash)
			if err != nil || candidateFilesPtr == nil || len(*candidateFilesPtr) == 0 {
				continue
			}
			candidateFiles := *candidateFilesPtr

			// Now check if this torrent actually has the files we need
			// This handles: single episode in season pack, season pack containing episodes, etc.
			matchType := s.getMatchTypeFromTitle(targetRelease, candidateRelease, candidateFiles, req.IgnorePatterns)
			if matchType != "" {
				matchedTorrents = append(matchedTorrents, torrent)
				matchTypeCounts[matchType]++
				log.Debug().
					Str("targetTitle", req.TorrentName).
					Str("existingTorrent", torrent.Name).
					Int("instanceID", instanceID).
					Str("instanceName", instance.Name).
					Str("matchType", matchType).
					Msg("Found matching torrent with required files")
			}
		}

		// Add all matches from this instance
		if len(matchedTorrents) > 0 {
			candidateMatchType := "release-match"
			var topCount int
			for mt, count := range matchTypeCounts {
				if count > topCount {
					topCount = count
					candidateMatchType = mt
				}
			}

			response.Candidates = append(response.Candidates, CrossSeedCandidate{
				InstanceID:   instanceID,
				InstanceName: instance.Name,
				Torrents:     matchedTorrents,
				MatchType:    candidateMatchType,
			})
			totalCandidates += len(matchedTorrents)
		}
	}

	log.Info().
		Str("targetTitle", req.TorrentName).
		Str("sourceIndexer", req.SourceIndexer).
		Int("instancesSearched", len(searchInstanceIDs)).
		Int("totalMatches", totalCandidates).
		Msg("Found existing torrents matching title")

	return response, nil
}

// CrossSeed attempts to add a new torrent for cross-seeding
// It finds existing 100% complete torrents that match the content and adds the new torrent
// paused to the same location with matching category and ATM state
func (s *Service) CrossSeed(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
	if req.TorrentData == "" {
		return nil, fmt.Errorf("torrent_data is required")
	}

	// Decode base64 torrent data
	torrentBytes, err := s.decodeTorrentData(req.TorrentData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode torrent data: %w", err)
	}

	// Parse torrent metadata to get name, hash, and files for validation
	torrentName, torrentHash, sourceFiles, err := s.parseTorrentMetadata(torrentBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse torrent: %w", err)
	}
	sourceRelease := s.releaseCache.Parse(torrentName)

	// Use FindCandidates to locate matching torrents
	findReq := &FindCandidatesRequest{
		TorrentName:            torrentName,
		TargetInstanceIDs:      req.TargetInstanceIDs,
		FindIndividualEpisodes: req.FindIndividualEpisodes,
	}
	if len(req.IgnorePatterns) > 0 {
		findReq.IgnorePatterns = append([]string(nil), req.IgnorePatterns...)
	}

	candidatesResp, err := s.FindCandidates(ctx, findReq)
	if err != nil {
		return nil, fmt.Errorf("failed to find candidates: %w", err)
	}

	response := &CrossSeedResponse{
		Success: false,
		Results: make([]InstanceCrossSeedResult, 0),
		TorrentInfo: &TorrentInfo{
			Name: torrentName,
			Hash: torrentHash,
		},
	}

	if response.TorrentInfo != nil {
		response.TorrentInfo.TotalFiles = len(sourceFiles)
		var totalSize int64
		for _, f := range sourceFiles {
			totalSize += f.Size
		}
		if totalSize > 0 {
			response.TorrentInfo.Size = totalSize
		}
	}

	// Process each instance with matching candidates
	for _, candidate := range candidatesResp.Candidates {
		result := s.processCrossSeedCandidate(ctx, candidate, torrentBytes, torrentHash, torrentName, req, sourceRelease, sourceFiles)
		response.Results = append(response.Results, result)
		if result.Success {
			response.Success = true
		}
	}

	// If no candidates found, return appropriate response
	if len(candidatesResp.Candidates) == 0 {
		// Try all target instances or all instances if not specified
		targetInstanceIDs := req.TargetInstanceIDs
		if len(targetInstanceIDs) == 0 {
			allInstances, err := s.instanceStore.List(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to list instances: %w", err)
			}
			for _, inst := range allInstances {
				targetInstanceIDs = append(targetInstanceIDs, inst.ID)
			}
		}

		for _, instanceID := range targetInstanceIDs {
			instance, err := s.instanceStore.Get(ctx, instanceID)
			if err != nil {
				log.Warn().
					Int("instanceID", instanceID).
					Err(err).
					Msg("Failed to get instance info")
				continue
			}

			response.Results = append(response.Results, InstanceCrossSeedResult{
				InstanceID:   instanceID,
				InstanceName: instance.Name,
				Success:      false,
				Status:       "no_match",
				Message:      "No matching torrents found with required files",
			})
		}
	}

	return response, nil
}

// processCrossSeedCandidate processes a single candidate for cross-seeding
func (s *Service) processCrossSeedCandidate(
	ctx context.Context,
	candidate CrossSeedCandidate,
	torrentBytes []byte,
	torrentHash,
	torrentName string,
	req *CrossSeedRequest,
	sourceRelease rls.Release,
	sourceFiles qbt.TorrentFiles,
) InstanceCrossSeedResult {
	result := InstanceCrossSeedResult{
		InstanceID:   candidate.InstanceID,
		InstanceName: candidate.InstanceName,
		Success:      false,
		Status:       "error",
	}

	// Check if torrent already exists
	existingTorrents, err := s.syncManager.GetAllTorrents(ctx, candidate.InstanceID)
	if err != nil {
		result.Message = fmt.Sprintf("Failed to check existing torrents: %v", err)
		return result
	}

	// Check by hash (v1 or v2)
	for _, existing := range existingTorrents {
		if existing.Hash == torrentHash || existing.InfohashV1 == torrentHash || existing.InfohashV2 == torrentHash {
			result.Success = false
			result.Status = "exists"
			result.Message = "Torrent already exists in this instance"
			result.MatchedTorrent = &MatchedTorrent{
				Hash:     existing.Hash,
				Name:     existing.Name,
				Progress: existing.Progress,
				Size:     existing.Size,
			}
			return result
		}
	}

	// Find the best matching torrent (100% complete)
	var matchedTorrent *qbt.Torrent
	for _, t := range candidate.Torrents {
		if t.Progress >= 1.0 {
			matchedTorrent = &t
			break
		}
	}

	if matchedTorrent == nil {
		result.Status = "no_match"
		result.Message = "No 100% complete matching torrent found"
		return result
	}

	candidateFilesPtr, err := s.syncManager.GetTorrentFiles(ctx, candidate.InstanceID, matchedTorrent.Hash)
	if err != nil || candidateFilesPtr == nil || len(*candidateFilesPtr) == 0 {
		result.Status = "no_match"
		if err != nil {
			result.Message = fmt.Sprintf("Failed to load candidate files: %v", err)
		} else {
			result.Message = "Candidate torrent has no file metadata available"
		}
		return result
	}
	candidateFiles := *candidateFilesPtr

	candidateRelease := s.releaseCache.Parse(matchedTorrent.Name)
	matchType := s.getMatchType(sourceRelease, candidateRelease, sourceFiles, candidateFiles, req.IgnorePatterns)
	if matchType == "" {
		result.Status = "no_match"
		result.Message = "Candidate torrent does not contain the required files"
		return result
	}
	if matchType == "partial-contains" {
		// Candidate provides only a subset of the desired season pack.
		result.Status = "no_match"
		result.Message = "Candidate torrent only contains a subset of the season pack files"
		return result
	}

	// Get torrent properties to extract save path
	props, err := s.syncManager.GetTorrentProperties(ctx, candidate.InstanceID, matchedTorrent.Hash)
	if err != nil {
		result.Message = fmt.Sprintf("Failed to get torrent properties: %v", err)
		return result
	}

	// Determine the appropriate save path for cross-seeding
	savePath := s.determineSavePath(torrentName, matchedTorrent, props)

	// Build options for adding the torrent
	options := make(map[string]string)

	startPaused := true
	if req.StartPaused != nil {
		startPaused = *req.StartPaused
	}
	if startPaused {
		options["paused"] = "true"
		options["stopped"] = "true"
	} else {
		options["paused"] = "false"
		options["stopped"] = "false"
	}

	// Skip hash checking since we're pointing to existing files
	options["skip_checking"] = "true"

	// Use category from request or matched torrent
	category := req.Category
	if category == "" {
		category = matchedTorrent.Category
	}
	if category != "" {
		options["category"] = category
	}

	// Handle AutoTMM and save path (use AutoManaged field from Torrent struct)
	if matchedTorrent.AutoManaged {
		options["autoTMM"] = "true"
	} else {
		options["autoTMM"] = "false"
		// Use the determined save path (handles season packs, custom paths, etc.)
		if savePath != "" {
			options["savepath"] = savePath
		}
	}

	addCrossSeedTag := true
	if req.AddCrossSeedTag != nil {
		addCrossSeedTag = *req.AddCrossSeedTag
	}

	tagSet := make(map[string]struct{})
	finalTags := make([]string, 0)
	addTag := func(tag string) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return
		}
		if _, exists := tagSet[tag]; exists {
			return
		}
		tagSet[tag] = struct{}{}
		finalTags = append(finalTags, tag)
	}

	for _, tag := range req.Tags {
		addTag(tag)
	}

	if len(req.Tags) == 0 && matchedTorrent.Tags != "" {
		split := strings.Split(matchedTorrent.Tags, ",")
		for _, tag := range split {
			addTag(tag)
		}
	}

	if addCrossSeedTag {
		addTag("cross-seed")
	}

	if len(finalTags) > 0 {
		options["tags"] = strings.Join(finalTags, ",")
	}

	// Add the torrent
	err = s.syncManager.AddTorrent(ctx, candidate.InstanceID, torrentBytes, options)
	if err != nil {
		// If adding fails, try with recheck enabled (skip_checking=false)
		log.Warn().
			Err(err).
			Int("instanceID", candidate.InstanceID).
			Str("torrentHash", torrentHash).
			Msg("Failed to add cross-seed torrent, retrying with recheck enabled")

		// Remove skip_checking and add with recheck
		delete(options, "skip_checking")
		err = s.syncManager.AddTorrent(ctx, candidate.InstanceID, torrentBytes, options)
		if err != nil {
			result.Message = fmt.Sprintf("Failed to add torrent even with recheck: %v", err)
			log.Error().
				Err(err).
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Msg("Failed to add cross-seed torrent after retry")
			return result
		}

		result.Message = fmt.Sprintf("Added torrent with recheck to %s (match: %s)", props.SavePath, matchType)
		log.Info().
			Int("instanceID", candidate.InstanceID).
			Str("instanceName", candidate.InstanceName).
			Msg("Successfully added cross-seed torrent with recheck")
	} else {
		result.Message = fmt.Sprintf("Added torrent paused to %s (match: %s)", props.SavePath, matchType)
	}

	// Wait for the torrent to be added and potentially rechecked
	newTorrent := s.waitForTorrentRecheck(ctx, candidate.InstanceID, torrentHash, &result)
	if newTorrent != nil {
		// If torrent is 100% complete (or very close), auto-resume it
		if newTorrent.Progress >= 0.999 {
			resumeErr := s.syncManager.BulkAction(ctx, candidate.InstanceID, []string{newTorrent.Hash}, "resume")
			if resumeErr != nil {
				log.Warn().
					Err(resumeErr).
					Int("instanceID", candidate.InstanceID).
					Str("hash", newTorrent.Hash).
					Msg("Failed to auto-resume 100% complete cross-seed torrent")
				result.Message += " (100% complete but failed to auto-resume)"
			} else {
				result.Message += " (100% complete, auto-resumed)"
				log.Info().
					Int("instanceID", candidate.InstanceID).
					Str("hash", newTorrent.Hash).
					Float64("progress", newTorrent.Progress).
					Msg("Auto-resumed 100% complete cross-seed torrent")
			}
		} else {
			// Torrent is not 100%, may be missing files (like .srt, sample, etc.)
			// This is expected and controlled by user's file layout
			log.Info().
				Int("instanceID", candidate.InstanceID).
				Str("hash", newTorrent.Hash).
				Float64("progress", newTorrent.Progress).
				Msg("Cross-seed torrent added but not 100% complete (may be missing optional files)")
			result.Message += fmt.Sprintf(" (%.1f%% complete, check manually)", newTorrent.Progress*100)
		}
	}

	// Success!
	result.Success = true
	result.Status = "added"
	result.MatchedTorrent = &MatchedTorrent{
		Hash:     matchedTorrent.Hash,
		Name:     matchedTorrent.Name,
		Progress: matchedTorrent.Progress,
		Size:     matchedTorrent.Size,
	}

	log.Info().
		Int("instanceID", candidate.InstanceID).
		Str("instanceName", candidate.InstanceName).
		Str("torrentHash", torrentHash).
		Str("matchedHash", matchedTorrent.Hash).
		Str("savePath", props.SavePath).
		Str("matchType", matchType).
		Bool("autoTMM", matchedTorrent.AutoManaged).
		Str("category", category).
		Msg("Successfully added cross-seed torrent")

	return result
}

// decodeTorrentData decodes base64-encoded torrent data
func (s *Service) decodeTorrentData(data string) ([]byte, error) {
	// Try standard base64
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		// Try URL-safe base64
		decoded, err = base64.URLEncoding.DecodeString(data)
		if err != nil {
			// Try raw base64 (no padding)
			decoded, err = base64.RawStdEncoding.DecodeString(data)
			if err != nil {
				return nil, fmt.Errorf("failed to decode base64: %w", err)
			}
		}
	}
	return decoded, nil
}

// parseTorrentName extracts the name and info hash from torrent bytes using anacrolix/torrent
func (s *Service) parseTorrentName(torrentBytes []byte) (name string, hash string, err error) {
	name, hash, _, err = s.parseTorrentMetadata(torrentBytes)
	return name, hash, err
}

func (s *Service) parseTorrentMetadata(torrentBytes []byte) (name string, hash string, files qbt.TorrentFiles, err error) {
	mi, err := metainfo.Load(bytes.NewReader(torrentBytes))
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to parse torrent metainfo: %w", err)
	}

	info, err := mi.UnmarshalInfo()
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to unmarshal torrent info: %w", err)
	}

	name = info.Name
	hash = mi.HashInfoBytes().HexString()

	if name == "" {
		return "", "", nil, fmt.Errorf("torrent has no name")
	}

	files = buildTorrentFilesFromInfo(name, info)

	return name, hash, files, nil
}

func buildTorrentFilesFromInfo(rootName string, info metainfo.Info) qbt.TorrentFiles {
	var files qbt.TorrentFiles

	if len(info.Files) == 0 {
		// Single file torrent
		files = make(qbt.TorrentFiles, 1)
		files[0] = struct {
			Availability float32 `json:"availability"`
			Index        int     `json:"index"`
			IsSeed       bool    `json:"is_seed,omitempty"`
			Name         string  `json:"name"`
			PieceRange   []int   `json:"piece_range"`
			Priority     int     `json:"priority"`
			Progress     float32 `json:"progress"`
			Size         int64   `json:"size"`
		}{
			Availability: 1,
			Index:        0,
			IsSeed:       true,
			Name:         rootName,
			PieceRange:   []int{0, 0},
			Priority:     0,
			Progress:     1,
			Size:         info.Length,
		}
		return files
	}

	files = make(qbt.TorrentFiles, len(info.Files))
	for i, f := range info.Files {
		displayPath := f.DisplayPath(&info)
		name := rootName
		if info.IsDir() && displayPath != "" {
			name = strings.Join([]string{rootName, displayPath}, "/")
		} else if !info.IsDir() && displayPath != "" {
			name = displayPath
		}

		pieceStart := f.BeginPieceIndex(info.PieceLength)
		pieceEnd := f.EndPieceIndex(info.PieceLength)

		files[i] = struct {
			Availability float32 `json:"availability"`
			Index        int     `json:"index"`
			IsSeed       bool    `json:"is_seed,omitempty"`
			Name         string  `json:"name"`
			PieceRange   []int   `json:"piece_range"`
			Priority     int     `json:"priority"`
			Progress     float32 `json:"progress"`
			Size         int64   `json:"size"`
		}{
			Availability: 1,
			Index:        i,
			IsSeed:       true,
			Name:         name,
			PieceRange:   []int{pieceStart, pieceEnd},
			Priority:     0,
			Progress:     1,
			Size:         f.Length,
		}
	}

	return files
}

// determineSavePath determines the appropriate save path for cross-seeding
// This handles various scenarios:
// - Season pack being added when individual episodes exist
// - Individual episode being added when a season pack exists
// - Custom paths and directory structures
func (s *Service) determineSavePath(newTorrentName string, matchedTorrent *qbt.Torrent, props *qbt.TorrentProperties) string {
	// Default to the matched torrent's save path
	baseSavePath := props.SavePath

	// Parse both torrent names to understand what we're dealing with
	newTorrentRelease := s.releaseCache.Parse(newTorrentName)
	matchedRelease := s.releaseCache.Parse(matchedTorrent.Name)

	// Scenario 1: New torrent is a season pack, matched torrent is a single episode
	// In this case, we want to use the parent directory of the episode
	if newTorrentRelease.Series > 0 && newTorrentRelease.Episode == 0 &&
		matchedRelease.Series > 0 && matchedRelease.Episode > 0 {
		// New is season pack (has series but no episode)
		// Matched is single episode (has both series and episode)
		// Use parent directory of the matched torrent's content path
		log.Debug().
			Str("newTorrent", newTorrentName).
			Str("matchedTorrent", matchedTorrent.Name).
			Str("baseSavePath", baseSavePath).
			Msg("Cross-seeding season pack from individual episode, using parent directory")

		// If the matched torrent is in a subdirectory, use the parent
		// This handles: /downloads/Show.S01E01/ -> /downloads/
		return baseSavePath
	}

	// Scenario 2: New torrent is a single episode, matched torrent is a season pack
	// Use the matched torrent's save path directly - the files are already there
	if newTorrentRelease.Series > 0 && newTorrentRelease.Episode > 0 &&
		matchedRelease.Series > 0 && matchedRelease.Episode == 0 {
		log.Debug().
			Str("newTorrent", newTorrentName).
			Str("matchedTorrent", matchedTorrent.Name).
			Str("savePath", baseSavePath).
			Msg("Cross-seeding individual episode from season pack")

		// The season pack already has the episode files, use its path directly
		return baseSavePath
	}

	// Scenario 3: Both are the same type (both season packs or both single episodes)
	// Or non-episodic content (movies, etc.)
	// Use the matched torrent's save path as-is
	log.Debug().
		Str("newTorrent", newTorrentName).
		Str("matchedTorrent", matchedTorrent.Name).
		Str("savePath", baseSavePath).
		Msg("Cross-seeding same content type, using matched torrent's path")

	return baseSavePath
}

// parseMusicReleaseFromTorrentName extracts music-specific metadata from torrent name
// First tries RLS's built-in parsing, then falls back to manual "Artist - Album" format parsing
func parseMusicReleaseFromTorrentName(baseRelease rls.Release, torrentName string) rls.Release {
	// First, try RLS's built-in parsing on the torrent name directly
	// This can handle complex release names like "Artist-Album-Edition-Source-Year-GROUP"
	torrentRelease := rls.ParseString(torrentName)

	// If RLS detected it as music and extracted artist/title, use that
	if torrentRelease.Type == rls.Music && torrentRelease.Artist != "" && torrentRelease.Title != "" {
		// Use RLS's parsed results but preserve any content-based detection from baseRelease
		musicRelease := torrentRelease
		// Keep any fields from content detection that might be more accurate
		if baseRelease.Type == rls.Music {
			musicRelease.Type = rls.Music
		}
		return musicRelease
	}

	// Fallback: use our manual parsing approach for simpler names
	musicRelease := baseRelease
	musicRelease.Type = rls.Music // Ensure it's marked as music

	cleanName := torrentName

	// Extract release group if present [GROUP]
	if strings.Contains(cleanName, "[") && strings.Contains(cleanName, "]") {
		groupStart := strings.LastIndex(cleanName, "[")
		groupEnd := strings.LastIndex(cleanName, "]")
		if groupEnd > groupStart {
			musicRelease.Group = strings.TrimSpace(cleanName[groupStart+1 : groupEnd])
			cleanName = strings.TrimSpace(cleanName[:groupStart])
		}
	}

	// Remove year (YYYY) from the end for parsing
	if strings.Contains(cleanName, "(") && strings.Contains(cleanName, ")") {
		yearStart := strings.LastIndex(cleanName, "(")
		yearEnd := strings.LastIndex(cleanName, ")")
		if yearEnd > yearStart {
			cleanName = strings.TrimSpace(cleanName[:yearStart])
		}
	}

	// Parse "Artist - Album" format
	if parts := strings.Split(cleanName, " - "); len(parts) >= 2 {
		musicRelease.Artist = strings.TrimSpace(parts[0])
		// Join remaining parts as album title (in case there are multiple " - " separators)
		musicRelease.Title = strings.TrimSpace(strings.Join(parts[1:], " - "))
	}

	return musicRelease
}

// SearchTorrentMatches queries Torznab indexers for candidate torrents that match an existing torrent.
func (s *Service) SearchTorrentMatches(ctx context.Context, instanceID int, hash string, opts TorrentSearchOptions) (*TorrentSearchResponse, error) {
	if s.jackettService == nil {
		return nil, errors.New("torznab search is not configured")
	}

	if instanceID <= 0 {
		return nil, fmt.Errorf("invalid instance id: %d", instanceID)
	}
	if strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("torrent hash is required")
	}

	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("load instance: %w", err)
	}
	if instance == nil {
		return nil, fmt.Errorf("instance %d not found", instanceID)
	}

	sourceTorrent, err := s.getTorrentByHash(ctx, instanceID, hash)
	if err != nil {
		return nil, err
	}
	if sourceTorrent.Progress < 1.0 {
		return nil, fmt.Errorf("torrent %s is not fully downloaded (progress %.2f)", sourceTorrent.Name, sourceTorrent.Progress)
	}

	// Get files to find the largest file for better content type detection
	sourceFiles, err := s.syncManager.GetTorrentFiles(ctx, instanceID, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent files: %w", err)
	}

	// Parse both torrent name and largest file for content detection
	sourceRelease := s.releaseCache.Parse(sourceTorrent.Name)

	// For content type detection, use the largest file if available
	var contentDetectionRelease rls.Release = sourceRelease
	if sourceFiles != nil && len(*sourceFiles) > 0 {
		largestFile := s.findLargestFile(*sourceFiles)
		if largestFile != nil {
			largestFileRelease := s.releaseCache.Parse(largestFile.Name)
			// Use the largest file for content type detection, but enrich with torrent metadata
			largestFileRelease = enrichReleaseFromTorrent(largestFileRelease, sourceRelease)

			// Use largest file release for content type if it's more specific than torrent name
			if largestFileRelease.Type != rls.Unknown {
				contentDetectionRelease = largestFileRelease

				log.Debug().
					Str("torrentName", sourceTorrent.Name).
					Str("largestFile", largestFile.Name).
					Str("fileContentType", largestFileRelease.Type.String()).
					Str("torrentContentType", sourceRelease.Type.String()).
					Msg("[CROSSSEED-SEARCH] Using largest file for content type detection")
			}
		}
	}

	// Determine content type first to inform query generation
	var categories []int
	contentTypeStr := "unknown"
	isMusic := false

	switch contentDetectionRelease.Type {
	case rls.Movie:
		categories = []int{2000, 2010, 2040, 2050} // Movies, MoviesSD, MoviesHD, Movies4K
		contentTypeStr = "movie"
	case rls.Episode, rls.Series:
		categories = []int{5000, 5010, 5040, 5050} // TV, TVSD, TVHD, TV4K
		contentTypeStr = "tv"
	case rls.Music:
		categories = []int{3000} // Audio
		contentTypeStr = "music"
		isMusic = true
	case rls.Audiobook:
		categories = []int{3000} // Audio (audiobooks use same category)
		contentTypeStr = "audiobook"
		isMusic = true
	case rls.Book:
		categories = []int{8000, 8010} // Books, BooksEbook
		contentTypeStr = "book"
	case rls.Comic:
		categories = []int{8020} // BooksComics
		contentTypeStr = "comic"
	case rls.Game:
		categories = []int{4000} // PC
		contentTypeStr = "game"
	case rls.App:
		categories = []int{4000} // PC
		contentTypeStr = "app"
	default:
		// Fallback logic based on series/episode/year detection for unknown types
		if contentDetectionRelease.Series > 0 || contentDetectionRelease.Episode > 0 {
			categories = []int{5000, 5010, 5040, 5050} // TV categories
			contentTypeStr = "tv"
		} else if contentDetectionRelease.Year > 0 {
			categories = []int{2000, 2010, 2040, 2050} // Movie categories
			contentTypeStr = "movie"
		}
		// If we can't determine type, search all categories (don't set any)
		// TODO: probably flag to stop any other content type matching
	}

	query := strings.TrimSpace(opts.Query)
	if query == "" {
		// Use the appropriate release object based on content type
		var queryRelease rls.Release
		if isMusic && contentDetectionRelease.Type == rls.Music {
			// For music, create a proper music release object by parsing the torrent name as music
			queryRelease = parseMusicReleaseFromTorrentName(sourceRelease, sourceTorrent.Name)
		} else {
			// For other content types, use the torrent name release
			queryRelease = sourceRelease
		}

		// Build a better search query from parsed release info instead of using full filename
		if queryRelease.Title != "" {
			if isMusic {
				// For music, use artist and title format if available
				if queryRelease.Artist != "" {
					query = queryRelease.Artist + " " + queryRelease.Title
				} else {
					query = queryRelease.Title
				}
			} else {
				// For non-music, start with the title
				query = queryRelease.Title
			}

			// Add year if available
			if queryRelease.Year > 0 {
				query += fmt.Sprintf(" %d", queryRelease.Year)
			}

			// For TV series, add season/episode info (but not for music)
			if !isMusic && queryRelease.Series > 0 {
				if queryRelease.Episode > 0 {
					query += fmt.Sprintf(" S%02dE%02d", queryRelease.Series, queryRelease.Episode)
				} else {
					query += fmt.Sprintf(" S%02d", queryRelease.Series)
				}
			}

			log.Debug().
				Str("originalName", sourceTorrent.Name).
				Str("generatedQuery", query).
				Str("contentType", contentTypeStr).
				Msg("[CROSSSEED-SEARCH] Generated search query from parsed release")
		} else {
			// Fallback to full name if parsing failed
			query = sourceTorrent.Name
			log.Debug().
				Str("originalName", sourceTorrent.Name).
				Msg("[CROSSSEED-SEARCH] Using full filename as query (parsing failed)")
		}
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 40
	}
	requestLimit := limit * 3
	if requestLimit < limit {
		requestLimit = limit
	}

	searchReq := &jackett.TorznabSearchRequest{
		Query:      query,
		Limit:      requestLimit,
		IndexerIDs: opts.IndexerIDs,
	}

	// Apply category filtering to the search request
	if len(categories) > 0 {
		searchReq.Categories = categories

		// Add season/episode info for TV content
		if sourceRelease.Series > 0 {
			season := sourceRelease.Series
			searchReq.Season = &season

			if sourceRelease.Episode > 0 {
				episode := sourceRelease.Episode
				searchReq.Episode = &episode
			}
		}

		// Use the appropriate release object for logging based on content type
		var logRelease rls.Release
		if isMusic && contentDetectionRelease.Type == rls.Music {
			// For music, create a proper music release object by parsing the torrent name as music
			logRelease = parseMusicReleaseFromTorrentName(sourceRelease, sourceTorrent.Name)
		} else {
			logRelease = sourceRelease
		}

		logEvent := log.Debug().
			Str("torrentName", sourceTorrent.Name).
			Str("contentType", contentTypeStr).
			Ints("categories", categories).
			Int("year", logRelease.Year)

		// Show different metadata based on content type
		if !isMusic {
			// For TV/Movies, show series/episode data
			logEvent = logEvent.
				Str("releaseType", logRelease.Type.String()).
				Int("series", logRelease.Series).
				Int("episode", logRelease.Episode)
		} else {
			// For music, show music-specific metadata
			logEvent = logEvent.
				Str("releaseType", "music").
				Str("artist", logRelease.Artist).
				Str("title", logRelease.Title).
				Str("disc", logRelease.Disc).
				Str("source", logRelease.Source).
				Str("group", logRelease.Group)
		}

		logEvent.Msg("[CROSSSEED-SEARCH] Applied RLS-based content type filtering")
	}

	searchResp, err := s.jackettService.Search(ctx, searchReq)
	if err != nil {
		return nil, fmt.Errorf("torznab search failed: %w", err)
	}

	// Load automation settings to get size tolerance percentage
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load cross-seed settings for size validation, using default tolerance")
		settings = &models.CrossSeedAutomationSettings{
			SizeMismatchTolerancePercent: 5.0, // Default to 5% tolerance
		}
	}

	type scoredResult struct {
		result jackett.SearchResult
		score  float64
		reason string
	}

	scored := make([]scoredResult, 0, len(searchResp.Results))
	seen := make(map[string]struct{})
	sizeFilteredCount := 0
	releaseFilteredCount := 0

	for _, res := range searchResp.Results {
		key := res.GUID
		if key == "" {
			key = res.DownloadURL
		}
		if key != "" {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}

		candidateRelease := s.releaseCache.Parse(res.Title)
		if !s.releasesMatch(sourceRelease, candidateRelease, opts.FindIndividualEpisodes) {
			releaseFilteredCount++
			continue
		}

		// Size validation: check if candidate size is within tolerance of source size
		if !s.isSizeWithinTolerance(sourceTorrent.Size, res.Size, settings.SizeMismatchTolerancePercent) {
			sizeFilteredCount++
			log.Debug().
				Str("sourceTitle", sourceTorrent.Name).
				Str("candidateTitle", res.Title).
				Int64("sourceSize", sourceTorrent.Size).
				Int64("candidateSize", res.Size).
				Float64("tolerancePercent", settings.SizeMismatchTolerancePercent).
				Msg("[CROSSSEED-SEARCH] Candidate filtered out due to size mismatch")
			continue
		}

		score, reason := evaluateReleaseMatch(sourceRelease, candidateRelease)
		if score <= 0 {
			score = 1.0
		}

		scored = append(scored, scoredResult{
			result: res,
			score:  score,
			reason: reason,
		})
	}

	// Log filtering statistics
	totalResults := len(searchResp.Results)
	matchedResults := len(scored)
	log.Info().
		Str("torrentName", sourceTorrent.Name).
		Int("totalResults", totalResults).
		Int("releaseFiltered", releaseFilteredCount).
		Int("sizeFiltered", sizeFilteredCount).
		Int("finalMatches", matchedResults).
		Float64("tolerancePercent", settings.SizeMismatchTolerancePercent).
		Msg("[CROSSSEED-SEARCH] Search filtering completed")

	sourceInfo := TorrentInfo{
		InstanceID:   instanceID,
		InstanceName: instance.Name,
		Hash:         sourceTorrent.Hash,
		Name:         sourceTorrent.Name,
		Category:     sourceTorrent.Category,
		Size:         sourceTorrent.Size,
		Progress:     sourceTorrent.Progress,
	}

	if len(scored) == 0 {
		return &TorrentSearchResponse{
			SourceTorrent: sourceInfo,
			Results:       []TorrentSearchResult{},
		}, nil
	}

	files, err := s.syncManager.GetTorrentFiles(ctx, instanceID, sourceTorrent.Hash)
	if err == nil && files != nil {
		sourceInfo.TotalFiles = len(*files)
		sourceInfo.FileCount = len(*files)
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			if scored[i].result.Seeders == scored[j].result.Seeders {
				return scored[i].result.PublishDate.After(scored[j].result.PublishDate)
			}
			return scored[i].result.Seeders > scored[j].result.Seeders
		}
		return scored[i].score > scored[j].score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	results := make([]TorrentSearchResult, 0, len(scored))
	for _, item := range scored {
		res := item.result
		results = append(results, TorrentSearchResult{
			Indexer:              res.Indexer,
			IndexerID:            res.IndexerID,
			Title:                res.Title,
			DownloadURL:          res.DownloadURL,
			InfoURL:              res.InfoURL,
			Size:                 res.Size,
			Seeders:              res.Seeders,
			Leechers:             res.Leechers,
			CategoryID:           res.CategoryID,
			CategoryName:         res.CategoryName,
			PublishDate:          res.PublishDate.Format(time.RFC3339),
			DownloadVolumeFactor: res.DownloadVolumeFactor,
			UploadVolumeFactor:   res.UploadVolumeFactor,
			GUID:                 res.GUID,
			IMDbID:               res.IMDbID,
			TVDbID:               res.TVDbID,
			MatchReason:          item.reason,
			MatchScore:           item.score,
		})
	}

	s.cacheSearchResults(instanceID, sourceTorrent.Hash, results)

	return &TorrentSearchResponse{
		SourceTorrent: sourceInfo,
		Results:       results,
	}, nil
}

// ApplyTorrentSearchResults downloads and adds torrents selected from search results for cross-seeding.
func (s *Service) ApplyTorrentSearchResults(ctx context.Context, instanceID int, hash string, req *ApplyTorrentSearchRequest) (*ApplyTorrentSearchResponse, error) {
	if s.jackettService == nil {
		return nil, errors.New("torznab search is not configured")
	}

	if req == nil || len(req.Selections) == 0 {
		return nil, fmt.Errorf("no selections provided")
	}

	if _, err := s.getTorrentByHash(ctx, instanceID, hash); err != nil {
		return nil, err
	}

	cachedSelections := s.getCachedSearchResults(instanceID, hash)
	if len(cachedSelections) == 0 {
		return nil, fmt.Errorf("no cached cross-seed search results found for torrent %s; please run a search before applying selections", hash)
	}

	startPaused := true
	if req.StartPaused != nil {
		startPaused = *req.StartPaused
	}

	useTag := req.UseTag
	tagName := strings.TrimSpace(req.TagName)
	if useTag && tagName == "" {
		tagName = "cross-seed"
	}

	results := make([]TorrentSearchAddResult, 0, len(req.Selections))

	for _, selection := range req.Selections {
		downloadURL := strings.TrimSpace(selection.DownloadURL)
		guid := strings.TrimSpace(selection.GUID)

		if selection.IndexerID <= 0 || (downloadURL == "" && guid == "") {
			results = append(results, TorrentSearchAddResult{
				Title:   selection.Title,
				Indexer: selection.Indexer,
				Success: false,
				Error:   "invalid selection",
			})
			continue
		}

		cachedResult, err := s.resolveSelectionFromCache(cachedSelections, selection)
		if err != nil {
			results = append(results, TorrentSearchAddResult{
				Title:   selection.Title,
				Indexer: selection.Indexer,
				Success: false,
				Error:   err.Error(),
			})
			continue
		}

		indexerName := selection.Indexer
		if indexerName == "" {
			indexerName = cachedResult.Indexer
		}

		title := selection.Title
		if title == "" {
			title = cachedResult.Title
		}

		torrentBytes, err := s.jackettService.DownloadTorrent(ctx, cachedResult.IndexerID, cachedResult.DownloadURL)
		if err != nil {
			results = append(results, TorrentSearchAddResult{
				Title:   title,
				Indexer: indexerName,
				Success: false,
				Error:   fmt.Sprintf("download torrent: %v", err),
			})
			continue
		}

		startPausedCopy := startPaused
		addCrossSeedTag := useTag

		payload := &CrossSeedRequest{
			TorrentData:       base64.StdEncoding.EncodeToString(torrentBytes),
			TargetInstanceIDs: []int{instanceID},
			StartPaused:       &startPausedCopy,
			AddCrossSeedTag:   &addCrossSeedTag,
		}

		if useTag {
			payload.Tags = []string{tagName}
		}

		resp, err := s.CrossSeed(ctx, payload)
		if err != nil {
			results = append(results, TorrentSearchAddResult{
				Title:   title,
				Indexer: indexerName,
				Success: false,
				Error:   err.Error(),
			})
			continue
		}

		torrentName := ""
		if resp.TorrentInfo != nil {
			torrentName = resp.TorrentInfo.Name
		}

		results = append(results, TorrentSearchAddResult{
			Title:           title,
			Indexer:         indexerName,
			TorrentName:     torrentName,
			Success:         resp.Success,
			InstanceResults: resp.Results,
		})
	}

	return &ApplyTorrentSearchResponse{
		Results: results,
	}, nil
}

func (s *Service) cacheSearchResults(instanceID int, hash string, results []TorrentSearchResult) {
	if s.searchResultCache == nil || len(results) == 0 {
		return
	}

	key := searchResultCacheKey(instanceID, hash)

	cloned := make([]TorrentSearchResult, len(results))
	copy(cloned, results)

	s.searchResultCache.Set(key, cloned, ttlcache.DefaultTTL)
}

func (s *Service) getCachedSearchResults(instanceID int, hash string) []TorrentSearchResult {
	if s.searchResultCache == nil {
		return nil
	}

	key := searchResultCacheKey(instanceID, hash)
	if cached, found := s.searchResultCache.Get(key); found {
		return cached
	}

	return nil
}

func (s *Service) resolveSelectionFromCache(cached []TorrentSearchResult, selection TorrentSearchSelection) (*TorrentSearchResult, error) {
	if len(cached) == 0 {
		return nil, errors.New("no cached search results available")
	}

	downloadURL := strings.TrimSpace(selection.DownloadURL)
	guid := strings.TrimSpace(selection.GUID)

	if downloadURL == "" && guid == "" {
		return nil, fmt.Errorf("selection %s is missing identifiers", selection.Title)
	}

	for i := range cached {
		result := &cached[i]
		if result.IndexerID != selection.IndexerID {
			continue
		}

		if guid != "" && result.GUID != "" && guid == result.GUID {
			return result, nil
		}

		if downloadURL != "" && downloadURL == result.DownloadURL {
			return result, nil
		}
	}

	return nil, fmt.Errorf("selection %s does not match cached search results", selection.Title)
}

func searchResultCacheKey(instanceID int, hash string) string {
	cleanHash := strings.ToLower(strings.TrimSpace(hash))
	if cleanHash == "" {
		return fmt.Sprintf("%d", instanceID)
	}
	return fmt.Sprintf("%d:%s", instanceID, cleanHash)
}

// getTorrentByHash retrieves a torrent by matching any known hash variant.
func (s *Service) getTorrentByHash(ctx context.Context, instanceID int, hash string) (*qbt.Torrent, error) {
	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("load torrents: %w", err)
	}

	needle := strings.ToLower(strings.TrimSpace(hash))
	for _, torrent := range torrents {
		candidates := []string{
			strings.ToLower(torrent.Hash),
			strings.ToLower(torrent.InfohashV1),
			strings.ToLower(torrent.InfohashV2),
		}

		for _, candidate := range candidates {
			if candidate != "" && candidate == needle {
				t := torrent
				return &t, nil
			}
		}
	}

	return nil, fmt.Errorf("torrent %s not found in instance %d", hash, instanceID)
}

func evaluateReleaseMatch(source, candidate rls.Release) (float64, string) {
	score := 1.0
	reasons := make([]string, 0, 6)

	if source.Group != "" && candidate.Group != "" && strings.EqualFold(source.Group, candidate.Group) {
		score += 0.35
		reasons = append(reasons, fmt.Sprintf("group %s", candidate.Group))
	}
	if source.Resolution != "" && candidate.Resolution != "" && strings.EqualFold(source.Resolution, candidate.Resolution) {
		score += 0.15
		reasons = append(reasons, fmt.Sprintf("resolution %s", candidate.Resolution))
	}
	if source.Source != "" && candidate.Source != "" && strings.EqualFold(source.Source, candidate.Source) {
		score += 0.1
		reasons = append(reasons, fmt.Sprintf("source %s", candidate.Source))
	}
	if source.Series > 0 && candidate.Series == source.Series {
		reasons = append(reasons, fmt.Sprintf("season %d", source.Series))
		score += 0.1
		if source.Episode > 0 && candidate.Episode == source.Episode {
			reasons = append(reasons, fmt.Sprintf("episode %d", source.Episode))
			score += 0.1
		}
	}
	if source.Year > 0 && candidate.Year == source.Year {
		score += 0.05
		reasons = append(reasons, fmt.Sprintf("year %d", source.Year))
	}
	if len(source.Codec) > 0 && len(candidate.Codec) > 0 && strings.EqualFold(source.Codec[0], candidate.Codec[0]) {
		score += 0.05
		reasons = append(reasons, fmt.Sprintf("codec %s", candidate.Codec[0]))
	}
	if len(source.Audio) > 0 && len(candidate.Audio) > 0 && strings.EqualFold(source.Audio[0], candidate.Audio[0]) {
		score += 0.05
		reasons = append(reasons, fmt.Sprintf("audio %s", candidate.Audio[0]))
	}

	if len(reasons) == 0 {
		reasons = append(reasons, "title match")
	}

	return score, strings.Join(reasons, ", ")
}

// waitForTorrentRecheck waits for a torrent to finish rechecking after being added
// This can take several minutes for large torrents, so we poll periodically
func (s *Service) waitForTorrentRecheck(ctx context.Context, instanceID int, torrentHash string, result *InstanceCrossSeedResult) *qbt.Torrent {
	const (
		maxWaitTime  = 5 * time.Minute // Maximum time to wait for recheck
		pollInterval = 2 * time.Second // How often to check status
		initialWait  = 500 * time.Millisecond
	)

	// Initial wait for torrent to appear
	time.Sleep(initialWait)

	startTime := time.Now()
	lastLogTime := startTime

	for {
		// Check if we've exceeded max wait time
		if time.Since(startTime) > maxWaitTime {
			log.Warn().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Dur("waited", time.Since(startTime)).
				Msg("Timeout waiting for torrent recheck to complete")
			result.Message += " (timeout waiting for recheck)"
			return nil
		}

		// Check context cancellation
		if ctx.Err() != nil {
			log.Warn().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Err(ctx.Err()).
				Msg("Context cancelled while waiting for recheck")
			return nil
		}

		// Force sync to get latest state from qBittorrent
		qbtSyncManager, err := s.syncManager.GetQBittorrentSyncManager(ctx, instanceID)
		if err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Msg("Failed to get sync manager while waiting for recheck")
			time.Sleep(pollInterval)
			continue
		}

		if syncErr := qbtSyncManager.Sync(ctx); syncErr != nil {
			log.Warn().
				Err(syncErr).
				Int("instanceID", instanceID).
				Msg("Failed to sync while waiting for recheck, will retry")
			time.Sleep(pollInterval)
			continue
		}

		// Get current torrents
		torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
		if err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Msg("Failed to get torrents while waiting for recheck")
			time.Sleep(pollInterval)
			continue
		}

		// Find the torrent
		var torrent *qbt.Torrent
		for _, t := range torrents {
			if t.Hash == torrentHash || t.InfohashV1 == torrentHash || t.InfohashV2 == torrentHash {
				torrent = &t
				break
			}
		}

		if torrent == nil {
			log.Warn().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Msg("Torrent not found after adding")
			return nil
		}

		// Check if torrent is still checking
		isChecking := torrent.State == qbt.TorrentStateCheckingDl ||
			torrent.State == qbt.TorrentStateCheckingUp ||
			torrent.State == qbt.TorrentStateCheckingResumeData ||
			torrent.State == qbt.TorrentStateAllocating

		if isChecking {
			// Log periodically (every 10 seconds) to show progress
			if time.Since(lastLogTime) > 10*time.Second {
				log.Info().
					Int("instanceID", instanceID).
					Str("hash", torrent.Hash).
					Str("state", string(torrent.State)).
					Float64("progress", torrent.Progress).
					Dur("elapsed", time.Since(startTime)).
					Msg("Waiting for torrent recheck to complete")
				lastLogTime = time.Now()
			}

			time.Sleep(pollInterval)
			continue
		}

		// Torrent is done checking
		log.Info().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("state", string(torrent.State)).
			Float64("progress", torrent.Progress).
			Dur("elapsed", time.Since(startTime)).
			Msg("Torrent recheck completed")

		return torrent
	}
}

// findLargestFile returns the file with the largest size from a list of torrent files.
// This is useful for content type detection as the largest file usually represents the main content.
func (s *Service) findLargestFile(files qbt.TorrentFiles) *struct {
	Availability float32 `json:"availability"`
	Index        int     `json:"index"`
	IsSeed       bool    `json:"is_seed,omitempty"`
	Name         string  `json:"name"`
	PieceRange   []int   `json:"piece_range"`
	Priority     int     `json:"priority"`
	Progress     float32 `json:"progress"`
	Size         int64   `json:"size"`
} {
	if len(files) == 0 {
		return nil
	}

	largestIndex := 0
	largestSize := files[0].Size

	for i := 1; i < len(files); i++ {
		if files[i].Size > largestSize {
			largestIndex = i
			largestSize = files[i].Size
		}
	}

	return &files[largestIndex]
}

// isSizeWithinTolerance checks if two torrent sizes are within the specified tolerance percentage.
// A tolerance of 5.0 means the candidate size can be ±5% of the source size.
func (s *Service) isSizeWithinTolerance(sourceSize, candidateSize int64, tolerancePercent float64) bool {
	if sourceSize == 0 || candidateSize == 0 {
		return sourceSize == candidateSize // Both must be zero to match
	}

	if tolerancePercent < 0 {
		tolerancePercent = 0 // Negative tolerance doesn't make sense
	}

	// If tolerance is 0, require exact match
	if tolerancePercent == 0 {
		return sourceSize == candidateSize
	}

	// Calculate acceptable size range
	tolerance := float64(sourceSize) * (tolerancePercent / 100.0)
	minAcceptableSize := float64(sourceSize) - tolerance
	maxAcceptableSize := float64(sourceSize) + tolerance

	candidateSizeFloat := float64(candidateSize)
	return candidateSizeFloat >= minAcceptableSize && candidateSizeFloat <= maxAcceptableSize
}
