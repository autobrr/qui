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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/filesmanager"
	"github.com/autobrr/qui/internal/services/jackett"
)

// releaseKey is a comparable struct for matching releases across different torrents
// It uses the actual parsed data from rls.Release instead of inventing string formats
type releaseKey struct {
	// TV shows: series and episode
	series  int
	episode int

	// Date-based releases: year/month/day
	year  int
	month int
	day   int
}

// makeReleaseKey creates a releaseKey from a parsed release
// Returns the zero value if the release doesn't have identifiable metadata
func makeReleaseKey(r rls.Release) releaseKey {
	// TV episode
	if r.Series > 0 && r.Episode > 0 {
		return releaseKey{
			series:  r.Series,
			episode: r.Episode,
		}
	}

	// TV season (no specific episode)
	if r.Series > 0 {
		return releaseKey{
			series: r.Series,
		}
	}

	// Date-based release
	if r.Year > 0 && r.Month > 0 && r.Day > 0 {
		return releaseKey{
			year:  r.Year,
			month: r.Month,
			day:   r.Day,
		}
	}

	// Year-based release (movies, software, etc.)
	if r.Year > 0 {
		return releaseKey{
			year: r.Year,
		}
	}

	// Content without clear identifying metadata - use zero value
	return releaseKey{}
}

// Service provides cross-seed functionality
type Service struct {
	instanceStore *models.InstanceStore
	syncManager   *qbittorrent.SyncManager
	filesManager  *filesmanager.Service
	releaseCache  *ReleaseCache

	automationStore *models.CrossSeedStore
	jackettService  *jackett.Service

	automationMu     sync.Mutex
	automationCancel context.CancelFunc
	automationWake   chan struct{}
	runActive        atomic.Bool
	runMu            sync.Mutex
}

// NewService creates a new cross-seed service
func NewService(
	instanceStore *models.InstanceStore,
	syncManager *qbittorrent.SyncManager,
	filesManager *filesmanager.Service,
	automationStore *models.CrossSeedStore,
	jackettService *jackett.Service,
) *Service {
	return &Service{
		instanceStore:   instanceStore,
		syncManager:     syncManager,
		filesManager:    filesManager,
		releaseCache:    NewReleaseCache(),
		automationStore: automationStore,
		jackettService:  jackettService,
		automationWake:  make(chan struct{}, 1),
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
	if s.automationStore == nil {
		return nil, errors.New("automation storage not configured")
	}
	if settings == nil {
		return nil, errors.New("settings cannot be nil")
	}

	if settings.RunIntervalMinutes <= 0 {
		settings.RunIntervalMinutes = 120
	}
	if settings.MaxResultsPerRun <= 0 {
		settings.MaxResultsPerRun = 50
	}

	updated, err := s.automationStore.UpsertSettings(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("persist automation settings: %w", err)
	}

	s.signalAutomationWake()

	return updated, nil
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
		TorrentName:    result.Title,
		IgnorePatterns: append([]string(nil), settings.IgnorePatterns...),
		SourceIndexer:  sourceIndexer,
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
			if !s.releasesMatch(targetRelease, candidateRelease) {
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

// releasesMatch checks if two releases are related using fuzzy matching
// This allows matching similar content that isn't exactly the same
func (s *Service) releasesMatch(source, candidate rls.Release) bool {
	// Title should match closely but not necessarily exactly
	// This handles variations in title formatting
	sourceTitleLower := strings.ToLower(strings.TrimSpace(source.Title))
	candidateTitleLower := strings.ToLower(strings.TrimSpace(candidate.Title))

	if sourceTitleLower == "" || candidateTitleLower == "" {
		return false
	}

	// Check if titles are similar (exact match or one contains the other)
	if sourceTitleLower != candidateTitleLower &&
		!strings.Contains(sourceTitleLower, candidateTitleLower) &&
		!strings.Contains(candidateTitleLower, sourceTitleLower) {
		return false
	}

	// Year should match if both are present
	if source.Year > 0 && candidate.Year > 0 && source.Year != candidate.Year {
		return false
	}

	// For TV shows, season must match but episodes can differ
	// This allows matching single episodes with season packs
	if source.Series > 0 || candidate.Series > 0 {
		// If one has a season but the other doesn't, skip season check
		if source.Series > 0 && candidate.Series > 0 {
			if source.Series != candidate.Series {
				return false
			}
		}
		// Don't enforce episode matching here - we'll handle that in file matching
		// This allows a single episode (e.g., S01E05) to match a season pack (S01)
	}

	// Resolution matching is optional - different qualities can cross-seed if files match
	// Don't enforce resolution, version, group, etc. - let file matching decide

	return true
}

// getMatchTypeFromTitle checks if a candidate torrent has files matching what we want based on parsed title
func (s *Service) getMatchTypeFromTitle(targetRelease, candidateRelease rls.Release, candidateFiles qbt.TorrentFiles, ignorePatterns []string) string {
	// Build candidate release keys from actual files WITH ENRICHMENT
	candidateReleases := make(map[releaseKey]int64)
	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, ignorePatterns) {
			// Parse file and enrich with torrent metadata
			fileRelease := s.releaseCache.Parse(cf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, candidateRelease)

			// Extract release key from enriched release
			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				candidateReleases[key] = cf.Size
			}
		}
	}

	// Check if candidate has what we need
	if targetRelease.Series > 0 && targetRelease.Episode > 0 {
		// Looking for specific episode - check if it exists in candidate files
		targetKey := releaseKey{
			series:  targetRelease.Series,
			episode: targetRelease.Episode,
		}
		if _, exists := candidateReleases[targetKey]; exists {
			return "partial-in-pack"
		}
	} else if targetRelease.Series > 0 {
		// Looking for season pack - check if ANY episodes from this season exist in candidate files
		for key := range candidateReleases {
			if key.series == targetRelease.Series && key.episode > 0 {
				return "partial-contains"
			}
		}
	} else if targetRelease.Year > 0 && targetRelease.Month > 0 && targetRelease.Day > 0 {
		// Date-based release - check for exact date match
		targetKey := releaseKey{
			year:  targetRelease.Year,
			month: targetRelease.Month,
			day:   targetRelease.Day,
		}
		if _, exists := candidateReleases[targetKey]; exists {
			return "partial-in-pack"
		}
	} else {
		// Non-episodic content - check if any candidate files match
		if len(candidateReleases) > 0 {
			return "partial-in-pack"
		}
	}

	return ""
}

