// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package dirscan provides directory scanning functionality to find media files
// and match them against Torznab indexers for cross-seeding.
package dirscan

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/moistari/rls"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
)

// Config holds configuration for the directory scanner service.
type Config struct {
	// SchedulerInterval is how often to check for scheduled scans.
	SchedulerInterval time.Duration

	// MaxJitter is the maximum random delay before starting a scheduled scan.
	MaxJitter time.Duration

	// MaxConcurrentRuns limits how many scans can run across all directories.
	// This helps avoid stampeding indexers if multiple directories are due at once.
	MaxConcurrentRuns int
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		SchedulerInterval: 1 * time.Minute,
		MaxJitter:         30 * time.Second,
		MaxConcurrentRuns: 1,
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
	// Optional store for tracker display-name resolution (shared with cross-seed).
	trackerCustomizationStore *models.TrackerCustomizationStore

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

	// In-memory progress snapshot keyed by runID (for live UI updates).
	runProgress map[int64]*runProgress
	progressMu  sync.Mutex

	// Scheduler control
	schedulerCtx    context.Context
	schedulerCancel context.CancelFunc
	schedulerWg     sync.WaitGroup

	// Global run semaphore to cap concurrent scans.
	runSem chan struct{}
}

// NewService creates a new directory scanner service.
func NewService(
	cfg Config,
	store *models.DirScanStore,
	instanceStore *models.InstanceStore,
	syncManager *qbittorrent.SyncManager,
	jackettService *jackett.Service,
	arrService *arr.Service, // optional, for external ID lookup
	trackerCustomizationStore *models.TrackerCustomizationStore, // optional, for display-name resolution
) *Service {
	if cfg.SchedulerInterval <= 0 {
		cfg.SchedulerInterval = DefaultConfig().SchedulerInterval
	}
	if cfg.MaxJitter <= 0 {
		cfg.MaxJitter = DefaultConfig().MaxJitter
	}
	if cfg.MaxConcurrentRuns <= 0 {
		cfg.MaxConcurrentRuns = DefaultConfig().MaxConcurrentRuns
	}

	// Initialize components
	parser := NewParser(nil) // nil uses default normalizer
	searcher := NewSearcher(jackettService, parser)
	injector := NewInjector(jackettService, syncManager, syncManager, instanceStore, trackerCustomizationStore)

	return &Service{
		cfg:                       cfg,
		store:                     store,
		instanceStore:             instanceStore,
		syncManager:               syncManager,
		jackettService:            jackettService,
		arrService:                arrService,
		trackerCustomizationStore: trackerCustomizationStore,
		parser:                    parser,
		searcher:                  searcher,
		injector:                  injector,
		directoryMu:               make(map[int]*sync.Mutex),
		cancelFuncs:               make(map[int64]context.CancelFunc),
		runProgress:               make(map[int64]*runProgress),
		runSem:                    make(chan struct{}, cfg.MaxConcurrentRuns),
	}
}

