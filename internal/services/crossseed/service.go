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
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/filesmanager"
	"github.com/autobrr/qui/internal/services/jackett"
)

// instanceProvider captures the instance store methods the service relies on.
type instanceProvider interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
	List(ctx context.Context) ([]*models.Instance, error)
}

// qbittorrentSync exposes the sync manager functionality needed by the service.
type qbittorrentSync interface {
	GetAllTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error)
	GetTorrentFiles(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error)
	GetTorrentProperties(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error)
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error
	BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error
	GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error)
	ExtractDomainFromURL(urlStr string) string
	GetQBittorrentSyncManager(ctx context.Context, instanceID int) (*qbt.SyncManager, error)
}

// Service provides cross-seed functionality
type Service struct {
	instanceStore instanceProvider
	syncManager   qbittorrentSync
	filesManager  *filesmanager.Service
	releaseCache  *ReleaseCache
	// searchResultCache stores the most recent search results per torrent hash so that
	// apply requests can be validated without trusting client-provided URLs.
	searchResultCache *ttlcache.Cache[string, []TorrentSearchResult]
	// asyncFilteringCache stores async filtering state by torrent key for UI polling
	asyncFilteringCache *ttlcache.Cache[string, *AsyncIndexerFilteringState]
	indexerDomainCache  *ttlcache.Cache[string, string]

	automationStore *models.CrossSeedStore
	jackettService  *jackett.Service

	automationMu     sync.Mutex
	automationCancel context.CancelFunc
	automationWake   chan struct{}
	runActive        atomic.Bool

	searchMu     sync.RWMutex
	searchCancel context.CancelFunc
	searchState  *searchRunState

	// domainMappings provides static mappings between tracker domains and indexer domains
	domainMappings map[string][]string
}

const (
	searchResultCacheTTL         = 5 * time.Minute
	indexerDomainCacheTTL        = 1 * time.Minute
	contentFilteringWaitTimeout  = 5 * time.Second
	contentFilteringPollInterval = 150 * time.Millisecond
)

// initializeDomainMappings returns a hardcoded mapping of tracker domains to indexer domains.
// This helps map tracker domains (from existing torrents) to indexer domains (from Jackett/Prowlarr)
// for better indexer matching when tracker has no correlation with indexer name/domain.
//
// Format: tracker_domain -> []indexer_domains
func initializeDomainMappings() map[string][]string {
	return map[string][]string{
		"landof.tv":      {"broadcasthe.net"},
		"flacsfor.me":    {"redacted.sh"},
		"home.opsfet.ch": {"orpheus.network"},
	}
}

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

	asyncFilteringCache := ttlcache.New(ttlcache.Options[string, *AsyncIndexerFilteringState]{}.
		SetDefaultTTL(searchResultCacheTTL)) // Use same TTL as search results
	indexerDomainCache := ttlcache.New(ttlcache.Options[string, string]{}.
		SetDefaultTTL(indexerDomainCacheTTL))

	return &Service{
		instanceStore:       instanceStore,
		syncManager:         syncManager,
		filesManager:        filesManager,
		releaseCache:        NewReleaseCache(),
		searchResultCache:   searchCache,
		asyncFilteringCache: asyncFilteringCache,
		indexerDomainCache:  indexerDomainCache,
		automationStore:     automationStore,
		jackettService:      jackettService,
		automationWake:      make(chan struct{}, 1),
		domainMappings:      initializeDomainMappings(),
	}
}

// ErrAutomationRunning indicates a cross-seed automation run is already in progress.
var ErrAutomationRunning = errors.New("cross-seed automation already running")

// ErrSearchRunActive indicates a search automation run is in progress.
var ErrSearchRunActive = errors.New("cross-seed search run already running")

// ErrInvalidWebhookRequest indicates a webhook check payload failed validation.
var ErrInvalidWebhookRequest = errors.New("invalid webhook request")

// ErrWebhookInstanceNotFound indicates the requested instance does not exist.
var ErrWebhookInstanceNotFound = errors.New("cross-seed instance not found")

// AutomationRunOptions configures a manual automation run.
type AutomationRunOptions struct {
	RequestedBy string
	Mode        models.CrossSeedRunMode
	DryRun      bool
	Limit       int
}

// SearchRunOptions configures how the library search automation operates.
type SearchRunOptions struct {
	InstanceID             int
	Categories             []string
	Tags                   []string
	IntervalSeconds        int
	IndexerIDs             []int
	CooldownMinutes        int
	FindIndividualEpisodes bool
	RequestedBy            string
	StartPaused            bool
	CategoryOverride       *string
	TagsOverride           []string
}

// SearchRunStatus summarises the current state of the active search run.
type SearchRunStatus struct {
	Running        bool                           `json:"running"`
	Run            *models.CrossSeedSearchRun     `json:"run,omitempty"`
	CurrentTorrent *SearchCandidateStatus         `json:"currentTorrent,omitempty"`
	RecentResults  []models.CrossSeedSearchResult `json:"recentResults"`
	NextRunAt      *time.Time                     `json:"nextRunAt,omitempty"`
}

