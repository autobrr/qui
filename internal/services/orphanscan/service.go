// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package orphanscan finds and removes orphan files not associated with any torrent.
package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/notifications"
)

// healthChecker is the subset of *qbittorrent.Client methods needed for readiness checks.
// Extracted as an interface to enable unit testing without a real qBittorrent connection.
type healthChecker interface {
	IsHealthy() bool
	GetLastRecoveryTime() time.Time
	GetLastSyncUpdate() time.Time
}

// Service handles orphan file scanning and deletion.
type Service struct {
	cfg           Config
	instanceStore *models.InstanceStore
	store         *models.OrphanScanStore
	syncManager   *qbittorrent.SyncManager
	notifier      notifications.Notifier

	// Per-instance mutex to prevent overlapping scans
	instanceMu map[int]*sync.Mutex
	mu         sync.Mutex // protects instanceMu map

	// In-memory cancel handles keyed by runID
	cancelFuncs map[int64]context.CancelFunc
	cancelMu    sync.Mutex

	// Memoize "settled" status per instance/recovery to avoid repeating the 60s settling check
	// on every scan. This resets automatically when the client recovers (recovery time changes).
	settledMu           sync.RWMutex
	settledRecoveryTime map[int]time.Time

	// Providers for testing (nil = use real sync manager)
	getAllTorrentsProvider       func(ctx context.Context, instanceID int) ([]qbt.Torrent, error)
	getTorrentFilesBatchProvider func(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	getClientProvider            func(ctx context.Context, instanceID int) (healthChecker, error)
	listInstancesProvider        func(ctx context.Context) ([]*models.Instance, error)
	getLastCompletedRunProvider  func(ctx context.Context, instanceID int) (*models.OrphanScanRun, error)
}

// NewService creates a new orphan scan service.
func NewService(cfg Config, instanceStore *models.InstanceStore, store *models.OrphanScanStore, syncManager *qbittorrent.SyncManager, notifier notifications.Notifier) *Service {
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
		cfg:                 cfg,
		instanceStore:       instanceStore,
		store:               store,
		syncManager:         syncManager,
		notifier:            notifier,
		instanceMu:          make(map[int]*sync.Mutex),
		cancelFuncs:         make(map[int64]context.CancelFunc),
		settledRecoveryTime: make(map[int]time.Time),
	}
}

func (s *Service) isSettledForRecovery(instanceID int, recoveryTime time.Time) bool {
	if recoveryTime.IsZero() {
		return false
	}

	s.settledMu.RLock()
	settledRecoveryTime, ok := s.settledRecoveryTime[instanceID]
	s.settledMu.RUnlock()

	return ok && settledRecoveryTime.Equal(recoveryTime)
}

func (s *Service) markSettledForRecovery(instanceID int, recoveryTime time.Time) {
	if recoveryTime.IsZero() {
		return
	}

	s.settledMu.Lock()
	s.settledRecoveryTime[instanceID] = recoveryTime
	s.settledMu.Unlock()
}

func (s *Service) clearSettledForRecovery(instanceID int, recoveryTime time.Time) {
	if recoveryTime.IsZero() {
		return
	}

	s.settledMu.Lock()
	settledRecoveryTime, ok := s.settledRecoveryTime[instanceID]
	if ok && settledRecoveryTime.Equal(recoveryTime) {
		delete(s.settledRecoveryTime, instanceID)
	}
	s.settledMu.Unlock()
}

// getAllTorrents returns all torrents for an instance, using the provider if set.
func (s *Service) getAllTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error) {
	if s.getAllTorrentsProvider != nil {
		return s.getAllTorrentsProvider(ctx, instanceID)
	}
	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get all torrents: %w", err)
	}
	return torrents, nil
}

// getTorrentFilesBatch returns files for multiple torrents, using the provider if set.
func (s *Service) getTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	if s.getTorrentFilesBatchProvider != nil {
		return s.getTorrentFilesBatchProvider(ctx, instanceID, hashes)
	}
	files, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		return nil, fmt.Errorf("get torrent files batch: %w", err)
	}
	return files, nil
}

// getClient returns a client for an instance, using the provider if set.
func (s *Service) getClient(ctx context.Context, instanceID int) (healthChecker, error) {
	if s.getClientProvider != nil {
		return s.getClientProvider(ctx, instanceID)
	}
	client, err := s.syncManager.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get client: %w", err)
	}
	return client, nil
}

func (s *Service) listInstances(ctx context.Context) ([]*models.Instance, error) {
	if s.listInstancesProvider != nil {
		return s.listInstancesProvider(ctx)
	}
	instances, err := s.instanceStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	return instances, nil
}

func (s *Service) getLastCompletedRun(ctx context.Context, instanceID int) (*models.OrphanScanRun, error) {
	if s.getLastCompletedRunProvider != nil {
		return s.getLastCompletedRunProvider(ctx, instanceID)
	}
	if s.store == nil {
		return nil, errors.New("orphan scan store unavailable")
	}
	return s.store.GetLastCompletedRun(ctx, instanceID)
}

func scanRootsFromTorrents(torrents []qbt.Torrent) []string {
	scanRoots := make(map[string]struct{})
	for i := range torrents {
		savePath := filepath.Clean(torrents[i].SavePath)
		if savePath == "" || !filepath.IsAbs(savePath) {
			continue
		}
		scanRoots[savePath] = struct{}{}
	}

	roots := make([]string, 0, len(scanRoots))
	for r := range scanRoots {
		roots = append(roots, r)
	}
	return roots
}

func scanRootsOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}

	for _, ra := range a {
		na := normalizePath(ra)
		if na == "" {
			continue
		}
		for _, rb := range b {
			nb := normalizePath(rb)
			if nb == "" {
				continue
			}
			if na == nb || isPathUnderNormalized(na, nb) || isPathUnderNormalized(nb, na) {
				return true
			}
		}
	}
	return false
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
	// Immediately terminate interrupted deletions after restart.
	// We do NOT attempt to resume; user can run a new scan if desired.
	if err := s.store.MarkDeletingRunsFailed(ctx, "Deletion interrupted by restart"); err != nil {
		return fmt.Errorf("mark deleting runs failed: %w", err)
	}

	// Mark old pending/scanning runs as failed (they won't resume).
	// Note: preview_ready is intentionally excluded - valid to keep around indefinitely.
	if err := s.store.MarkStuckRunsFailed(ctx, s.cfg.StuckRunThreshold, []string{"pending", "scanning"}); err != nil {
		return fmt.Errorf("mark stuck runs failed: %w", err)
	}
	return nil
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

			// Pre-check 1: Get client (cheap, before 60s settling)
			client, clientErr := s.getClient(ctx, scan.instanceID)
			if clientErr != nil {
				log.Debug().Err(clientErr).Int("instance", scan.instanceID).
					Msg("orphanscan: skipping scheduled scan (client unavailable)")
				return
			}

			// Pre-check 2: Readiness gates (health, recovery grace, sync freshness)
			if readinessErr := checkReadinessGates(client); readinessErr != nil {
				log.Debug().Err(readinessErr).Int("instance", scan.instanceID).
					Msg("orphanscan: skipping scheduled scan (readiness check failed)")
				return
			}

			recoveryTime := client.GetLastRecoveryTime()
			if !s.isSettledForRecovery(scan.instanceID, recoveryTime) {
				// Pre-check 3: Settling (expensive - samples 4x over 60s)
				settleResult, settleErr := s.checkSettled(ctx, scan.instanceID, client)
				if settleErr != nil {
					log.Debug().Err(settleErr).Int("instance", scan.instanceID).
						Msg("orphanscan: skipping scheduled scan (settling check error)")
					return
				}
				if !settleResult.settled {
					log.Debug().
						Int("instance", scan.instanceID).
						Str("reason", settleResult.reason).
						Msg("orphanscan: skipping scheduled scan (not settled)")
					return
				}

				// Avoid a second 60s settling loop inside buildFileMap for this scheduled run.
				s.markSettledForRecovery(scan.instanceID, recoveryTime)
			}

			// All pre-checks passed - now safe to create run and execute
			if _, err := s.TriggerScan(ctx, scan.instanceID, "scheduled"); err != nil {
				if !errors.Is(err, ErrScanInProgress) {
					log.Error().Err(err).Int("instance", scan.instanceID).
						Msg("orphanscan: scheduled scan failed")
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
	// Atomically check for active runs and create a new one.
	// This avoids TOCTOU races between HasActiveRun and CreateRun,
	// and avoids mutex deadlocks when a goroutine is stuck in a blocking call.
	runID, err := s.store.CreateRunIfNoActive(ctx, instanceID, triggeredBy)
	if errors.Is(err, models.ErrRunAlreadyActive) {
		return 0, ErrScanInProgress
	}
	if err != nil {
		return 0, err
	}

	// Create cancellable context for this run
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
		// If deletion is truly in progress (in-memory cancel func exists), refuse to cancel mid-delete.
		// If there's no cancel func, we assume this is a stale DB state (e.g. after restart) and allow
		// cancellation to unblock future scans.
		s.cancelMu.Lock()
		_, inProgress := s.cancelFuncs[runID]
		s.cancelMu.Unlock()
		if inProgress {
			return ErrCannotCancelDuringDeletion
		}
		if warnErr := s.store.UpdateRunWarning(ctx, runID, "Deletion was interrupted; marked canceled"); warnErr != nil {
			log.Warn().Err(warnErr).Int64("runID", runID).Msg("orphanscan: failed to set warning on interrupted deletion")
		}
		if err := s.store.UpdateRunStatus(ctx, runID, "canceled"); err != nil {
			return fmt.Errorf("update run status: %w", err)
		}
		return nil

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
		s.failRun(ctx, runID, instanceID, "failed to get settings")
		return
	}
	if settings == nil {
		defaults := DefaultSettings()
		settings = &models.OrphanScanSettings{
			InstanceID:          instanceID,
			Enabled:             defaults.Enabled,
			GracePeriodMinutes:  defaults.GracePeriodMinutes,
			IgnorePaths:         defaults.IgnorePaths,
			ScanIntervalHours:   defaults.ScanIntervalHours,
			PreviewSort:         defaults.PreviewSort,
			MaxFilesPerRun:      defaults.MaxFilesPerRun,
			AutoCleanupEnabled:  defaults.AutoCleanupEnabled,
			AutoCleanupMaxFiles: defaults.AutoCleanupMaxFiles,
		}
	}

	// Build file map
	result, err := s.buildFileMap(ctx, instanceID)
	if err != nil {
		// Check if this was a cancellation - preserve canceled status instead of marking failed
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled during file map build")
			return
		}
		log.Error().Err(err).Msg("orphanscan: failed to build file map")
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to build file map: %v", err))
		return
	}

	tfm := result.fileMap
	scanRoots := result.scanRoots

	log.Info().
		Int("files", tfm.Len()).
		Int("roots", len(scanRoots)).
		Int("torrentCount", result.torrentCount).
		Msg("orphanscan: built file map")

	// Update scan paths
	if err := s.store.UpdateRunScanPaths(ctx, runID, scanRoots); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update scan paths")
	}

	if len(scanRoots) == 0 {
		log.Warn().Msg("orphanscan: no scan roots found")
		s.failRun(ctx, runID, instanceID, "no scan roots found (no torrents with absolute save paths)")
		return
	}

	// Normalize ignore paths
	ignorePaths, err := NormalizeIgnorePaths(settings.IgnorePaths)
	if err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled during ignore path normalization")
			return
		}
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("invalid ignore paths: %v", err))
		return
	}

	gracePeriod := time.Duration(settings.GracePeriodMinutes) * time.Minute

	// Walk each scan root and collect orphans.
	// Note: we intentionally do NOT enforce MaxFilesPerRun during the walk.
	// We want the max-files cap to be applied *after sorting* so the preview
	// and truncation reflect the user's configured ordering.
	var allOrphans []OrphanFile
	var walkErrors []string

	for _, root := range scanRoots {
		if ctx.Err() != nil {
			s.markCanceled(ctx, runID)
			return
		}

		orphans, _, err := walkScanRoot(ctx, root, tfm, ignorePaths, gracePeriod, 0)
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
	}

	// Deduplicate across scan roots.
	// Some instances can produce overlapping scan roots (e.g. /data and /data/subdir),
	// which would otherwise result in the same absolute path appearing multiple times.
	allOrphans = dedupeOrphans(allOrphans)

	previewSort := strings.TrimSpace(settings.PreviewSort)
	if previewSort == "" {
		previewSort = "size_desc"
	}

	sort.Slice(allOrphans, func(i, j int) bool {
		a, b := allOrphans[i], allOrphans[j]

		switch previewSort {
		case "directory_size_desc":
			da := strings.ToLower(filepath.Clean(filepath.Dir(a.Path)))
			db := strings.ToLower(filepath.Clean(filepath.Dir(b.Path)))
			if da != db {
				return da < db
			}
			if a.Size != b.Size {
				return a.Size > b.Size
			}
			return strings.ToLower(a.Path) < strings.ToLower(b.Path)
		default: // "size_desc"
			if a.Size != b.Size {
				return a.Size > b.Size
			}
			return strings.ToLower(a.Path) < strings.ToLower(b.Path)
		}
	})

	maxFiles := settings.MaxFilesPerRun
	if maxFiles <= 0 {
		maxFiles = DefaultSettings().MaxFilesPerRun
	}
	truncated := maxFiles > 0 && len(allOrphans) > maxFiles
	if truncated {
		allOrphans = allOrphans[:maxFiles]
	}

	var bytesFound int64
	for _, o := range allOrphans {
		bytesFound += o.Size
	}

	// If no orphans found but we had walk errors, all roots likely failed
	if len(allOrphans) == 0 && len(walkErrors) > 0 {
		errMsg := fmt.Sprintf("Failed to access %d scan path(s):\n%s", len(walkErrors), strings.Join(walkErrors, "\n"))
		s.failRun(ctx, runID, instanceID, errMsg)
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
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to insert files: %v", err))
		return
	}

	// Update run with files found
	if err := s.store.UpdateRunFoundStats(ctx, runID, len(allOrphans), truncated, bytesFound); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update files found")
	}

	// If no orphans found, mark as completed (clean) instead of preview_ready
	if len(allOrphans) == 0 {
		if err := s.store.UpdateRunCompleted(ctx, runID, 0, 0, 0); err != nil {
			if ctx.Err() != nil {
				log.Info().Int64("run", runID).Msg("orphanscan: scan canceled before marking completed")
				return
			}
			log.Error().Err(err).Msg("orphanscan: failed to update run status to completed")
			return
		}
		startedAt, completedAt := s.getRunTimes(ctx, runID)
		s.notify(ctx, notifications.Event{
			Type:                     notifications.EventOrphanScanCompleted,
			InstanceID:               instanceID,
			OrphanScanRunID:          runID,
			OrphanScanFilesDeleted:   0,
			OrphanScanFoldersDeleted: 0,
			StartedAt:                startedAt,
			CompletedAt:              completedAt,
		})
		log.Info().Int64("run", runID).Msg("orphanscan: clean (no orphan files found)")
		return
	}

	// Mark as preview ready (orphans found)
	if err := s.store.UpdateRunStatus(ctx, runID, "preview_ready"); err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled before marking preview_ready")
			return
		}
		log.Error().Err(err).Msg("orphanscan: failed to update run status to preview_ready")
		return
	}

	log.Info().Int64("run", runID).Int("files", len(allOrphans)).Msg("orphanscan: preview ready")

	// Check if auto-cleanup should be triggered for scheduled scans
	s.maybeAutoCleanup(ctx, instanceID, runID, settings, len(allOrphans))
}