// Start starts the scheduler loop.
func (s *Service) Start(ctx context.Context) error {
	if err := s.recoverStuckRuns(); err != nil {
		log.Error().Err(err).Msg("dirscan: failed to recover stuck runs")
	}

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

	if s.cfg.MaxJitter > 0 {
		jitter, err := randomDuration(s.cfg.MaxJitter)
		if err != nil {
			log.Debug().Err(err).Int("directoryID", directoryID).Msg("dirscan: failed to generate jitter")
		} else if jitter > 0 {
			timer := time.NewTimer(jitter)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}

	s.startRun(ctx, directoryID, runID)
}

func randomDuration(maxDuration time.Duration) (time.Duration, error) {
	if maxDuration <= 0 {
		return 0, nil
	}

	n, err := rand.Int(rand.Reader, big.NewInt(int64(maxDuration)))
	if err != nil {
		return 0, fmt.Errorf("generate random duration: %w", err)
	}
	return time.Duration(n.Int64()), nil
}

func (s *Service) startRun(parent context.Context, directoryID int, runID int64) {
	if s == nil || directoryID <= 0 || runID <= 0 {
		return
	}

	if parent == nil {
		parent = context.Background()
	}

	runCtx, cancel := context.WithCancel(parent)
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
}

// StartManualScan starts a manual scan for a directory.
func (s *Service) StartManualScan(ctx context.Context, directoryID int) (int64, error) {
	runID, err := s.store.CreateRunIfNoActive(ctx, directoryID, "manual")
	if err != nil {
		return 0, fmt.Errorf("create run: %w", err)
	}

	// Use Background() as parent so the scan survives after the HTTP request completes.
	s.startRun(context.Background(), directoryID, runID)

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

	if err := s.store.UpdateRunCanceled(context.Background(), run.ID); err != nil {
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
		if progress, ok := s.getRunProgress(run.ID); ok {
			runCopy := *run
			runCopy.MatchesFound = progress.matchesFound
			runCopy.TorrentsAdded = progress.torrentsAdded
			return &runCopy, nil
		}
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
	defer s.clearRunProgress(runID)

	l := log.With().
		Int("directoryID", directoryID).
		Int64("runID", runID).
		Logger()

	if !s.acquireRunSlot(ctx, runID, &l) {
		s.markRunCanceled(context.Background(), runID, &l, "while waiting for run slot")
		return
	}
	defer s.releaseRunSlot()

	// Acquire per-directory mutex
	dirMu := s.getDirectoryMutex(directoryID)
	dirMu.Lock()
	defer dirMu.Unlock()

	// Transition from queued to scanning once we have a run slot and hold the directory lock.
	if err := s.store.UpdateRunStatus(ctx, runID, models.DirScanRunStatusScanning); err != nil {
		l.Debug().Err(err).Msg("dirscan: failed to update run status to scanning")
	}

	s.updateDirectoryLastScan(ctx, directoryID, &l)

	if s.handleCancellation(ctx, runID, &l, "before start") {
		return
	}

	dir, ok := s.validateDirectory(ctx, directoryID, runID, &l)
	if !ok {
		return
	}

	l.Info().Str("path", dir.Path).Msg("dirscan: starting scan")

	scanResult, fileIDIndex, ok := s.runScanPhase(ctx, dir, runID, &l)
	if !ok {
		return
	}

	if s.handleCancellation(ctx, runID, &l, "before search phase") {
		return
	}

	settings, matcher, ok := s.loadSettingsAndMatcher(ctx, runID, &l)
	if !ok {
		return
	}

	matchesFound, torrentsAdded := s.runSearchAndInjectPhase(ctx, dir, scanResult, fileIDIndex, settings, matcher, runID, &l)
	s.finalizeRun(ctx, runID, scanResult, matchesFound, torrentsAdded, &l)
}

func (s *Service) updateDirectoryLastScan(ctx context.Context, directoryID int, l *zerolog.Logger) {
	if s == nil || s.store == nil || directoryID <= 0 {
		return
	}
	if err := s.store.UpdateDirectoryLastScan(ctx, directoryID); err != nil && l != nil {
		l.Error().Err(err).Msg("dirscan: failed to update last scan timestamp")
	}
}

func (s *Service) markRunCanceled(ctx context.Context, runID int64, l *zerolog.Logger, reason string) {
	if s == nil || s.store == nil || runID <= 0 {
		return
	}
	if err := s.store.UpdateRunCanceled(ctx, runID); err != nil && l != nil {
		l.Debug().Err(err).Str("reason", reason).Msg("dirscan: failed to mark run canceled")
	}
}

func (s *Service) loadSettingsAndMatcher(ctx context.Context, runID int64, l *zerolog.Logger) (*models.DirScanSettings, *Matcher, bool) {
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		if l != nil {
			l.Error().Err(err).Msg("dirscan: failed to get settings")
		}
		s.markRunFailed(ctx, runID, fmt.Sprintf("get settings: %v", err), l)
		return nil, nil, false
	}
	if settings == nil {
		settings = &models.DirScanSettings{
			MatchMode:            models.MatchModeStrict,
			SizeTolerancePercent: 2.0,
		}
	}

	matcher := NewMatcher(matchModeFromSettings(settings), settings.SizeTolerancePercent)
	return settings, matcher, true
}

func matchModeFromSettings(settings *models.DirScanSettings) MatchMode {
	if settings != nil && settings.MatchMode == models.MatchModeFlexible {
		return MatchModeFlexible
	}
	return MatchModeStrict
}

func (s *Service) finalizeRun(ctx context.Context, runID int64, scanResult *ScanResult, matchesFound, torrentsAdded int, l *zerolog.Logger) {
	if s == nil || s.store == nil || scanResult == nil || runID <= 0 {
		return
	}

	if ctx.Err() != nil {
		filesFound := scanResult.TotalFiles + scanResult.SkippedFiles
		if err := s.store.UpdateRunStats(context.Background(), runID, filesFound, scanResult.SkippedFiles, matchesFound, torrentsAdded); err != nil && l != nil {
			l.Debug().Err(err).Msg("dirscan: failed to persist run stats before cancel")
		}

		if l != nil {
			l.Info().Msg("dirscan: scan canceled during search/inject")
		}
		s.markRunCanceled(context.Background(), runID, l, "canceled during search/inject")
		return
	}

	if err := s.store.UpdateRunCompleted(context.Background(), runID, matchesFound, torrentsAdded); err != nil {
		if l != nil {
			l.Error().Err(err).Msg("dirscan: failed to mark run as completed")
		}
		return
	}

	if l != nil {
		l.Info().
			Int("matchesFound", matchesFound).
			Int("torrentsAdded", torrentsAdded).
			Msg("dirscan: scan completed")
	}
}

func (s *Service) acquireRunSlot(ctx context.Context, runID int64, l *zerolog.Logger) bool {
	if s == nil || s.runSem == nil {
		return true
	}

	select {
	case s.runSem <- struct{}{}:
		return true
	case <-ctx.Done():
		if l != nil {
			l.Debug().Int64("runID", runID).Msg("dirscan: canceled while waiting for run slot")
		}
		return false
	}
}

func (s *Service) releaseRunSlot() {
	if s == nil || s.runSem == nil {
		return
	}
	select {
	case <-s.runSem:
	default:
	}
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
func (s *Service) runScanPhase(ctx context.Context, dir *models.DirScanDirectory, runID int64, l *zerolog.Logger) (*ScanResult, map[string]string, bool) {
	scanner := NewScanner()

	// Build FileID index from qBittorrent torrents for already-seeding detection.
	// This is best-effort; if it fails, scanning continues without seeding skips.
	fileIDIndex := make(map[string]string)
	if s.syncManager != nil {
		if index, err := s.buildFileIDIndex(ctx, dir.TargetInstanceID, l); err != nil {
			l.Debug().Err(err).Msg("dirscan: failed to build FileID index, continuing without seeding detection")
		} else if len(index) > 0 {
			fileIDIndex = index
			scanner.SetFileIDIndex(index)
		}
	}

	scanResult, err := scanner.ScanDirectory(ctx, dir.Path)
	if err != nil {
		l.Error().Err(err).Msg("dirscan: failed to scan directory")
		s.markRunFailed(ctx, runID, fmt.Sprintf("scan failed: %v", err), l)
		return nil, nil, false
	}

	// Update status to searching
	if err := s.store.UpdateRunStatus(ctx, runID, models.DirScanRunStatusSearching); err != nil {
		l.Error().Err(err).Msg("dirscan: failed to update run status")
	}

	// Update run stats with scan results
	filesFound := scanResult.TotalFiles + scanResult.SkippedFiles
	if err := s.store.UpdateRunStats(ctx, runID, filesFound, scanResult.SkippedFiles, 0, 0); err != nil {
		l.Error().Err(err).Msg("dirscan: failed to update run stats")
	}

	l.Info().
		Int("searchees", len(scanResult.Searchees)).
		Int("filesFound", filesFound).
		Int("filesEligible", scanResult.TotalFiles).
		Int("filesSkipped", scanResult.SkippedFiles).
		Int64("totalSize", scanResult.TotalSize).
		Msg("dirscan: scan phase complete")

	return scanResult, fileIDIndex, true
}

// runSearchAndInjectPhase searches indexers for each searchee and injects matches.
func (s *Service) runSearchAndInjectPhase(
	ctx context.Context,
	dir *models.DirScanDirectory,
	scanResult *ScanResult,
	fileIDIndex map[string]string,
	settings *models.DirScanSettings,
	matcher *Matcher,
	runID int64,
	l *zerolog.Logger,
) (matchesFound, torrentsAdded int) {
	injectedTVGroups := make(map[tvGroupKey]struct{})

	for _, searchee := range scanResult.Searchees {
		workItems := buildSearcheeWorkItems(searchee, s.parser)
		var canceled bool
		matchesFound, torrentsAdded, canceled = s.processWorkItems(
			ctx,
			dir,
			workItems,
			fileIDIndex,
			injectedTVGroups,
			settings,
			matcher,
			runID,
			matchesFound,
			torrentsAdded,
			l,
		)
		if canceled {
			if l != nil {
				l.Info().Msg("dirscan: search phase canceled")
			}
			return matchesFound, torrentsAdded
		}
	}

	return matchesFound, torrentsAdded
}

func (s *Service) processWorkItems(
	ctx context.Context,
	dir *models.DirScanDirectory,
	items []searcheeWorkItem,
	fileIDIndex map[string]string,
	injectedTVGroups map[tvGroupKey]struct{},
	settings *models.DirScanSettings,
	matcher *Matcher,
	runID int64,
	matchesFound int,
	torrentsAdded int,
	l *zerolog.Logger,
) (matchesFoundUpdated, torrentsAddedUpdated int, canceled bool) {
	matchesFoundUpdated = matchesFound
	torrentsAddedUpdated = torrentsAdded

	for _, item := range items {
		if ctx.Err() != nil {
			return matchesFoundUpdated, torrentsAddedUpdated, true
		}
		if shouldSkipWorkItem(item, fileIDIndex, injectedTVGroups, l) {
			continue
		}

		match := s.processSearchee(ctx, dir, item.searchee, settings, matcher, runID, l)
		if match == nil {
			continue
		}

		matchesFoundUpdated++
		if match.injected {
			torrentsAddedUpdated++
			markTVGroupInjected(injectedTVGroups, item.tvGroup)
		}

		s.setRunProgress(runID, matchesFoundUpdated, torrentsAddedUpdated)
	}

	return matchesFoundUpdated, torrentsAddedUpdated, false
}

func shouldSkipWorkItem(
	item searcheeWorkItem,
	fileIDIndex map[string]string,
	injectedTVGroups map[tvGroupKey]struct{},
	l *zerolog.Logger,
) bool {
	if item.searchee == nil {
		return true
	}
	if isAlreadySeedingByFileID(item.searchee, fileIDIndex) {
		if l != nil {
			l.Debug().Str("name", item.searchee.Name).Msg("dirscan: skipping searchee already seeding (FileID)")
		}
		return true
	}
	if item.tvGroup == nil {
		return false
	}
	_, alreadyInjected := injectedTVGroups[*item.tvGroup]
	return alreadyInjected
}

func markTVGroupInjected(injectedTVGroups map[tvGroupKey]struct{}, key *tvGroupKey) {
	if key == nil {
		return
	}
	injectedTVGroups[*key] = struct{}{}
}

type runProgress struct {
	matchesFound  int
	torrentsAdded int
	updatedAt     time.Time
}

func (s *Service) setRunProgress(runID int64, matchesFound, torrentsAdded int) {
	if s == nil || runID <= 0 {
		return
	}

	s.progressMu.Lock()
	defer s.progressMu.Unlock()

	entry, ok := s.runProgress[runID]
	if !ok {
		entry = &runProgress{}
		s.runProgress[runID] = entry
	}
	entry.matchesFound = matchesFound
	entry.torrentsAdded = torrentsAdded
	entry.updatedAt = time.Now()
}

func (s *Service) getRunProgress(runID int64) (*runProgress, bool) {
	if s == nil || runID <= 0 {
		return nil, false
	}

	s.progressMu.Lock()
	defer s.progressMu.Unlock()

	entry, ok := s.runProgress[runID]
	if !ok || entry == nil {
		return nil, false
	}
	cp := *entry
	return &cp, true
}

func (s *Service) clearRunProgress(runID int64) {
	if s == nil || runID <= 0 {
		return
	}

	s.progressMu.Lock()
	delete(s.runProgress, runID)
	s.progressMu.Unlock()
}

func (s *Service) recoverStuckRuns() error {
	if s == nil || s.store == nil {
		return nil
	}

	recoveryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	affected, err := s.store.MarkActiveRunsFailed(recoveryCtx, "Scan interrupted by restart")
	if err != nil {
		return fmt.Errorf("mark active runs failed: %w", err)
	}
	if affected > 0 {
		log.Info().Int64("runs", affected).Msg("dirscan: marked interrupted runs as failed")
	}

	return nil
}

func isAlreadySeedingByFileID(searchee *Searchee, index map[string]string) bool {
	if searchee == nil || len(searchee.Files) == 0 || len(index) == 0 {
		return false
	}
	for _, f := range searchee.Files {
		if f == nil || f.FileID.IsZero() {
			return false
		}
		if _, ok := index[string(f.FileID.Bytes())]; !ok {
			return false
		}
	}
	return true
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
	runID int64,
	l *zerolog.Logger,
) *searcheeMatch {
	searcheeSize := CalculateTotalSize(searchee)
	minSize, maxSize := CalculateSizeRange(searcheeSize, settings.SizeTolerancePercent)

	meta, arrLookupName := s.buildSearcheeMetadata(searchee)
	contentInfo := determineContentInfo(meta, searchee)

	l.Debug().
		Str("name", searchee.Name).
		Int64("size", searcheeSize).
		Int("files", len(searchee.Files)).
		Str("contentType", contentInfo.ContentType).
		Msg("dirscan: searching for searchee")

	contentType := normalizeContentType(contentInfo.ContentType)

	filteredIndexers := s.filterIndexersForContent(ctx, &contentInfo, l)

	// Lookup external IDs via arr service if not already present in TRaSH naming
	s.lookupExternalIDs(ctx, meta, contentType, arrLookupName, l)

	// Search indexers
	response := s.searchForSearchee(ctx, searchee, meta, filteredIndexers, contentInfo.Categories, l)
	if response == nil || len(response.Results) == 0 {
		return nil
	}

	l.Debug().
		Str("name", searchee.Name).
		Int("results", len(response.Results)).
		Msg("dirscan: got search results")

	// Try to match and inject
	return s.tryMatchResults(ctx, dir, searchee, response, minSize, maxSize, contentType, settings, matcher, runID, l)
}

func (s *Service) buildSearcheeMetadata(searchee *Searchee) (meta *SearcheeMetadata, arrLookupName string) {
	parsedMeta := s.parser.Parse(searchee.Name)
	arrLookupName = searchee.Name

	// Prefer the largest video file name for content detection (mirrors cross-seed's "largest file" heuristic).
	contentFile := selectLargestVideoFile(searchee.Files)
	if contentFile == nil {
		return parsedMeta, arrLookupName
	}

	base := filepath.Base(contentFile.Path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if name == "" {
		return parsedMeta, arrLookupName
	}

	arrLookupName = name
	fileMeta := s.parser.Parse(name)
	if shouldPreferFileMetadata(parsedMeta, fileMeta) {
		applyFileMetadata(parsedMeta, fileMeta)
	}

	return parsedMeta, arrLookupName
}

func determineContentInfo(meta *SearcheeMetadata, searchee *Searchee) crossseed.ContentTypeInfo {
	contentInfo := crossseed.DetermineContentType(meta.Release)
	if !contentInfo.IsMusic || meta.Release == nil || !hasAnyVideoFile(searchee.Files) {
		return contentInfo
	}

	forced := *meta.Release
	if forced.Series > 0 || forced.Episode > 0 {
		forced.Type = rls.Episode
	} else {
		forced.Type = rls.Movie
	}

	contentInfo = crossseed.DetermineContentType(&forced)
	meta.IsMusic = false
	meta.IsTV = contentInfo.ContentType == "tv"
	meta.IsMovie = contentInfo.ContentType == "movie"
	return contentInfo
}

func normalizeContentType(contentType string) string {
	if contentType == "" {
		return "unknown"
	}
	return contentType
}

func (s *Service) filterIndexersForContent(ctx context.Context, contentInfo *crossseed.ContentTypeInfo, l *zerolog.Logger) []int {
	if s.jackettService == nil || contentInfo == nil || len(contentInfo.RequiredCaps) == 0 {
		return nil
	}

	filteredIndexers, err := s.jackettService.FilterIndexersForCapabilities(
		ctx, nil, contentInfo.RequiredCaps, contentInfo.Categories,
	)
	if err != nil {
		if l != nil {
			l.Debug().Err(err).Msg("dirscan: failed to filter indexers by capabilities, using all")
		}
		return nil
	}

	if l != nil {
		l.Debug().Int("indexers", len(filteredIndexers)).Msg("dirscan: filtered indexers by capabilities")
	}
	return filteredIndexers
}

// searchForSearchee searches indexers and waits for results.
func (s *Service) searchForSearchee(
	ctx context.Context,
	searchee *Searchee,
	meta *SearcheeMetadata,
	indexerIDs []int,
	categories []int,
	l *zerolog.Logger,
) *jackett.SearchResponse {
	resultsCh := make(chan *jackett.SearchResponse, 1)
	errCh := make(chan error, 1)

	searchReq := &SearchRequest{
		Searchee:   searchee,
		Metadata:   meta,       // Pass parsed metadata with external IDs
		IndexerIDs: indexerIDs, // Use capability-filtered indexers
		Categories: categories,
		Limit:      50,
		OnAllComplete: func(response *jackett.SearchResponse, err error) {
			if err != nil {
				errCh <- err
				return
			}
			resultsCh <- response
		},
	}

	searchCtx := jackett.WithSearchPriority(ctx, jackett.RateLimitPriorityBackground)
	if err := s.searcher.Search(searchCtx, searchReq); err != nil {
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
	contentType string,
	settings *models.DirScanSettings,
	matcher *Matcher,
	runID int64,
	l *zerolog.Logger,
) *searcheeMatch {
	for i := range response.Results {
		result := &response.Results[i]

		if minSize > 0 && result.Size < minSize {
			continue
		}
		if maxSize > 0 && result.Size > maxSize {
			continue
		}

		if exists, ok := s.searchResultAlreadyExists(ctx, dir.TargetInstanceID, result, l); ok && exists {
			continue
		}

		match := s.tryMatchAndInject(ctx, dir, searchee, result, contentType, settings, matcher, runID, l)
		if match != nil {
			return match
		}
	}
	return nil
}

func (s *Service) searchResultAlreadyExists(ctx context.Context, instanceID int, result *jackett.SearchResult, l *zerolog.Logger) (exists, checked bool) {
	if s == nil || s.injector == nil || result == nil {
		return false, false
	}

	var hashes []string
	if result.InfoHashV1 != "" {
		hashes = append(hashes, result.InfoHashV1)
	}
	if result.InfoHashV2 != "" {
		hashes = append(hashes, result.InfoHashV2)
	}
	if len(hashes) == 0 {
		return false, false
	}

	found, err := s.injector.TorrentExistsAny(ctx, instanceID, hashes)
	if err != nil {
		if l != nil {
			l.Debug().Err(err).Msg("dirscan: failed to check torrent exists from search result hashes")
		}
		return false, true
	}
	if found && l != nil {
		l.Debug().
			Str("title", result.Title).
			Strs("hashes", hashes).
			Msg("dirscan: search result already in qBittorrent")
	}
	return found, true
}

// tryMatchAndInject downloads a torrent, matches files, and injects if successful.
func (s *Service) tryMatchAndInject(
	ctx context.Context,
	dir *models.DirScanDirectory,
	searchee *Searchee,
	result *jackett.SearchResult,
	contentType string,
	settings *models.DirScanSettings,
	matcher *Matcher,
	runID int64,
	l *zerolog.Logger,
) *searcheeMatch {
	torrentData, parsed := s.downloadAndParseTorrent(ctx, result, l)
	if parsed == nil {
		return nil
	}

	matchResult := matcher.Match(searchee, parsed.Files)
	accept, reason := shouldAcceptDirScanMatch(matchResult, parsed, settings)
	if !accept {
		l.Debug().
			Str("title", result.Title).
			Bool("perfect", matchResult.IsPerfectMatch).
			Bool("partial", matchResult.IsPartialMatch).
			Float64("matchRatio", matchResult.MatchRatio).
			Str("reason", reason).
			Msg("dirscan: no match")
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

	category := settings.Category
	if dir.Category != "" {
		category = dir.Category
	}

	tags := mergeStringLists(settings.Tags, dir.Tags)

	injectReq := &InjectRequest{
		InstanceID:     dir.TargetInstanceID,
		TorrentBytes:   torrentData,
		ParsedTorrent:  parsed,
		Searchee:       searchee,
		MatchResult:    matchResult,
		SearchResult:   result,
		QbitPathPrefix: dir.QbitPathPrefix,
		Category:       category,
		Tags:           tags,
		StartPaused:    settings.StartPaused,
	}

	trackerDomain := crossseed.ParseTorrentAnnounceDomain(torrentData)

	// Once we are about to inject, reflect that in run status so the UI can distinguish
	// pure searching from active injection attempts.
	if updateErr := s.store.UpdateRunStatus(ctx, runID, models.DirScanRunStatusInjecting); updateErr != nil {
		l.Debug().Err(updateErr).Msg("dirscan: failed to update run status to injecting")
	}

	injectResult, err := s.injector.Inject(ctx, injectReq)
	injected := err == nil && injectResult.Success
	if err != nil {
		l.Warn().Err(err).Str("name", searchee.Name).Msg("dirscan: failed to inject torrent")
	} else {
		l.Info().Str("name", searchee.Name).Bool("success", injectResult.Success).Msg("dirscan: injected torrent")
	}

	s.recordRunInjection(ctx, dir, runID, searchee, parsed, contentType, result, injectReq.Category, injectReq.Tags, trackerDomain, injectResult, err)

	return &searcheeMatch{searchee: searchee, torrentData: torrentData, parsedTorrent: parsed, matchResult: matchResult, injected: injected}
}

func (s *Service) resolveTrackerDisplayName(ctx context.Context, incomingTrackerDomain, indexerName string) string {
	var customizations []*models.TrackerCustomization
	if s.trackerCustomizationStore != nil {
		if customs, err := s.trackerCustomizationStore.List(ctx); err == nil {
			customizations = customs
		}
	}
	return models.ResolveTrackerDisplayName(incomingTrackerDomain, indexerName, customizations)
}

func (s *Service) recordRunInjection(
	ctx context.Context,
	dir *models.DirScanDirectory,
	runID int64,
	searchee *Searchee,
	parsed *ParsedTorrent,
	contentType string,
	result *jackett.SearchResult,
	category string,
	tags []string,
	trackerDomain string,
	injectResult *InjectResult,
	injectErr error,
) {
	if s == nil || s.store == nil || runID <= 0 || dir == nil || parsed == nil || searchee == nil || result == nil || injectResult == nil {
		return
	}

	status := models.DirScanRunInjectionStatusAdded
	errorMessage := ""
	if injectErr != nil || !injectResult.Success {
		status = models.DirScanRunInjectionStatusFailed
		switch {
		case injectResult.ErrorMessage != "":
			errorMessage = injectResult.ErrorMessage
		case injectErr != nil:
			errorMessage = injectErr.Error()
		default:
			errorMessage = "unknown injection failure"
		}
	}

	// Fallback: use the matched search result indexer name for display-name resolution.
	indexerName := result.Indexer
	trackerDisplayName := s.resolveTrackerDisplayName(ctx, trackerDomain, indexerName)

	inj := &models.DirScanRunInjection{
		RunID:              runID,
		DirectoryID:        dir.ID,
		Status:             status,
		SearcheeName:       searchee.Name,
		TorrentName:        parsed.Name,
		InfoHash:           parsed.InfoHash,
		ContentType:        contentType,
		IndexerName:        indexerName,
		TrackerDomain:      trackerDomain,
		TrackerDisplayName: trackerDisplayName,
		LinkMode:           injectResult.Mode,
		SavePath:           injectResult.SavePath,
		Category:           category,
		Tags:               tags,
		ErrorMessage:       errorMessage,
	}

	if err := s.store.CreateRunInjection(ctx, inj); err != nil {
		log.Debug().Err(err).Int("directoryID", dir.ID).Int64("runID", runID).Msg("dirscan: failed to record injection event")
	}
}

func mergeStringLists(values ...[]string) []string {
	out := make([]string, 0)
	seen := make(map[string]struct{})
	for _, list := range values {
		for _, v := range list {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func shouldAcceptDirScanMatch(match *MatchResult, parsed *ParsedTorrent, settings *models.DirScanSettings) (accept bool, reason string) {
	if match == nil || parsed == nil || settings == nil {
		return false, "missing match context"
	}

	if match.IsPerfectMatch {
		return true, "perfect match"
	}

	if !settings.AllowPartial {
		return false, "partial matches disabled"
	}

	piecePercent, ok := matchedTorrentPiecePercent(match, parsed)
	if !ok {
		return false, "invalid torrent piece sizing"
	}

	if piecePercent < settings.MinPieceRatio {
		return false, fmt.Sprintf("matched ratio %.2f%% below minimum %.2f%%", piecePercent, settings.MinPieceRatio)
	}

	// If the torrent has extra files (not on disk), qBittorrent may need to download them.
	// Optionally validate piece-boundary safety to avoid corrupting existing files.
	if len(match.UnmatchedTorrentFiles) > 0 && !settings.SkipPieceBoundarySafetyCheck {
		unsafe, res := hasUnsafeUnmatchedTorrentFiles(parsed, match)
		if unsafe {
			return false, res.Reason
		}
	}

	return true, "partial match accepted"
}

func matchedTorrentPiecePercent(match *MatchResult, parsed *ParsedTorrent) (percent float64, ok bool) {
	if match == nil || parsed == nil || parsed.PieceLength <= 0 {
		return 0, false
	}

	var totalBytes int64
	if parsed.TotalSize > 0 {
		totalBytes = parsed.TotalSize
	} else {
		for _, f := range parsed.Files {
			totalBytes += f.Size
		}
	}
	if totalBytes <= 0 {
		return 0, false
	}

	var matchedBytes int64
	for _, p := range match.MatchedFiles {
		if p.TorrentFile.Size <= 0 {
			continue
		}
		matchedBytes += p.TorrentFile.Size
	}
	if matchedBytes <= 0 {
		return 0, true
	}

	pieceLength := parsed.PieceLength
	totalPieces := (totalBytes + pieceLength - 1) / pieceLength
	if totalPieces <= 0 {
		return 0, false
	}
	availablePieces := matchedBytes / pieceLength
	return (float64(availablePieces) / float64(totalPieces)) * 100, true
}

func hasUnsafeUnmatchedTorrentFiles(parsed *ParsedTorrent, match *MatchResult) (unsafe bool, result crossseed.PieceBoundarySafetyResult) {
	if parsed == nil || match == nil {
		return false, crossseed.PieceBoundarySafetyResult{Safe: true, Reason: "missing context"}
	}
	if parsed.PieceLength <= 0 {
		return true, crossseed.PieceBoundarySafetyResult{Safe: false, Reason: "invalid piece length"}
	}

	// If we have no unmatched torrent files, nothing can be downloaded, so it's safe.
	if len(match.UnmatchedTorrentFiles) == 0 {
		return false, crossseed.PieceBoundarySafetyResult{Safe: true, Reason: "no unmatched torrent files"}
	}

	contentPaths := make(map[string]struct{}, len(match.MatchedFiles))
	for _, p := range match.MatchedFiles {
		contentPaths[p.TorrentFile.Path] = struct{}{}
	}

	files := make([]crossseed.TorrentFileForBoundaryCheck, 0, len(parsed.Files))
	for _, f := range parsed.Files {
		_, isContent := contentPaths[f.Path]
		files = append(files, crossseed.TorrentFileForBoundaryCheck{
			Path:      f.Path,
			Size:      f.Size,
			IsContent: isContent,
		})
	}

	res := crossseed.CheckPieceBoundarySafety(files, parsed.PieceLength)
	return !res.Safe, res
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

	// Overlay live progress for the currently active run (if any) without requiring DB writes.
	for i, run := range runs {
		if run == nil {
			continue
		}
		if run.Status != models.DirScanRunStatusQueued &&
			run.Status != models.DirScanRunStatusScanning &&
			run.Status != models.DirScanRunStatusSearching &&
			run.Status != models.DirScanRunStatusInjecting {
			continue
		}
		if progress, ok := s.getRunProgress(run.ID); ok {
			runCopy := *run
			runCopy.MatchesFound = progress.matchesFound
			runCopy.TorrentsAdded = progress.torrentsAdded
			runs[i] = &runCopy
		}
	}

	return runs, nil
}

// ListRunInjections returns injection attempts (added/failed) for a run.
func (s *Service) ListRunInjections(ctx context.Context, directoryID int, runID int64, limit, offset int) ([]*models.DirScanRunInjection, error) {
	injections, err := s.store.ListRunInjections(ctx, directoryID, runID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list run injections: %w", err)
	}
	return injections, nil
}

// ListFiles returns scanned files for a directory.
func (s *Service) ListFiles(ctx context.Context, directoryID int, status *models.DirScanFileStatus, limit, offset int) ([]*models.DirScanFile, error) {
	files, err := s.store.ListFiles(ctx, directoryID, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return files, nil
}

// mapContentTypeToARR maps dirscan content type to ARR content type for ID lookup.
func mapContentTypeToARR(contentType string) arr.ContentType {
	switch contentType {
	case "movie":
		return arr.ContentTypeMovie
	case "tv":
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
