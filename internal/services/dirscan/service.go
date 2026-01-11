// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package dirscan provides directory scanning functionality to find media files
// and match them against Torznab indexers for cross-seeding.
package dirscan

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/moistari/rls"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/jackett"
)

// Config holds configuration for the directory scanner service.
type Config struct {
	// SchedulerInterval is how often to check for scheduled scans.
	SchedulerInterval time.Duration

	// MaxJitter is the maximum random delay before starting a scheduled scan.
	MaxJitter time.Duration
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		SchedulerInterval: 1 * time.Minute,
		MaxJitter:         30 * time.Second,
	}
}

// Service handles directory scanning and torrent matching.
type Service struct {
	cfg            Config
	store          *models.DirScanStore
	instanceStore  *models.InstanceStore
	syncManager    *qbittorrent.SyncManager
	jackettService *jackett.Service
	arrService     *arr.Service // ARR service for external ID lookup (optional)

	// Components for search/match/inject
	parser   *Parser
	searcher *Searcher
	injector *Injector

	// Per-directory mutex to prevent overlapping scans.
	directoryMu map[int]*sync.Mutex
	mu          sync.Mutex // protects directoryMu map

	// In-memory cancel handles keyed by runID.
	cancelFuncs map[int64]context.CancelFunc
	cancelMu    sync.Mutex

	// Scheduler control
	schedulerCtx    context.Context
	schedulerCancel context.CancelFunc
	schedulerWg     sync.WaitGroup
}

// NewService creates a new directory scanner service.
func NewService(
	cfg Config,
	store *models.DirScanStore,
	instanceStore *models.InstanceStore,
	syncManager *qbittorrent.SyncManager,
	jackettService *jackett.Service,
	arrService *arr.Service, // optional, for external ID lookup
) *Service {
	if cfg.SchedulerInterval <= 0 {
		cfg.SchedulerInterval = DefaultConfig().SchedulerInterval
	}
	if cfg.MaxJitter <= 0 {
		cfg.MaxJitter = DefaultConfig().MaxJitter
	}

	// Initialize components
	parser := NewParser(nil) // nil uses default normalizer
	searcher := NewSearcher(jackettService, parser)
	injector := NewInjector(jackettService, syncManager, syncManager)

	return &Service{
		cfg:            cfg,
		store:          store,
		instanceStore:  instanceStore,
		syncManager:    syncManager,
		jackettService: jackettService,
		arrService:     arrService,
		parser:         parser,
		searcher:       searcher,
		injector:       injector,
		directoryMu:    make(map[int]*sync.Mutex),
		cancelFuncs:    make(map[int64]context.CancelFunc),
	}
}

// Start starts the scheduler loop.
func (s *Service) Start(ctx context.Context) error {
	s.schedulerCtx, s.schedulerCancel = context.WithCancel(ctx)
	s.schedulerWg.Add(1)
	go s.runScheduler()
	log.Info().Msg("dirscan: scheduler started")
	return nil
}

// Stop stops the scheduler and waits for completion.
func (s *Service) Stop() {
	if s.schedulerCancel != nil {
		s.schedulerCancel()
	}
	s.schedulerWg.Wait()
	log.Info().Msg("dirscan: scheduler stopped")
}

// runScheduler periodically checks for directories due for scanning.
func (s *Service) runScheduler() {
	defer s.schedulerWg.Done()

	ticker := time.NewTicker(s.cfg.SchedulerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.schedulerCtx.Done():
			return
		case <-ticker.C:
			s.checkScheduledScans()
		}
	}
}

// checkScheduledScans checks all enabled directories and triggers scans if due.
func (s *Service) checkScheduledScans() {
	ctx := s.schedulerCtx

	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("dirscan: failed to get settings")
		return
	}
	if settings == nil || !settings.Enabled {
		return
	}

	directories, err := s.store.ListEnabledDirectories(ctx)
	if err != nil {
		log.Error().Err(err).Msg("dirscan: failed to list enabled directories")
		return
	}

	for _, dir := range directories {
		if s.isDueForScan(dir) {
			go s.triggerScheduledScan(dir.ID)
		}
	}
}