func dedupeOrphans(allOrphans []OrphanFile) []OrphanFile {
	if len(allOrphans) <= 1 {
		return allOrphans
	}

	byPath := make(map[string]OrphanFile, len(allOrphans))
	for _, o := range allOrphans {
		key := normalizePath(o.Path)
		existing, ok := byPath[key]
		if !ok {
			byPath[key] = o
			continue
		}
		if o.Size > existing.Size {
			existing.Size = o.Size
		}
		if o.ModifiedAt.After(existing.ModifiedAt) {
			existing.ModifiedAt = o.ModifiedAt
		}
		byPath[key] = existing
	}

	out := make([]OrphanFile, 0, len(byPath))
	for _, o := range byPath {
		out = append(out, o)
	}
	return out
}

// maybeAutoCleanup checks if auto-cleanup should be triggered for a scheduled scan.
// Auto-cleanup is only performed when:
// 1. The scan was triggered by the scheduler (not manual)
// 2. AutoCleanupEnabled is true in settings
// 3. The number of files found is <= AutoCleanupMaxFiles threshold
func (s *Service) maybeAutoCleanup(ctx context.Context, instanceID int, runID int64, settings *models.OrphanScanSettings, filesFound int) {
	// Get the run to check how it was triggered
	run, err := s.store.GetRun(ctx, runID)
	if err != nil || run == nil {
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: failed to get run for auto-cleanup check")
		return
	}

	// Only auto-cleanup for scheduled scans (manual scans always show preview)
	if run.TriggeredBy != "scheduled" {
		return
	}

	// Check if auto-cleanup is enabled
	if settings == nil || !settings.AutoCleanupEnabled {
		return
	}

	// Check file count threshold (safety check for anomalies)
	maxFiles := settings.AutoCleanupMaxFiles
	if maxFiles <= 0 {
		maxFiles = 100 // Default threshold
	}
	if filesFound > maxFiles {
		log.Info().
			Int64("run", runID).
			Int("filesFound", filesFound).
			Int("threshold", maxFiles).
			Msg("orphanscan: skipping auto-cleanup (file count exceeds threshold)")
		return
	}

	log.Info().
		Int64("run", runID).
		Int("filesFound", filesFound).
		Msg("orphanscan: triggering auto-cleanup for scheduled scan")

	// Trigger deletion - ConfirmDeletion runs in a goroutine
	if err := s.ConfirmDeletion(ctx, instanceID, runID); err != nil {
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: auto-cleanup failed to start deletion")
	}
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
		s.failRun(ctx, runID, instanceID, "failed to get run details")
		return
	}

	// Build fresh file map for re-checking
	fileMapResult, err := s.buildFileMap(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to rebuild file map for deletion")
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to rebuild file map: %v", err))
		return
	}
	tfm := fileMapResult.fileMap

	// Load ignore paths before deletion loop
	settings, err := s.store.GetSettings(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instance", instanceID).Msg("orphanscan: failed to load settings for deletion")
	}
	var ignorePaths []string
	if settings != nil {
		ignorePaths, err = NormalizeIgnorePaths(settings.IgnorePaths)
		if err != nil {
			log.Warn().Err(err).Int("instance", instanceID).Msg("orphanscan: invalid ignore paths during deletion, using unnormalized paths")
			ignorePaths = settings.IgnorePaths // Fall back to unnormalized to preserve protection
		}
	}

	// Get files for deletion
	files, err := s.store.GetFilesForDeletion(ctx, runID)
	if err != nil {
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to get files: %v", err))
		return
	}

	var filesDeleted int
	var bytesReclaimed int64
	var deletedOrMissingPaths []string

	// Track deletion failures for user-facing error reporting
	var failedDeletes int
	var sawReadOnly bool
	var sawPermissionDenied bool

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
			failedDeletes++
			continue
		}

		disp, err := safeDeleteTarget(scanRoot, f.FilePath, tfm, ignorePaths)
		if err != nil {
			s.updateFileStatus(ctx, f.ID, "failed", err.Error())
			log.Warn().Err(err).Str("path", f.FilePath).Msg("orphanscan: failed to delete target")
			failedDeletes++

			// Detect error type for user-facing message
			if isReadOnlyFSError(err) || strings.Contains(err.Error(), "read-only file system") {
				sawReadOnly = true
			} else if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") || strings.Contains(err.Error(), "operation not permitted") {
				sawPermissionDenied = true
			}
			continue
		}

		switch disp {
		case deleteDispositionSkippedInUse:
			s.updateFileStatus(ctx, f.ID, "skipped", "file is now in use by a torrent")
		case deleteDispositionSkippedMissing:
			s.updateFileStatus(ctx, f.ID, "skipped", "file no longer exists")
			deletedOrMissingPaths = append(deletedOrMissingPaths, f.FilePath)
		case deleteDispositionSkippedIgnored:
			s.updateFileStatus(ctx, f.ID, "skipped", "path is protected by ignore paths")
		case deleteDispositionDeleted:
			s.updateFileStatus(ctx, f.ID, "deleted", "")
			filesDeleted++
			bytesReclaimed += f.FileSize
			deletedOrMissingPaths = append(deletedOrMissingPaths, f.FilePath)
		default:
			s.updateFileStatus(ctx, f.ID, "failed", "unknown delete result")
			failedDeletes++
		}
	}

	// Clean up empty directories
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

	// Build user-facing error message if deletion failures occurred
	var failureMessage string
	if failedDeletes > 0 {
		if sawReadOnly {
			failureMessage = fmt.Sprintf("Deletion failed for %d file(s): filesystem is read-only. If running via Docker, remove ':ro' from the volume mapping for your downloads path.", failedDeletes)
		} else if sawPermissionDenied {
			failureMessage = fmt.Sprintf("Deletion failed for %d file(s): permission denied. Check that the qui process has write access to the download directories.", failedDeletes)
		} else {
			failureMessage = fmt.Sprintf("Deletion failed for %d file(s). Check the file details for specific errors.", failedDeletes)
		}
	}

	// Determine final status based on deletion results
	if failedDeletes > 0 && filesDeleted == 0 {
		// All deletions failed - mark as failed
		if err := s.store.UpdateRunFailed(ctx, runID, failureMessage); err != nil {
			log.Error().Err(err).Msg("orphanscan: failed to mark run as failed")
			return
		}
		startedAt, completedAt := s.getRunTimes(ctx, runID)
		s.notify(ctx, notifications.Event{
			Type:            notifications.EventOrphanScanFailed,
			InstanceID:      instanceID,
			OrphanScanRunID: runID,
			ErrorMessage:    failureMessage,
			StartedAt:       startedAt,
			CompletedAt:     completedAt,
		})
		log.Warn().
			Int64("run", runID).
			Int("failedDeletes", failedDeletes).
			Msg("orphanscan: deletion failed (no files deleted)")
		return
	}

	// Mark as completed (possibly with partial failure warning)
	if err := s.store.UpdateRunCompleted(ctx, runID, filesDeleted, foldersDeleted, bytesReclaimed); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update run completed")
		return
	}

	startedAt, completedAt := s.getRunTimes(ctx, runID)
	s.notify(ctx, notifications.Event{
		Type:                     notifications.EventOrphanScanCompleted,
		InstanceID:               instanceID,
		OrphanScanRunID:          runID,
		OrphanScanFilesDeleted:   filesDeleted,
		OrphanScanFoldersDeleted: foldersDeleted,
		StartedAt:                startedAt,
		CompletedAt:              completedAt,
	})

	// Add warning for partial failures
	if failedDeletes > 0 {
		if err := s.store.UpdateRunWarning(ctx, runID, failureMessage); err != nil {
			log.Error().Err(err).Msg("orphanscan: failed to update run warning")
		}
	}

	log.Info().
		Int64("run", runID).
		Int("filesDeleted", filesDeleted).
		Int("foldersDeleted", foldersDeleted).
		Int("failedDeletes", failedDeletes).
		Int64("bytesReclaimed", bytesReclaimed).
		Msg("orphanscan: deletion complete")
}

