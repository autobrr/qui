// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package orphanscan finds and removes orphan files not associated with any torrent.
package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// Service handles orphan file scanning and deletion.
type Service struct {
	cfg           Config
	instanceStore *models.InstanceStore
	store         *models.OrphanScanStore
	syncManager   *qbittorrent.SyncManager

	// Per-instance mutex to prevent overlapping scans
	instanceMu map[int]*sync.Mutex
	mu         sync.Mutex // protects instanceMu map

	// In-memory cancel handles keyed by runID
	cancelFuncs map[int64]context.CancelFunc
	cancelMu    sync.Mutex
}

// NewService creates a new orphan scan service.
func NewService(cfg Config, instanceStore *models.InstanceStore, store *models.OrphanScanStore, syncManager *qbittorrent.SyncManager) *Service {
	if cfg.SchedulerInterval <= 0 {
		cfg.SchedulerInterval = DefaultConfig().SchedulerInterval
	}
	if cfg.MaxJitter <= 0 {
		cfg.MaxJitter = DefaultConfig().MaxJitter
	}
	if cfg.StuckRunThreshold <= 0 {
		cfg.StuckRunThreshold = DefaultConfig().StuckRunThreshold
	}
	return &Service{
		cfg:           cfg,
		instanceStore: instanceStore,
		store:         store,
		syncManager:   syncManager,
		instanceMu:    make(map[int]*sync.Mutex),
		cancelFuncs:   make(map[int64]context.CancelFunc),
	}
}

// Start starts the background scheduler.
func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	go s.loop(ctx)
}

func (s *Service) loop(ctx context.Context) {
	// Recover stuck runs from previous crash
	if err := s.recoverStuckRuns(ctx); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to recover stuck runs")
	}

	ticker := time.NewTicker(s.cfg.SchedulerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkScheduledScans(ctx)
		}
	}
}

func (s *Service) recoverStuckRuns(ctx context.Context) error {
	// Mark old pending/scanning runs as failed (they won't resume)
	// Note: preview_ready is intentionally excluded - valid to keep around indefinitely
	// Note: deleting is excluded - let user decide how to handle interrupted deletions
	return s.store.MarkStuckRunsFailed(ctx, s.cfg.StuckRunThreshold, []string{"pending", "scanning"})
}

func (s *Service) checkScheduledScans(ctx context.Context) {
	instances, err := s.instanceStore.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to list instances")
		return
	}

	// Collect due instances with their jitter-adjusted trigger times
	type scheduledScan struct {
		instanceID int
		triggerAt  time.Time
	}
	var due []scheduledScan

	now := time.Now()
	for _, inst := range instances {
		// Gate 1: instance must be active and have local access
		if !inst.IsActive || !inst.HasLocalFilesystemAccess {
			continue
		}

		// Gate 2: orphan scan must be enabled for this instance
		settings, err := s.store.GetSettings(ctx, inst.ID)
		if err != nil || settings == nil || !settings.Enabled {
			continue
		}

		// Gate 3: check if scan is due (last completed + interval <= now)
		lastRun, err := s.store.GetLastCompletedRun(ctx, inst.ID)
		if err != nil {
			continue
		}

		interval := time.Duration(settings.ScanIntervalHours) * time.Hour
		var nextDue time.Time
		if lastRun == nil {
			nextDue = now // Never run, due now
		} else if lastRun.CompletedAt != nil {
			nextDue = lastRun.CompletedAt.Add(interval)
		} else {
			continue // No completion time, skip
		}

		if now.Before(nextDue) {
			continue // Not due yet
		}

		// Compute jitter-adjusted trigger time (non-blocking)
		jitter := time.Duration(rand.Int63n(int64(s.cfg.MaxJitter)))
		due = append(due, scheduledScan{
			instanceID: inst.ID,
			triggerAt:  now.Add(jitter),
		})
	}

	// Launch goroutines for each due scan (jitter handled via timer)
	for _, scan := range due {
		scan := scan // capture
		go func() {
			// Wait for jitter-adjusted trigger time
			delay := time.Until(scan.triggerAt)
			if delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return // Shutdown, don't start new scan
				}
			}

			// Check shutdown again before starting
			if ctx.Err() != nil {
				return
			}

			// TriggerScan internally uses context.Background() for the scan goroutine
			// so shutdown won't cancel in-progress scans, only prevents new starts
			if _, err := s.TriggerScan(ctx, scan.instanceID, "scheduled"); err != nil {
				if !errors.Is(err, ErrScanInProgress) {
					log.Error().Err(err).Int("instance", scan.instanceID).Msg("orphanscan: scheduled scan failed")
				}
			}
		}()
	}
}