// isDueForScan checks if a directory is due for scheduled scanning.
func (s *Service) isDueForScan(dir *models.DirScanDirectory) bool {
	if dir.LastScanAt == nil {
		return true
	}

	nextScan := dir.LastScanAt.Add(time.Duration(dir.ScanIntervalMinutes) * time.Minute)
	return time.Now().After(nextScan)
}

// triggerScheduledScan triggers a scan for a directory if no active scan exists.
func (s *Service) triggerScheduledScan(directoryID int) {
	ctx := s.schedulerCtx

	// Try to create a run; if one is already active, this will fail gracefully
	runID, err := s.store.CreateRunIfNoActive(ctx, directoryID, "scheduled")
	if err != nil {
		// ErrDirScanRunAlreadyActive is expected if a scan is in progress
		if !errors.Is(err, models.ErrDirScanRunAlreadyActive) {
			log.Error().Err(err).Int("directoryID", directoryID).Msg("dirscan: failed to create scheduled run")
		}
		return
	}

	s.executeScan(ctx, directoryID, runID)
}

// StartManualScan starts a manual scan for a directory.
func (s *Service) StartManualScan(ctx context.Context, directoryID int) (int64, error) {
	runID, err := s.store.CreateRunIfNoActive(ctx, directoryID, "manual")
	if err != nil {
		return 0, fmt.Errorf("create run: %w", err)
	}

	// Create cancellable context for this run.
	// Use Background() as parent so the scan survives after the HTTP request completes.
	runCtx, cancel := context.WithCancel(context.Background())
	s.cancelMu.Lock()
	s.cancelFuncs[runID] = cancel
	s.cancelMu.Unlock()

	go func() {
		defer func() {
			s.cancelMu.Lock()
			delete(s.cancelFuncs, runID)
			s.cancelMu.Unlock()
		}()
		s.executeScan(runCtx, directoryID, runID)
	}()

	return runID, nil
}

// CancelScan cancels an active scan.
func (s *Service) CancelScan(ctx context.Context, directoryID int) error {
	run, err := s.store.GetActiveRun(ctx, directoryID)
	if err != nil {
		return fmt.Errorf("get active run: %w", err)
	}
	if run == nil {
		return nil // No active run
	}

	s.cancelMu.Lock()
	cancel, ok := s.cancelFuncs[run.ID]
	s.cancelMu.Unlock()

	if ok {
		cancel()
	}

	if err := s.store.UpdateRunCanceled(ctx, run.ID); err != nil {
		return fmt.Errorf("update run canceled: %w", err)
	}
	return nil
}

// GetStatus returns the status of a directory's current or most recent scan.
func (s *Service) GetStatus(ctx context.Context, directoryID int) (*models.DirScanRun, error) {
	// First check for active run
	run, err := s.store.GetActiveRun(ctx, directoryID)
	if err != nil {
		return nil, fmt.Errorf("get active run: %w", err)
	}
	if run != nil {
		return run, nil
	}

	// Get most recent run
	runs, err := s.store.ListRuns(ctx, directoryID, 1)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	if len(runs) > 0 {
		return runs[0], nil
	}

	return nil, nil
}