func (s *Service) markCanceled(ctx context.Context, runID int64) {
	if err := s.store.UpdateRunStatus(ctx, runID, "canceled"); err != nil {
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: failed to mark run canceled")
	}
}

func (s *Service) failRun(ctx context.Context, runID int64, instanceID int, message string) {
	if ctx.Err() != nil {
		log.Info().Int64("run", runID).Msg("orphanscan: run canceled, skipping failure update")
		return
	}
	if err := s.store.UpdateRunFailed(ctx, runID, message); err != nil {
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: failed to mark run failed")
		return
	}

	startedAt, completedAt := s.getRunTimes(ctx, runID)
	s.notify(ctx, notifications.Event{
		Type:            notifications.EventOrphanScanFailed,
		InstanceID:      instanceID,
		OrphanScanRunID: runID,
		ErrorMessage:    message,
		StartedAt:       startedAt,
		CompletedAt:     completedAt,
	})
}

func (s *Service) warnRun(ctx context.Context, runID int64, message string) {
	if ctx.Err() != nil {
		return
	}
	if err := s.store.UpdateRunWarning(ctx, runID, message); err != nil {
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: failed to update warning")
	}
}

func (s *Service) notify(ctx context.Context, event notifications.Event) {
	if s == nil || s.notifier == nil {
		return
	}
	s.notifier.Notify(ctx, event)
}

