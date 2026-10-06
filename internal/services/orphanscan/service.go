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
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/notifications"
)

// healthChecker is the subset of *qbittorrent.Client methods needed for readiness checks.
type healthChecker interface {
	IsHealthy() bool
	GetLastSyncUpdate() time.Time
}

// syncReader is the slice of the sync manager an orphan scan reads. ADR 0005.
type syncReader interface {
	GetAllTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error)
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	GetAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error)
	GetCategories(ctx context.Context, instanceID int) (map[string]qbt.Category, error)
	CategorySavePathsNest(ctx context.Context, instanceID int) (bool, error)
}

type clientReadiness interface {
	Client(ctx context.Context, instanceID int) (healthChecker, error)
}

type instanceReader interface {
	List(ctx context.Context) ([]*models.Instance, error)
	Get(ctx context.Context, id int) (*models.Instance, error)
}

type lastRunReader interface {
	GetLastCompletedRun(ctx context.Context, instanceID int) (*models.OrphanScanRun, error)
}

// syncClients adapts the sync manager's concrete *Client to healthChecker.
type syncClients struct{ sm *qbittorrent.SyncManager }

func (c syncClients) Client(ctx context.Context, instanceID int) (healthChecker, error) {
	client, err := c.sm.GetClient(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// Service handles orphan file scanning and deletion.
type Service struct {
	cfg         Config
	store       *models.OrphanScanStore
	sync        syncReader
	clients     clientReadiness
	instances   instanceReader
	lastRuns    lastRunReader
	notifier    notifications.Notifier
	backendPool *fsops.Pool

	activityPublisher activity.Publisher

	// Per-instance mutex to prevent overlapping scans
	instanceMu map[int]*sync.Mutex
	mu         sync.Mutex // protects instanceMu map

	// In-memory cancel handles keyed by runID
	cancelFuncs map[int64]context.CancelFunc
	cancelMu    sync.Mutex
}

// NewService creates a new orphan scan service.
func NewService(cfg Config, instanceStore *models.InstanceStore, store *models.OrphanScanStore, syncManager *qbittorrent.SyncManager, notifier notifications.Notifier, backendPool *fsops.Pool) *Service {
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
		cfg:               cfg,
		store:             store,
		sync:              syncManager,
		clients:           syncClients{sm: syncManager},
		instances:         instanceStore,
		lastRuns:          store,
		notifier:          notifier,
		backendPool:       backendPool,
		activityPublisher: activity.NopPublisher{},
		instanceMu:        make(map[int]*sync.Mutex),
		cancelFuncs:       make(map[int64]context.CancelFunc),
	}
}

// SetActivityPublisher wires the qui server-event hub so orphan scan run status
// transitions are pushed to connected clients instead of polled. Safe to call
// once at startup.
func (s *Service) SetActivityPublisher(publisher activity.Publisher) {
	if s == nil || publisher == nil {
		return
	}
	s.activityPublisher = publisher
}

// emitRun signals connected clients that an orphan scan run changed status.
// Call only after the status transition has been persisted and any held lock
// released; never inside per-file progress loops.
func (s *Service) emitRun(instanceID int, runID int64) {
	if s == nil || s.activityPublisher == nil {
		return
	}
	s.activityPublisher.Publish(activity.Event{
		Kind:       activity.KindOrphanScanRun,
		InstanceID: instanceID,
		ResourceID: strconv.FormatInt(runID, 10),
	})
}

// validDefaultSavePath checks the default save path qBittorrent reported so it
// can serve as a scan root. Files sitting directly in it are invisible to a
// torrent-derived root set.
//
// A path that cannot be used is an error rather than an empty root: silently
// falling back to torrent-derived roots would report a clean scan over a
// narrower tree than the user asked for (discussion #2365).
func validDefaultSavePath(d fsops.PathDialect, reported string) (string, error) {
	savePath := d.Clean(reported)
	if savePath == "." {
		return "", errors.New("qBittorrent reported an empty default save path")
	}
	if !d.IsAbs(savePath) {
		return "", fmt.Errorf("qBittorrent default save path %q is not absolute", savePath)
	}
	return savePath, nil
}

// walkForScope walks one root, collecting directory candidates only when the
// run will actually use them.
func walkForScope(ctx context.Context, root string, tfm *TorrentFileMap, ignorePaths []string,
	gracePeriod time.Duration, backend fsops.Backend, collectDirs bool,
) ([]OrphanFile, []AbandonedDir, error) {
	if !collectDirs {
		orphans, _, err := walkScanRoot(ctx, root, tfm, ignorePaths, gracePeriod, 0, backend)
		return orphans, nil, err
	}
	return walkScanRootCollectingDirs(ctx, root, tfm, ignorePaths, gracePeriod, backend)
}

// isMissingRoot separates "not there" from "could not be read". qBittorrent
// creates a category directory only on the first torrent, so an unused category
// is a root that does not exist and cannot hide an orphan. Every other error
// still means the tree went unread, which must not report clean (#2365).
func isMissingRoot(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

// rootsNoTorrentPointsAt returns the declared roots that no torrent save path
// already covers. Their absence is expected, so it is not worth reporting.
func rootsNoTorrentPointsAt(d fsops.PathDialect, declared, derived []string) []string {
	fromTorrent := make(map[string]struct{}, len(derived))
	for _, root := range derived {
		fromTorrent[d.Clean(root)] = struct{}{}
	}

	var only []string
	for _, root := range declared {
		if _, ok := fromTorrent[d.Clean(root)]; !ok {
			only = append(only, d.Clean(root))
		}
	}
	return only
}

// unreachableCoveredRoots reports the roots pruning dropped that are not on
// disk. Pruning is a walk optimisation, not a coverage decision: without this an
// unmounted save path nested under a walked parent reads as a clean scan
// (discussion #2483).
func unreachableCoveredRoots(ctx context.Context, scanRoots, walkRoots []string, expectedAbsent map[string]struct{}, backend fsops.Backend) (errs, missing []string) {
	d := backend.Paths()
	walked := make(map[string]struct{}, len(walkRoots))
	for _, root := range walkRoots {
		walked[root] = struct{}{}
	}

	for _, root := range scanRoots {
		if _, ok := walked[root]; ok {
			continue
		}
		_, err := backend.Stat(ctx, root)
		switch {
		case err == nil:
		case isMissingRoot(err):
			if _, expected := expectedAbsent[d.Clean(root)]; expected {
				continue
			}
			missing = append(missing, fmt.Sprintf("%s: %v", root, err))
		default:
			errs = append(errs, fmt.Sprintf("%s: %v", root, err))
		}
	}
	return errs, missing
}

// pruneNestedScanRoots drops roots already covered by another root in the set so
// an ancestor and its descendants are not walked twice. Callers keep the full
// set for run.ScanPaths; only the walk is narrowed.
func pruneNestedScanRoots(ctx context.Context, roots []string, backend fsops.Backend) []string {
	d := backend.Paths()
	cleaned := make([]string, len(roots))
	for i, root := range roots {
		cleaned[i] = d.Clean(root)
	}

	pruned := make([]string, 0, len(roots))
	for i, root := range roots {
		covered := false
		for j := range roots {
			// Keep case-distinct trees: a folded match does not prove walk coverage.
			if !isPathUnderNormalized(d, cleaned[i], cleaned[j]) {
				continue
			}
			covered = true
			// WalkDir skips symlinks, including a root that is itself a symlink.
			for dir := d.Dir(cleaned[i]); ; dir = d.Dir(dir) {
				info, err := backend.Lstat(ctx, dir)
				if err != nil || !info.IsDir || info.IsSymlink {
					covered = false
					break
				}
				if dir == cleaned[j] {
					break
				}
			}
			if covered {
				break
			}
		}
		if !covered {
			pruned = append(pruned, root)
		}
	}
	return pruned
}

func scanRootsFromTorrents(d fsops.PathDialect, torrents []qbt.Torrent) []string {
	scanRoots := make(map[string]struct{})
	for i := range torrents {
		addAbsoluteScanRoot(d, scanRoots, torrents[i].SavePath)

		// Auto TMM can rewrite save_path to a category root without moving the
		// payload. content_path still points at the real file/folder on disk, and
		// using it directly is enough for conservative overlap detection.
		addAbsoluteScanRoot(d, scanRoots, torrents[i].ContentPath)
	}

	roots := make([]string, 0, len(scanRoots))
	for r := range scanRoots {
		roots = append(roots, r)
	}
	return roots
}

func scanRootsOverlap(d fsops.PathDialect, a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}

	for _, ra := range a {
		na := normalizePath(d, ra)
		if na == "" {
			continue
		}
		for _, rb := range b {
			nb := normalizePath(d, rb)
			if nb == "" {
				continue
			}
			if na == nb || isPathUnderNormalized(d, na, nb) || isPathUnderNormalized(d, nb, na) {
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

	// Crash recovery operates in bulk without per-run identifiers, so emit a coarse
	// signal that prompts clients to refetch any runs they were tracking.
	s.emitRun(0, 0)
	return nil
}

func (s *Service) checkScheduledScans(ctx context.Context) {
	instances, err := s.instances.List(ctx)
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
		// Gate 1: instance must be active and able to read its files
		if !inst.IsActive || !models.FilesystemCapabilitiesOf(inst).Has(models.CapabilityRead) {
			continue
		}

		// Gate 2: orphan scan must be enabled for this instance
		settings, err := s.store.GetSettings(ctx, inst.ID)
		if err != nil || settings == nil || !settings.Enabled {
			continue
		}

		// Gate 3: check if scan is due (last finished scan + interval <= now)
		lastRun, err := s.store.GetLastFinishedScan(ctx, inst.ID)
		if err != nil {
			continue
		}

		interval := time.Duration(settings.ScanIntervalHours) * time.Hour
		var nextDue time.Time
		switch {
		case lastRun == nil:
			nextDue = now // Never run, due now
		case lastRun.Status == "preview_ready":
			// A preview records no finish time. Its start is at most one walk earlier.
			nextDue = lastRun.StartedAt.Add(interval)
		case lastRun.CompletedAt != nil:
			nextDue = lastRun.CompletedAt.Add(interval)
		default:
			continue // No completion time, skip
		}

		if now.Before(nextDue) {
			continue // Not due yet
		}

		// Compute jitter-adjusted trigger time (non-blocking)
		jitter := time.Duration(rand.Int63n(int64(s.cfg.MaxJitter))) //nolint:gosec // G404: schedule jitter, not a security decision
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
			client, clientErr := s.clients.Client(ctx, scan.instanceID)
			if clientErr != nil {
				log.Debug().Err(clientErr).Int("instance", scan.instanceID).
					Msg("orphanscan: skipping scheduled scan (client unavailable)")
				return
			}

			// Pre-check 2: Readiness gates (health + fresh sync)
			if readinessErr := checkReadinessGates(client); readinessErr != nil {
				log.Debug().Err(readinessErr).Int("instance", scan.instanceID).
					Msg("orphanscan: skipping scheduled scan (readiness check failed)")
				return
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

	// Run created (pending) - notify clients so they begin tracking it.
	s.emitRun(instanceID, runID)

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
		if err := s.store.UpdateRunStatus(ctx, runID, "canceled"); err != nil {
			return err
		}
		s.emitRun(run.InstanceID, runID)
		return nil

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
		s.emitRun(run.InstanceID, runID)
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

	instance, err := s.instances.Get(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("load instance %d: %w", instanceID, err)
	}
	if current := models.FilesystemAccessMode(instance); current != run.FilesystemMode {
		logModeChanged(runID, run.FilesystemMode, current)
		// Conditional on preview_ready, so a racing confirm cannot fail a run that is already deleting.
		failed, updateErr := s.store.FailPreviewReadyRun(ctx, runID, FilesystemModeChangedMessage)
		if updateErr != nil {
			return fmt.Errorf("fail run %d after a filesystem access change: %w", runID, updateErr)
		}
		// ErrFilesystemModeChanged means this call failed the run, which a scheduled caller notifies about.
		if !failed {
			return fmt.Errorf("%w: the run is no longer preview_ready", ErrInvalidRunStatus)
		}
		s.emitRun(instanceID, runID)
		return ErrFilesystemModeChanged
	}
	// Refused before the lock and without touching the run, so the preview stays reviewable.
	if !canDeleteOrphans(models.FilesystemCapabilitiesOf(instance)) {
		return fmt.Errorf("orphan scan deletion in %s mode: %w", run.FilesystemMode, fsops.ErrNotCapable)
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

// PathDialect is the path grammar of the backend instanceID is scanned through.
// Resolve builds a remote backend without dialing it.
func (s *Service) PathDialect(ctx context.Context, instanceID int) (fsops.PathDialect, error) {
	if s.backendPool == nil {
		return nil, errors.New("backend pool not configured")
	}
	backend, _, err := s.backendPool.Resolve(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return backend.Paths(), nil
}

// canDeleteOrphans reports whether caps can delete a preview. It needs Identity
// as well as Write, because the walker spots a second path to a torrent's file
// (a bind mount inside the scanned tree) only by its file identity.
func canDeleteOrphans(caps models.FilesystemCapabilities) bool {
	return caps.Write && caps.Identity
}

// logModeChanged records the modes a refused run was scanned under and would
// have deleted under. A preview walked under another mode was never reviewed as
// paths on this backend, and its protection was chosen for the old one.
func logModeChanged(runID int64, runMode, current models.FilesystemMode) {
	log.Info().Int64("run", runID).Str("scannedWith", string(runMode)).Str("now", string(current)).
		Msg("orphanscan: refusing deletion, filesystem access changed since the scan")
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
	s.emitRun(instanceID, runID)

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

	if s.backendPool == nil {
		s.failRun(ctx, runID, instanceID, "backend pool not configured")
		return
	}
	backend, instance, err := s.backendPool.Require(ctx, instanceID, models.CapabilityRead)
	if err != nil {
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to get backend: %v", err))
		return
	}
	// The label comes from the row the backend was built from. Deletion trusts
	// it, so a scan that cannot record it stops here instead of leaving a
	// preview labeled "none".
	mode := models.FilesystemAccessMode(instance)
	if err := s.store.UpdateRunFilesystemMode(ctx, runID, mode); err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled while saving filesystem mode")
			return
		}
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to save filesystem mode: %v", err))
		return
	}

	d := backend.Paths()

	// Build file map
	scope := scopeFromSettings(settings)
	result, err := s.buildFileMap(ctx, instance, backend, scope)
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

	// A save path that is not mounted on the qui host would otherwise fail the
	// run on lstat, with no way for the user to exclude it (issue #2483).
	rootsBeforeIgnore := len(scanRoots)
	scanRoots = filterCoveredScanRoots(d, scanRoots, settings.IgnorePaths)

	log.Info().
		Int("files", tfm.Len()).
		Int("roots", len(scanRoots)).
		Int("torrentCount", result.torrentCount).
		Msg("orphanscan: built file map")

	var scanWarnings []string
	if skippedRoots := filterCoveredScanRoots(d, result.skippedRoots, settings.IgnorePaths); len(skippedRoots) > 0 {
		warnMsg := fmt.Sprintf(
			"Skipped %d scan path(s) because qBittorrent had transitional torrents with unavailable file lists:\n%s",
			len(skippedRoots),
			strings.Join(skippedRoots, "\n"),
		)
		scanWarnings = append(scanWarnings, warnMsg)
	}

	// Update scan paths
	if err := s.store.UpdateRunScanPaths(ctx, runID, scanRoots); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update scan paths")
	}

	if len(scanRoots) == 0 {
		log.Warn().Msg("orphanscan: no scan roots found")
		if rootsBeforeIgnore > 0 {
			s.failRun(ctx, runID, instanceID, "no scan roots left: ignore paths cover every scan path")
			return
		}
		if len(result.skippedRoots) > 0 {
			s.failRun(ctx, runID, instanceID, "no scan roots available: qBittorrent still has transitional torrents with unavailable file lists")
			return
		}
		s.failRun(ctx, runID, instanceID, "no scan roots found (no torrents with absolute save paths)")
		return
	}

	allIgnorePaths := scanIgnorePaths(ctx, settings.IgnorePaths, scanRoots, result, backend)

	// Normalize ignore paths
	ignorePaths, err := NormalizeIgnorePaths(d, allIgnorePaths)
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

	// run.ScanPaths keeps every root so deletion still resolves the narrowest
	// one per file, but walking an ancestor already covers its descendants.
	walkRoots := pruneNestedScanRoots(ctx, scanRoots, backend)
	expectedAbsent := make(map[string]struct{}, len(result.declaredOnlyRoots))
	for _, root := range result.declaredOnlyRoots {
		expectedAbsent[root] = struct{}{}
	}
	walkErrors, missingRoots := unreachableCoveredRoots(ctx, scanRoots, walkRoots, expectedAbsent, backend)

	var orphanOnlyDirs []AbandonedDir
	anyRootScanned := false

	for _, root := range walkRoots {
		if ctx.Err() != nil {
			s.markCanceled(ctx, instanceID, runID)
			return
		}

		orphans, dirs, err := walkForScope(ctx, root, tfm, ignorePaths, gracePeriod, backend, scope.AbandonedDirs)
		if err != nil {
			if ctx.Err() != nil {
				s.markCanceled(ctx, instanceID, runID)
				return
			}
			if isMissingRoot(err) {
				log.Debug().Err(err).Str("root", root).Msg("orphanscan: scan root is not on disk")
				if _, expected := expectedAbsent[d.Clean(root)]; !expected {
					missingRoots = append(missingRoots, fmt.Sprintf("%s: %v", root, err))
				}
				continue
			}
			log.Error().Err(err).Str("root", root).Msg("orphanscan: walk error")
			walkErrors = append(walkErrors, fmt.Sprintf("%s: %v", root, err))
			continue
		}

		anyRootScanned = true
		allOrphans = append(allOrphans, orphans...)
		orphanOnlyDirs = append(orphanOnlyDirs, dirs...)
	}

	allOrphans = dedupeOrphans(d, allOrphans)
	maxFiles := settings.MaxFilesPerRun
	if maxFiles <= 0 {
		maxFiles = DefaultSettings().MaxFilesPerRun
	}

	// Files rank first. If they already exceed the cap, no directory can enter
	// the preview. At the cap, still check directories to set Truncated correctly.
	if scope.AbandonedDirs && len(allOrphans) <= maxFiles {
		abandoned := abandonedDirCandidates(ctx, sortDeepestFirst(orphanOnlyDirs), allOrphans, scanRoots, ignorePaths, result.categoryPaths, gracePeriod, backend)
		log.Info().Int("abandonedDirs", len(abandoned)).Msg("orphanscan: collected abandoned directories")
		allOrphans = dedupeOrphans(d, append(allOrphans, abandoned...))
	}

	previewSort := strings.TrimSpace(settings.PreviewSort)
	if previewSort == "" {
		previewSort = "size_desc"
	}

	less := truncationLess(d, previewSort)
	sort.Slice(allOrphans, func(i, j int) bool { return less(allOrphans[i], allOrphans[j]) })

	truncated := maxFiles > 0 && len(allOrphans) > maxFiles
	if truncated {
		allOrphans = allOrphans[:maxFiles]
	}

	var bytesFound int64
	for _, o := range allOrphans {
		bytesFound += o.Size
	}

	// A mode change mid-scan cuts a remote walk short as a lost connection. The
	// roots walked before it would otherwise land as a preview no confirm can use.
	reloaded, err := s.instances.Get(ctx, instanceID)
	if err != nil {
		if ctx.Err() != nil {
			s.markCanceled(ctx, instanceID, runID)
			return
		}
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to reload instance after the scan: %v", err))
		return
	}
	if current := models.FilesystemAccessMode(reloaded); current != mode {
		log.Info().Int64("run", runID).Str("scannedWith", string(mode)).Str("now", string(current)).
			Msg("orphanscan: failing scan, filesystem access changed while it ran")
		s.failRun(ctx, runID, instanceID, ScanModeChangedMessage)
		return
	}

	inaccessible := slices.Concat(walkErrors, missingRoots)
	if !anyRootScanned && (len(inaccessible) > 0 || len(scanWarnings) > 0) {
		errMsg := "No scan paths completed:\n" + strings.Join(slices.Concat(inaccessible, scanWarnings), "\n\n")
		s.failRun(ctx, runID, instanceID, errMsg)
		return
	}

	if len(inaccessible) > 0 {
		warnMsg := fmt.Sprintf("Partial scan: %d path(s) inaccessible:\n%s", len(inaccessible), strings.Join(inaccessible, "\n"))
		scanWarnings = append(scanWarnings, warnMsg)
	}
	partial := len(scanWarnings) > 0
	var scanWarning string
	if partial {
		scanWarnings = append(scanWarnings, "Automatic cleanup is disabled for partial scans. Review the results before deleting files.")
		scanWarning = strings.Join(scanWarnings, "\n\n")
		if err := s.store.UpdateRunPartial(ctx, runID, scanWarning); err != nil {
			s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to save partial scan result: %v", err))
			return
		}
	}

	log.Info().Int("orphans", len(allOrphans)).Bool("truncated", truncated).Msg("orphanscan: scan complete")

	// Convert to model files
	modelFiles := make([]models.OrphanScanFile, len(allOrphans))
	for i, o := range allOrphans {
		modTime := o.ModifiedAt
		modelFiles[i] = models.OrphanScanFile{
			FilePath:       o.Path,
			FileSize:       o.Size,
			IsAbandonedDir: o.IsAbandonedDir,
			ModifiedAt:     &modTime,
			Status:         "pending",
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

	// Runs without orphan results do not need a deletion preview.
	if len(allOrphans) == 0 {
		if err := s.store.UpdateRunCompleted(ctx, runID, 0, 0, 0, scanWarning); err != nil {
			if ctx.Err() != nil {
				log.Info().Int64("run", runID).Msg("orphanscan: scan canceled before marking completed")
				return
			}
			log.Error().Err(err).Msg("orphanscan: failed to update run status to completed")
			return
		}
		s.emitRun(instanceID, runID)
		startedAt, completedAt := s.getRunTimes(ctx, runID)
		s.notify(ctx, notifications.Event{
			Type:                     notifications.EventOrphanScanCompleted,
			InstanceID:               instanceID,
			OrphanScanRunID:          runID,
			OrphanScanPartial:        partial,
			ErrorMessage:             scanWarning,
			OrphanScanFilesDeleted:   0,
			OrphanScanFoldersDeleted: 0,
			StartedAt:                startedAt,
			CompletedAt:              completedAt,
		})
		log.Info().Int64("run", runID).Bool("partial", partial).Msg("orphanscan: no orphan files found")
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
	s.emitRun(instanceID, runID)

	log.Info().Int64("run", runID).Int("files", len(allOrphans)).Msg("orphanscan: preview ready")
	if partial {
		startedAt, _ := s.getRunTimes(ctx, runID)
		s.notify(ctx, notifications.Event{
			Type:                 notifications.EventOrphanScanCompleted,
			InstanceID:           instanceID,
			OrphanScanRunID:      runID,
			OrphanScanPartial:    true,
			OrphanScanFilesFound: len(allOrphans),
			ErrorMessage:         scanWarning,
			StartedAt:            startedAt,
			CompletedAt:          new(time.Now()),
		})
	}

	// Check if auto-cleanup should be triggered for scheduled scans
	// The threshold is a file count: directories are zero-risk removals and
	// must not push a small cleanup over it.
	filesFound := 0
	for i := range allOrphans {
		if !allOrphans[i].IsAbandonedDir {
			filesFound++
		}
	}
	s.maybeAutoCleanup(ctx, instanceID, runID, settings, filesFound)
}

// truncationLess orders the entries a run stores. This decides what the
// max-files cap keeps, not what the preview shows: the preview re-sorts on read.
//
// Files and directories are ranked separately, and files first. One comparator
// spanning both is cyclic, because a file can sort between a directory and its
// child under previewSort while the depth rule puts the child first, and
// sort.Slice on a cyclic comparator returns an arbitrary order.
func truncationLess(d fsops.PathDialect, previewSort string) func(a, b OrphanFile) bool {
	return func(a, b OrphanFile) bool {
		if a.IsAbandonedDir != b.IsAbandonedDir {
			return !a.IsAbandonedDir
		}

		// The deepest directory has to come first, or the cap can keep a parent
		// while cutting its child, and that parent can never be removed because
		// the child still blocks it. Every later scan would re-select the same
		// parent and nothing would ever go.
		if a.IsAbandonedDir {
			if len(a.Path) != len(b.Path) {
				return len(a.Path) > len(b.Path)
			}
			return a.Path < b.Path
		}

		switch previewSort {
		case "directory_size_desc":
			da := strings.ToLower(d.Clean(d.Dir(a.Path)))
			db := strings.ToLower(d.Clean(d.Dir(b.Path)))
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
	}
}

func dedupeOrphans(d fsops.PathDialect, allOrphans []OrphanFile) []OrphanFile {
	if len(allOrphans) <= 1 {
		return allOrphans
	}

	byPath := make(map[string]OrphanFile, len(allOrphans))
	for _, o := range allOrphans {
		key := normalizePath(d, o.Path)
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
	if run.Partial {
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
		// ConfirmDeletion failed the run without notifying, and nobody watches a scheduled run.
		if errors.Is(err, ErrFilesystemModeChanged) {
			startedAt, completedAt := s.getRunTimes(ctx, runID)
			s.notify(ctx, notifications.Event{
				Type:            notifications.EventOrphanScanFailed,
				InstanceID:      instanceID,
				OrphanScanRunID: runID,
				ErrorMessage:    FilesystemModeChangedMessage,
				StartedAt:       startedAt,
				CompletedAt:     completedAt,
			})
			return
		}
		if errors.Is(err, fsops.ErrNotCapable) {
			log.Info().Err(err).Int64("run", runID).Msg("orphanscan: skipping auto-cleanup, this filesystem access cannot delete")
			warning := "Automatic cleanup does not run for remote instances yet. Review the preview instead."
			if warnErr := s.store.UpdateRunWarning(ctx, runID, warning); warnErr != nil {
				log.Error().Err(warnErr).Int64("run", runID).Msg("orphanscan: failed to record the skipped auto-cleanup")
				return
			}
			s.emitRun(instanceID, runID)
			return
		}
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: auto-cleanup failed to start deletion")
	}
}

func (s *Service) executeDeletion(ctx context.Context, instanceID int, runID int64) {
	log.Info().Int("instance", instanceID).Int64("run", runID).Msg("orphanscan: starting deletion")

	// Conditional on preview_ready, so a run a concurrent confirm just failed cannot go on to delete.
	started, err := s.store.StartRunDeletion(ctx, runID)
	if err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update run status to deleting")
		return
	}
	if !started {
		log.Info().Int64("run", runID).Msg("orphanscan: run is no longer ready for review, deleting nothing")
		return
	}
	s.emitRun(instanceID, runID)

	// Get run details
	run, err := s.store.GetRun(ctx, runID)
	if err != nil || run == nil {
		s.failRun(ctx, runID, instanceID, "failed to get run details")
		return
	}

	if s.backendPool == nil {
		s.failRun(ctx, runID, instanceID, "backend pool not configured")
		return
	}
	// ConfirmDeletion admitted this run on the instance's mode and
	// capabilities, but the instance can change before this goroutine runs.
	deleteBackend, instance, err := s.backendPool.Require(ctx, instanceID, models.CapabilityWrite)
	if errors.Is(err, fsops.ErrNotCapable) {
		log.Info().Err(err).Int64("run", runID).Msg("orphanscan: refusing deletion, filesystem access changed after the confirm")
		s.failRun(ctx, runID, instanceID, FilesystemModeChangedMessage)
		return
	}
	if err != nil {
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to get backend: %v", err))
		return
	}
	mode := models.FilesystemAccessMode(instance)
	if mode != run.FilesystemMode {
		logModeChanged(runID, run.FilesystemMode, mode)
		s.failRun(ctx, runID, instanceID, FilesystemModeChangedMessage)
		return
	}
	// Require(Write) alone is not enough while a backend can gain Write before Identity.
	if !canDeleteOrphans(models.FilesystemCapabilitiesOf(instance)) {
		s.failRun(ctx, runID, instanceID, "Deleting orphan files needs write access and file identity, and this instance's filesystem access lacks one of them.")
		return
	}
	d := deleteBackend.Paths()

	// Settings drive both the ignore paths and the scan scope, and a thinner
	// scope means a thinner protection map. Guessing at defaults here would
	// quietly widen what this run may delete, so a failed read stops it.
	settings, err := s.store.GetSettings(ctx, instanceID)
	if err != nil {
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to load settings for deletion: %v", err))
		return
	}
	var configuredIgnorePaths []string
	if settings != nil {
		configuredIgnorePaths = settings.IgnorePaths
	}

	// Get files for deletion
	files, err := s.store.GetFilesForDeletion(ctx, runID)
	if err != nil {
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to get files: %v", err))
		return
	}

	// Directories are removed after the files, deepest first, so a tree emptied
	// by this run collapses in one pass.
	var dirEntries []*models.OrphanScanFile
	fileEntries := make([]*models.OrphanScanFile, 0, len(files))
	for _, f := range files {
		if f.IsAbandonedDir {
			dirEntries = append(dirEntries, f)
			continue
		}
		fileEntries = append(fileEntries, f)
	}
	sort.Slice(dirEntries, func(i, j int) bool {
		return len(dirEntries[i].FilePath) > len(dirEntries[j].FilePath)
	})

	// The declared roots have to be in place before cross-instance overlap
	// detection decides whose torrents to merge into the protection map.
	// Recheck category protection for previewed directories even if the user
	// disabled directory cleanup after the scan.
	scope := scopeFromSettings(settings).withPersistedRoots(run.ScanPaths)
	scope.AbandonedDirs = true

	// Build fresh file map for re-checking
	fileMapResult, err := s.buildFileMap(ctx, instance, deleteBackend, scope)
	if err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to rebuild file map for deletion")
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to rebuild file map: %v", err))
		return
	}
	tfm := fileMapResult.fileMap

	rawIgnorePaths := scanIgnorePaths(ctx, configuredIgnorePaths, run.ScanPaths, fileMapResult, deleteBackend)
	ignorePaths, err := NormalizeIgnorePaths(d, rawIgnorePaths)
	if err != nil {
		log.Warn().Err(err).Int("instance", instanceID).Msg("orphanscan: invalid ignore paths during deletion, using unnormalized paths")
		ignorePaths = rawIgnorePaths // Fall back to unnormalized to preserve protection
	}

	var filesDeleted int
	var bytesReclaimed int64

	// Track deletion failures for user-facing error reporting
	var failedDeletes int
	var sawReadOnly bool
	var sawPermissionDenied bool

	// Delete files
	for _, f := range fileEntries {
		if ctx.Err() != nil {
			// Canceled mid-deletion - mark remaining as skipped
			log.Warn().Msg("orphanscan: deletion canceled mid-progress")
			break
		}

		// Find the scan root for this file
		scanRoot := findScanRoot(d, f.FilePath, run.ScanPaths)
		if scanRoot == "" {
			s.updateFileStatus(ctx, f.ID, "failed", "no matching scan root")
			failedDeletes++
			continue
		}

		disp, err := safeDeleteTarget(ctx, scanRoot, f.FilePath, tfm, ignorePaths, deleteBackend)
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
		case deleteDispositionSkippedIgnored:
			s.updateFileStatus(ctx, f.ID, "skipped", "path is protected by ignore paths")
		case deleteDispositionDeleted:
			s.updateFileStatus(ctx, f.ID, "deleted", "")
			filesDeleted++
			bytesReclaimed += f.FileSize
		default:
			s.updateFileStatus(ctx, f.ID, "failed", "unknown delete result")
			failedDeletes++
		}
	}

	var foldersDeleted int

	// The protected sets are the same for every entry, so normalize them once
	// rather than once per directory.
	normScanRoots := normalizePaths(d, fileMapResult.scanRoots)
	normCategoryPaths := normalizePaths(d, fileMapResult.categoryPaths)

	// Remove the abandoned directories the preview listed. safeDeleteEmptyDir
	// refuses a non-empty directory, so anything that gained content since the
	// scan is reported rather than removed.
	for _, dir := range dirEntries {
		if ctx.Err() != nil {
			break
		}

		scanRoot := findScanRoot(d, dir.FilePath, run.ScanPaths)
		if scanRoot == "" {
			s.updateFileStatus(ctx, dir.ID, "failed", "no matching scan root")
			failedDeletes++
			continue
		}
		if isIgnoredPath(d, dir.FilePath, ignorePaths) {
			s.updateFileStatus(ctx, dir.ID, "skipped", "path is protected by ignore paths")
			continue
		}
		// Re-check against the roots and categories as they stand now, not as
		// the preview saw them: a category created or repointed since then makes
		// an already-listed directory a live destination again. A scan root is a
		// configured destination, so it stays even when empty.
		normDir := normalizePath(d, dir.FilePath)
		if slices.Contains(normScanRoots, normDir) {
			s.updateFileStatus(ctx, dir.ID, "skipped", "directory is now a scan root")
			continue
		}
		if isCategoryDestinationNormalized(d, normDir, normCategoryPaths) {
			s.updateFileStatus(ctx, dir.ID, "skipped", "directory is now a category destination")
			continue
		}
		// A torrent that has not written its payload yet still owns its save
		// path, and that directory is empty on disk right now.
		if tfm.HasAnyInDir(normDir) {
			s.updateFileStatus(ctx, dir.ID, "skipped", "directory is now used by a torrent")
			continue
		}

		disp, err := safeDeleteEmptyDir(ctx, scanRoot, dir.FilePath, deleteBackend)
		if err != nil {
			s.updateFileStatus(ctx, dir.ID, "failed", err.Error())
			log.Warn().Err(err).Str("path", dir.FilePath).Msg("orphanscan: failed to delete abandoned directory")
			failedDeletes++
			continue
		}

		switch disp {
		case deleteDispositionSkippedMissing:
			s.updateFileStatus(ctx, dir.ID, "skipped", "directory no longer exists")
		case deleteDispositionSkippedNotDirectory:
			s.updateFileStatus(ctx, dir.ID, "skipped", "path is no longer a directory")
		case deleteDispositionSkippedNotEmpty:
			s.updateFileStatus(ctx, dir.ID, "skipped", "directory still holds something this run did not delete")
		case deleteDispositionDeleted:
			s.updateFileStatus(ctx, dir.ID, "deleted", "")
			foldersDeleted++
		default:
			s.updateFileStatus(ctx, dir.ID, "failed", "unknown delete result")
			failedDeletes++
		}
	}

	// Build user-facing error message if deletion failures occurred
	var failureMessage string
	if failedDeletes > 0 {
		if sawReadOnly {
			failureMessage = fmt.Sprintf("Deletion failed for %d item(s): filesystem is read-only. If running via Docker, remove ':ro' from the volume mapping for your downloads path.", failedDeletes)
		} else if sawPermissionDenied {
			failureMessage = fmt.Sprintf("Deletion failed for %d item(s): permission denied. Check that the qui process has write access to the download directories.", failedDeletes)
		} else {
			failureMessage = fmt.Sprintf("Deletion failed for %d item(s). Check the details for specific errors.", failedDeletes)
		}
	}

	// Determine final status based on deletion results. A run that removed
	// directories did work, even when no file could go, and UpdateRunFailed
	// records neither count.
	if failedDeletes > 0 && filesDeleted == 0 && foldersDeleted == 0 {
		// All deletions failed - mark as failed
		failureMessage = strings.TrimSpace(run.ErrorMessage + "\n\n" + failureMessage)
		if err := s.store.UpdateRunFailed(ctx, runID, failureMessage); err != nil {
			log.Error().Err(err).Msg("orphanscan: failed to mark run as failed")
			return
		}
		s.emitRun(instanceID, runID)
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
			Msg("orphanscan: deletion failed (nothing deleted)")
		return
	}

	warningMessage := run.ErrorMessage
	if failedDeletes > 0 {
		warningMessage = strings.TrimSpace(warningMessage + "\n\n" + failureMessage)
	}

	// Mark as completed (possibly with partial failure warning)
	if err := s.store.UpdateRunCompleted(ctx, runID, filesDeleted, foldersDeleted, bytesReclaimed, warningMessage); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update run completed")
		return
	}
	s.emitRun(instanceID, runID)

	startedAt, completedAt := s.getRunTimes(ctx, runID)
	s.notify(ctx, notifications.Event{
		Type:                     notifications.EventOrphanScanCompleted,
		InstanceID:               instanceID,
		OrphanScanRunID:          runID,
		OrphanScanPartial:        run.Partial,
		OrphanScanFilesFound:     run.FilesFound,
		ErrorMessage:             warningMessage,
		OrphanScanFilesDeleted:   filesDeleted,
		OrphanScanFoldersDeleted: foldersDeleted,
		StartedAt:                startedAt,
		CompletedAt:              completedAt,
	})

	log.Info().
		Int64("run", runID).
		Int("filesDeleted", filesDeleted).
		Int("foldersDeleted", foldersDeleted).
		Int("failedDeletes", failedDeletes).
		Int64("bytesReclaimed", bytesReclaimed).
		Msg("orphanscan: deletion complete")
}

func (s *Service) markCanceled(ctx context.Context, instanceID int, runID int64) {
	if err := s.store.UpdateRunStatus(ctx, runID, "canceled"); err != nil {
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: failed to mark run canceled")
		return
	}
	s.emitRun(instanceID, runID)
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
	s.emitRun(instanceID, runID)

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

// checkReadinessGates performs cheap pre-checks before building a file map.
func checkReadinessGates(client healthChecker) error {
	if !client.IsHealthy() {
		return errors.New("qBittorrent client is unhealthy")
	}

	lastSync := client.GetLastSyncUpdate()
	if lastSync.IsZero() {
		return errors.New("instance not ready: waiting for first sync")
	}
	if time.Since(lastSync) > MaxSyncAge {
		return fmt.Errorf("sync data stale (last sync %v ago, threshold %v)",
			time.Since(lastSync).Round(time.Second), MaxSyncAge)
	}

	return nil
}

//nolint:exhaustive // Only transient torrent states belong here; all others are non-transient.
func isTransientTorrentStateForOrphanScan(state qbt.TorrentState) bool {
	switch state {
	case qbt.TorrentStateMetaDl,
		qbt.TorrentStateCheckingResumeData,
		qbt.TorrentStateCheckingDl,
		qbt.TorrentStateCheckingUp,
		qbt.TorrentStateAllocating,
		qbt.TorrentStateMoving:
		return true
	default:
		return false
	}
}

func sortedRoots(roots map[string]struct{}) []string {
	items := make([]string, 0, len(roots))
	for root := range roots {
		items = append(items, root)
	}
	sort.Strings(items)
	return items
}

func mergeRootLists(d fsops.PathDialect, rootLists ...[]string) []string {
	merged := make(map[string]struct{})
	for _, roots := range rootLists {
		for _, root := range roots {
			merged[d.Clean(root)] = struct{}{}
		}
	}
	return sortedRoots(merged)
}

func isSameOrDescendantPath(d fsops.PathDialect, path, base string) bool {
	nPath := normalizePath(d, path)
	nBase := normalizePath(d, base)
	return nPath == nBase || isPathUnderNormalized(d, nPath, nBase)
}

func filterCoveredScanRoots(d fsops.PathDialect, scanRoots, covers []string) []string {
	filtered := make([]string, 0, len(scanRoots))
	for _, root := range scanRoots {
		covered := false
		for _, cover := range covers {
			if isSameOrDescendantPath(d, root, cover) {
				covered = true
				break
			}
		}
		if !covered {
			filtered = append(filtered, d.Clean(root))
		}
	}
	return filtered
}

func isSameRoot(ctx context.Context, first, second string, backend fsops.Backend) bool {
	d := backend.Paths()
	first = d.Clean(first)
	second = d.Clean(second)
	if first == second {
		return true
	}
	if normalizePath(d, first) != normalizePath(d, second) {
		return false
	}
	firstInfo, firstErr := backend.Lstat(ctx, first)
	secondInfo, secondErr := backend.Lstat(ctx, second)
	return firstErr == nil && secondErr == nil && sameRootIdentity(firstInfo, secondInfo)
}

func sameRootIdentity(first, second *fsops.LstatInfo) bool {
	return first.FileIDErr == nil && second.FileIDErr == nil &&
		!first.FileID.IsZero() && first.FileID == second.FileID
}

func metadataIgnoreRoots(ctx context.Context, scanRoots, metadataRoots []string, backend fsops.Backend) []string {
	d := backend.Paths()
	ignored := make([]string, 0, len(metadataRoots))
	for _, metadataRoot := range metadataRoots {
		normalizedMetadataRoot := normalizePath(d, metadataRoot)
		nested := false
		for _, scanRoot := range scanRoots {
			normalizedScanRoot := normalizePath(d, scanRoot)
			if isSameRoot(ctx, metadataRoot, scanRoot, backend) {
				nested = false
				break
			}
			if isPathUnderNormalized(d, normalizedMetadataRoot, normalizedScanRoot) {
				nested = true
			}
		}
		if nested {
			ignored = append(ignored, d.Clean(metadataRoot))
		}
	}
	return ignored
}

func scanIgnorePaths(ctx context.Context, configured, scanRoots []string, result *buildFileMapResult, backend fsops.Backend) []string {
	ignored := append(append([]string(nil), configured...), result.skippedRoots...)
	return append(ignored, metadataIgnoreRoots(ctx, scanRoots, result.metadataRoots, backend)...)
}

// dedupeCaseVariantRoots drops a scan root when an earlier root differs from it
// only by case AND both name the same directory on the backend, which is what a
// case-insensitive filesystem gives us when qBittorrent reports two spellings of
// one save path (issue #2314). Without the identity confirmation this would
// silently stop scanning a second, genuinely different directory on a
// case-sensitive filesystem.
// Roots are compared in the given order; the first spelling wins.
//
// Backend.Lstat, never Stat: a symlink that differs from its target only by
// case would look like the same directory through Stat, and dropping the real
// directory in favour of the symlink would scan nothing at all.
func dedupeCaseVariantRoots(ctx context.Context, roots []string, backend fsops.Backend) []string {
	if len(roots) < 2 {
		return roots
	}

	d := backend.Paths()
	kept := make([]string, 0, len(roots))
	seen := make(map[string]*fsops.LstatInfo, len(roots))
	for _, root := range roots {
		norm := normalizePath(d, root)
		info, _ := backend.Lstat(ctx, root) // nil info on error

		if prev, ok := seen[norm]; ok && prev != nil && info != nil && sameRootIdentity(prev, info) {
			log.Debug().Str("root", root).Msg("orphanscan: dropped scan root that is the same directory as an earlier root")
			continue
		}

		kept = append(kept, root)
		if _, ok := seen[norm]; !ok {
			seen[norm] = info
		}
	}
	return kept
}

func addAbsoluteScanRoot(d fsops.PathDialect, scanRoots map[string]struct{}, root string) {
	root = d.Clean(root)
	if root == "" || !d.IsAbs(root) {
		return
	}
	scanRoots[root] = struct{}{}
}

func torrentRootFolder(files qbt.TorrentFiles) string {
	rootFolder := ""

	for _, f := range files {
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		parts := strings.Split(name, "/")
		if len(parts) <= 1 {
			return ""
		}
		if rootFolder == "" {
			rootFolder = parts[0]
			continue
		}
		if rootFolder != parts[0] {
			return ""
		}
	}

	return rootFolder
}

func actualSavePathFromContentPath(d fsops.PathDialect, savePath, contentPath string, files qbt.TorrentFiles) string {
	savePath = d.Clean(savePath)
	contentPath = d.Clean(contentPath)
	if contentPath == "" || !d.IsAbs(contentPath) || len(files) == 0 {
		return ""
	}
	if savePath != "" && d.IsAbs(savePath) && contentPath == savePath {
		return savePath
	}

	sep := d.Separator()
	var actualSavePath string
	if len(files) == 1 {
		firstFileName := d.Clean(d.FromSlash(files[0].Name))
		actualSavePath = strings.TrimSuffix(contentPath, sep+firstFileName)
		if actualSavePath == "" || actualSavePath == contentPath {
			actualSavePath = d.Dir(contentPath)
		}
	} else {
		rootFolder := torrentRootFolder(files)
		if rootFolder == "" {
			actualSavePath = contentPath
		} else {
			actualSavePath = strings.TrimSuffix(contentPath, sep+d.FromSlash(rootFolder))
			if actualSavePath == "" || actualSavePath == contentPath {
				firstFileName := d.Clean(d.FromSlash(files[0].Name))
				actualSavePath = strings.TrimSuffix(contentPath, sep+firstFileName)
				if actualSavePath == "" || actualSavePath == contentPath {
					actualSavePath = d.Dir(contentPath)
				}
			}
		}
	}
	if actualSavePath == "" || !d.IsAbs(actualSavePath) {
		return ""
	}

	return d.Clean(actualSavePath)
}

// buildFileMapResult contains the file map plus metadata for storage
type buildFileMapResult struct {
	fileMap       *TorrentFileMap
	scanRoots     []string
	skippedRoots  []string
	metadataRoots []string
	categoryPaths []string
	// declaredOnlyRoots are scope roots no torrent points at. qBittorrent
	// creates a category directory only on the first torrent, so these are
	// allowed to be absent and say nothing about the health of the library.
	declaredOnlyRoots []string
	torrentCount      int
}

func buildFileMapFromTorrents(d fsops.PathDialect, torrents []qbt.Torrent, filesByHash map[string]qbt.TorrentFiles) (*buildFileMapResult, error) {
	tfm := NewTorrentFileMap(d)
	scanRoots := make(map[string]struct{})
	skippedRoots := make(map[string]struct{})
	metadataRoots := make(map[string]struct{})
	stableMissingFiles := 0

	for i := range torrents {
		torrent := torrents[i]
		savePath := d.Clean(torrent.SavePath)
		hasAbsSavePath := savePath != "" && d.IsAbs(savePath)
		files, ok := filesByHash[canonicalizeHash(torrent.Hash)]
		hasFiles := ok && len(files) > 0

		if !hasFiles {
			if torrent.HasMetadata != nil && !*torrent.HasMetadata {
				if hasAbsSavePath {
					metadataRoots[savePath] = struct{}{}
				}
				continue
			}

			if isTransientTorrentStateForOrphanScan(torrent.State) {
				if hasAbsSavePath {
					skippedRoots[savePath] = struct{}{}
				}
				continue
			}

			stableMissingFiles++
			continue
		}

		if !hasAbsSavePath {
			continue
		}

		scanRoots[savePath] = struct{}{}
		for _, f := range files {
			tfm.Add(normalizePath(d, d.Join(savePath, d.FromSlash(f.Name))))
		}

		// Auto TMM can update save_path to the category root without moving the
		// payload. content_path still reflects the real on-disk location.
		actualSavePath := actualSavePathFromContentPath(d, savePath, torrent.ContentPath, files)
		if actualSavePath != "" && actualSavePath != savePath {
			scanRoots[actualSavePath] = struct{}{}
			for _, f := range files {
				tfm.Add(normalizePath(d, d.Join(actualSavePath, d.FromSlash(f.Name))))
			}
		}
	}

	if stableMissingFiles > 0 {
		return nil, fmt.Errorf("%d stable torrents returned no files - partial data detected", stableMissingFiles)
	}

	skippedRootList := sortedRoots(skippedRoots)
	scanRootList := filterCoveredScanRoots(d, sortedRoots(scanRoots), skippedRootList)

	return &buildFileMapResult{
		fileMap:       tfm,
		scanRoots:     scanRootList,
		skippedRoots:  skippedRootList,
		metadataRoots: sortedRoots(metadataRoots),
		torrentCount:  len(torrents),
	}, nil
}

// getOverlapCandidateInstances returns the other instances whose torrents can
// protect scanned's files. Overlap compares path strings, which only mean the
// same file on one filesystem. Instances stored against the same SSH host and
// port pair in both modes, whatever their key, pin or local flag, since a
// seedbox can be reached over SSH by one instance and through a mount by
// another. A local scan also pairs with every local instance. The endpoint and
// mode come from scanned, the row the walk's backend was built from, so a later
// edit cannot pick peers for an endpoint the walk never touched.
func (s *Service) getOverlapCandidateInstances(ctx context.Context, scanned *models.Instance) ([]*models.Instance, error) {
	instances, err := s.instances.List(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]*models.Instance, 0, len(instances))
	for _, inst := range instances {
		if inst == nil || inst.ID == scanned.ID || !inst.IsActive {
			continue
		}
		if _, ok := overlapPeer(scanned, inst); ok {
			candidates = append(candidates, inst)
		}
	}
	return candidates, nil
}

// overlapPeer reports whether inst can protect scanned's files and names why, so a
// local scan blocked by a remote-only peer does not call it local-access.
func overlapPeer(scanned, inst *models.Instance) (label string, ok bool) {
	switch {
	case inst.HasLocalFilesystemAccess && models.FilesystemAccessMode(scanned) != models.FilesystemModeRemote:
		return "local-access instance", true
	case scanned.SSHHost != "" && inst.SSHHost == scanned.SSHHost && inst.SSHPort == scanned.SSHPort:
		return "remote instance on the same host", true
	}
	return "", false
}

func (s *Service) buildInstanceScanRoots(ctx context.Context, d fsops.PathDialect, instanceID int, timeout time.Duration) ([]string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	client, err := s.clients.Client(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}
	if readinessErr := checkReadinessGates(client); readinessErr != nil {
		return nil, readinessErr
	}

	torrents, err := s.sync.GetAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	return scanRootsFromTorrents(d, torrents), nil
}

func (s *Service) instanceScanRootsForOverlap(ctx context.Context, d fsops.PathDialect, instanceID int) (roots []string, source string, err error) {
	roots, err = s.buildInstanceScanRoots(ctx, d, instanceID, 15*time.Second)
	if err == nil {
		return roots, "live", nil
	}

	lastRun, lastErr := s.lastRuns.GetLastCompletedRun(ctx, instanceID)
	if lastErr != nil {
		return nil, "", err
	}
	if lastRun == nil || len(lastRun.ScanPaths) == 0 {
		return nil, "", err
	}

	return lastRun.ScanPaths, "last_completed_run", nil
}

func (s *Service) buildInstanceFileMap(ctx context.Context, instanceID int, timeout time.Duration, backend fsops.Backend) (*buildFileMapResult, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	client, err := s.clients.Client(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}
	if readinessErr := checkReadinessGates(client); readinessErr != nil {
		return nil, readinessErr
	}

	torrents, err := s.sync.GetAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	filesCtx := qbittorrent.WithForceFilesRefresh(ctx)
	filesByHash, err := s.sync.GetTorrentFilesBatch(filesCtx, instanceID, torrentHashes(torrents))
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent files: %w", err)
	}

	result, err := buildFileMapFromTorrents(backend.Paths(), torrents, filesByHash)
	if err != nil {
		return nil, err
	}
	result.scanRoots = dedupeCaseVariantRoots(ctx, result.scanRoots, backend)
	return result, nil
}

// buildFileMap builds the protection map and scan roots for scanned, the row
// backend was built from. Roots the operator declared through scope join the
// set before overlap detection runs, so torrents another instance on the same
// filesystem seeds under them stay protected.
func (s *Service) buildFileMap(ctx context.Context, scanned *models.Instance, backend fsops.Backend, scope scanScope) (*buildFileMapResult, error) {
	instanceID := scanned.ID
	d := backend.Paths()
	result, err := s.buildInstanceFileMap(ctx, instanceID, 5*time.Minute, backend)
	if err != nil {
		return nil, err
	}

	extraRoots, categoryPaths, err := s.declaredScanRoots(ctx, d, instanceID, scope)
	if err != nil {
		return nil, err
	}
	result.categoryPaths = categoryPaths
	result.declaredOnlyRoots = rootsNoTorrentPointsAt(d, extraRoots, result.scanRoots)
	// Deletion stays bounded by the roots the run recorded, so those roots must
	// take part in overlap detection even when the settings behind them have
	// since changed. Without this a file another instance picked up after the
	// preview would be missing from the protection map.
	extraRoots = append(extraRoots, scope.PersistedRoots...)
	if len(extraRoots) > 0 {
		result.scanRoots = dedupeCaseVariantRoots(ctx, append(result.scanRoots, extraRoots...), backend)
	}

	candidates, err := s.getOverlapCandidateInstances(ctx, scanned)
	if err != nil {
		return nil, err
	}
	for _, inst := range candidates {
		peer, _ := overlapPeer(scanned, inst)
		otherRoots, source, rootsErr := s.instanceScanRootsForOverlap(ctx, d, inst.ID)
		if rootsErr != nil {
			return nil, fmt.Errorf(
				"could not determine scan roots for other %s (id=%d name=%q): %w",
				peer, inst.ID, inst.Name, rootsErr,
			)
		}

		if !scanRootsOverlap(d, result.scanRoots, otherRoots) {
			confirmedRoots, err := s.buildInstanceScanRoots(ctx, d, inst.ID, 90*time.Second)
			if err != nil {
				return nil, fmt.Errorf("could not confirm non-overlapping scan roots for other %s (id=%d name=%q): %w", peer, inst.ID, inst.Name, err)
			}
			if !scanRootsOverlap(d, result.scanRoots, confirmedRoots) {
				continue
			}
		}

		otherResult, err := s.buildInstanceFileMap(ctx, inst.ID, 2*time.Minute, backend)
		if err != nil {
			return nil, fmt.Errorf("overlapping %s unavailable (id=%d name=%q): %w", peer, inst.ID, inst.Name, err)
		}

		added := result.fileMap.MergeFrom(otherResult.fileMap)
		result.skippedRoots = mergeRootLists(d, result.skippedRoots, otherResult.skippedRoots)
		result.metadataRoots = mergeRootLists(d, result.metadataRoots, otherResult.metadataRoots)
		result.scanRoots = filterCoveredScanRoots(d, result.scanRoots, result.skippedRoots)
		log.Debug().
			Int("instance", inst.ID).
			Int("filesAdded", added).
			Str("rootsSource", source).
			Msg("orphanscan: merged cross-instance torrent files (overlapping scan roots)")
	}

	return result, nil
}

func torrentHashes(torrents []qbt.Torrent) []string {
	hashes := make([]string, 0, len(torrents))
	for i := range torrents {
		hashes = append(hashes, torrents[i].Hash)
	}
	return hashes
}

// findScanRoot finds the scan root that contains the given path.
func findScanRoot(d fsops.PathDialect, path string, scanRoots []string) string {
	nPath := normalizePath(d, path)

	longest := ""
	for _, root := range scanRoots {
		nRoot := normalizePath(d, root)
		if nPath != nRoot && !isPathUnderNormalized(d, nPath, nRoot) {
			continue
		}
		if len(root) > len(longest) {
			longest = root
		}
	}
	return longest
}