// executeScan performs the actual directory scan.
func (s *Service) executeScan(ctx context.Context, directoryID int, runID int64) {
	// Acquire per-directory mutex
	dirMu := s.getDirectoryMutex(directoryID)
	dirMu.Lock()
	defer dirMu.Unlock()

	l := log.With().
		Int("directoryID", directoryID).
		Int64("runID", runID).
		Logger()

	// Update last scan timestamp
	if err := s.store.UpdateDirectoryLastScan(ctx, directoryID); err != nil {
		l.Error().Err(err).Msg("dirscan: failed to update last scan timestamp")
	}

	// Check for cancellation
	if s.handleCancellation(ctx, runID, &l, "before start") {
		return
	}

	// Validate and get directory configuration
	dir, ok := s.validateDirectory(ctx, directoryID, runID, &l)
	if !ok {
		return
	}

	l.Info().Str("path", dir.Path).Msg("dirscan: starting scan")

	// Phase 1: Scanning - Walk directory and collect files
	scanResult, ok := s.runScanPhase(ctx, dir.Path, runID, &l)
	if !ok {
		return
	}

	// Check for cancellation before search phase
	if s.handleCancellation(ctx, runID, &l, "before search phase") {
		return
	}

	// Get settings for matching configuration
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		l.Error().Err(err).Msg("dirscan: failed to get settings")
		s.markRunFailed(ctx, runID, fmt.Sprintf("get settings: %v", err), &l)
		return
	}
	if settings == nil {
		settings = &models.DirScanSettings{
			MatchMode:            models.MatchModeStrict,
			SizeTolerancePercent: 2.0,
		}
	}

	// Create matcher with settings
	matchMode := MatchModeStrict
	if settings.MatchMode == models.MatchModeFlexible {
		matchMode = MatchModeFlexible
	}
	matcher := NewMatcher(matchMode, settings.SizeTolerancePercent)

	// Phase 2 & 3: Search, match, and inject
	matchesFound, torrentsAdded := s.runSearchAndInjectPhase(ctx, dir, scanResult, settings, matcher, runID, &l)

	// Mark run as completed
	if err := s.store.UpdateRunCompleted(ctx, runID, matchesFound, torrentsAdded); err != nil {
		l.Error().Err(err).Msg("dirscan: failed to mark run as completed")
		return
	}

	l.Info().
		Int("matchesFound", matchesFound).
		Int("torrentsAdded", torrentsAdded).
		Msg("dirscan: scan completed")
}

// handleCancellation checks for context cancellation and updates the run status.
// Returns true if the scan was canceled and should stop.
func (s *Service) handleCancellation(ctx context.Context, runID int64, l *zerolog.Logger, phase string) bool {
	if ctx.Err() == nil {
		return false
	}
	l.Info().Msgf("dirscan: scan canceled %s", phase)
	// Use background context since the run context is canceled
	if err := s.store.UpdateRunCanceled(context.Background(), runID); err != nil {
		l.Error().Err(err).Msg("dirscan: failed to mark run as canceled")
	}
	return true
}

// validateDirectory validates the directory configuration and target instance.
// Returns the directory and true if valid, or nil and false if invalid.
func (s *Service) validateDirectory(ctx context.Context, directoryID int, runID int64, l *zerolog.Logger) (*models.DirScanDirectory, bool) {
	dir, err := s.store.GetDirectory(ctx, directoryID)
	if err != nil {
		l.Error().Err(err).Msg("dirscan: failed to get directory")
		s.markRunFailed(ctx, runID, err.Error(), l)
		return nil, false
	}

	instance, err := s.instanceStore.Get(ctx, dir.TargetInstanceID)
	if err != nil {
		l.Error().Err(err).Msg("dirscan: failed to get target instance")
		s.markRunFailed(ctx, runID, fmt.Sprintf("failed to get target instance: %v", err), l)
		return nil, false
	}

	if !instance.HasLocalFilesystemAccess {
		errMsg := "target instance does not have local filesystem access"
		l.Error().Msg("dirscan: " + errMsg)
		s.markRunFailed(ctx, runID, errMsg, l)
		return nil, false
	}

	return dir, true
}

// runScanPhase executes the directory scanning phase.
// Returns the scan result and true if successful, or nil and false on failure.
func (s *Service) runScanPhase(ctx context.Context, path string, runID int64, l *zerolog.Logger) (*ScanResult, bool) {
	scanner := NewScanner()

	// TODO: Build FileID index from qBittorrent torrents for already-seeding detection
	// This will be implemented when we have the SyncManager integration

	scanResult, err := scanner.ScanDirectory(ctx, path)
	if err != nil {
		l.Error().Err(err).Msg("dirscan: failed to scan directory")
		s.markRunFailed(ctx, runID, fmt.Sprintf("scan failed: %v", err), l)
		return nil, false
	}

	// Update status to searching
	if err := s.store.UpdateRunStatus(ctx, runID, models.DirScanRunStatusSearching); err != nil {
		l.Error().Err(err).Msg("dirscan: failed to update run status")
	}

	// Update run stats with scan results
	if err := s.store.UpdateRunStats(ctx, runID, scanResult.TotalFiles, scanResult.SkippedFiles, 0, 0); err != nil {
		l.Error().Err(err).Msg("dirscan: failed to update run stats")
	}

	l.Info().
		Int("searchees", len(scanResult.Searchees)).
		Int("totalFiles", scanResult.TotalFiles).
		Int64("totalSize", scanResult.TotalSize).
		Msg("dirscan: scan phase complete")

	return scanResult, true
}