// getMatchType determines if files match for cross-seeding
// Returns "exact" for perfect match, "partial" for season pack partial matches,
// "size" for total size match, or "" for no match
func (s *Service) getMatchType(sourceRelease, candidateRelease rls.Release, sourceFiles, candidateFiles qbt.TorrentFiles, ignorePatterns []string) string {
	// Build map of source files (name -> size) and (releaseKey -> size)
	// Parse each file with rls and enrich with torrent metadata
	sourceMap := make(map[string]int64)
	sourceReleaseKeys := make(map[releaseKey]int64)
	totalSourceSize := int64(0)

	for _, sf := range sourceFiles {
		if !shouldIgnoreFile(sf.Name, ignorePatterns) {
			sourceMap[sf.Name] = sf.Size

			// Parse file and enrich with torrent metadata (cached)
			fileRelease := s.releaseCache.Parse(sf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, sourceRelease)

			// Extract release key using parsed data
			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				sourceReleaseKeys[key] = sf.Size

				// Log enriched metadata for debugging
				if fileRelease.Group == "" && enrichedRelease.Group != "" {
					log.Debug().
						Str("file", sf.Name).
						Str("enrichedGroup", enrichedRelease.Group).
						Msg("Enriched file with group from torrent")
				}
			}

			totalSourceSize += sf.Size
		}
	}

	// Build candidate maps with enrichment
	candidateMap := make(map[string]int64)
	candidateReleaseKeys := make(map[releaseKey]int64)
	totalCandidateSize := int64(0)

	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, ignorePatterns) {
			candidateMap[cf.Name] = cf.Size

			// Parse file and enrich with torrent metadata (cached)
			fileRelease := s.releaseCache.Parse(cf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, candidateRelease)

			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				candidateReleaseKeys[key] = cf.Size

				// Log enriched metadata for debugging
				if fileRelease.Resolution == "" && enrichedRelease.Resolution != "" {
					log.Debug().
						Str("file", cf.Name).
						Str("enrichedResolution", enrichedRelease.Resolution).
						Msg("Enriched file with resolution from torrent")
				}
			}

			totalCandidateSize += cf.Size
		}
	}

	// Check for exact file match (same paths and sizes)
	exactMatch := true
	for path, size := range sourceMap {
		if candidateSize, exists := candidateMap[path]; !exists || candidateSize != size {
			exactMatch = false
			break
		}
	}

	if exactMatch && len(sourceMap) == len(candidateMap) {
		return "exact"
	}

	// Check for partial match (season pack scenario, date-based releases, etc.)
	// Scenario 1: Single episode/release contained in pack
	// Scenario 2: Pack contains multiple single episodes/releases
	if len(sourceReleaseKeys) > 0 && len(candidateReleaseKeys) > 0 {
		// Check if source files are contained in candidate (source episode in candidate pack)
		sourceInCandidate := s.checkPartialMatch(sourceReleaseKeys, candidateReleaseKeys)
		if sourceInCandidate {
			return "partial-in-pack"
		}

		// Check if candidate files are contained in source (candidate episode in source pack)
		candidateInSource := s.checkPartialMatch(candidateReleaseKeys, sourceReleaseKeys)
		if candidateInSource {
			return "partial-contains"
		}
	}

	// Size match for same content with different structure
	if totalSourceSize > 0 && totalSourceSize == totalCandidateSize && len(sourceMap) > 0 {
		return "size"
	}

	return ""
}