func (s *Service) getInstanceMutex(instanceID int) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.instanceMu[instanceID] == nil {
		s.instanceMu[instanceID] = &sync.Mutex{}
	}
	return s.instanceMu[instanceID]
}

// TriggerScan starts a new orphan scan for an instance.
// Returns the run ID or an error if a scan is already in progress.
func (s *Service) TriggerScan(ctx context.Context, instanceID int, triggeredBy string) (int64, error) {
	mu := s.getInstanceMutex(instanceID)
	if !mu.TryLock() {
		return 0, ErrScanInProgress
	}
	// Note: unlock happens in goroutine after scan completes

	// Also check DB for stuck runs (from previous process restart)
	hasActive, err := s.store.HasActiveRun(ctx, instanceID)
	if err != nil {
		mu.Unlock()
		return 0, err
	}
	if hasActive {
		mu.Unlock()
		return 0, ErrScanInProgress
	}

	runID, err := s.store.CreateRun(ctx, instanceID, triggeredBy)
	if err != nil {
		mu.Unlock()
		return 0, err
	}

	// Create cancellable context for this run
	runCtx, cancel := context.WithCancel(context.Background())
	s.cancelMu.Lock()
	s.cancelFuncs[runID] = cancel
	s.cancelMu.Unlock()

	go func() {
		defer mu.Unlock()
		defer func() {
			s.cancelMu.Lock()
			delete(s.cancelFuncs, runID)
			s.cancelMu.Unlock()
		}()
		s.executeScan(runCtx, instanceID, runID)
	}()

	return runID, nil
}

// CancelRun cancels a pending or in-progress scan.
func (s *Service) CancelRun(ctx context.Context, runID int64) error {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run == nil {
		return ErrRunNotFound
	}

	switch run.Status {
	case "pending", "scanning", "preview_ready":
		// Cancel in-memory context if running
		s.cancelMu.Lock()
		if cancel, ok := s.cancelFuncs[runID]; ok {
			cancel()
		}
		s.cancelMu.Unlock()

		// Mark as canceled in DB
		return s.store.UpdateRunStatus(ctx, runID, "canceled")

	case "deleting":
		// Deletion in progress - refuse to cancel mid-delete for safety
		return ErrCannotCancelDuringDeletion

	case "completed", "failed", "canceled":
		return fmt.Errorf("%w: %s", ErrRunAlreadyFinished, run.Status)

	default:
		return fmt.Errorf("%w: %s", ErrInvalidRunStatus, run.Status)
	}
}

// ConfirmDeletion starts the deletion phase for a preview-ready scan.
func (s *Service) ConfirmDeletion(ctx context.Context, instanceID int, runID int64) error {
	run, err := s.store.GetRunByInstance(ctx, instanceID, runID)
	if err != nil {
		return err
	}
	if run == nil {
		return ErrRunNotFound
	}
	if run.Status != "preview_ready" {
		return fmt.Errorf("%w: %s", ErrInvalidRunStatus, run.Status)
	}

	mu := s.getInstanceMutex(instanceID)
	if !mu.TryLock() {
		return ErrScanInProgress
	}

	// Create cancellable context for deletion
	runCtx, cancel := context.WithCancel(context.Background())
	s.cancelMu.Lock()
	s.cancelFuncs[runID] = cancel
	s.cancelMu.Unlock()

	go func() {
		defer mu.Unlock()
		defer func() {
			s.cancelMu.Lock()
			delete(s.cancelFuncs, runID)
			s.cancelMu.Unlock()
		}()
		s.executeDeletion(runCtx, instanceID, runID)
	}()

	return nil
}