// searcheeDelay is the minimum delay between processing each searchee.
// This prevents overloading indexers when scanning large directories.
// 60 seconds ensures we don't hit rate limits on Prowlarr/Jackett.
const searcheeDelay = 60 * time.Second

// runSearchAndInjectPhase searches indexers for each searchee and injects matches.
func (s *Service) runSearchAndInjectPhase(
	ctx context.Context,
	dir *models.DirScanDirectory,
	scanResult *ScanResult,
	settings *models.DirScanSettings,
	matcher *Matcher,
	runID int64,
	l *zerolog.Logger,
) (matchesFound, torrentsAdded int) {
	// Update status to searching
	if err := s.store.UpdateRunStatus(ctx, runID, models.DirScanRunStatusSearching); err != nil {
		l.Error().Err(err).Msg("dirscan: failed to update run status to searching")
	}

	for i, searchee := range scanResult.Searchees {
		// Check for cancellation
		if ctx.Err() != nil {
			l.Info().Msg("dirscan: search phase canceled")
			return matchesFound, torrentsAdded
		}

		// Add delay between searchees to avoid overloading indexers
		if i > 0 {
			select {
			case <-ctx.Done():
				l.Info().Msg("dirscan: search phase canceled during delay")
				return matchesFound, torrentsAdded
			case <-time.After(searcheeDelay):
			}
		}

		match := s.processSearchee(ctx, dir, searchee, settings, matcher, l)
		if match != nil {
			matchesFound++
			if match.injected {
				torrentsAdded++
			}
		}
	}

	return matchesFound, torrentsAdded
}

// searcheeMatch holds the result of processing a searchee.
type searcheeMatch struct {
	searchee      *Searchee
	torrentData   []byte
	parsedTorrent *ParsedTorrent
	matchResult   *MatchResult
	injected      bool
}

// processSearchee searches for and processes a single searchee.
func (s *Service) processSearchee(
	ctx context.Context,
	dir *models.DirScanDirectory,
	searchee *Searchee,
	settings *models.DirScanSettings,
	matcher *Matcher,
	l *zerolog.Logger,
) *searcheeMatch {
	searcheeSize := CalculateTotalSize(searchee)
	minSize, maxSize := CalculateSizeRange(searcheeSize, settings.SizeTolerancePercent)

	// Parse metadata and determine content type
	meta := s.parser.Parse(searchee.Name)
	contentInfo := determineContentType(meta)

	l.Debug().
		Str("name", searchee.Name).
		Int64("size", searcheeSize).
		Int("files", len(searchee.Files)).
		Str("contentType", contentInfo.contentType).
		Msg("dirscan: searching for searchee")

	// Filter indexers by capability (like cross-seed does)
	var filteredIndexers []int
	if len(contentInfo.requiredCaps) > 0 && s.jackettService != nil {
		var err error
		filteredIndexers, err = s.jackettService.FilterIndexersForCapabilities(
			ctx, nil, contentInfo.requiredCaps, contentInfo.categories,
		)
		if err != nil {
			l.Debug().Err(err).Msg("dirscan: failed to filter indexers by capabilities, using all")
		} else {
			l.Debug().Int("indexers", len(filteredIndexers)).Msg("dirscan: filtered indexers by capabilities")
		}
	}

	// Lookup external IDs via arr service if not already present in TRaSH naming
	s.lookupExternalIDs(ctx, meta, contentInfo.contentType, searchee.Name, l)

	// Search indexers
	response := s.searchForSearchee(ctx, searchee, meta, filteredIndexers, l)
	if response == nil || len(response.Results) == 0 {
		return nil
	}

	l.Debug().
		Str("name", searchee.Name).
		Int("results", len(response.Results)).
		Msg("dirscan: got search results")

	// Try to match and inject
	return s.tryMatchResults(ctx, dir, searchee, response, minSize, maxSize, settings, matcher, l)
}

