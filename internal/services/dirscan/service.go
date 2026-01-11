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

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
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
) *Service {
	if cfg.SchedulerInterval <= 0 {
		cfg.SchedulerInterval = DefaultConfig().SchedulerInterval
	}
	if cfg.MaxJitter <= 0 {
		cfg.MaxJitter = DefaultConfig().MaxJitter
	}

	return &Service{
		cfg:            cfg,
		store:          store,
		instanceStore:  instanceStore,
		syncManager:    syncManager,
		jackettService: jackettService,
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

	// Create cancellable context for this run
	runCtx, cancel := context.WithCancel(ctx)
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

	// Phase 2: Searching - Search indexers for matches
	// TODO: Implement - will search Torznab indexers for each searchee
	_ = scanResult // Used in future phases

	// Phase 3: Injecting - Inject matched torrents
	// TODO: Implement in Phase 4

	// For now, mark as completed with 0 matches
	if err := s.store.UpdateRunCompleted(ctx, runID, 0, 0); err != nil {
		l.Error().Err(err).Msg("dirscan: failed to mark run as completed")
		return
	}

	l.Info().Msg("dirscan: scan completed")
}

// handleCancellation checks for context cancellation and updates the run status.
// Returns true if the scan was canceled and should stop.
func (s *Service) handleCancellation(ctx context.Context, runID int64, l *zerolog.Logger, phase string) bool {
	if ctx.Err() == nil {
		return false
	}
	l.Info().Msgf("dirscan: scan canceled %s", phase)
	if err := s.store.UpdateRunCanceled(ctx, runID); err != nil {
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

// markRunFailed marks a run as failed with the given error message.
func (s *Service) markRunFailed(ctx context.Context, runID int64, errMsg string, l *zerolog.Logger) {
	if err := s.store.UpdateRunFailed(ctx, runID, errMsg); err != nil {
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