// SearchCandidateStatus exposes metadata about the torrent currently being processed.
type SearchCandidateStatus struct {
	TorrentHash string   `json:"torrentHash"`
	TorrentName string   `json:"torrentName"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
}

type searchRunState struct {
	run   *models.CrossSeedSearchRun
	opts  SearchRunOptions
	queue []qbt.Torrent
	index int

	currentCandidate *SearchCandidateStatus
	recentResults    []models.CrossSeedSearchResult
	nextWake         time.Time
	lastError        error
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

// StartSearchRun launches an on-demand search automation run for a single instance.
func (s *Service) StartSearchRun(ctx context.Context, opts SearchRunOptions) (*models.CrossSeedSearchRun, error) {
	if s.automationStore == nil || s.jackettService == nil {
		return nil, errors.New("cross-seed automation not configured")
	}

	if err := s.validateSearchRunOptions(ctx, &opts); err != nil {
		return nil, err
	}

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings != nil {
		if opts.CategoryOverride == nil {
			opts.CategoryOverride = settings.Category
		}
		if len(opts.TagsOverride) == 0 && len(settings.Tags) > 0 {
			opts.TagsOverride = append([]string(nil), settings.Tags...)
		}
		opts.StartPaused = settings.StartPaused
		if !settings.FindIndividualEpisodes {
			opts.FindIndividualEpisodes = false
		} else if !opts.FindIndividualEpisodes {
			opts.FindIndividualEpisodes = settings.FindIndividualEpisodes
		}
	}
	opts.TagsOverride = normalizeStringSlice(opts.TagsOverride)

	s.searchMu.Lock()
	if s.searchCancel != nil {
		s.searchMu.Unlock()
		return nil, ErrSearchRunActive
	}

	newRun := &models.CrossSeedSearchRun{
		InstanceID:      opts.InstanceID,
		Status:          models.CrossSeedSearchRunStatusRunning,
		StartedAt:       time.Now().UTC(),
		Filters:         models.CrossSeedSearchFilters{Categories: append([]string(nil), opts.Categories...), Tags: append([]string(nil), opts.Tags...)},
		IndexerIDs:      append([]int(nil), opts.IndexerIDs...),
		IntervalSeconds: opts.IntervalSeconds,
		CooldownMinutes: opts.CooldownMinutes,
		Results:         []models.CrossSeedSearchResult{},
	}

	storedRun, err := s.automationStore.CreateSearchRun(ctx, newRun)
	if err != nil {
		s.searchMu.Unlock()
		return nil, err
	}

	state := &searchRunState{
		run:           storedRun,
		opts:          opts,
		recentResults: make([]models.CrossSeedSearchResult, 0, 10),
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.searchCancel = cancel
	s.searchState = state
	s.searchMu.Unlock()

	go s.searchRunLoop(runCtx, state)

	return storedRun, nil
}

// CancelSearchRun stops the active search run, if any.
func (s *Service) CancelSearchRun() {
	s.searchMu.RLock()
	cancel := s.searchCancel
	s.searchMu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// GetSearchRunStatus returns the latest information about the active search run.
func (s *Service) GetSearchRunStatus(ctx context.Context) (*SearchRunStatus, error) {
	status := &SearchRunStatus{Running: false, RecentResults: []models.CrossSeedSearchResult{}}

	s.searchMu.RLock()
	state := s.searchState
	if state != nil {
		status.Running = true
		status.Run = cloneSearchRun(state.run)
		if state.currentCandidate != nil {
			candidate := *state.currentCandidate
			status.CurrentTorrent = &candidate
		}
		if len(state.recentResults) > 0 {
			status.RecentResults = append(status.RecentResults, state.recentResults...)
		}
		if !state.nextWake.IsZero() {
			next := state.nextWake
			status.NextRunAt = &next
		}
	}
	s.searchMu.RUnlock()

	return status, nil
}

func cloneSearchRun(run *models.CrossSeedSearchRun) *models.CrossSeedSearchRun {
	if run == nil {
		return nil
	}

	cloned := *run
	cloned.Filters = models.CrossSeedSearchFilters{
		Categories: append([]string(nil), run.Filters.Categories...),
		Tags:       append([]string(nil), run.Filters.Tags...),
	}
	cloned.IndexerIDs = append([]int(nil), run.IndexerIDs...)
	cloned.Results = append([]models.CrossSeedSearchResult(nil), run.Results...)

	return &cloned
}

// ListSearchRuns returns stored search automation history for an instance.
func (s *Service) ListSearchRuns(ctx context.Context, instanceID, limit, offset int) ([]*models.CrossSeedSearchRun, error) {
	if s.automationStore == nil {
		return []*models.CrossSeedSearchRun{}, nil
	}
	return s.automationStore.ListSearchRuns(ctx, instanceID, limit, offset)
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
				} else {
					log.Warn().Err(err).Msg("Cross-seed automation run failed")
				}
			}
			continue
		}

		s.waitTimer(ctx, timer, nextDelay)
	}
}

func (s *Service) validateSearchRunOptions(ctx context.Context, opts *SearchRunOptions) error {
	if opts == nil {
		return fmt.Errorf("options cannot be nil")
	}
	if opts.InstanceID <= 0 {
		return fmt.Errorf("instance id must be positive")
	}
	if opts.IntervalSeconds < 60 {
		opts.IntervalSeconds = 60
	}
	if opts.CooldownMinutes < 720 {
		opts.CooldownMinutes = 720
	}
	opts.Categories = normalizeStringSlice(opts.Categories)
	opts.Tags = normalizeStringSlice(opts.Tags)
	opts.IndexerIDs = uniquePositiveInts(opts.IndexerIDs)
	if opts.RequestedBy == "" {
		opts.RequestedBy = "manual"
	}

	instance, err := s.instanceStore.Get(ctx, opts.InstanceID)
	if err != nil {
		return fmt.Errorf("load instance: %w", err)
	}
	if instance == nil {
		return fmt.Errorf("instance %d not found", opts.InstanceID)
	}

	return nil
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
	interval := max(time.Duration(intervalMinutes)*time.Minute, time.Minute)

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

	releaseFetchLimit := max(int(math.Ceil(float64(limit)*1.5)), limit)

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

	// Pre-fetch all indexer info (names and domains) for performance
	indexerInfo, err := s.jackettService.GetEnabledIndexersInfo(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to fetch indexer info, will use fallback lookups")
		indexerInfo = make(map[int]jackett.EnabledIndexerInfo) // Empty map as fallback
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

		status, infoHash, procErr := s.processAutomationCandidate(ctx, run, settings, result, opts, indexerInfo)
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

func (s *Service) processAutomationCandidate(ctx context.Context, run *models.CrossSeedRun, settings *models.CrossSeedAutomationSettings, result jackett.SearchResult, opts AutomationRunOptions, indexerInfo map[int]jackett.EnabledIndexerInfo) (models.CrossSeedFeedItemStatus, *string, error) {
	sourceIndexer := result.Indexer
	if resolved := jackett.GetIndexerNameFromInfo(indexerInfo, result.IndexerID); resolved != "" {
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

	torrentBytes, err := s.jackettService.DownloadTorrent(ctx, jackett.TorrentDownloadRequest{
		IndexerID:   result.IndexerID,
		DownloadURL: result.DownloadURL,
		GUID:        result.GUID,
		Title:       result.Title,
		Size:        result.Size,
	})
	if err != nil {
		run.TorrentsFailed++
		return models.CrossSeedFeedItemStatusFailed, nil, fmt.Errorf("download torrent: %w", err)
	}

	encodedTorrent := base64.StdEncoding.EncodeToString(torrentBytes)
	startPaused := settings.StartPaused

	skipIfExists := true
	req := &CrossSeedRequest{
		TorrentData:       encodedTorrent,
		TargetInstanceIDs: append([]int(nil), settings.TargetInstanceIDs...),
		Tags:              append([]string(nil), settings.Tags...),
		IgnorePatterns:    append([]string(nil), settings.IgnorePatterns...),
		SkipIfExists:      &skipIfExists,
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

	log.Debug().
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
	torrentName, torrentHash, sourceFiles, err := ParseTorrentMetadata(torrentBytes)
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
		for tag := range strings.SplitSeq(matchedTorrent.Tags, ",") {
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
		log.Debug().
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
				log.Debug().
					Int("instanceID", candidate.InstanceID).
					Str("hash", newTorrent.Hash).
					Float64("progress", newTorrent.Progress).
					Msg("Auto-resumed 100% complete cross-seed torrent")
			}
		} else {
			// Torrent is not 100%, may be missing files (like .srt, sample, etc.)
			// This is expected and controlled by user's file layout
			log.Debug().
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

// AnalyzeTorrentForSearchAsync analyzes a torrent and performs capability filtering immediately,
// while optionally performing content filtering asynchronously in the background.
// This allows the UI to update immediately with capability results while waiting for content filtering.
func (s *Service) AnalyzeTorrentForSearchAsync(ctx context.Context, instanceID int, hash string, enableContentFiltering bool) (*AsyncTorrentAnalysis, error) {
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

	// Pre-fetch all indexer info (names and domains) for performance
	var indexerInfo map[int]jackett.EnabledIndexerInfo
	if s.jackettService != nil {
		indexerInfo, err = s.jackettService.GetEnabledIndexersInfo(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to fetch indexer info during analysis, using fallback lookups")
			indexerInfo = make(map[int]jackett.EnabledIndexerInfo) // Empty map as fallback
		}
	} else {
		indexerInfo = make(map[int]jackett.EnabledIndexerInfo)
	}

	// Get files to find the largest file for better content type detection
	sourceFiles, err := s.syncManager.GetTorrentFiles(ctx, instanceID, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent files: %w", err)
	}

	// Parse and detect content type
	sourceRelease := s.releaseCache.Parse(sourceTorrent.Name)
	contentDetectionRelease := sourceRelease

	if sourceFiles != nil && len(*sourceFiles) > 0 {
		largestFile := FindLargestFile(*sourceFiles)
		if largestFile != nil {
			largestFileRelease := s.releaseCache.Parse(largestFile.Name)
			largestFileRelease = enrichReleaseFromTorrent(largestFileRelease, sourceRelease)
			if largestFileRelease.Type != rls.Unknown {
				contentDetectionRelease = largestFileRelease
			}
		}
	}

	// Use unified content type detection
	contentInfo := DetermineContentType(contentDetectionRelease)

	// Get all available indexers first
	var allIndexers []int
	if s.jackettService != nil {
		indexersResponse, err := s.jackettService.GetIndexers(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get indexers during async analysis")
		} else {
			for _, indexer := range indexersResponse.Indexers {
				if indexer.Configured {
					if id, err := strconv.Atoi(indexer.ID); err == nil {
						allIndexers = append(allIndexers, id)
					}
				}
			}
		}
	}

	// Build base TorrentInfo
	torrentInfo := &TorrentInfo{
		InstanceID:       instanceID,
		InstanceName:     instance.Name,
		Hash:             sourceTorrent.Hash,
		Name:             sourceTorrent.Name,
		Category:         sourceTorrent.Category,
		Size:             sourceTorrent.Size,
		Progress:         sourceTorrent.Progress,
		ContentType:      contentInfo.ContentType,
		SearchType:       contentInfo.SearchType,
		SearchCategories: contentInfo.Categories,
		RequiredCaps:     contentInfo.RequiredCaps,
	}

	// Initialize filtering state
	filteringState := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: false,
		ContentCompleted:      false,
		ExcludedIndexers:      make(map[int]string),
		ContentMatches:        make([]string, 0),
	}

	result := &AsyncTorrentAnalysis{
		TorrentInfo:    torrentInfo,
		FilteringState: filteringState,
	}

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Ints("allIndexers", allIndexers).
		Bool("enableContentFiltering", enableContentFiltering).
		Msg("[CROSSSEED-ASYNC] Starting async torrent analysis")

	// Phase 1: Capability filtering (fast, synchronous)
	if len(allIndexers) > 0 && s.jackettService != nil {
		capabilityIndexers, err := s.jackettService.FilterIndexersForCapabilities(ctx, allIndexers, contentInfo.RequiredCaps, contentInfo.Categories)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to filter indexers by capabilities during async analysis")
			capabilityIndexers = allIndexers
		}

		filteringState.CapabilityIndexers = capabilityIndexers
		filteringState.CapabilitiesCompleted = true
		torrentInfo.AvailableIndexers = capabilityIndexers

		// Store initial state in cache immediately for UI polling
		if s.asyncFilteringCache != nil {
			cacheKey := asyncFilteringCacheKey(instanceID, hash)

			// Check if content filtering has already completed to avoid overwriting
			if existing, found := s.asyncFilteringCache.Get(cacheKey); found && existing.ContentCompleted {
				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Str("cacheKey", cacheKey).
					Bool("existingContentCompleted", existing.ContentCompleted).
					Int("existingFilteredCount", len(existing.FilteredIndexers)).
					Msg("[CROSSSEED-ASYNC] Skipping initial cache storage - content filtering already completed")
			} else {
				// Create a copy of the initial state
				cachedState := &AsyncIndexerFilteringState{
					CapabilitiesCompleted: true,
					ContentCompleted:      false,
					CapabilityIndexers:    append([]int(nil), capabilityIndexers...),
					FilteredIndexers:      append([]int(nil), capabilityIndexers...), // Initially same as capability indexers
					ExcludedIndexers:      make(map[int]string),
					ContentMatches:        make([]string, 0),
				}
				s.asyncFilteringCache.Set(cacheKey, cachedState, ttlcache.DefaultTTL)

				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Str("cacheKey", cacheKey).
					Bool("contentCompleted", cachedState.ContentCompleted).
					Int("capabilityIndexersCount", len(cachedState.CapabilityIndexers)).
					Int("filteredIndexersCount", len(cachedState.FilteredIndexers)).
					Msg("[CROSSSEED-ASYNC] Stored initial filtering state in cache")
			}
		}

		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Int("allIndexersCount", len(allIndexers)).
			Int("capabilityIndexersCount", len(capabilityIndexers)).
			Msg("[CROSSSEED-ASYNC] Capability filtering completed synchronously")

		// Phase 2: Content filtering (slow, potentially async)
		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Bool("enableContentFiltering", enableContentFiltering).
			Int("capabilityIndexersCount", len(capabilityIndexers)).
			Msg("[CROSSSEED-ASYNC] Phase 2: Content filtering decision")

		if enableContentFiltering {
			if len(capabilityIndexers) > 0 {
				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Int("capabilityIndexersCount", len(capabilityIndexers)).
					Msg("[CROSSSEED-ASYNC] Starting background content filtering")
				// Start content filtering in background
				go s.performAsyncContentFiltering(context.Background(), instanceID, hash, capabilityIndexers, indexerInfo, filteringState)
			} else {
				// No indexers left after capability filtering
				filteringState.FilteredIndexers = []int{}
				filteringState.ContentCompleted = true
				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Msg("[CROSSSEED-ASYNC] No indexers remain after capability filtering, skipping content filtering")
			}
		} else {
			// Content filtering disabled, mark as completed
			filteringState.FilteredIndexers = capabilityIndexers
			filteringState.ContentCompleted = true
			torrentInfo.FilteredIndexers = capabilityIndexers
		}
	} else {
		// No indexers available or jackett service unavailable
		filteringState.CapabilityIndexers = []int{}
		filteringState.FilteredIndexers = []int{}
		filteringState.CapabilitiesCompleted = true
		filteringState.ContentCompleted = true
	}

	return result, nil
}

// performAsyncContentFiltering performs content filtering in the background and updates the filtering state
// This method handles concurrent access to the state safely
func (s *Service) performAsyncContentFiltering(ctx context.Context, instanceID int, hash string, indexerIDs []int, indexerInfo map[int]jackett.EnabledIndexerInfo, state *AsyncIndexerFilteringState) {
	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Ints("indexerIDs", indexerIDs).
		Msg("[CROSSSEED-ASYNC] Starting background content filtering")

	filteredIndexers, excludedIndexers, contentMatches, err := s.filterIndexersByExistingContent(ctx, instanceID, hash, indexerIDs, indexerInfo)

	// Use atomic-like updates to avoid race conditions
	// Note: In a production system, you might want to use sync.Mutex for more complex state updates

	if err != nil {
		log.Warn().Err(err).Msg("Failed to filter indexers by existing content during async filtering")
		state.Error = fmt.Sprintf("Content filtering failed: %v", err)
		state.FilteredIndexers = indexerIDs // Fall back to capability-filtered list
		state.ExcludedIndexers = nil
		state.ContentMatches = nil
	} else {
		state.FilteredIndexers = filteredIndexers
		state.ExcludedIndexers = excludedIndexers
		state.ContentMatches = contentMatches
	}

	// Mark content filtering as completed (this should be the last operation)
	state.ContentCompleted = true

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Int("originalIndexerCount", len(indexerIDs)).
		Int("filteredIndexerCount", len(state.FilteredIndexers)).
		Int("excludedIndexerCount", len(state.ExcludedIndexers)).
		Bool("contentCompleted", state.ContentCompleted).
		Msg("[CROSSSEED-ASYNC] Content filtering completed successfully")

	// Store the completed state in cache for UI polling
	if s.asyncFilteringCache != nil {
		cacheKey := asyncFilteringCacheKey(instanceID, hash)
		// Create a copy of the state to avoid race conditions
		cachedState := &AsyncIndexerFilteringState{
			CapabilitiesCompleted: state.CapabilitiesCompleted,
			ContentCompleted:      state.ContentCompleted,
			CapabilityIndexers:    append([]int(nil), state.CapabilityIndexers...),
			FilteredIndexers:      append([]int(nil), state.FilteredIndexers...),
			ExcludedIndexers:      make(map[int]string),
			ContentMatches:        append([]string(nil), state.ContentMatches...),
		}
		// Copy excluded indexers map
		maps.Copy(cachedState.ExcludedIndexers, state.ExcludedIndexers)

		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Str("cacheKey", cacheKey).
			Bool("contentCompleted", cachedState.ContentCompleted).
			Int("filteredIndexersCount", len(cachedState.FilteredIndexers)).
			Msg("[CROSSSEED-ASYNC] Storing completed content filtering state in cache")

		s.asyncFilteringCache.Set(cacheKey, cachedState, ttlcache.DefaultTTL)

		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Str("cacheKey", cacheKey).
			Msg("[CROSSSEED-ASYNC] Stored completed filtering state in cache")
	}

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Int("inputIndexersCount", len(indexerIDs)).
		Int("filteredIndexersCount", len(state.FilteredIndexers)).
		Int("excludedIndexersCount", len(state.ExcludedIndexers)).
		Int("contentMatchesCount", len(state.ContentMatches)).
		Msg("[CROSSSEED-ASYNC] Background content filtering completed")
}

// AnalyzeTorrentForSearch analyzes a torrent and returns metadata about how it would be searched,
// without actually performing the search. This method now uses async filtering with immediate capability
// results and optional content filtering for better performance.
func (s *Service) AnalyzeTorrentForSearch(ctx context.Context, instanceID int, hash string) (*TorrentInfo, error) {
	// Check if we have cached async state with completed content filtering
	if s.asyncFilteringCache != nil {
		cacheKey := asyncFilteringCacheKey(instanceID, hash)
		if cached, found := s.asyncFilteringCache.Get(cacheKey); found && cached.ContentCompleted {
			// We have completed filtering results, use those instead of running new analysis
			asyncResult, err := s.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, false) // Don't restart content filtering
			if err != nil {
				// Fall back to cached state if torrent analysis fails
				log.Warn().Err(err).Msg("Failed to get torrent info, using cached filtering state only")
				return &TorrentInfo{
					AvailableIndexers:         cached.CapabilityIndexers,
					FilteredIndexers:          cached.FilteredIndexers,
					ExcludedIndexers:          cached.ExcludedIndexers,
					ContentMatches:            cached.ContentMatches,
					ContentFilteringCompleted: cached.ContentCompleted,
				}, nil
			}

			torrentInfo := asyncResult.TorrentInfo
			// Use the completed filtering results from cache
			torrentInfo.AvailableIndexers = cached.CapabilityIndexers
			torrentInfo.FilteredIndexers = cached.FilteredIndexers
			torrentInfo.ExcludedIndexers = cached.ExcludedIndexers
			torrentInfo.ContentMatches = cached.ContentMatches
			torrentInfo.ContentFilteringCompleted = cached.ContentCompleted

			log.Debug().
				Str("torrentHash", hash).
				Int("instanceID", instanceID).
				Bool("contentCompleted", cached.ContentCompleted).
				Int("filteredIndexersCount", len(cached.FilteredIndexers)).
				Msg("[CROSSSEED-ANALYZE] Using cached content filtering results")

			return torrentInfo, nil
		}
	}

	// Use the async version with content filtering enabled
	asyncResult, err := s.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, true)
	if err != nil {
		return nil, err
	}

	// Return immediate results with capability filtering
	// Content filtering will continue in background but we don't wait for it
	torrentInfo := asyncResult.TorrentInfo

	// Use capability-filtered indexers as the primary result
	if asyncResult.FilteringState.CapabilitiesCompleted {
		torrentInfo.AvailableIndexers = asyncResult.FilteringState.CapabilityIndexers
		// For immediate response, use capability indexers as filtered indexers
		// The UI can poll for refined results if needed
		torrentInfo.FilteredIndexers = asyncResult.FilteringState.CapabilityIndexers
		torrentInfo.ContentFilteringCompleted = asyncResult.FilteringState.ContentCompleted

		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Bool("capabilitiesCompleted", asyncResult.FilteringState.CapabilitiesCompleted).
			Bool("contentCompleted", asyncResult.FilteringState.ContentCompleted).
			Int("capabilityIndexersCount", len(asyncResult.FilteringState.CapabilityIndexers)).
			Msg("[CROSSSEED-ANALYZE] Returning immediate capability-filtered results")
	} else {
		log.Warn().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Msg("[CROSSSEED-ANALYZE] Capability filtering not completed, returning empty results")

		torrentInfo.AvailableIndexers = []int{}
		torrentInfo.FilteredIndexers = []int{}
		torrentInfo.ContentFilteringCompleted = false
	}

	return torrentInfo, nil
} // SearchTorrentMatches queries Torznab indexers for candidate torrents that match an existing torrent.
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
		largestFile := FindLargestFile(*sourceFiles)
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

	// Use unified content type detection with expanded categories for search
	contentInfo := DetermineContentType(contentDetectionRelease)

	sourceInfo := TorrentInfo{
		InstanceID:       instanceID,
		InstanceName:     instance.Name,
		Hash:             sourceTorrent.Hash,
		Name:             sourceTorrent.Name,
		Category:         sourceTorrent.Category,
		Size:             sourceTorrent.Size,
		Progress:         sourceTorrent.Progress,
		ContentType:      contentInfo.ContentType,
		SearchType:       contentInfo.SearchType,
		SearchCategories: contentInfo.Categories,
		RequiredCaps:     contentInfo.RequiredCaps,
	}

	query := strings.TrimSpace(opts.Query)
	if query == "" {
		// Use the appropriate release object based on content type
		var queryRelease rls.Release
		if contentInfo.IsMusic && contentDetectionRelease.Type == rls.Music {
			// For music, create a proper music release object by parsing the torrent name as music
			queryRelease = ParseMusicReleaseFromTorrentName(sourceRelease, sourceTorrent.Name)
		} else {
			// For other content types, use the torrent name release
			queryRelease = sourceRelease
		}

		// Build a better search query from parsed release info instead of using full filename
		if queryRelease.Title != "" {
			if contentInfo.IsMusic {
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

			// For TV series, add season/episode info (but not for music)
			if !contentInfo.IsMusic && queryRelease.Series > 0 {
				if queryRelease.Episode > 0 {
					query += fmt.Sprintf(" S%02dE%02d", queryRelease.Series, queryRelease.Episode)
				} else {
					query += fmt.Sprintf(" S%02d", queryRelease.Series)
				}
			}

			log.Debug().
				Str("originalName", sourceTorrent.Name).
				Str("generatedQuery", query).
				Str("contentType", contentInfo.ContentType).
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
	requestLimit := max(limit*3, limit)

	// Apply indexer filtering (capabilities first, then optionally content filtering async)
	var filteredIndexerIDs []int
	cacheKey := asyncFilteringCacheKey(instanceID, hash)

	// Check for cached content-filtered results first
	if s.asyncFilteringCache != nil {
		if cached, found := s.asyncFilteringCache.Get(cacheKey); found {
			log.Debug().
				Str("torrentHash", hash).
				Int("instanceID", instanceID).
				Bool("contentCompleted", cached.ContentCompleted).
				Int("filteredIndexersCount", len(cached.FilteredIndexers)).
				Int("capabilityIndexersCount", len(cached.CapabilityIndexers)).
				Int("excludedCount", len(cached.ExcludedIndexers)).
				Ints("providedIndexers", opts.IndexerIDs).
				Msg("[CROSSSEED-SEARCH] Found cached filtering state")

			if cached.ContentCompleted && len(cached.FilteredIndexers) > 0 {
				// Content filtering is complete, use the refined results
				filteredIndexerIDs = cached.FilteredIndexers
				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Ints("cachedFilteredIndexers", filteredIndexerIDs).
					Ints("providedIndexers", opts.IndexerIDs).
					Bool("contentCompleted", cached.ContentCompleted).
					Int("excludedCount", len(cached.ExcludedIndexers)).
					Msg("[CROSSSEED-SEARCH] Using cached content-filtered indexers")
			} else if len(cached.CapabilityIndexers) > 0 {
				// Content filtering not complete, but use capability results
				filteredIndexerIDs = cached.CapabilityIndexers
				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Ints("cachedCapabilityIndexers", filteredIndexerIDs).
					Bool("contentCompleted", cached.ContentCompleted).
					Msg("[CROSSSEED-SEARCH] Using cached capability-filtered indexers")
			}
		} else {
			log.Debug().
				Str("torrentHash", hash).
				Int("instanceID", instanceID).
				Str("cacheKey", cacheKey).
				Ints("providedIndexers", opts.IndexerIDs).
				Msg("[CROSSSEED-SEARCH] No cached filtering state found")
		}
	}

	// Only perform new filtering if no cache found
	if len(filteredIndexerIDs) == 0 {
		log.Debug().
			Str("torrentHash", hash).
			Int("instanceID", instanceID).
			Msg("[CROSSSEED-SEARCH] Performing new filtering")

		asyncAnalysis, err := s.filterIndexerIDsForTorrentAsync(ctx, instanceID, hash, opts.IndexerIDs, true)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to perform async indexer filtering for torrent search, using original list")
			filteredIndexerIDs = opts.IndexerIDs
		} else {
			// Use capability-filtered indexers immediately for search
			if asyncAnalysis.FilteringState.CapabilitiesCompleted {
				filteredIndexerIDs = asyncAnalysis.FilteringState.CapabilityIndexers
				sourceInfo = *asyncAnalysis.TorrentInfo

				log.Debug().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Ints("originalIndexers", opts.IndexerIDs).
					Ints("capabilityFilteredIndexers", filteredIndexerIDs).
					Bool("contentFilteringInProgress", !asyncAnalysis.FilteringState.ContentCompleted).
					Msg("[CROSSSEED-SEARCH] Using capability-filtered indexers for immediate search")
			} else {
				log.Warn().
					Str("torrentHash", hash).
					Int("instanceID", instanceID).
					Msg("[CROSSSEED-SEARCH] Capability filtering not completed, using original indexer list")
				filteredIndexerIDs = opts.IndexerIDs
			}

			// Keep runtime-derived torrent fields in sync with latest state
			sourceInfo.InstanceID = instanceID
			sourceInfo.InstanceName = instance.Name
			sourceInfo.Hash = sourceTorrent.Hash
			sourceInfo.Name = sourceTorrent.Name
			sourceInfo.Category = sourceTorrent.Category
			sourceInfo.Size = sourceTorrent.Size
			sourceInfo.Progress = sourceTorrent.Progress
			sourceInfo.ContentType = contentInfo.ContentType
			sourceInfo.SearchType = contentInfo.SearchType
		}
	}

	// Update sourceInfo fields that should always be current (regardless of filtering source)
	sourceInfo.SearchCategories = contentInfo.Categories
	sourceInfo.RequiredCaps = contentInfo.RequiredCaps

	if len(filteredIndexerIDs) == 0 {
		log.Debug().
			Str("torrentName", sourceTorrent.Name).
			Ints("originalIndexers", opts.IndexerIDs).
			Msg("[CROSSSEED-SEARCH] All indexers filtered out - no suitable indexers remain")

		// Return empty response instead of error to avoid breaking the UI
		return &TorrentSearchResponse{
			SourceTorrent: sourceInfo,
			Results:       []TorrentSearchResult{},
		}, nil
	}

	log.Debug().
		Str("torrentName", sourceTorrent.Name).
		Ints("originalIndexers", opts.IndexerIDs).
		Ints("filteredIndexers", filteredIndexerIDs).
		Msg("[CROSSSEED-SEARCH] Applied indexer filtering")

	searchReq := &jackett.TorznabSearchRequest{
		Query:      query,
		Limit:      requestLimit,
		IndexerIDs: filteredIndexerIDs,
		CacheMode:  opts.CacheMode,
	}

	// Add music-specific parameters if we have them
	if contentInfo.IsMusic {
		// Parse music information from the source release or torrent name
		var musicRelease rls.Release
		if sourceRelease.Type == rls.Music && sourceRelease.Artist != "" {
			musicRelease = sourceRelease
		} else {
			// Try to parse music info from torrent name
			musicRelease = ParseMusicReleaseFromTorrentName(sourceRelease, sourceTorrent.Name)
		}

		if musicRelease.Artist != "" {
			searchReq.Artist = musicRelease.Artist
		}
		if musicRelease.Title != "" {
			searchReq.Album = musicRelease.Title // For music, Title represents the album
		}
	}

	// Apply category filtering to the search request with indexer-specific optimization
	if len(contentInfo.Categories) > 0 {
		// If specific indexers are requested, optimize categories for those indexers
		if len(opts.IndexerIDs) > 0 && s.jackettService != nil {
			optimizedCategories := s.jackettService.GetOptimalCategoriesForIndexers(ctx, contentInfo.Categories, opts.IndexerIDs)
			searchReq.Categories = optimizedCategories

			log.Debug().
				Str("torrentName", sourceTorrent.Name).
				Str("contentType", contentInfo.ContentType).
				Ints("originalCategories", contentInfo.Categories).
				Ints("optimizedCategories", optimizedCategories).
				Ints("targetIndexers", opts.IndexerIDs).
				Msg("[CROSSSEED-SEARCH] Optimized categories for target indexers")
		} else {
			// Use original categories if no specific indexers or jackett service unavailable
			searchReq.Categories = contentInfo.Categories
		}

		// Add season/episode info for TV content
		if sourceRelease.Series > 0 {
			season := sourceRelease.Series
			searchReq.Season = &season

			if sourceRelease.Episode > 0 {
				episode := sourceRelease.Episode
				searchReq.Episode = &episode
			}
		}

		// Add year info if available
		if sourceRelease.Year > 0 {
			searchReq.Year = sourceRelease.Year
		}

		// Use the appropriate release object for logging based on content type
		var logRelease rls.Release
		if contentInfo.IsMusic && contentDetectionRelease.Type == rls.Music {
			// For music, create a proper music release object by parsing the torrent name as music
			logRelease = ParseMusicReleaseFromTorrentName(sourceRelease, sourceTorrent.Name)
		} else {
			logRelease = sourceRelease
		}

		logEvent := log.Debug().
			Str("torrentName", sourceTorrent.Name).
			Str("contentType", contentInfo.ContentType).
			Ints("categories", contentInfo.Categories).
			Int("year", logRelease.Year)

		// Show different metadata based on content type
		if !contentInfo.IsMusic {
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
	log.Debug().
		Str("torrentName", sourceTorrent.Name).
		Int("totalResults", totalResults).
		Int("releaseFiltered", releaseFilteredCount).
		Int("sizeFiltered", sizeFilteredCount).
		Int("finalMatches", matchedResults).
		Float64("tolerancePercent", settings.SizeMismatchTolerancePercent).
		Msg("[CROSSSEED-SEARCH] Search filtering completed")

	if len(scored) == 0 {
		return &TorrentSearchResponse{
			SourceTorrent: sourceInfo,
			Results:       []TorrentSearchResult{},
			Cache:         searchResp.Cache,
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
		Cache:         searchResp.Cache,
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

		torrentBytes, err := s.jackettService.DownloadTorrent(ctx, jackett.TorrentDownloadRequest{
			IndexerID:   cachedResult.IndexerID,
			DownloadURL: cachedResult.DownloadURL,
			GUID:        cachedResult.GUID,
			Title:       cachedResult.Title,
			Size:        cachedResult.Size,
		})
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

// asyncFilteringCacheKey generates a cache key for async filtering state
func asyncFilteringCacheKey(instanceID int, hash string) string {
	cleanHash := strings.ToLower(strings.TrimSpace(hash))
	if cleanHash == "" {
		return fmt.Sprintf("async:%d", instanceID)
	}
	return fmt.Sprintf("async:%d:%s", instanceID, cleanHash)
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

func (s *Service) searchRunLoop(ctx context.Context, state *searchRunState) {
	defer func() {
		canceled := ctx.Err() == context.Canceled
		s.finalizeSearchRun(state, canceled)
	}()

	if err := s.refreshSearchQueue(ctx, state); err != nil {
		state.lastError = err
		return
	}

	interval := time.Duration(state.opts.IntervalSeconds) * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		candidate, err := s.nextSearchCandidate(ctx, state)
		if err != nil {
			state.lastError = err
			return
		}
		if candidate == nil {
			return
		}

		s.setCurrentCandidate(state, candidate)

		if err := s.processSearchCandidate(ctx, state, candidate); err != nil {
			state.lastError = err
		}

		if interval > 0 {
			s.setNextWake(state, time.Now().Add(interval))
			t := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			s.setNextWake(state, time.Time{})
		}
	}
}

func (s *Service) finalizeSearchRun(state *searchRunState, canceled bool) {
	completed := time.Now().UTC()
	s.searchMu.Lock()
	state.run.CompletedAt = &completed
	if s.searchState == state {
		s.searchState.currentCandidate = nil
	}
	if canceled && state.lastError == nil {
		state.run.Status = models.CrossSeedSearchRunStatusCanceled
		msg := "search run canceled"
		state.run.ErrorMessage = &msg
	} else if state.lastError != nil {
		state.run.Status = models.CrossSeedSearchRunStatusFailed
		errMsg := state.lastError.Error()
		state.run.ErrorMessage = &errMsg
	} else {
		state.run.Status = models.CrossSeedSearchRunStatusSuccess
	}
	s.searchMu.Unlock()

	if updated, err := s.automationStore.UpdateSearchRun(context.Background(), state.run); err == nil {
		s.searchMu.Lock()
		if s.searchState == state {
			state.run = updated
			s.searchState.run = updated
		}
		s.searchMu.Unlock()
	} else {
		log.Warn().Err(err).Msg("failed to persist search run state")
	}

	s.searchMu.Lock()
	if s.searchState == state {
		s.searchState = nil
		s.searchCancel = nil
	}
	s.searchMu.Unlock()
}

// deduplicateSourceTorrents removes duplicate torrents from the search queue by keeping only
// the oldest instance of each unique content. This prevents searching the same content multiple
// times when cross-seeds exist in the source instance.
//
// The deduplication works by:
// 1. Parsing each torrent's release info (title, year, series, episode, group)
// 2. Grouping torrents with matching content using the same logic as cross-seed matching
// 3. For each group, keeping only the torrent with the earliest AddedOn timestamp
//
// This significantly reduces API calls and processing time when an instance contains multiple
// cross-seeds of the same content from different trackers, while enforcing strict matching so
// that season packs never collapse individual-episode queue entries.
func (s *Service) deduplicateSourceTorrents(torrents []qbt.Torrent) []qbt.Torrent {
	if len(torrents) <= 1 {
		return torrents
	}

	// Parse all torrents and track their releases
	type torrentWithRelease struct {
		torrent qbt.Torrent
		release rls.Release
	}

	parsed := make([]torrentWithRelease, 0, len(torrents))
	for _, torrent := range torrents {
		release := s.releaseCache.Parse(torrent.Name)
		parsed = append(parsed, torrentWithRelease{
			torrent: torrent,
			release: release,
		})
	}

	// Group torrents by matching content
	// We'll track the oldest torrent for each unique content group
	type contentGroup struct {
		oldest     *qbt.Torrent
		addedOn    int64
		duplicates []string // Track duplicate names for logging
	}

	groups := make(map[int]*contentGroup)
	groupIndex := 0

	for i := range parsed {
		current := &parsed[i]

		// Try to find an existing group this torrent belongs to
		foundGroup := -1
		for _, existing := range parsed[:i] {
			if s.releasesMatch(current.release, existing.release, false) {
				// Find which group this existing torrent belongs to
				for groupID, group := range groups {
					if group.oldest.Hash == existing.torrent.Hash {
						foundGroup = groupID
						break
					}
					// Check if any duplicate in the group matches
					if slices.Contains(group.duplicates, existing.torrent.Hash) {
						foundGroup = groupID
					}
					if foundGroup != -1 {
						break
					}
				}
				if foundGroup != -1 {
					break
				}
			}
		}

		if foundGroup == -1 {
			// Create new group with this torrent as the first member
			groups[groupIndex] = &contentGroup{
				oldest:     &current.torrent,
				addedOn:    current.torrent.AddedOn,
				duplicates: []string{},
			}
			groupIndex++
		} else {
			// Add to existing group, update oldest if this one is older
			group := groups[foundGroup]
			if current.torrent.AddedOn < group.addedOn {
				// Current torrent is older, make it the representative
				group.duplicates = append(group.duplicates, group.oldest.Hash)
				group.oldest = &current.torrent
				group.addedOn = current.torrent.AddedOn
			} else {
				// Keep existing oldest, track this as duplicate
				group.duplicates = append(group.duplicates, current.torrent.Hash)
			}
		}
	}

	// Build deduplicated list from group representatives
	deduplicated := make([]qbt.Torrent, 0, len(groups))
	totalDuplicates := 0

	for _, group := range groups {
		deduplicated = append(deduplicated, *group.oldest)
		if len(group.duplicates) > 0 {
			totalDuplicates += len(group.duplicates)
			log.Debug().
				Str("representative", group.oldest.Name).
				Str("representativeHash", group.oldest.Hash).
				Int64("addedOn", group.oldest.AddedOn).
				Int("duplicateCount", len(group.duplicates)).
				Strs("duplicateHashes", group.duplicates).
				Msg("[CROSSSEED-DEDUP] Grouped duplicate content, keeping oldest")
		}
	}

	log.Info().
		Int("originalCount", len(torrents)).
		Int("deduplicatedCount", len(deduplicated)).
		Int("duplicatesRemoved", totalDuplicates).
		Int("uniqueContentGroups", len(groups)).
		Msg("[CROSSSEED-DEDUP] Source torrent deduplication completed")

	return deduplicated
}

func (s *Service) refreshSearchQueue(ctx context.Context, state *searchRunState) error {
	torrents, err := s.syncManager.GetAllTorrents(ctx, state.opts.InstanceID)
	if err != nil {
		return fmt.Errorf("list torrents: %w", err)
	}

	filtered := make([]qbt.Torrent, 0, len(torrents))
	for _, torrent := range torrents {
		if matchesSearchFilters(&torrent, state.opts) {
			filtered = append(filtered, torrent)
		}
	}

	// Deduplicate source torrents to avoid searching the same content multiple times
	// when cross-seeds exist in the source instance
	deduplicated := s.deduplicateSourceTorrents(filtered)

	state.queue = deduplicated
	state.index = 0
	s.searchMu.Lock()
	state.run.TotalTorrents = len(deduplicated)
	s.searchMu.Unlock()
	s.persistSearchRun(state)

	return nil
}

func (s *Service) nextSearchCandidate(ctx context.Context, state *searchRunState) (*qbt.Torrent, error) {
	for {
		if state.index >= len(state.queue) {
			return nil, nil
		}

		torrent := state.queue[state.index]
		state.index++

		skip, err := s.shouldSkipCandidate(ctx, state, &torrent)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		return &torrent, nil
	}
}

func (s *Service) shouldSkipCandidate(ctx context.Context, state *searchRunState, torrent *qbt.Torrent) (bool, error) {
	if torrent == nil {
		return true, nil
	}
	if torrent.Hash == "" {
		return true, nil
	}
	if torrent.Progress < 1.0 {
		return true, nil
	}

	if s.automationStore == nil {
		return false, nil
	}

	last, found, err := s.automationStore.GetSearchHistory(ctx, state.opts.InstanceID, torrent.Hash)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}

	cooldown := time.Duration(state.opts.CooldownMinutes) * time.Minute
	if cooldown <= 0 {
		return false, nil
	}

	return time.Since(last) < cooldown, nil
}

func (s *Service) processSearchCandidate(ctx context.Context, state *searchRunState, torrent *qbt.Torrent) error {
	s.searchMu.Lock()
	state.run.Processed++
	s.searchMu.Unlock()
	processedAt := time.Now().UTC()

	if s.automationStore != nil {
		if err := s.automationStore.UpsertSearchHistory(ctx, state.opts.InstanceID, torrent.Hash, processedAt); err != nil {
			log.Debug().Err(err).Msg("failed to update search history")
		}
	}

	// Use async filtering for better performance - capability filtering returns immediately
	asyncAnalysis, err := s.filterIndexerIDsForTorrentAsync(ctx, state.opts.InstanceID, torrent.Hash, state.opts.IndexerIDs, true)
	if err != nil {
		s.searchMu.Lock()
		state.run.TorrentsFailed++
		s.searchMu.Unlock()
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Added:        false,
			Message:      fmt.Sprintf("analyze torrent: %v", err),
			ProcessedAt:  processedAt,
		})
		s.persistSearchRun(state)
		return err
	}

	filteringState := asyncAnalysis.FilteringState
	allowedIndexerIDs, skipReason := s.resolveAllowedIndexerIDs(ctx, torrent.Hash, filteringState, state.opts.IndexerIDs)

	if len(allowedIndexerIDs) > 0 {
		log.Debug().
			Str("torrentHash", torrent.Hash).
			Int("instanceID", state.opts.InstanceID).
			Ints("originalIndexers", state.opts.IndexerIDs).
			Ints("selectedIndexers", allowedIndexerIDs).
			Bool("contentFilteringCompleted", filteringState != nil && filteringState.ContentCompleted).
			Msg("[CROSSSEED-SEARCH-AUTO] Using resolved indexer set for automation search")
	}

	if len(allowedIndexerIDs) == 0 {
		s.searchMu.Lock()
		state.run.TorrentsSkipped++
		s.searchMu.Unlock()
		if skipReason == "" {
			skipReason = "no indexers support required caps"
		}
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Added:        false,
			Message:      skipReason,
			ProcessedAt:  processedAt,
		})
		s.persistSearchRun(state)
		return nil
	}

	searchResp, err := s.SearchTorrentMatches(ctx, state.opts.InstanceID, torrent.Hash, TorrentSearchOptions{
		IndexerIDs:             allowedIndexerIDs,
		FindIndividualEpisodes: state.opts.FindIndividualEpisodes,
	})
	if err != nil {
		s.searchMu.Lock()
		state.run.TorrentsFailed++
		s.searchMu.Unlock()
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Added:        false,
			Message:      fmt.Sprintf("search failed: %v", err),
			ProcessedAt:  processedAt,
		})
		s.persistSearchRun(state)
		return err
	}

	if len(searchResp.Results) == 0 {
		s.searchMu.Lock()
		state.run.TorrentsSkipped++
		s.searchMu.Unlock()
		s.appendSearchResult(state, models.CrossSeedSearchResult{
			TorrentHash:  torrent.Hash,
			TorrentName:  torrent.Name,
			IndexerName:  "",
			ReleaseTitle: "",
			Added:        false,
			Message:      "no matches returned",
			ProcessedAt:  processedAt,
		})
		s.persistSearchRun(state)
		return nil
	}

	successCount := 0
	nonSuccessAttempt := false
	var attemptErrors []string

	for _, match := range searchResp.Results {
		attemptResult, err := s.executeCrossSeedSearchAttempt(ctx, state, torrent, match, processedAt)
		if attemptResult != nil {
			if attemptResult.Added {
				s.searchMu.Lock()
				state.run.TorrentsAdded++
				s.searchMu.Unlock()
				successCount++
			} else {
				nonSuccessAttempt = true
			}
			s.appendSearchResult(state, *attemptResult)
		}
		if err != nil {
			attemptErrors = append(attemptErrors, fmt.Sprintf("%s: %v", match.Indexer, err))
		}
	}

	if successCount > 0 {
		s.persistSearchRun(state)
		return nil
	}

	if len(attemptErrors) > 0 {
		s.searchMu.Lock()
		state.run.TorrentsFailed++
		s.searchMu.Unlock()
		s.persistSearchRun(state)
		return fmt.Errorf("cross-seed matches failed: %s", attemptErrors[0])
	}

	if nonSuccessAttempt {
		s.searchMu.Lock()
		state.run.TorrentsSkipped++
		s.searchMu.Unlock()
		s.persistSearchRun(state)
		return nil
	}

	// Fallback: treat as skipped if no attempts recorded for some reason
	s.searchMu.Lock()
	state.run.TorrentsSkipped++
	s.searchMu.Unlock()
	s.persistSearchRun(state)
	return nil
}

func (s *Service) resolveAllowedIndexerIDs(ctx context.Context, torrentHash string, filteringState *AsyncIndexerFilteringState, fallback []int) ([]int, string) {
	if filteringState == nil {
		return fallback, ""
	}

	if filteringState.CapabilitiesCompleted && !filteringState.ContentCompleted {
		completed, waited, timedOut := s.waitForContentFilteringCompletion(ctx, torrentHash, filteringState)
		if waited > 0 {
			log.Debug().
				Str("torrentHash", torrentHash).
				Dur("waited", waited).
				Bool("completed", completed).
				Bool("timedOut", timedOut).
				Msg("[CROSSSEED-SEARCH-AUTO] Waited for content filtering during seeded search")
		}
	}

	if filteringState.ContentCompleted {
		if len(filteringState.FilteredIndexers) > 0 {
			return filteringState.FilteredIndexers, ""
		}
		return nil, s.describeFilteringSkipReason(filteringState)
	}

	if filteringState.CapabilitiesCompleted {
		if len(filteringState.CapabilityIndexers) > 0 {
			return filteringState.CapabilityIndexers, ""
		}
		return nil, "no indexers support required caps"
	}

	if len(fallback) > 0 {
		return fallback, ""
	}

	return fallback, ""
}

func (s *Service) waitForContentFilteringCompletion(ctx context.Context, torrentHash string, state *AsyncIndexerFilteringState) (bool, time.Duration, bool) {
	if state == nil {
		return false, 0, false
	}
	if state.ContentCompleted {
		return true, 0, false
	}

	start := time.Now()
	waitCtx, cancel := context.WithTimeout(ctx, contentFilteringWaitTimeout)
	defer cancel()

	ticker := time.NewTicker(contentFilteringPollInterval)
	defer ticker.Stop()

	for {
		if state.ContentCompleted {
			return true, time.Since(start), false
		}

		select {
		case <-ticker.C:
			if state.ContentCompleted {
				return true, time.Since(start), false
			}
		case <-waitCtx.Done():
			err := waitCtx.Err()
			return state.ContentCompleted, time.Since(start), errors.Is(err, context.DeadlineExceeded)
		}
	}
}

func (s *Service) describeFilteringSkipReason(state *AsyncIndexerFilteringState) string {
	if state == nil {
		return "no indexers available"
	}
	if len(state.ExcludedIndexers) > 0 {
		return fmt.Sprintf("skipped: already seeded from %d tracker(s)", len(state.ExcludedIndexers))
	}
	if state.Error != "" {
		return fmt.Sprintf("content filtering failed: %s", state.Error)
	}
	if state.CapabilitiesCompleted && len(state.CapabilityIndexers) == 0 {
		return "no indexers support required caps"
	}
	return "no eligible indexers after filtering"
}

func (s *Service) executeCrossSeedSearchAttempt(ctx context.Context, state *searchRunState, torrent *qbt.Torrent, match TorrentSearchResult, processedAt time.Time) (*models.CrossSeedSearchResult, error) {
	result := &models.CrossSeedSearchResult{
		TorrentHash:  torrent.Hash,
		TorrentName:  torrent.Name,
		IndexerName:  match.Indexer,
		ReleaseTitle: match.Title,
		ProcessedAt:  processedAt,
	}

	data, err := s.jackettService.DownloadTorrent(ctx, jackett.TorrentDownloadRequest{
		IndexerID:   match.IndexerID,
		DownloadURL: match.DownloadURL,
		GUID:        match.GUID,
		Title:       match.Title,
		Size:        match.Size,
	})
	if err != nil {
		result.Message = fmt.Sprintf("download failed: %v", err)
		return result, fmt.Errorf("download failed: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	startPaused := state.opts.StartPaused
	skipIfExists := true
	request := &CrossSeedRequest{
		TorrentData:            encoded,
		TargetInstanceIDs:      []int{state.opts.InstanceID},
		StartPaused:            &startPaused,
		Tags:                   append([]string(nil), state.opts.TagsOverride...),
		Category:               "",
		FindIndividualEpisodes: state.opts.FindIndividualEpisodes,
		SkipIfExists:           &skipIfExists,
	}
	if state.opts.CategoryOverride != nil && strings.TrimSpace(*state.opts.CategoryOverride) != "" {
		cat := *state.opts.CategoryOverride
		request.Category = cat
	}
	resp, err := s.CrossSeed(ctx, request)
	if err != nil {
		result.Message = fmt.Sprintf("cross-seed failed: %v", err)
		return result, fmt.Errorf("cross-seed failed: %w", err)
	}

	if resp.Success {
		result.Added = true
		result.Message = fmt.Sprintf("added via %s", match.Indexer)
		return result, nil
	}

	result.Added = false
	result.Message = fmt.Sprintf("no instances accepted torrent via %s", match.Indexer)
	return result, nil
}

// filterIndexerIDsForTorrentAsync performs indexer filtering with async content filtering support.
// This allows immediate return of capability-filtered results while content filtering continues in background.
func (s *Service) filterIndexerIDsForTorrentAsync(ctx context.Context, instanceID int, hash string, requested []int, enableContentFiltering bool) (*AsyncTorrentAnalysis, error) {
	cacheKey := asyncFilteringCacheKey(instanceID, hash)

	// Check if we already have completed content filtering to avoid overwriting
	if s.asyncFilteringCache != nil {
		if existing, found := s.asyncFilteringCache.Get(cacheKey); found && existing.ContentCompleted {
			log.Warn().
				Str("torrentHash", hash).
				Int("instanceID", instanceID).
				Str("cacheKey", cacheKey).
				Bool("existingContentCompleted", existing.ContentCompleted).
				Int("existingFilteredCount", len(existing.FilteredIndexers)).
				Msg("[CROSSSEED-ASYNC] WARNING: Avoiding overwrite of completed content filtering")

			// Return existing completed state instead of creating new filtering
			return &AsyncTorrentAnalysis{
				FilteringState: existing,
				TorrentInfo:    nil, // We don't have the original TorrentInfo, but it's not needed for search
			}, nil
		}
	}

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Str("cacheKey", cacheKey).
		Bool("enableContentFiltering", enableContentFiltering).
		Ints("requestedIndexers", requested).
		Msg("[CROSSSEED-ASYNC] Starting new async filtering (may overwrite existing cache)")

	// Use the async analysis method
	return s.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, enableContentFiltering)
}

// GetAsyncFilteringStatus returns the current status of async filtering for a torrent.
// This can be used by the UI to poll for updates after capability filtering is complete.
func (s *Service) GetAsyncFilteringStatus(ctx context.Context, instanceID int, hash string) (*AsyncIndexerFilteringState, error) {
	if instanceID <= 0 {
		return nil, fmt.Errorf("invalid instance id: %d", instanceID)
	}
	if strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("torrent hash is required")
	}

	// Try to get cached state first
	if s.asyncFilteringCache != nil {
		cacheKey := asyncFilteringCacheKey(instanceID, hash)
		if cached, found := s.asyncFilteringCache.Get(cacheKey); found {
			log.Debug().
				Str("torrentHash", hash).
				Int("instanceID", instanceID).
				Bool("capabilitiesCompleted", cached.CapabilitiesCompleted).
				Bool("contentCompleted", cached.ContentCompleted).
				Msg("[CROSSSEED-ASYNC] Retrieved cached filtering status")
			return cached, nil
		}
	}

	// If no cached state, run analysis to generate initial state
	// This handles cases where the cache has expired or this is a new request
	asyncResult, err := s.AnalyzeTorrentForSearchAsync(ctx, instanceID, hash, true)
	if err != nil {
		return nil, err
	}

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Bool("capabilitiesCompleted", asyncResult.FilteringState.CapabilitiesCompleted).
		Bool("contentCompleted", asyncResult.FilteringState.ContentCompleted).
		Msg("[CROSSSEED-ASYNC] Generated new filtering status (no cache)")

	return asyncResult.FilteringState, nil
}

// filterIndexersByExistingContent removes indexers for which we already have matching content
// with the same tracker domains. This reduces redundant cross-seed searches by avoiding indexers
// we already have from the same tracker sources.
//
// The filtering works by:
// 1. Getting the source torrent being searched for and parsing its release info
// 2. Retrieving cached torrents from the source instance (no additional qBittorrent calls)
// 3. For each indexer, checking if existing torrents from matching tracker domains contain similar content
// 4. Removing indexers where we already have matching content from associated tracker domains
//
// This is similar to how indexers are filtered for tracker capability mismatches,
// but focuses on content duplication rather than technical capabilities.
func (s *Service) filterIndexersByExistingContent(ctx context.Context, instanceID int, hash string, indexerIDs []int, indexerInfo map[int]jackett.EnabledIndexerInfo) ([]int, map[int]string, []string, error) {
	if len(indexerIDs) == 0 {
		return indexerIDs, nil, nil, nil
	}

	// If indexer info not provided, fetch it ourselves
	if indexerInfo == nil && s.jackettService != nil {
		var err error
		indexerInfo, err = s.jackettService.GetEnabledIndexersInfo(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to fetch indexer info for content filtering, proceeding without filtering")
			return indexerIDs, nil, nil, nil
		}
	}
	if indexerInfo == nil {
		indexerInfo = make(map[int]jackett.EnabledIndexerInfo)
	}

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Ints("inputIndexers", indexerIDs).
		Int("inputCount", len(indexerIDs)).
		Msg("[CROSSSEED-FILTER] *** FILTER FUNCTION CALLED ***")

	// TEMPORARY TEST: Remove all indexers to see if the filtering is working
	testFilteringEnabled := false // Set to true to test if filtering is working
	if testFilteringEnabled {
		log.Debug().Msg("[CROSSSEED-FILTER] *** TEST MODE: FILTERING OUT ALL INDEXERS ***")
		return []int{}, nil, nil, nil
	}

	log.Debug().
		Str("torrentHash", hash).
		Int("instanceID", instanceID).
		Ints("inputIndexers", indexerIDs).
		Int("inputCount", len(indexerIDs)).
		Msg("[CROSSSEED-FILTER] Starting indexer content filtering")

	// Get the source torrent being searched for
	sourceTorrent, err := s.getTorrentByHash(ctx, instanceID, hash)
	if err != nil {
		return indexerIDs, nil, nil, err
	}

	log.Debug().
		Str("sourceTorrentName", sourceTorrent.Name).
		Str("sourceTorrentHash", sourceTorrent.Hash).
		Msg("[CROSSSEED-FILTER] Source torrent info")

	// Parse the source torrent to understand what content we're looking for
	sourceRelease := s.releaseCache.Parse(sourceTorrent.Name)

	log.Debug().
		Str("sourceTitle", sourceRelease.Title).
		Str("sourceGroup", sourceRelease.Group).
		Int("sourceSeries", sourceRelease.Series).
		Int("sourceEpisode", sourceRelease.Episode).
		Int("sourceYear", sourceRelease.Year).
		Str("sourceType", sourceRelease.Type.String()).
		Msg("[CROSSSEED-FILTER] Parsed source release info")

	// Get cached torrents from the active instance only
	instanceTorrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return indexerIDs, nil, nil, fmt.Errorf("failed to get cached instance torrents: %w", err)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("cachedInstanceTorrents", len(instanceTorrents)).
		Msg("[CROSSSEED-FILTER] Retrieved cached instance torrents")

	type matchedTorrent struct {
		view           qbittorrent.CrossInstanceTorrentView
		trackerDomains []string
	}

	var (
		matchedContent   []matchedTorrent
		contentMatches   []string
		potentialMatches []string
	)

	for _, crossTorrent := range instanceTorrents {
		// Skip the source torrent itself
		if crossTorrent.InstanceID == instanceID && crossTorrent.Hash == sourceTorrent.Hash {
			continue
		}

		// Parse the existing torrent to see if it matches the content we're looking for
		existingRelease := s.releaseCache.Parse(crossTorrent.Name)
		if !s.releasesMatch(sourceRelease, existingRelease, false) {
			continue
		}

		matchLabel := fmt.Sprintf("%s (%s)", crossTorrent.Name, crossTorrent.InstanceName)
		potentialMatches = append(potentialMatches, fmt.Sprintf("%s (Instance: %s)", crossTorrent.Name, crossTorrent.InstanceName))
		contentMatches = append(contentMatches, matchLabel)

		trackerDomains := s.extractTrackerDomainsFromTorrent(crossTorrent.TorrentView.Torrent)
		matchedContent = append(matchedContent, matchedTorrent{
			view:           crossTorrent,
			trackerDomains: trackerDomains,
		})

		log.Debug().
			Str("matchingTorrentName", crossTorrent.Name).
			Str("matchingInstanceName", crossTorrent.InstanceName).
			Strs("trackerDomains", trackerDomains).
			Msg("[CROSSSEED-FILTER] Found content match with tracker domains")
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("contentMatches", len(matchedContent)).
		Strs("potentialMatches", potentialMatches).
		Msg("[CROSSSEED-FILTER] Content matching analysis")

	// Check each indexer to see if we already have content that matches what it would provide
	var filteredIndexerIDs []int
	excludedIndexers := make(map[int]string) // Track why indexers were excluded

	for _, indexerID := range indexerIDs {
		shouldIncludeIndexer := true
		exclusionReason := ""

		// Get indexer information
		indexerName := jackett.GetIndexerNameFromInfo(indexerInfo, indexerID)
		if indexerName == "" {
			// If we can't get indexer info, include it to be safe
			filteredIndexerIDs = append(filteredIndexerIDs, indexerID)
			log.Debug().
				Int("indexerID", indexerID).
				Msg("[CROSSSEED-FILTER] Including indexer: could not get indexer name")
			continue
		}

		// Skip searching indexers that already provided the source torrent
		if sourceTorrent != nil && s.torrentMatchesIndexer(*sourceTorrent, indexerName) {
			shouldIncludeIndexer = false
			exclusionReason = "already seeded from this tracker"
		}

		// Check if we already have content that this indexer would likely provide
		if shouldIncludeIndexer && len(matchedContent) > 0 {
			for _, match := range matchedContent {
				if s.trackerDomainsMatchIndexer(match.trackerDomains, indexerName) {
					exclusionReason = fmt.Sprintf("has matching content from %s (%s)", match.view.InstanceName, match.view.Name)
					shouldIncludeIndexer = false
					break
				}
			}
		}

		if shouldIncludeIndexer {
			filteredIndexerIDs = append(filteredIndexerIDs, indexerID)
			log.Debug().
				Int("indexerID", indexerID).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-FILTER] INCLUDING indexer: no matching content found")
		} else {
			excludedIndexers[indexerID] = exclusionReason
		}
	}

	log.Debug().
		Str("torrentHash", hash).
		Int("inputCount", len(indexerIDs)).
		Int("outputCount", len(filteredIndexerIDs)).
		Int("excludedCount", len(excludedIndexers)).
		Interface("excludedIndexers", excludedIndexers).
		Msg("[CROSSSEED-FILTER] Content filtering completed")

	return filteredIndexerIDs, excludedIndexers, contentMatches, nil
}

// torrentMatchesIndexer checks if a torrent came from a tracker associated with the given indexer.
func (s *Service) torrentMatchesIndexer(torrent qbt.Torrent, indexerName string) bool {
	trackerDomains := s.extractTrackerDomainsFromTorrent(torrent)
	return s.trackerDomainsMatchIndexer(trackerDomains, indexerName)
}

// trackerDomainsMatchIndexer checks if any of the provided domains align with the target indexer.
func (s *Service) trackerDomainsMatchIndexer(trackerDomains []string, indexerName string) bool {
	if len(trackerDomains) == 0 {
		return false
	}

	normalizedIndexerName := s.normalizeIndexerName(indexerName)
	specificIndexerDomain := s.getCachedIndexerDomain(indexerName)

	// Check hardcoded domain mappings first
	for _, trackerDomain := range trackerDomains {
		normalizedTrackerDomain := strings.ToLower(trackerDomain)

		// Check if this tracker domain maps to the indexer domain
		if mappedDomains, exists := s.domainMappings[normalizedTrackerDomain]; exists {
			for _, mappedDomain := range mappedDomains {
				normalizedMappedDomain := strings.ToLower(mappedDomain)

				// Check if mapped domain matches indexer name or specific indexer domain
				if normalizedMappedDomain == normalizedIndexerName ||
					(specificIndexerDomain != "" && normalizedMappedDomain == strings.ToLower(specificIndexerDomain)) {
					log.Debug().
						Str("matchType", "hardcoded_mapping").
						Str("trackerDomain", trackerDomain).
						Str("mappedDomain", mappedDomain).
						Str("indexerName", indexerName).
						Str("specificIndexerDomain", specificIndexerDomain).
						Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Hardcoded domain mapping ***")
					return true
				}
			}
		}
	}

	// Check if any tracker domain matches or contains the indexer name
	for _, domain := range trackerDomains {
		normalizedDomain := strings.ToLower(domain)

		// 1. Direct match: normalized indexer name matches domain
		if normalizedIndexerName == normalizedDomain {
			log.Debug().
				Str("matchType", "direct").
				Str("domain", domain).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Direct match ***")
			return true
		}

		// 2. Check if torrent domain matches the specific indexer's domain
		if specificIndexerDomain != "" {
			normalizedSpecificDomain := strings.ToLower(specificIndexerDomain)

			// Direct domain match
			if normalizedDomain == normalizedSpecificDomain {
				log.Debug().
					Str("matchType", "specific_indexer_domain_direct").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Specific indexer domain direct match ***")
				return true
			}

			// Check if indexer name matches the indexer domain (handles cases where indexer name is the domain)
			if normalizedIndexerName == normalizedSpecificDomain {
				log.Debug().
					Str("matchType", "indexer_name_to_specific_domain").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Indexer name matches specific domain ***")
				return true
			}
		}

		// 3. Partial match: domain contains normalized indexer name or vice versa
		if strings.Contains(normalizedDomain, normalizedIndexerName) {
			log.Debug().
				Str("matchType", "domain_contains_indexer").
				Str("domain", domain).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Domain contains indexer ***")
			return true
		}
		if strings.Contains(normalizedIndexerName, normalizedDomain) {
			log.Debug().
				Str("matchType", "indexer_contains_domain").
				Str("domain", domain).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Indexer contains domain ***")
			return true
		}

		// 4. Check partial matches against the specific indexer domain
		if specificIndexerDomain != "" {
			normalizedSpecificDomain := strings.ToLower(specificIndexerDomain)

			// Check if torrent domain contains indexer domain or vice versa
			if strings.Contains(normalizedDomain, normalizedSpecificDomain) {
				log.Debug().
					Str("matchType", "torrent_domain_contains_specific_indexer_domain").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Torrent domain contains specific indexer domain ***")
				return true
			}
			if strings.Contains(normalizedSpecificDomain, normalizedDomain) {
				log.Debug().
					Str("matchType", "specific_indexer_domain_contains_torrent_domain").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Specific indexer domain contains torrent domain ***")
				return true
			}
		} // Handle TLD variations and domain normalization
		domainWithoutTLD := normalizedDomain
		for _, suffix := range []string{".cc", ".org", ".net", ".com", ".to", ".me", ".tv", ".xyz"} {
			if strings.HasSuffix(domainWithoutTLD, suffix) {
				domainWithoutTLD = strings.TrimSuffix(domainWithoutTLD, suffix)
				break
			}
		}

		// Normalize the domain name for comparison (remove hyphens, dots, etc.)
		normalizedDomainName := s.normalizeDomainName(domainWithoutTLD)

		// Direct match after normalization
		if normalizedIndexerName == normalizedDomainName {
			log.Debug().
				Str("matchType", "normalized_match").
				Str("domain", domain).
				Str("normalizedDomainName", normalizedDomainName).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized domain match ***")
			return true
		}

		// Partial match after normalization
		if strings.Contains(normalizedDomainName, normalizedIndexerName) {
			log.Debug().
				Str("matchType", "normalized_domain_contains_indexer").
				Str("domain", domain).
				Str("normalizedDomainName", normalizedDomainName).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized domain contains indexer ***")
			return true
		}
		if strings.Contains(normalizedIndexerName, normalizedDomainName) {
			log.Debug().
				Str("matchType", "normalized_indexer_contains_domain").
				Str("domain", domain).
				Str("normalizedDomainName", normalizedDomainName).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized indexer contains domain ***")
			return true
		}

		// 5. Check normalized matches against the specific indexer domain with TLD normalization
		if specificIndexerDomain != "" {
			normalizedSpecificDomain := strings.ToLower(specificIndexerDomain)

			// Remove TLD from indexer domain for comparison
			indexerDomainWithoutTLD := normalizedSpecificDomain
			for _, suffix := range []string{".cc", ".org", ".net", ".com", ".to", ".me", ".tv", ".xyz"} {
				if strings.HasSuffix(indexerDomainWithoutTLD, suffix) {
					indexerDomainWithoutTLD = strings.TrimSuffix(indexerDomainWithoutTLD, suffix)
					break
				}
			}

			// Normalize indexer domain name
			normalizedIndexerDomainName := s.normalizeDomainName(indexerDomainWithoutTLD)

			// Compare normalized torrent domain with normalized indexer domain
			if normalizedDomainName == normalizedIndexerDomainName {
				log.Debug().
					Str("matchType", "normalized_specific_indexer_domain_match").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("normalizedTorrentDomain", normalizedDomainName).
					Str("normalizedIndexerDomain", normalizedIndexerDomainName).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized specific indexer domain match ***")
				return true
			}

			// Partial matches with normalized indexer domains
			if strings.Contains(normalizedDomainName, normalizedIndexerDomainName) {
				log.Debug().
					Str("matchType", "normalized_torrent_domain_contains_specific_indexer").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("normalizedTorrentDomain", normalizedDomainName).
					Str("normalizedIndexerDomain", normalizedIndexerDomainName).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized torrent domain contains specific indexer domain ***")
				return true
			}
			if strings.Contains(normalizedIndexerDomainName, normalizedDomainName) {
				log.Debug().
					Str("matchType", "normalized_specific_indexer_domain_contains_torrent").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("normalizedTorrentDomain", normalizedDomainName).
					Str("normalizedIndexerDomain", normalizedIndexerDomainName).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - Normalized specific indexer domain contains torrent domain ***")
				return true
			}

			// Check TLD-stripped match against specific indexer domain
			if domainWithoutTLD == indexerDomainWithoutTLD {
				log.Debug().
					Str("matchType", "tld_stripped_specific_indexer_domain").
					Str("torrentDomain", domain).
					Str("indexerDomain", specificIndexerDomain).
					Str("torrentDomainWithoutTLD", domainWithoutTLD).
					Str("indexerDomainWithoutTLD", indexerDomainWithoutTLD).
					Str("indexerName", indexerName).
					Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - TLD stripped specific indexer domain match ***")
				return true
			}
		} // Check original TLD-stripped match for backward compatibility
		if normalizedIndexerName == domainWithoutTLD {
			log.Debug().
				Str("matchType", "tld_stripped").
				Str("domain", domain).
				Str("domainWithoutTLD", domainWithoutTLD).
				Str("indexerName", indexerName).
				Msg("[CROSSSEED-DOMAIN] *** MATCH FOUND - TLD stripped match ***")
			return true
		}
	}

	return false
}

// getCachedIndexerDomain returns a cached specific domain for the given indexer when available.
func (s *Service) getCachedIndexerDomain(indexerName string) string {
	if s.jackettService == nil || indexerName == "" {
		return ""
	}

	if s.indexerDomainCache != nil {
		if cached, ok := s.indexerDomainCache.Get(indexerName); ok {
			return cached
		}
	}

	domain, err := s.jackettService.GetIndexerDomain(context.Background(), indexerName)
	if err != nil || domain == "" {
		return ""
	}

	if s.indexerDomainCache != nil {
		s.indexerDomainCache.Set(indexerName, domain, indexerDomainCacheTTL)
	}

	return domain
}

// normalizeIndexerName normalizes indexer names for comparison
func (s *Service) normalizeIndexerName(indexerName string) string {
	normalized := strings.ToLower(strings.TrimSpace(indexerName))

	// Remove common suffixes from indexer names
	suffixes := []string{
		" (api)", "(api)",
		" api", "api",
		" (prowlarr)", "(prowlarr)",
		" prowlarr", "prowlarr",
		" (jackett)", "(jackett)",
		" jackett", "jackett",
	}

	for _, suffix := range suffixes {
		if strings.HasSuffix(normalized, suffix) {
			normalized = strings.TrimSuffix(normalized, suffix)
			normalized = strings.TrimSpace(normalized)
			break
		}
	}

	return s.normalizeDomainName(normalized)
}

// normalizeDomainName normalizes domain names for comparison by removing common separators
func (s *Service) normalizeDomainName(domainName string) string {
	// Remove hyphens, underscores, dots (except TLD), and spaces
	normalized := strings.ReplaceAll(domainName, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, ".", "")
	normalized = strings.ReplaceAll(normalized, " ", "")

	return normalized
}

// extractTrackerDomainsFromTorrent extracts unique tracker domains from a torrent
func (s *Service) extractTrackerDomainsFromTorrent(torrent qbt.Torrent) []string {
	domains := make(map[string]struct{})

	// Add primary tracker domain
	if torrent.Tracker != "" && s.syncManager != nil {
		if domain := s.syncManager.ExtractDomainFromURL(torrent.Tracker); domain != "" && domain != "Unknown" {
			domains[domain] = struct{}{}
		}
	}

	// Add domains from all trackers
	for _, tracker := range torrent.Trackers {
		if tracker.Url != "" && s.syncManager != nil {
			if domain := s.syncManager.ExtractDomainFromURL(tracker.Url); domain != "" && domain != "Unknown" {
				domains[domain] = struct{}{}
			}
		}
	}

	// Convert to slice
	var result []string
	for domain := range domains {
		result = append(result, domain)
	}

	return result
}

func (s *Service) appendSearchResult(state *searchRunState, result models.CrossSeedSearchResult) {
	s.searchMu.Lock()
	state.run.Results = append(state.run.Results, result)
	if s.searchState == state {
		state.recentResults = append(state.recentResults, result)
		if len(state.recentResults) > 10 {
			state.recentResults = state.recentResults[len(state.recentResults)-10:]
		}
	}
	s.searchMu.Unlock()
}

func (s *Service) persistSearchRun(state *searchRunState) {
	updated, err := s.automationStore.UpdateSearchRun(context.Background(), state.run)
	if err != nil {
		log.Debug().Err(err).Msg("failed to persist search run progress")
		return
	}
	s.searchMu.Lock()
	if s.searchState == state {
		state.run = updated
	}
	s.searchMu.Unlock()
}

func (s *Service) setCurrentCandidate(state *searchRunState, torrent *qbt.Torrent) {
	status := &SearchCandidateStatus{
		TorrentHash: torrent.Hash,
		TorrentName: torrent.Name,
		Category:    torrent.Category,
		Tags:        splitTags(torrent.Tags),
	}
	s.searchMu.Lock()
	if s.searchState == state {
		state.currentCandidate = status
	}
	s.searchMu.Unlock()
}

func (s *Service) setNextWake(state *searchRunState, next time.Time) {
	s.searchMu.Lock()
	if s.searchState == state {
		state.nextWake = next
	}
	s.searchMu.Unlock()
}

func matchesSearchFilters(torrent *qbt.Torrent, opts SearchRunOptions) bool {
	if torrent == nil {
		return false
	}
	if len(opts.Categories) > 0 {
		matched := slices.Contains(opts.Categories, torrent.Category)
		if !matched {
			return false
		}
	}
	if len(opts.Tags) > 0 {
		torrentTags := splitTags(torrent.Tags)
		matched := false
		for _, tag := range torrentTags {
			for _, desired := range opts.Tags {
				if strings.EqualFold(tag, desired) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func normalizeStringSlice(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func uniquePositiveInts(values []int) []int {
	seen := make(map[int]struct{})
	result := make([]int, 0, len(values))
	for _, v := range values {
		if v <= 0 {
			continue
		}
		if _, exists := seen[v]; exists {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
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
				log.Debug().
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
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("state", string(torrent.State)).
			Float64("progress", torrent.Progress).
			Dur("elapsed", time.Since(startTime)).
			Msg("Torrent recheck completed")

		return torrent
	}
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

// CheckWebhook checks if a release announced by autobrr can be cross-seeded with existing torrents.
// This endpoint is designed for autobrr webhook integration where autobrr sends parsed release metadata
// and we check if any existing torrents across our instances match, indicating a cross-seed opportunity.
func (s *Service) CheckWebhook(ctx context.Context, req *WebhookCheckRequest) (*WebhookCheckResponse, error) {
	if req.TorrentName == "" {
		return nil, fmt.Errorf("%w: torrentName is required", ErrInvalidWebhookRequest)
	}
	if req.InstanceID <= 0 {
		return nil, fmt.Errorf("%w: instanceId is required and must be a positive integer", ErrInvalidWebhookRequest)
	}

	// Parse the incoming release using rls - this extracts all metadata from the torrent name
	incomingRelease := s.releaseCache.Parse(req.TorrentName)

	// Get automation settings for sizeMismatchTolerancePercent
	// Note: We always use strict matching (findIndividualEpisodes=false) for webhook checks
	// because season packs and individual episodes have incompatible file structures.
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load automation settings for webhook check, using defaults")
		settings = &models.CrossSeedAutomationSettings{
			SizeMismatchTolerancePercent: 5.0,
		}
	}

	// Get the target instance
	instance, err := s.instanceStore.Get(ctx, req.InstanceID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			return nil, fmt.Errorf("%w: instance %d not found", ErrWebhookInstanceNotFound, req.InstanceID)
		}
		return nil, fmt.Errorf("failed to get instance %d: %w", req.InstanceID, err)
	}
	instances := []*models.Instance{instance}

	var matches []WebhookCheckMatch

	// Search each instance for matching torrents
	for _, instance := range instances {
		// Get all torrents from this instance using cached sync data
		torrentsView, err := s.syncManager.GetCachedInstanceTorrents(ctx, instance.ID)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instance.ID).Msg("Failed to get torrents from instance")
			continue
		}

		// Convert CrossInstanceTorrentView to qbt.Torrent for matching
		torrents := make([]qbt.Torrent, len(torrentsView))
		for i, tv := range torrentsView {
			torrents[i] = tv.Torrent
		}

		// Check each torrent for a match
		for _, torrent := range torrents {
			// Parse the existing torrent's release info
			existingRelease := s.releaseCache.Parse(torrent.Name)

			// Check if releases match using strict matching (always false for findIndividualEpisodes)
			// We use strict matching for cross-seed validation because season packs and individual
			// episodes have different file structures and cannot be cross-seeded, even if the
			// content is related. qBittorrent would have to recheck/redownload everything.
			if !s.releasesMatch(incomingRelease, existingRelease, false) {
				continue
			}

			// Determine match type
			matchType := "metadata"
			var sizeDiff float64

			if req.Size > 0 && torrent.Size > 0 {
				// Calculate size difference percentage
				if torrent.Size > 0 {
					diff := math.Abs(float64(req.Size) - float64(torrent.Size))
					sizeDiff = (diff / float64(torrent.Size)) * 100.0
				}

				// Check if size is within tolerance
				if s.isSizeWithinTolerance(int64(req.Size), torrent.Size, settings.SizeMismatchTolerancePercent) {
					if sizeDiff < 0.1 {
						matchType = "exact"
					} else {
						matchType = "size"
					}
				} else {
					// Size is outside tolerance, skip this match
					log.Debug().
						Str("incomingName", req.TorrentName).
						Str("existingName", torrent.Name).
						Uint64("incomingSize", req.Size).
						Int64("existingSize", torrent.Size).
						Float64("sizeDiff", sizeDiff).
						Float64("tolerance", settings.SizeMismatchTolerancePercent).
						Msg("Skipping match due to size mismatch")
					continue
				}
			}

			matches = append(matches, WebhookCheckMatch{
				InstanceID:   instance.ID,
				InstanceName: instance.Name,
				TorrentHash:  torrent.Hash,
				TorrentName:  torrent.Name,
				MatchType:    matchType,
				SizeDiff:     sizeDiff,
			})
		}
	}

	// Build response
	canCrossSeed := len(matches) > 0
	recommendation := "skip"
	if canCrossSeed {
		recommendation = "download"
	}

	return &WebhookCheckResponse{
		CanCrossSeed:   canCrossSeed,
		Matches:        matches,
		Recommendation: recommendation,
	}, nil
}