// checkPartialMatch checks if subset files are contained in superset files
// Returns true if all subset files have matching release keys and sizes in superset
func (s *Service) checkPartialMatch(subset, superset map[releaseKey]int64) bool {
	if len(subset) == 0 || len(superset) == 0 {
		return false
	}

	matchCount := 0
	for key, size := range subset {
		if superSize, exists := superset[key]; exists && superSize == size {
			matchCount++
		}
	}

	// Consider it a match if at least 80% of subset files are found
	threshold := float64(len(subset)) * 0.8
	return float64(matchCount) >= threshold
}

// enrichReleaseFromTorrent enriches file release info with metadata from torrent name
// This fills in missing group, resolution, codec, and other metadata from the season pack
func enrichReleaseFromTorrent(fileRelease rls.Release, torrentRelease rls.Release) rls.Release {
	enriched := fileRelease

	// Fill in missing group from torrent
	if enriched.Group == "" && torrentRelease.Group != "" {
		enriched.Group = torrentRelease.Group
	}

	// Fill in missing resolution from torrent
	if enriched.Resolution == "" && torrentRelease.Resolution != "" {
		enriched.Resolution = torrentRelease.Resolution
	}

	// Fill in missing codec from torrent
	if len(enriched.Codec) == 0 && len(torrentRelease.Codec) > 0 {
		enriched.Codec = torrentRelease.Codec
	}

	// Fill in missing audio from torrent
	if len(enriched.Audio) == 0 && len(torrentRelease.Audio) > 0 {
		enriched.Audio = torrentRelease.Audio
	}

	// Fill in missing source from torrent
	if enriched.Source == "" && torrentRelease.Source != "" {
		enriched.Source = torrentRelease.Source
	}

	// Fill in missing HDR info from torrent
	if len(enriched.HDR) == 0 && len(torrentRelease.HDR) > 0 {
		enriched.HDR = torrentRelease.HDR
	}

	// Fill in missing season from torrent (for season packs)
	if enriched.Series == 0 && torrentRelease.Series > 0 {
		enriched.Series = torrentRelease.Series
	}

	// Fill in missing year from torrent
	if enriched.Year == 0 && torrentRelease.Year > 0 {
		enriched.Year = torrentRelease.Year
	}

	return enriched
}

// shouldIgnoreFile checks if a file should be ignored based on patterns
func shouldIgnoreFile(filename string, patterns []string) bool {
	lower := strings.ToLower(filename)

	for _, pattern := range patterns {
		pattern = strings.ToLower(pattern)
		// Simple glob matching
		if strings.Contains(pattern, "*") {
			parts := strings.Split(pattern, "*")
			matches := true
			for _, part := range parts {
				if part != "" && !strings.Contains(lower, part) {
					matches = false
					break
				}
			}
			if matches {
				return true
			}
		} else if strings.HasSuffix(lower, pattern) {
			return true
		}
	}

	return false
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
		TorrentName:       torrentName,
		TargetInstanceIDs: req.TargetInstanceIDs,
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

	// Add tags
	tags := req.Tags
	if len(tags) == 0 && len(matchedTorrent.Tags) > 0 {
		tags = strings.Split(matchedTorrent.Tags, ", ")
	}
	if len(tags) > 0 {
		// Add a cross-seed tag to identify it
		tags = append(tags, "cross-seed")
		options["tags"] = strings.Join(tags, ",")
	} else {
		options["tags"] = "cross-seed"
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