func (s *Service) getRunTimes(ctx context.Context, runID int64) (*time.Time, *time.Time) {
	if s == nil || s.store == nil || runID <= 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	run, err := s.store.GetRun(ctx, runID)
	if err != nil || run == nil {
		return nil, nil
	}
	var startedAt *time.Time
	if !run.StartedAt.IsZero() {
		started := run.StartedAt
		startedAt = &started
	}
	return startedAt, run.CompletedAt
}

func (s *Service) updateFileStatus(ctx context.Context, fileID int64, status, errorMessage string) {
	if err := s.store.UpdateFileStatus(ctx, fileID, status, errorMessage); err != nil {
		log.Error().Err(err).Int64("file", fileID).Str("status", status).Msg("orphanscan: failed to update file status")
	}
}

// settlingResult contains the result of stability sampling
type settlingResult struct {
	settled        bool
	reason         string
	torrents       []qbt.Torrent // Final torrent list (reuse in buildFileMap)
	maxCheckingPct float64       // Max checking% seen across all samples
}

// checkReadinessGates performs cheap pre-checks before expensive settling.
// Returns nil if all checks pass, otherwise an error describing the failure.
func checkReadinessGates(client healthChecker) error {
	if !client.IsHealthy() {
		return errors.New("qBittorrent client is unhealthy")
	}

	recoveryTime := client.GetLastRecoveryTime()
	if !recoveryTime.IsZero() && time.Since(recoveryTime) < RecoveryGracePeriod {
		return fmt.Errorf(
			"qBittorrent recovered %v ago, waiting for grace period (%v)",
			time.Since(recoveryTime).Round(time.Second), RecoveryGracePeriod)
	}

	lastSync := client.GetLastSyncUpdate()
	if lastSync.IsZero() {
		return errors.New("instance not ready: waiting for first sync")
	}
	if !recoveryTime.IsZero() && lastSync.Before(recoveryTime) {
		return errors.New("instance not ready: waiting for sync after recovery")
	}

	if time.Since(lastSync) > MaxSyncAge {
		return fmt.Errorf("sync data stale (last sync %v ago, threshold %v)",
			time.Since(lastSync).Round(time.Second), MaxSyncAge)
	}

	return nil
}