func (s *Service) executeScan(ctx context.Context, instanceID int, runID int64) {
	log.Info().Int("instance", instanceID).Int64("run", runID).Msg("orphanscan: starting scan")

	// Update status to scanning
	if err := s.store.UpdateRunStatus(ctx, runID, "scanning"); err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled before marking scanning")
			return
		}
		log.Error().Err(err).Msg("orphanscan: failed to update run status")
		return
	}

	// Get settings (fall back to defaults if none exist yet)
	settings, err := s.store.GetSettings(ctx, instanceID)
	if err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled during settings fetch")
			return
		}
		s.failRun(ctx, runID, "failed to get settings")
		return
	}
	if settings == nil {
		defaults := DefaultSettings()
		settings = &models.OrphanScanSettings{
			InstanceID:         instanceID,
			Enabled:            defaults.Enabled,
			GracePeriodMinutes: defaults.GracePeriodMinutes,
			IgnorePaths:        defaults.IgnorePaths,
			ScanIntervalHours:  defaults.ScanIntervalHours,
			MaxFilesPerRun:     defaults.MaxFilesPerRun,
		}
	}

	// Build file map
	tfm, scanRoots, err := s.buildFileMap(ctx, instanceID)
	if err != nil {
		// Check if this was a cancellation - preserve canceled status instead of marking failed
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled during file map build")
			return
		}
		log.Error().Err(err).Msg("orphanscan: failed to build file map")
		s.failRun(ctx, runID, fmt.Sprintf("failed to build file map: %v", err))
		return
	}

	log.Info().Int("files", tfm.Len()).Int("roots", len(scanRoots)).Msg("orphanscan: built file map")

	// Update scan paths
	if err := s.store.UpdateRunScanPaths(ctx, runID, scanRoots); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update scan paths")
	}

	if len(scanRoots) == 0 {
		log.Warn().Msg("orphanscan: no scan roots found")
		s.failRun(ctx, runID, "no scan roots found (no torrents with absolute save paths)")
		return
	}

	// Normalize ignore paths
	ignorePaths, err := NormalizeIgnorePaths(settings.IgnorePaths)
	if err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled during ignore path normalization")
			return
		}
		s.failRun(ctx, runID, fmt.Sprintf("invalid ignore paths: %v", err))
		return
	}

	gracePeriod := time.Duration(settings.GracePeriodMinutes) * time.Minute

	// Walk each scan root and collect orphans
	var allOrphans []OrphanFile
	var walkErrors []string
	truncated := false
	remaining := settings.MaxFilesPerRun
	var bytesFound int64

	for _, root := range scanRoots {
		if ctx.Err() != nil {
			s.markCanceled(ctx, runID)
			return
		}

		orphans, wasTruncated, err := walkScanRoot(ctx, root, tfm, ignorePaths, gracePeriod, remaining)
		if err != nil {
			if ctx.Err() != nil {
				s.markCanceled(ctx, runID)
				return
			}
			log.Error().Err(err).Str("root", root).Msg("orphanscan: walk error")
			walkErrors = append(walkErrors, fmt.Sprintf("%s: %v", root, err))
			continue
		}

		allOrphans = append(allOrphans, orphans...)
		for _, o := range orphans {
			bytesFound += o.Size
		}
		remaining -= len(orphans)

		if wasTruncated || remaining <= 0 {
			truncated = true
			break
		}
	}

	// If no orphans found but we had walk errors, all roots likely failed
	if len(allOrphans) == 0 && len(walkErrors) > 0 {
		errMsg := fmt.Sprintf("Failed to access %d scan path(s):\n%s", len(walkErrors), strings.Join(walkErrors, "\n"))
		s.failRun(ctx, runID, errMsg)
		return
	}

	// Surface partial failures as warning (but continue with found orphans)
	if len(walkErrors) > 0 {
		warnMsg := fmt.Sprintf("Partial scan: %d path(s) inaccessible:\n%s", len(walkErrors), strings.Join(walkErrors, "\n"))
		s.warnRun(ctx, runID, warnMsg)
	}

	log.Info().Int("orphans", len(allOrphans)).Bool("truncated", truncated).Msg("orphanscan: scan complete")

	// Convert to model files
	modelFiles := make([]models.OrphanScanFile, len(allOrphans))
	for i, o := range allOrphans {
		modTime := o.ModifiedAt
		modelFiles[i] = models.OrphanScanFile{
			FilePath:   o.Path,
			FileSize:   o.Size,
			ModifiedAt: &modTime,
			Status:     "pending",
		}
	}

	// Insert files
	if err := s.store.InsertFiles(ctx, runID, modelFiles); err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled during file insertion")
			return
		}
		log.Error().Err(err).Msg("orphanscan: failed to insert files")
		s.failRun(ctx, runID, fmt.Sprintf("failed to insert files: %v", err))
		return
	}

	// Update run with files found
	if err := s.store.UpdateRunFoundStats(ctx, runID, len(allOrphans), truncated, bytesFound); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update files found")
	}

	// Mark as preview ready
	if err := s.store.UpdateRunStatus(ctx, runID, "preview_ready"); err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled before marking preview_ready")
			return
		}
		log.Error().Err(err).Msg("orphanscan: failed to update run status to preview_ready")
		return
	}

	log.Info().Int64("run", runID).Int("files", len(allOrphans)).Msg("orphanscan: preview ready")
}