// searchForSearchee searches indexers and waits for results.
func (s *Service) searchForSearchee(
	ctx context.Context,
	searchee *Searchee,
	meta *SearcheeMetadata,
	indexerIDs []int,
	l *zerolog.Logger,
) *jackett.SearchResponse {
	resultsCh := make(chan *jackett.SearchResponse, 1)
	errCh := make(chan error, 1)

	searchReq := &SearchRequest{
		Searchee:   searchee,
		Metadata:   meta,       // Pass parsed metadata with external IDs
		IndexerIDs: indexerIDs, // Use capability-filtered indexers
		Limit:      50,
		OnAllComplete: func(response *jackett.SearchResponse, err error) {
			if err != nil {
				errCh <- err
				return
			}
			resultsCh <- response
		},
	}

	if err := s.searcher.Search(ctx, searchReq); err != nil {
		l.Warn().Err(err).Str("name", searchee.Name).Msg("dirscan: search failed")
		return nil
	}

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		l.Warn().Err(err).Str("name", searchee.Name).Msg("dirscan: search error")
		return nil
	case response := <-resultsCh:
		return response
	}
}

// tryMatchResults iterates through search results trying to find and inject a match.
func (s *Service) tryMatchResults(
	ctx context.Context,
	dir *models.DirScanDirectory,
	searchee *Searchee,
	response *jackett.SearchResponse,
	minSize, maxSize int64,
	settings *models.DirScanSettings,
	matcher *Matcher,
	l *zerolog.Logger,
) *searcheeMatch {
	for i := range response.Results {
		result := &response.Results[i]

		if result.Size < minSize || result.Size > maxSize {
			continue
		}

		match := s.tryMatchAndInject(ctx, dir, searchee, result, settings, matcher, l)
		if match != nil {
			return match
		}
	}
	return nil
}

// tryMatchAndInject downloads a torrent, matches files, and injects if successful.
func (s *Service) tryMatchAndInject(
	ctx context.Context,
	dir *models.DirScanDirectory,
	searchee *Searchee,
	result *jackett.SearchResult,
	settings *models.DirScanSettings,
	matcher *Matcher,
	l *zerolog.Logger,
) *searcheeMatch {
	torrentData, parsed := s.downloadAndParseTorrent(ctx, result, l)
	if parsed == nil {
		return nil
	}

	matchResult := matcher.Match(searchee, parsed.Files)
	if !matchResult.IsMatch {
		l.Debug().Str("title", result.Title).Float64("matchRatio", matchResult.MatchRatio).Msg("dirscan: no match")
		return nil
	}

	// Check if this torrent already exists in qBittorrent
	exists, err := s.injector.TorrentExists(ctx, dir.TargetInstanceID, parsed.InfoHash)
	if err != nil {
		l.Debug().Err(err).Str("hash", parsed.InfoHash).Msg("dirscan: failed to check if torrent exists")
	} else if exists {
		l.Debug().Str("name", searchee.Name).Str("hash", parsed.InfoHash).Msg("dirscan: already in qBittorrent")
		return nil
	}

	l.Info().Str("name", searchee.Name).Str("torrent", parsed.Name).Str("hash", parsed.InfoHash).Msg("dirscan: found match")

	injectReq := &InjectRequest{
		InstanceID:     dir.TargetInstanceID,
		Searchee:       searchee,
		SearchResult:   result,
		QbitPathPrefix: dir.QbitPathPrefix,
		Category:       settings.Category,
		Tags:           settings.Tags,
		StartPaused:    settings.StartPaused,
	}

	injectResult, err := s.injector.Inject(ctx, injectReq)
	injected := err == nil && injectResult.Success
	if err != nil {
		l.Warn().Err(err).Str("name", searchee.Name).Msg("dirscan: failed to inject torrent")
	} else {
		l.Info().Str("name", searchee.Name).Bool("success", injectResult.Success).Msg("dirscan: injected torrent")
	}

	return &searcheeMatch{searchee: searchee, torrentData: torrentData, parsedTorrent: parsed, matchResult: matchResult, injected: injected}
}