// SampleStats holds statistics from a single torrent sample.
// Exported for testing.
type SampleStats struct {
	Count       int
	CheckingPct float64
	MetaDlPct   float64
}

// EvaluateSettlingSamples analyzes sample statistics to determine if qBit is settled.
// This is a pure function extracted for testability.
// syncAge is time since last sync (pass time.Since(lastSyncTime) from caller).
func EvaluateSettlingSamples(stats []SampleStats, syncAge time.Duration) (settled bool, reason string, maxCheckingPct float64) {
	if len(stats) == 0 {
		return false, "no samples provided", 0
	}

	// Check 1: Zero torrents is always suspicious
	if stats[len(stats)-1].Count == 0 {
		return false, "no torrents returned - instance not ready", 0
	}

	// Check 2: Samples must be stable within tolerance and detect batch loading
	seriesStr, minMax, batchJump := analyzeCountSeries(stats)
	tolerance := max(minMax.max/1000, SettlingCountToleranceMin) // 0.1% or minimum
	if minMax.max-minMax.min > tolerance {
		return false, fmt.Sprintf("torrent count not stable: [%s] (delta %d > tolerance %d)",
			seriesStr, minMax.max-minMax.min, tolerance), 0
	}
	if batchJump > 0 {
		return false, fmt.Sprintf("torrent count jump of %d detected: [%s] (batch loading)", batchJump, seriesStr), 0
	}

	// Check 3: Max checking% across ALL samples must be below threshold
	for _, st := range stats {
		maxCheckingPct = max(maxCheckingPct, st.CheckingPct)
	}
	if maxCheckingPct > MaxCheckingStatePercent {
		return false, fmt.Sprintf("%.1f%% of torrents in checking state (threshold: %.1f%%)",
			maxCheckingPct, MaxCheckingStatePercent), maxCheckingPct
	}

	// Check 4: Sync must be recent
	if syncAge > MaxSyncAge {
		return false, fmt.Sprintf("sync data stale (last sync %v ago, threshold %v)",
			syncAge.Round(time.Second), MaxSyncAge), maxCheckingPct
	}

	return true, "", maxCheckingPct
}

// countMinMax holds min/max values from sample analysis.
type countMinMax struct{ min, max int }

// analyzeCountSeries computes count series string, min/max, and detects batch loading jumps.
// Returns the series string, min/max values, and first batch jump >= 50 (or 0 if none).
func analyzeCountSeries(stats []SampleStats) (seriesStr string, minMax countMinMax, batchJump int) {
	counts := make([]string, len(stats))
	minMax = countMinMax{min: stats[0].Count, max: stats[0].Count}

	for i, st := range stats {
		counts[i] = strconv.Itoa(st.Count)
		minMax.min = min(minMax.min, st.Count)
		minMax.max = max(minMax.max, st.Count)

		// Detect batch loading (step increase >= 50)
		if i > 0 && batchJump == 0 {
			if jump := st.Count - stats[i-1].Count; jump >= 50 {
				batchJump = jump
			}
		}
	}

	return strings.Join(counts, ", "), minMax, batchJump
}

// countTorrentStates counts torrents in checking/loading and metaDL states.
func countTorrentStates(torrents []qbt.Torrent) (checkingCount, metaDlCount int) {
	for i := range torrents {
		switch torrents[i].State {
		case qbt.TorrentStateCheckingResumeData,
			qbt.TorrentStateCheckingDl,
			qbt.TorrentStateCheckingUp,
			qbt.TorrentStateAllocating:
			checkingCount++
		case qbt.TorrentStateMetaDl:
			metaDlCount++
		default:
			// Other states don't affect settling check
		}
	}
	return checkingCount, metaDlCount
}

// calculateStatePercentages computes checking and metaDL percentages.
func calculateStatePercentages(total, checkingCount, metaDlCount int) (checkingPct, metaDlPct float64) {
	if total > 0 {
		checkingPct = float64(checkingCount) / float64(total) * 100
		metaDlPct = float64(metaDlCount) / float64(total) * 100
	}
	return checkingPct, metaDlPct
}

// checkSettled samples torrent state multiple times to ensure qBit has finished loading.
// Returns error if context cancelled, otherwise returns settlingResult with final torrent list.
// Takes client to check sync freshness at the end.
func (s *Service) checkSettled(ctx context.Context, instanceID int, client healthChecker) (*settlingResult, error) {
	var stats []SampleStats
	var finalTorrents []qbt.Torrent

	for i := range SettlingSampleCount {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("settling check canceled: %w", ctx.Err())
			case <-time.After(SettlingSampleInterval):
			}
		}

		torrents, err := s.getAllTorrents(ctx, instanceID)
		if err != nil {
			return nil, fmt.Errorf("failed to get torrents (sample %d): %w", i+1, err)
		}

		checkingCount, metaDlCount := countTorrentStates(torrents)
		checkingPct, metaDlPct := calculateStatePercentages(len(torrents), checkingCount, metaDlCount)

		stats = append(stats, SampleStats{
			Count:       len(torrents),
			CheckingPct: checkingPct,
			MetaDlPct:   metaDlPct,
		})
		finalTorrents = torrents

		log.Debug().
			Int("sample", i+1).
			Int("count", len(torrents)).
			Int("checking", checkingCount).
			Int("metaDl", metaDlCount).
			Float64("checkingPct", checkingPct).
			Float64("metaDlPct", metaDlPct).
			Msg("orphanscan: settling sample")
	}

	// Use pure function for evaluation
	settled, reason, maxCheckingPct := EvaluateSettlingSamples(stats, time.Since(client.GetLastSyncUpdate()))
	return &settlingResult{
		settled:        settled,
		reason:         reason,
		torrents:       finalTorrents,
		maxCheckingPct: maxCheckingPct,
	}, nil
}