func (s *Service) executeDeletion(ctx context.Context, instanceID int, runID int64) {
	log.Info().Int("instance", instanceID).Int64("run", runID).Msg("orphanscan: starting deletion")

	// Update status to deleting
	if err := s.store.UpdateRunStatus(ctx, runID, "deleting"); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update run status to deleting")
		return
	}

	// Get run details
	run, err := s.store.GetRun(ctx, runID)
	if err != nil || run == nil {
		s.failRun(ctx, runID, "failed to get run details")
		return
	}

	// Build fresh file map for re-checking
	tfm, _, err := s.buildFileMap(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to rebuild file map for deletion")
		s.failRun(ctx, runID, fmt.Sprintf("failed to rebuild file map: %v", err))
		return
	}

	// Get files for deletion
	files, err := s.store.GetFilesForDeletion(ctx, runID)
	if err != nil {
		s.failRun(ctx, runID, fmt.Sprintf("failed to get files: %v", err))
		return
	}

	var filesDeleted int
	var bytesReclaimed int64
	var deletedOrMissingPaths []string

	// Delete files
	for _, f := range files {
		if ctx.Err() != nil {
			// Canceled mid-deletion - mark remaining as skipped
			log.Warn().Msg("orphanscan: deletion canceled mid-progress")
			break
		}

		// Find the scan root for this file
		scanRoot := findScanRoot(f.FilePath, run.ScanPaths)
		if scanRoot == "" {
			s.updateFileStatus(ctx, f.ID, "failed", "no matching scan root")
			continue
		}

		disp, err := safeDeleteFile(scanRoot, f.FilePath, tfm)
		if err != nil {
			s.updateFileStatus(ctx, f.ID, "failed", err.Error())
			log.Warn().Err(err).Str("path", f.FilePath).Msg("orphanscan: failed to delete file")
			continue
		}

		switch disp {
		case deleteDispositionSkippedInUse:
			s.updateFileStatus(ctx, f.ID, "skipped", "file is now in use by a torrent")
		case deleteDispositionSkippedMissing:
			s.updateFileStatus(ctx, f.ID, "skipped", "file no longer exists")
			deletedOrMissingPaths = append(deletedOrMissingPaths, f.FilePath)
		case deleteDispositionDeleted:
			s.updateFileStatus(ctx, f.ID, "deleted", "")
			filesDeleted++
			bytesReclaimed += f.FileSize
			deletedOrMissingPaths = append(deletedOrMissingPaths, f.FilePath)
		default:
			s.updateFileStatus(ctx, f.ID, "failed", "unknown delete result")
		}
	}

	// Clean up empty directories
	settings, err := s.store.GetSettings(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instance", instanceID).Msg("orphanscan: failed to load settings for directory cleanup")
	}
	var ignorePaths []string
	if settings != nil {
		ignorePaths, err = NormalizeIgnorePaths(settings.IgnorePaths)
		if err != nil {
			log.Warn().Err(err).Int("instance", instanceID).Msg("orphanscan: invalid ignore paths during directory cleanup, using unnormalized paths")
			ignorePaths = settings.IgnorePaths // Fall back to unnormalized to preserve protection
		}
	}

	var foldersDeleted int
	candidateDirs := collectCandidateDirsForCleanup(deletedOrMissingPaths, run.ScanPaths, ignorePaths)
	for _, dir := range candidateDirs {
		if ctx.Err() != nil {
			break
		}

		scanRoot := findScanRoot(dir, run.ScanPaths)
		if scanRoot == "" {
			continue
		}

		if err := safeDeleteEmptyDir(scanRoot, dir); err == nil {
			foldersDeleted++
		}
	}

	// Mark as completed
	if err := s.store.UpdateRunCompleted(ctx, runID, filesDeleted, foldersDeleted, bytesReclaimed); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update run completed")
		return
	}

	log.Info().
		Int64("run", runID).
		Int("filesDeleted", filesDeleted).
		Int("foldersDeleted", foldersDeleted).
		Int64("bytesReclaimed", bytesReclaimed).
		Msg("orphanscan: deletion complete")
}