// downloadAndParseTorrent downloads and parses a torrent file.
func (s *Service) downloadAndParseTorrent(ctx context.Context, result *jackett.SearchResult, l *zerolog.Logger) ([]byte, *ParsedTorrent) {
	torrentData, err := s.jackettService.DownloadTorrent(ctx, jackett.TorrentDownloadRequest{
		IndexerID:   result.IndexerID,
		DownloadURL: result.DownloadURL,
		GUID:        result.GUID,
	})
	if err != nil {
		l.Debug().Err(err).Str("title", result.Title).Msg("dirscan: failed to download torrent")
		return nil, nil
	}

	parsed, err := ParseTorrentBytes(torrentData)
	if err != nil {
		l.Debug().Err(err).Str("title", result.Title).Msg("dirscan: failed to parse torrent")
		return nil, nil
	}
	return torrentData, parsed
}

// markRunFailed marks a run as failed with the given error message.
// Uses background context to ensure the status update completes even if the run context is canceled.
func (s *Service) markRunFailed(_ context.Context, runID int64, errMsg string, l *zerolog.Logger) {
	if err := s.store.UpdateRunFailed(context.Background(), runID, errMsg); err != nil {
		l.Error().Err(err).Msg("dirscan: failed to mark run as failed")
	}
}

// getDirectoryMutex returns the mutex for a directory, creating one if needed.
func (s *Service) getDirectoryMutex(directoryID int) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()

	mu, ok := s.directoryMu[directoryID]
	if !ok {
		mu = &sync.Mutex{}
		s.directoryMu[directoryID] = mu
	}
	return mu
}

// GetSettings returns the global directory scanner settings.
func (s *Service) GetSettings(ctx context.Context) (*models.DirScanSettings, error) {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}
	return settings, nil
}

// UpdateSettings updates the global directory scanner settings.
func (s *Service) UpdateSettings(ctx context.Context, settings *models.DirScanSettings) (*models.DirScanSettings, error) {
	updated, err := s.store.UpdateSettings(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("update settings: %w", err)
	}
	return updated, nil
}

// ListDirectories returns all configured scan directories.
func (s *Service) ListDirectories(ctx context.Context) ([]*models.DirScanDirectory, error) {
	dirs, err := s.store.ListDirectories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list directories: %w", err)
	}
	return dirs, nil
}

// CreateDirectory creates a new scan directory.
func (s *Service) CreateDirectory(ctx context.Context, dir *models.DirScanDirectory) (*models.DirScanDirectory, error) {
	created, err := s.store.CreateDirectory(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}
	return created, nil
}

// GetDirectory returns a scan directory by ID.
func (s *Service) GetDirectory(ctx context.Context, id int) (*models.DirScanDirectory, error) {
	dir, err := s.store.GetDirectory(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get directory: %w", err)
	}
	return dir, nil
}

// UpdateDirectory updates a scan directory.
func (s *Service) UpdateDirectory(ctx context.Context, id int, params *models.DirScanDirectoryUpdateParams) (*models.DirScanDirectory, error) {
	updated, err := s.store.UpdateDirectory(ctx, id, params)
	if err != nil {
		return nil, fmt.Errorf("update directory: %w", err)
	}
	return updated, nil
}

// DeleteDirectory deletes a scan directory.
func (s *Service) DeleteDirectory(ctx context.Context, id int) error {
	if err := s.store.DeleteDirectory(ctx, id); err != nil {
		return fmt.Errorf("delete directory: %w", err)
	}
	return nil
}