// buildFileMapResult contains the file map plus metadata for storage
type buildFileMapResult struct {
	fileMap      *TorrentFileMap
	scanRoots    []string
	torrentCount int
}

func (s *Service) getOtherLocalInstances(ctx context.Context, excludeInstanceID int) ([]*models.Instance, error) {
	instances, err := s.listInstances(ctx)
	if err != nil {
		return nil, err
	}

	local := make([]*models.Instance, 0, len(instances))
	for _, inst := range instances {
		if inst == nil || inst.ID == excludeInstanceID {
			continue
		}
		if inst.IsActive && inst.HasLocalFilesystemAccess {
			local = append(local, inst)
		}
	}
	return local, nil
}

func (s *Service) buildInstanceScanRoots(ctx context.Context, instanceID int, timeout time.Duration) ([]string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	client, err := s.getClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}
	if readinessErr := checkReadinessGates(client); readinessErr != nil {
		return nil, readinessErr
	}

	torrents, err := s.getAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}
	if len(torrents) == 0 {
		return nil, fmt.Errorf("no torrents returned - instance not ready")
	}

	return scanRootsFromTorrents(torrents), nil
}

func (s *Service) instanceScanRootsForOverlap(ctx context.Context, instanceID int) (roots []string, source string, err error) {
	roots, err = s.buildInstanceScanRoots(ctx, instanceID, 15*time.Second)
	if err == nil {
		return roots, "live", nil
	}

	lastRun, lastErr := s.getLastCompletedRun(ctx, instanceID)
	if lastErr != nil {
		return nil, "", err
	}
	if lastRun == nil || len(lastRun.ScanPaths) == 0 {
		return nil, "", err
	}

	return lastRun.ScanPaths, "last_completed_run", nil
}

func (s *Service) buildInstanceScanRootsSettled(ctx context.Context, instanceID int, timeout time.Duration) (roots []string, err error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	client, err := s.getClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	recoveryTime := client.GetLastRecoveryTime()
	defer func() {
		if err != nil {
			s.clearSettledForRecovery(instanceID, recoveryTime)
		}
	}()

	if readinessErr := checkReadinessGates(client); readinessErr != nil {
		return nil, readinessErr
	}

	var torrents []qbt.Torrent
	if s.isSettledForRecovery(instanceID, recoveryTime) {
		torrents, err = s.getAllTorrents(ctx, instanceID)
		if err != nil {
			return nil, fmt.Errorf("failed to get torrents: %w", err)
		}
		if len(torrents) == 0 {
			return nil, fmt.Errorf("no torrents returned - instance not ready")
		}
	} else {
		settleResult, settleErr := s.checkSettled(ctx, instanceID, client)
		if settleErr != nil {
			return nil, fmt.Errorf("settling check failed: %w", settleErr)
		}
		if !settleResult.settled {
			return nil, fmt.Errorf("instance not settled: %s", settleResult.reason)
		}
		torrents = settleResult.torrents
	}

	roots = scanRootsFromTorrents(torrents)
	s.markSettledForRecovery(instanceID, recoveryTime)
	return roots, nil
}

func (s *Service) buildInstanceFileMap(ctx context.Context, instanceID int, timeout time.Duration) (result *buildFileMapResult, err error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Step 1: Get client and verify readiness
	client, err := s.getClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}
	recoveryTime := client.GetLastRecoveryTime()
	defer func() {
		if err != nil {
			s.clearSettledForRecovery(instanceID, recoveryTime)
		}
	}()

	if readinessErr := checkReadinessGates(client); readinessErr != nil {
		return nil, readinessErr
	}

	var torrents []qbt.Torrent
	if s.isSettledForRecovery(instanceID, recoveryTime) {
		torrents, err = s.getAllTorrents(ctx, instanceID)
		if err != nil {
			return nil, fmt.Errorf("failed to get torrents: %w", err)
		}
		if len(torrents) == 0 {
			return nil, fmt.Errorf("no torrents returned - instance not ready")
		}
		log.Debug().
			Int("torrentCount", len(torrents)).
			Msg("orphanscan: skipping settling check (already settled)")
	} else {
		// Step 2: Run settling check (samples torrent state multiple times)
		settleResult, settleErr := s.checkSettled(ctx, instanceID, client)
		if settleErr != nil {
			return nil, fmt.Errorf("settling check failed: %w", settleErr)
		}
		if !settleResult.settled {
			return nil, fmt.Errorf("instance not settled: %s", settleResult.reason)
		}

		log.Info().
			Int("torrentCount", len(settleResult.torrents)).
			Float64("maxCheckingPct", settleResult.maxCheckingPct).
			Msg("orphanscan: settling check passed")
		torrents = settleResult.torrents
	}

	// Step 3: Fetch and validate file data
	hashToTorrent, hashes := buildTorrentHashLookup(torrents)

	filesCtx := qbittorrent.WithForceFilesRefresh(ctx)
	filesByHash, err := s.getTorrentFilesBatch(filesCtx, instanceID, hashes)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent files: %w", err)
	}

	if err := validateFileCompleteness(torrents, filesByHash); err != nil {
		return nil, err
	}

	// Step 4: Build file map from validated data
	result = buildFileMapFromTorrents(hashToTorrent, filesByHash, len(torrents))
	s.markSettledForRecovery(instanceID, recoveryTime)
	return result, nil
}