func (s *Service) markCanceled(ctx context.Context, runID int64) {
	if err := s.store.UpdateRunStatus(ctx, runID, "canceled"); err != nil {
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: failed to mark run canceled")
	}
}

func (s *Service) failRun(ctx context.Context, runID int64, message string) {
	if ctx.Err() != nil {
		log.Info().Int64("run", runID).Msg("orphanscan: run canceled, skipping failure update")
		return
	}
	if err := s.store.UpdateRunFailed(ctx, runID, message); err != nil {
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: failed to mark run failed")
	}
}

func (s *Service) warnRun(ctx context.Context, runID int64, message string) {
	if ctx.Err() != nil {
		return
	}
	if err := s.store.UpdateRunWarning(ctx, runID, message); err != nil {
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: failed to update warning")
	}
}

func (s *Service) updateFileStatus(ctx context.Context, fileID int64, status, errorMessage string) {
	if err := s.store.UpdateFileStatus(ctx, fileID, status, errorMessage); err != nil {
		log.Error().Err(err).Int64("file", fileID).Str("status", status).Msg("orphanscan: failed to update file status")
	}
}

func (s *Service) buildFileMap(ctx context.Context, instanceID int) (*TorrentFileMap, []string, error) {
	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	// Build hash→torrent lookup with both original and canonical forms
	// SyncManager.GetTorrentFilesBatch returns map keyed by canonicalizeHash (lowercase+trimmed)
	hashToTorrent := make(map[string]qbt.Torrent, len(torrents)*2)
	hashes := make([]string, 0, len(torrents))
	for _, t := range torrents {
		hashes = append(hashes, t.Hash)
		// Store under both original and canonical to handle either key format
		hashToTorrent[t.Hash] = t
		hashToTorrent[canonicalizeHash(t.Hash)] = t
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get torrent files: %w", err)
	}

	tfm := NewTorrentFileMap()
	scanRoots := make(map[string]struct{})

	// Iterate the returned map - keys are canonical (lowercase)
	for hash, files := range filesByHash {
		t, ok := hashToTorrent[hash]
		if !ok {
			continue // Shouldn't happen given double-keying
		}
		savePath := filepath.Clean(t.SavePath)
		if !filepath.IsAbs(savePath) {
			continue // Skip non-absolute paths
		}
		scanRoots[savePath] = struct{}{}

		for _, f := range files {
			fullPath := filepath.Join(savePath, f.Name)
			tfm.Add(normalizePath(fullPath))
		}
	}

	roots := make([]string, 0, len(scanRoots))
	for r := range scanRoots {
		roots = append(roots, r)
	}

	return tfm, roots, nil
}

// findScanRoot finds the scan root that contains the given path.
func findScanRoot(path string, scanRoots []string) string {
	longest := ""
	for _, root := range scanRoots {
		if len(path) < len(root) || path[:len(root)] != root {
			continue
		}
		// Ensure it's at a path boundary.
		if len(path) > len(root) && path[len(root)] != filepath.Separator {
			continue
		}
		if len(root) > len(longest) {
			longest = root
		}
	}
	return longest
}