// ListRuns returns recent scan runs for a directory.
func (s *Service) ListRuns(ctx context.Context, directoryID, limit int) ([]*models.DirScanRun, error) {
	runs, err := s.store.ListRuns(ctx, directoryID, limit)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	return runs, nil
}

// ListFiles returns scanned files for a directory.
func (s *Service) ListFiles(ctx context.Context, directoryID int, status *models.DirScanFileStatus, limit, offset int) ([]*models.DirScanFile, error) {
	files, err := s.store.ListFiles(ctx, directoryID, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return files, nil
}

// Content type string constants.
const (
	contentTypeMovie   = "movie"
	contentTypeTV      = "tv"
	contentTypeMusic   = "music"
	contentTypeUnknown = "unknown"
)

// contentTypeInfo holds content type detection results.
type contentTypeInfo struct {
	contentType  string   // "movie", "tv", "music", etc.
	categories   []int    // Torznab category IDs
	requiredCaps []string // Required indexer capabilities
}

// determineContentType analyzes parsed metadata to determine content type and requirements.
// This mirrors the logic in crossseed.DetermineContentType but for dirscan metadata.
func determineContentType(meta *SearcheeMetadata) contentTypeInfo {
	info := contentTypeInfo{
		contentType:  contentTypeUnknown,
		categories:   []int{},
		requiredCaps: []string{},
	}

	if meta == nil || meta.Release == nil {
		return info
	}

	r := meta.Release
	switch r.Type {
	case rls.Movie:
		info.contentType = contentTypeMovie
		info.categories = []int{2000} // Movies
		info.requiredCaps = []string{"movie-search"}
	case rls.Episode, rls.Series:
		info.contentType = contentTypeTV
		info.categories = []int{5000} // TV
		info.requiredCaps = []string{"tv-search"}
	case rls.Music:
		info.contentType = contentTypeMusic
		info.categories = []int{3000} // Audio
		info.requiredCaps = []string{"music-search", "audio-search"}
	default:
		// If rls didn't detect type, use hints from our metadata
		switch {
		case meta.IsTV:
			info.contentType = contentTypeTV
			info.categories = []int{5000}
			info.requiredCaps = []string{"tv-search"}
		case meta.IsMovie:
			info.contentType = contentTypeMovie
			info.categories = []int{2000}
			info.requiredCaps = []string{"movie-search"}
		case meta.IsMusic:
			info.contentType = contentTypeMusic
			info.categories = []int{3000}
			info.requiredCaps = []string{"music-search", "audio-search"}
		}
	}

	return info
}

// mapContentTypeToARR maps dirscan content type to ARR content type for ID lookup.
func mapContentTypeToARR(contentType string) arr.ContentType {
	switch contentType {
	case contentTypeMovie:
		return arr.ContentTypeMovie
	case contentTypeTV:
		return arr.ContentTypeTV
	default:
		// No ARR lookup for music, books, games, adult, unknown, etc.
		return ""
	}
}

// lookupExternalIDs queries the arr service for external IDs and updates metadata.
func (s *Service) lookupExternalIDs(
	ctx context.Context,
	meta *SearcheeMetadata,
	contentType string,
	name string,
	l *zerolog.Logger,
) {
	if meta.HasExternalIDs() || s.arrService == nil {
		return
	}

	arrType := mapContentTypeToARR(contentType)
	if arrType == "" {
		return
	}

	result, err := s.arrService.LookupExternalIDs(ctx, name, arrType)
	if err != nil {
		l.Debug().Err(err).Msg("dirscan: arr ID lookup failed, continuing without IDs")
		return
	}

	if result == nil || result.IDs == nil {
		return
	}

	ids := result.IDs
	meta.SetExternalIDs(ids.IMDbID, ids.TMDbID, ids.TVDbID)
	l.Debug().
		Str("imdb", ids.IMDbID).
		Int("tmdb", ids.TMDbID).
		Int("tvdb", ids.TVDbID).
		Bool("fromCache", result.FromCache).
		Msg("dirscan: got external IDs from arr")
}