func (s *Service) buildFileMap(ctx context.Context, instanceID int) (*buildFileMapResult, error) {
	result, err := s.buildInstanceFileMap(ctx, instanceID, 5*time.Minute)
	if err != nil {
		return nil, err
	}

	otherLocalInstances, err := s.getOtherLocalInstances(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	for _, inst := range otherLocalInstances {
		otherRoots, source, rootsErr := s.instanceScanRootsForOverlap(ctx, inst.ID)
		if rootsErr != nil {
			return nil, fmt.Errorf(
				"could not determine scan roots for other local-access instance (id=%d name=%q): %w",
				inst.ID, inst.Name, rootsErr,
			)
		}

		if !scanRootsOverlap(result.scanRoots, otherRoots) {
			confirmedRoots, err := s.buildInstanceScanRootsSettled(ctx, inst.ID, 90*time.Second)
			if err != nil {
				return nil, fmt.Errorf("could not confirm non-overlapping scan roots for other local-access instance (id=%d name=%q): %w", inst.ID, inst.Name, err)
			}
			if !scanRootsOverlap(result.scanRoots, confirmedRoots) {
				continue
			}
		}

		otherResult, err := s.buildInstanceFileMap(ctx, inst.ID, 2*time.Minute)
		if err != nil {
			return nil, fmt.Errorf("overlapping local-access instance unavailable (id=%d name=%q): %w", inst.ID, inst.Name, err)
		}

		added := result.fileMap.MergeFrom(otherResult.fileMap)
		log.Debug().
			Int("instance", inst.ID).
			Int("filesAdded", added).
			Str("rootsSource", source).
			Msg("orphanscan: merged cross-instance torrent files (overlapping scan roots)")
	}

	return result, nil
}

// buildTorrentHashLookup creates a hash-to-torrent lookup map and hash list.
func buildTorrentHashLookup(torrents []qbt.Torrent) (hashToTorrent map[string]qbt.Torrent, hashes []string) {
	hashToTorrent = make(map[string]qbt.Torrent, len(torrents)*2)
	hashes = make([]string, 0, len(torrents))
	for i := range torrents {
		hashes = append(hashes, torrents[i].Hash)
		hashToTorrent[torrents[i].Hash] = torrents[i]
		hashToTorrent[canonicalizeHash(torrents[i].Hash)] = torrents[i]
	}
	return hashToTorrent, hashes
}

// validateFileCompleteness checks that all eligible torrents have file data.
func validateFileCompleteness(torrents []qbt.Torrent, filesByHash map[string]qbt.TorrentFiles) error {
	var missingFilesCount, eligibleCount int
	for i := range torrents {
		if torrents[i].State == qbt.TorrentStateMetaDl {
			continue // Not eligible - legitimately has no files
		}
		eligibleCount++
		canonHash := canonicalizeHash(torrents[i].Hash)
		if files, ok := filesByHash[canonHash]; !ok || len(files) == 0 {
			missingFilesCount++
		}
	}

	if missingFilesCount > MaxMissingFilesCount {
		return fmt.Errorf(
			"%d eligible torrents returned no files - partial data detected (scheduled scans will retry)",
			missingFilesCount)
	}

	log.Info().
		Int("torrents", len(torrents)).
		Int("eligible", eligibleCount).
		Int("missingFiles", missingFilesCount).
		Msg("orphanscan: file map completeness passed")
	return nil
}

// buildFileMapFromTorrents constructs the file map and scan roots from torrent data.
func buildFileMapFromTorrents(hashToTorrent map[string]qbt.Torrent, filesByHash map[string]qbt.TorrentFiles, torrentCount int) *buildFileMapResult {
	tfm := NewTorrentFileMap()
	scanRoots := make(map[string]struct{})

	for hash, files := range filesByHash {
		t, ok := hashToTorrent[hash]
		if !ok {
			continue
		}
		savePath := filepath.Clean(t.SavePath)
		if !filepath.IsAbs(savePath) {
			continue
		}
		scanRoots[savePath] = struct{}{}

		for _, f := range files {
			tfm.Add(normalizePath(filepath.Join(savePath, f.Name)))
		}
	}

	roots := make([]string, 0, len(scanRoots))
	for r := range scanRoots {
		roots = append(roots, r)
	}

	return &buildFileMapResult{
		fileMap:      tfm,
		scanRoots:    roots,
		torrentCount: torrentCount,
	}
}

// findScanRoot finds the scan root that contains the given path.
func findScanRoot(path string, scanRoots []string) string {
	nPath := normalizePath(path)

	longest := ""
	for _, root := range scanRoots {
		nRoot := normalizePath(root)
		if len(nPath) < len(nRoot) || nPath[:len(nRoot)] != nRoot {
			continue
		}
		// Ensure it's at a path boundary.
		if len(nPath) > len(nRoot) && nPath[len(nRoot)] != filepath.Separator {
			continue
		}
		if len(root) > len(longest) {
			longest = root
		}
	}
	return longest
}
