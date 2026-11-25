// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/notifications"
)

type healthChecker interface {
	IsHealthy() bool
	GetLastSyncUpdate() time.Time
}

type syncReader interface {
	GetTorrentRefsFresh(ctx context.Context, instanceID int, filter qbittorrent.TorrentRefFilter) ([]*qbt.Torrent, error)
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	InstanceFolders(ctx context.Context, d fsops.PathDialect, instanceID int) (qbittorrent.InstanceFolders, error)
}

type clientReadiness interface {
	Client(ctx context.Context, instanceID int) (healthChecker, error)
}

type instanceReader interface {
	List(ctx context.Context) ([]*models.Instance, error)
	Get(ctx context.Context, id int) (*models.Instance, error)
	GetHostKeyPin(instance *models.Instance) ([]byte, error)
}

type lastRunReader interface {
	GetLastCompletedRun(ctx context.Context, instanceID int) (*models.OrphanScanRun, error)
}

type syncClients struct{ sm *qbittorrent.SyncManager }

func (c syncClients) Client(ctx context.Context, instanceID int) (healthChecker, error) {
	client, err := c.sm.GetClient(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return client, nil
}

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
	crossSeedRoutes   *crossseedroute.Service
	transfers         transfers

	instanceMu          map[int]*sync.Mutex
	triggerMu           map[int]*sync.Mutex
	sshMu               map[int]*sync.Mutex
	sshChanges          map[int]uint64
	mu                  sync.Mutex
	beforeDeletionStart func()
	beforePreviewReady  func()

	cancelFuncs map[int64]context.CancelFunc
	cancelMu    sync.Mutex

	runs sync.WaitGroup
}

func NewService(cfg Config, instanceStore *models.InstanceStore, store *models.OrphanScanStore, syncManager *qbittorrent.SyncManager, notifier notifications.Notifier, backendPool *fsops.Pool) *Service {
	if cfg.SchedulerInterval <= 0 {
		cfg.SchedulerInterval = DefaultConfig().SchedulerInterval
	}
	if cfg.MaxJitter <= 0 {
		cfg.MaxJitter = DefaultConfig().MaxJitter
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
		triggerMu:         make(map[int]*sync.Mutex),
		sshMu:             make(map[int]*sync.Mutex),
		sshChanges:        make(map[int]uint64),
		cancelFuncs:       make(map[int64]context.CancelFunc),
	}
}

func (s *Service) SetActivityPublisher(publisher activity.Publisher) {
	if s == nil || publisher == nil {
		return
	}
	s.activityPublisher = publisher
}

func (s *Service) SetCrossSeedRoutes(routes *crossseedroute.Service) {
	s.crossSeedRoutes = routes
}

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

func walkForScope(ctx context.Context, root string, protected *protectedPaths,
	gracePeriod time.Duration, backend fsops.Backend, instance *models.Instance, collectDirs bool, keys *fileKeys,
) ([]OrphanFile, []AbandonedDir, error) {
	orphans, dirs, _, err := walkScanRootWithUnitFilter(ctx, root, protected, gracePeriod, 0, nil, backend, instance, collectDirs, keys)
	return orphans, dirs, err
}

func isMissingRoot(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

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

func pruneNestedScanRoots(ctx context.Context, roots []string, backend fsops.Backend) []string {
	d := backend.Paths()
	cleaned := make([]string, len(roots))
	for i, root := range roots {
		cleaned[i] = d.Clean(root)
	}

	pruned := make([]string, 0, len(roots))
	for i, root := range roots {
		if info, err := backend.Lstat(ctx, cleaned[i]); err == nil && info.IsSymlink {
			pruned = append(pruned, root)
			continue
		}
		covered := false
		for j := range roots {
			if isPathUnderNormalized(d, cleaned[i], cleaned[j]) && walkReaches(ctx, backend, cleaned[j], cleaned[i]) {
				covered = true
				break
			}
		}
		if !covered {
			pruned = append(pruned, root)
		}
	}
	return pruned
}

func walkReaches(ctx context.Context, backend fsops.Backend, cover, below string) bool {
	d := backend.Paths()
	for dir := d.Dir(below); ; dir = d.Dir(dir) {
		info, err := backend.Lstat(ctx, dir)
		if err != nil || !info.IsDir || info.IsSymlink {
			return false
		}
		if dir == cover || dir == cover+d.Separator() {
			return true
		}
		if d.Dir(dir) == dir {
			return false
		}
	}
}

func scanRootsFromTorrents(d fsops.PathDialect, torrents []*qbt.Torrent) []string {
	scanRoots := make(map[string]struct{})
	for _, torrent := range torrents {
		addAbsoluteScanRoot(d, scanRoots, torrent.SavePath)

		addAbsoluteScanRoot(d, scanRoots, torrent.ContentPath)
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

	isEmpty := func(p string) bool { return p == "" }
	normA := slices.DeleteFunc(normalizePaths(d, a), isEmpty)
	normB := slices.DeleteFunc(normalizePaths(d, b), isEmpty)
	for _, na := range normA {
		for _, nb := range normB {
			if na == nb || isPathUnderNormalized(d, na, nb) || isPathUnderNormalized(d, nb, na) {
				return true
			}
		}
	}
	return false
}

func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	if err := s.recoverStuckRuns(ctx); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to recover stuck runs")
	}
	go s.loop(ctx)
}

func (s *Service) loop(ctx context.Context) {
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
	if err := s.store.MarkDeletingRunsFailed(ctx, "Deletion interrupted by restart"); err != nil {
		return fmt.Errorf("mark deleting runs failed: %w", err)
	}

	if err := s.store.MarkInterruptedScansFailed(ctx, "Scan interrupted by restart"); err != nil {
		return fmt.Errorf("mark interrupted scans failed: %w", err)
	}

	s.emitRun(0, 0)
	return nil
}

func (s *Service) checkScheduledScans(ctx context.Context) {
	instances, err := s.instances.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to list instances")
		return
	}

	type scheduledScan struct {
		instanceID int
		triggerAt  time.Time
	}
	var due []scheduledScan

	now := time.Now()
	for _, inst := range instances {
		if !inst.IsActive || !models.FilesystemCapabilitiesOf(inst).Has(models.CapabilityRead) {
			continue
		}

		settings, err := s.store.GetSettings(ctx, inst.ID)
		if err != nil || settings == nil || !settings.Enabled {
			continue
		}

		lastRun, err := s.store.GetLastFinishedScan(ctx, inst.ID)
		if err != nil {
			continue
		}

		interval := time.Duration(settings.ScanIntervalHours) * time.Hour
		var nextDue time.Time
		switch {
		case lastRun == nil:
			nextDue = now
		case lastRun.Status == "preview_ready":
			nextDue = lastRun.StartedAt.Add(interval)
		case lastRun.CompletedAt != nil:
			nextDue = lastRun.CompletedAt.Add(interval)
		default:
			continue
		}

		if now.Before(nextDue) {
			continue
		}

		jitter := time.Duration(rand.Int63n(int64(s.cfg.MaxJitter))) //nolint:gosec // G404: schedule jitter, not a security decision
		due = append(due, scheduledScan{
			instanceID: inst.ID,
			triggerAt:  now.Add(jitter),
		})
	}

	for _, scan := range due {
		go func() {
			delay := time.Until(scan.triggerAt)
			if delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return
				}
			}

			if ctx.Err() != nil {
				return
			}

			client, clientErr := s.clients.Client(ctx, scan.instanceID)
			if clientErr != nil {
				log.Debug().Err(clientErr).Int("instance", scan.instanceID).
					Msg("orphanscan: skipping scheduled scan (client unavailable)")
				return
			}

			if readinessErr := checkReadinessGates(client); readinessErr != nil {
				log.Debug().Err(readinessErr).Int("instance", scan.instanceID).
					Msg("orphanscan: skipping scheduled scan (readiness check failed)")
				return
			}

			if _, err := s.TriggerScan(ctx, scan.instanceID, "scheduled"); err != nil {
				if !errors.Is(err, ErrScanInProgress) {
					log.Error().Err(err).Int("instance", scan.instanceID).
						Msg("orphanscan: scheduled scan failed")
				}
			}
		}()
	}
}

func (s *Service) getInstanceMutex(locks map[int]*sync.Mutex, instanceID int) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if locks[instanceID] == nil {
		locks[instanceID] = &sync.Mutex{}
	}
	return locks[instanceID]
}

func (s *Service) TriggerScan(ctx context.Context, instanceID int, triggeredBy string) (int64, error) {
	mu := s.getInstanceMutex(s.triggerMu, instanceID)
	mu.Lock()
	runID, err := s.store.CreateRunIfNoActive(ctx, instanceID, triggeredBy)
	mu.Unlock()
	if errors.Is(err, models.ErrRunAlreadyActive) {
		return 0, ErrScanInProgress
	}
	if err != nil {
		return 0, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.cancelMu.Lock()
	s.cancelFuncs[runID] = cancel
	s.cancelMu.Unlock()

	s.emitRun(instanceID, runID)

	s.runs.Go(func() {
		defer func() {
			s.cancelMu.Lock()
			delete(s.cancelFuncs, runID)
			s.cancelMu.Unlock()
		}()
		s.executeScan(runCtx, instanceID, runID)
	})

	return runID, nil
}

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
		s.cancelMu.Lock()
		if cancel, ok := s.cancelFuncs[runID]; ok {
			cancel()
		}
		s.cancelMu.Unlock()

		if err := s.store.UpdateRunStatus(ctx, runID, "canceled"); err != nil {
			return err
		}
		s.emitRun(run.InstanceID, runID)
		return nil

	case "deleting":
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
		return s.failChangedPreview(ctx, instanceID, runID)
	}
	if run.FilesystemEndpoint != "" && fsops.WalkedEndpoint(instance) != run.FilesystemEndpoint {
		log.Info().Int64("run", runID).Msg("orphanscan: refusing deletion, the SSH endpoint changed since the scan")
		return s.failChangedPreview(ctx, instanceID, runID)
	}
	if !runCanDelete(run, models.FilesystemCapabilitiesOf(instance)) {
		return fmt.Errorf("orphan scan deletion in %s mode: %w", run.FilesystemMode, fsops.ErrNotCapable)
	}

	mu := s.getInstanceMutex(s.instanceMu, instanceID)
	if !mu.TryLock() {
		return ErrScanInProgress
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.cancelMu.Lock()
	s.cancelFuncs[runID] = cancel
	s.cancelMu.Unlock()

	s.runs.Go(func() {
		defer mu.Unlock()
		defer func() {
			s.cancelMu.Lock()
			delete(s.cancelFuncs, runID)
			s.cancelMu.Unlock()
		}()
		s.executeDeletion(runCtx, instanceID, runID)
	})

	return nil
}

func (s *Service) ChangeSSH(ctx context.Context, instanceID int, change func() error) error {
	mu := s.getInstanceMutex(s.sshMu, instanceID)
	mu.Lock()
	defer mu.Unlock()

	deleting, err := s.store.HasDeletingRun(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("check for a running orphan scan deletion: %w", err)
	}
	if deleting {
		return ErrDeletionInProgress
	}
	if err := change(); err != nil {
		return err
	}
	s.mu.Lock()
	s.sshChanges[instanceID]++
	s.mu.Unlock()
	if err := s.failRemotePreviews(context.WithoutCancel(ctx), instanceID); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("orphanscan: failed to fail remote previews after an SSH change")
	}
	return nil
}

func (s *Service) sshChangeCount(instanceID int) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sshChanges[instanceID]
}

func (s *Service) failRemotePreviews(ctx context.Context, instanceID int) error {
	runIDs, err := s.store.RemotePreviewRunIDs(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("list remote previews of instance %d: %w", instanceID, err)
	}
	for _, runID := range runIDs {
		failed, err := s.store.FailPreviewReadyRun(ctx, runID, FilesystemModeChangedMessage)
		if err != nil {
			return fmt.Errorf("fail run %d after an SSH change: %w", runID, err)
		}
		if failed {
			s.emitRun(instanceID, runID)
		}
	}
	return nil
}

func (s *Service) failChangedPreview(ctx context.Context, instanceID int, runID int64) error {
	failed, err := s.store.FailPreviewReadyRun(ctx, runID, FilesystemModeChangedMessage)
	if err != nil {
		return fmt.Errorf("fail run %d after a filesystem access change: %w", runID, err)
	}
	if !failed {
		return fmt.Errorf("%w: the run is no longer preview_ready", ErrInvalidRunStatus)
	}
	s.emitRun(instanceID, runID)
	return ErrFilesystemModeChanged
}

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

func canDeleteOrphans(caps models.FilesystemCapabilities) bool {
	return caps.Write && caps.Identity
}

func runCanDelete(run *models.OrphanScanRun, caps models.FilesystemCapabilities) bool {
	if run.FilesystemMode == models.FilesystemModeRemote && !run.IdentityComplete {
		return false
	}
	return canDeleteOrphans(caps)
}

func logModeChanged(runID int64, runMode, current models.FilesystemMode) {
	log.Info().Int64("run", runID).Str("scannedWith", string(runMode)).Str("now", string(current)).
		Msg("orphanscan: refusing deletion, filesystem access changed since the scan")
}

func (s *Service) executeScan(ctx context.Context, instanceID int, runID int64) {
	log.Info().Int("instance", instanceID).Int64("run", runID).Msg("orphanscan: starting scan")

	if err := s.store.UpdateRunStatus(ctx, runID, "scanning"); err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled before marking scanning")
			return
		}
		log.Error().Err(err).Msg("orphanscan: failed to update run status")
		return
	}
	s.emitRun(instanceID, runID)

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
	sshMu := s.getInstanceMutex(s.sshMu, instanceID)
	sshMu.Lock()
	backend, instance, err := s.backendPool.Require(ctx, instanceID, models.CapabilityRead)
	sshChanges := s.sshChangeCount(instanceID)
	sshMu.Unlock()
	if err != nil {
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to get backend: %v", err))
		return
	}
	mode := models.FilesystemAccessMode(instance)
	if err := s.store.UpdateRunFilesystemMode(ctx, runID, mode, fsops.WalkedEndpoint(instance)); err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled while saving filesystem mode")
			return
		}
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to save filesystem mode: %v", err))
		return
	}

	d := backend.Paths()

	scope := scopeFromSettings(settings)
	result, err := s.buildFileMap(ctx, instance, backend, scope)
	if err != nil {
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

	rootsBeforeIgnore := len(scanRoots)
	scanRoots = filterCoveredScanRoots(d, scanRoots, settings.IgnorePaths)
	inUseRoots := ignoredSymlinkRoots(ctx, d, result.scanRoots, scanRoots, backend)

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

	allIgnorePaths := scanIgnorePaths(ctx, settings.IgnorePaths, scanRoots, result, backend, instance)

	protected, err := newProtectedPaths(d, allIgnorePaths, tfm)
	if err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled during ignore path normalization")
			return
		}
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("invalid ignore paths: %v", err))
		return
	}
	protected.setScanRoots(scanRoots)
	walkRoots := pruneNestedScanRoots(ctx, scanRoots, backend)
	inUseRoots = pruneNestedScanRoots(ctx, mergeRootLists(d, inUseRoots, protected.ignoredTorrentDirs(),
		linkedPeerRoots(ctx, result.peerRoots, scanRoots, walkRoots, backend)), backend)

	gracePeriod := time.Duration(settings.GracePeriodMinutes) * time.Minute

	var allOrphans []OrphanFile

	expectedAbsent := make(map[string]struct{}, len(result.declaredOnlyRoots))
	for _, root := range result.declaredOnlyRoots {
		expectedAbsent[root] = struct{}{}
	}
	walkErrors, missingRoots := unreachableCoveredRoots(ctx, scanRoots, walkRoots, expectedAbsent, backend)

	var orphanOnlyDirs []AbandonedDir
	anyRootScanned := false
	keys := newFileKeys()

	for _, root := range walkRoots {
		if ctx.Err() != nil {
			s.markCanceled(ctx, instanceID, runID)
			return
		}

		orphans, dirs, err := walkForScope(ctx, root, protected, gracePeriod, backend, instance, scope.AbandonedDirs, keys)
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
			keys.incomplete = true
			continue
		}

		anyRootScanned = true
		allOrphans = append(allOrphans, orphans...)
		orphanOnlyDirs = append(orphanOnlyDirs, dirs...)
	}

	for _, root := range inUseRoots {
		if err := recordInUse(ctx, root, protected, backend, instance, keys); err != nil {
			if ctx.Err() != nil {
				s.markCanceled(ctx, instanceID, runID)
				return
			}
			if isMissingRoot(err) {
				log.Debug().Err(err).Str("root", root).Msg("orphanscan: ignored path is not on disk")
				continue
			}
			log.Error().Err(err).Str("root", root).Msg("orphanscan: walk error")
			walkErrors = append(walkErrors, fmt.Sprintf("%s: %v", root, err))
			keys.incomplete = true
		}
	}

	allOrphans = dropAliasOrphans(ctx, backend, instance, dedupeOrphans(d, allOrphans), keys)
	if err := s.store.UpdateRunIdentityComplete(ctx, runID, !keys.incomplete); err != nil {
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: failed to record file identity")
	}
	maxFiles := settings.MaxFilesPerRun
	if maxFiles <= 0 {
		maxFiles = DefaultSettings().MaxFilesPerRun
	}

	if scope.AbandonedDirs && len(allOrphans) <= maxFiles {
		abandoned := abandonedDirCandidates(ctx, sortDeepestFirst(orphanOnlyDirs), allOrphans, scanRoots, result.categoryPaths, protected, gracePeriod, backend)
		if len(abandoned) > 0 && models.FilesystemCapabilitiesOf(instance).Identity {
			reached, err := collectInUseIDs(ctx, backend, instance, reachedDirs(d, result, keys.fileDirs), true)
			switch {
			case ctx.Err() != nil:
				s.markCanceled(ctx, instanceID, runID)
				return
			case err != nil:
				log.Warn().Err(err).Int64("run", runID).Msg("orphanscan: listing no abandoned directory, cannot read the identity of a torrent folder")
				scanWarnings = append(scanWarnings, fmt.Sprintf("No abandoned directories are listed: could not check which folders torrents use: %v", err))
				abandoned = nil
			default:
				abandoned = dropDirsTorrentsReach(ctx, abandoned, reached, backend)
			}
		}
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

	reloaded, err := s.instances.Get(ctx, instanceID)
	if err != nil {
		if ctx.Err() != nil {
			s.markCanceled(ctx, instanceID, runID)
			return
		}
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to reload instance after the scan: %v", err))
		return
	}
	if current, endpointChanged := models.FilesystemAccessMode(reloaded), fsops.WalkedEndpoint(reloaded) != fsops.WalkedEndpoint(instance); current != mode || endpointChanged {
		log.Info().Int64("run", runID).Str("scannedWith", string(mode)).Str("now", string(current)).Bool("endpointChanged", endpointChanged).
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
	if partial {
		scanWarnings = append(scanWarnings, "Automatic cleanup is disabled for partial scans. Review the results before deleting files.")
	}
	if keys.incomplete && len(allOrphans) > 0 && mode == models.FilesystemModeRemote && models.FilesystemCapabilitiesOf(instance).Identity {
		scanWarnings = append(scanWarnings, IdentityIncompleteMessage)
	}
	scanWarning := strings.Join(scanWarnings, "\n\n")
	if partial {
		if err := s.store.UpdateRunPartial(ctx, runID, scanWarning); err != nil {
			s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to save partial scan result: %v", err))
			return
		}
	} else if scanWarning != "" {
		if err := s.store.UpdateRunWarning(ctx, runID, scanWarning); err != nil {
			s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to save scan warning: %v", err))
			return
		}
	}

	log.Info().Int("orphans", len(allOrphans)).Bool("truncated", truncated).Msg("orphanscan: scan complete")

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

	if err := s.store.InsertFiles(ctx, runID, modelFiles); err != nil {
		if ctx.Err() != nil {
			log.Info().Int64("run", runID).Msg("orphanscan: scan canceled during file insertion")
			return
		}
		log.Error().Err(err).Msg("orphanscan: failed to insert files")
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to insert files: %v", err))
		return
	}

	if err := s.store.UpdateRunFoundStats(ctx, runID, len(allOrphans), truncated, bytesFound); err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update files found")
	}

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

	if s.beforePreviewReady != nil {
		s.beforePreviewReady()
	}
	sshMu.Lock()
	if mode == models.FilesystemModeRemote && s.sshChangeCount(instanceID) != sshChanges {
		sshMu.Unlock()
		log.Info().Int64("run", runID).Msg("orphanscan: failing scan, SSH settings changed while it ran")
		s.failRun(ctx, runID, instanceID, ScanModeChangedMessage)
		return
	}
	err = s.store.UpdateRunStatus(ctx, runID, "preview_ready")
	sshMu.Unlock()
	if err != nil {
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

	filesFound := 0
	for i := range allOrphans {
		if !allOrphans[i].IsAbandonedDir {
			filesFound++
		}
	}
	s.maybeAutoCleanup(ctx, instanceID, runID, settings, filesFound)
}

func truncationLess(d fsops.PathDialect, previewSort string) func(a, b OrphanFile) bool {
	return func(a, b OrphanFile) bool {
		if a.IsAbandonedDir != b.IsAbandonedDir {
			return !a.IsAbandonedDir
		}

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
		default:
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
		key := fsops.NormalizePath(d, o.Path)
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

func (s *Service) maybeAutoCleanup(ctx context.Context, instanceID int, runID int64, settings *models.OrphanScanSettings, filesFound int) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil || run == nil {
		log.Error().Err(err).Int64("run", runID).Msg("orphanscan: failed to get run for auto-cleanup check")
		return
	}

	if run.TriggeredBy != "scheduled" {
		return
	}
	if run.Partial {
		return
	}

	if settings == nil || !settings.AutoCleanupEnabled {
		return
	}

	maxFiles := settings.AutoCleanupMaxFiles
	if maxFiles <= 0 {
		maxFiles = 100
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

	if err := s.ConfirmDeletion(ctx, instanceID, runID); err != nil {
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
			reason := AutoCleanupNoIdentityMessage
			if strings.Contains(run.ErrorMessage, IdentityIncompleteMessage) {
				reason = AutoCleanupSkippedMessage
			}
			warning := strings.TrimSpace(run.ErrorMessage + "\n\n" + reason)
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

	mu := s.getInstanceMutex(s.sshMu, instanceID)
	mu.Lock()
	if s.beforeDeletionStart != nil {
		s.beforeDeletionStart()
	}
	started, err := s.store.StartRunDeletion(ctx, runID)
	mu.Unlock()
	if err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to update run status to deleting")
		return
	}
	if !started {
		log.Info().Int64("run", runID).Msg("orphanscan: run is no longer ready for review, deleting nothing")
		return
	}
	s.emitRun(instanceID, runID)

	run, err := s.store.GetRun(ctx, runID)
	if err != nil || run == nil {
		s.failRun(ctx, runID, instanceID, "failed to get run details")
		return
	}

	if s.backendPool == nil {
		s.failRun(ctx, runID, instanceID, "backend pool not configured")
		return
	}
	deleteBackend, instance, err := s.backendPool.Require(ctx, instanceID, models.CapabilityWrite, models.CapabilityIdentity)
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
	if fsops.WalkedEndpoint(instance) != run.FilesystemEndpoint {
		log.Info().Int64("run", runID).Msg("orphanscan: refusing deletion, the SSH endpoint changed after the confirm")
		s.failRun(ctx, runID, instanceID, FilesystemModeChangedMessage)
		return
	}
	d := deleteBackend.Paths()

	settings, err := s.store.GetSettings(ctx, instanceID)
	if err != nil {
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to load settings for deletion: %v", err))
		return
	}
	var configuredIgnorePaths []string
	if settings != nil {
		configuredIgnorePaths = settings.IgnorePaths
	}

	files, err := s.store.GetFilesForDeletion(ctx, runID)
	if err != nil {
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to get files: %v", err))
		return
	}

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

	scope := scopeFromSettings(settings).withPersistedRoots(run.ScanPaths)
	scope.AbandonedDirs = true
	scope.TorrentPaths = true

	fileMapResult, err := s.buildFileMap(ctx, instance, deleteBackend, scope)
	if err != nil {
		log.Error().Err(err).Msg("orphanscan: failed to rebuild file map for deletion")
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("failed to rebuild file map: %v", err))
		return
	}

	rawIgnorePaths := scanIgnorePaths(ctx, configuredIgnorePaths, run.ScanPaths, fileMapResult, deleteBackend, instance)
	protected, err := newProtectedPaths(d, rawIgnorePaths, fileMapResult.fileMap)
	if err != nil {
		s.failRun(ctx, runID, instanceID, fmt.Sprintf("invalid ignore paths: %v", err))
		return
	}

	var filesDeleted int
	var bytesReclaimed int64

	var sshChanged bool
	checkSSH := func() bool {
		if mode != models.FilesystemModeRemote {
			return false
		}
		current, err := s.instances.Get(ctx, instanceID)
		if err != nil {
			log.Warn().Err(err).Int64("run", runID).Msg("orphanscan: stopping deletion, cannot reload the instance")
			sshChanged = true
		} else if fsops.WalkedEndpoint(current) != run.FilesystemEndpoint {
			log.Info().Int64("run", runID).Msg("orphanscan: stopping deletion, the SSH settings changed during it")
			sshChanged = true
		}
		return sshChanged
	}
	skipRest := func(entries []*models.OrphanScanFile) {
		for _, e := range entries {
			s.updateFileStatus(ctx, e.ID, "skipped", "the SSH settings changed during the deletion")
		}
	}

	if len(fileEntries) > 0 || len(dirEntries) > 0 {
		var files []string
		if len(fileEntries) > 0 {
			files = fileMapResult.torrentPaths
		}
		inUse, err := collectInUseIDs(ctx, deleteBackend, instance, files, false)
		if err == nil && len(dirEntries) > 0 {
			var dirs *inUseIDs
			if dirs, err = collectInUseIDs(ctx, deleteBackend, instance, reachedDirs(d, fileMapResult, inUse.fileDirs), true); err == nil {
				maps.Copy(inUse.keys, dirs.keys)
			}
		}
		if err != nil {
			log.Warn().Err(err).Int64("run", runID).Msg("orphanscan: stopping deletion, cannot read the identity of a torrent path")
			switch {
			case checkSSH():
				s.failRun(ctx, runID, instanceID, FilesystemModeChangedMessage)
			case errors.Is(err, fsops.ErrConnectionLost):
				s.failRun(ctx, runID, instanceID, "Deletion stopped before deleting anything: the connection to the remote instance was lost while qui checked which files torrents use. Run a new scan to try again.")
			default:
				what := "files"
				if len(dirEntries) > 0 {
					what = "files or folders"
				}
				s.failRun(ctx, runID, instanceID, fmt.Sprintf("Deletion stopped before deleting anything: could not check which %s torrents use: %v. Fix access to that path or remove the torrent, then run a new scan.", what, err))
			}
			return
		}
		protected.inUse = inUse
	}

	var failedDeletes int
	var sawReadOnly bool
	var sawPermissionDenied bool
	var sawConnectionLost bool
	classifyFailure := func(err error) {
		switch {
		case errors.Is(err, fsops.ErrConnectionLost):
			sawConnectionLost = true
		case isReadOnlyFSError(err) || strings.Contains(err.Error(), "read-only file system"):
			sawReadOnly = true
		case os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") || strings.Contains(err.Error(), "operation not permitted"):
			sawPermissionDenied = true
		}
	}

	for i, f := range fileEntries {
		if ctx.Err() != nil {
			log.Warn().Msg("orphanscan: deletion canceled mid-progress")
			break
		}
		if checkSSH() {
			skipRest(fileEntries[i:])
			break
		}

		scanRoot := findScanRoot(d, f.FilePath, run.ScanPaths)
		if scanRoot == "" {
			s.updateFileStatus(ctx, f.ID, "failed", "no matching scan root")
			failedDeletes++
			continue
		}

		disp, err := safeDeleteTarget(ctx, scanRoot, f.FilePath, protected, deleteBackend)
		if err != nil {
			s.updateFileStatus(ctx, f.ID, "failed", err.Error())
			log.Warn().Err(err).Str("path", f.FilePath).Msg("orphanscan: failed to delete target")
			failedDeletes++
			classifyFailure(err)
			continue
		}

		switch disp {
		case deleteDispositionSkippedInUse:
			s.updateFileStatus(ctx, f.ID, "skipped", "file is now in use by a torrent")
		case deleteDispositionSkippedInUseThroughAlias:
			s.updateFileStatus(ctx, f.ID, "skipped", "a torrent now uses this file through another path")
		case deleteDispositionSkippedFolderInUseThroughAlias:
			s.updateFileStatus(ctx, f.ID, "skipped", "a torrent now uses a file in this folder through another path")
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

	normScanRoots := normalizePaths(d, fileMapResult.scanRoots)
	normCategoryPaths := normalizePaths(d, fileMapResult.categoryPaths)

	if sshChanged {
		skipRest(dirEntries)
		dirEntries = nil
	}
	for i, dir := range dirEntries {
		if ctx.Err() != nil {
			break
		}
		if checkSSH() {
			skipRest(dirEntries[i:])
			break
		}

		scanRoot := findScanRoot(d, dir.FilePath, run.ScanPaths)
		if scanRoot == "" {
			s.updateFileStatus(ctx, dir.ID, "failed", "no matching scan root")
			failedDeletes++
			continue
		}
		if protected.ignored(dir.FilePath) {
			s.updateFileStatus(ctx, dir.ID, "skipped", "path is protected by ignore paths")
			continue
		}
		normDir := fsops.NormalizePath(d, dir.FilePath)
		if slices.Contains(normScanRoots, normDir) {
			s.updateFileStatus(ctx, dir.ID, "skipped", "directory is now a scan root")
			continue
		}
		if isCategoryDestinationNormalized(d, normDir, normCategoryPaths) {
			s.updateFileStatus(ctx, dir.ID, "skipped", "directory is now a category, download or monitored folder")
			continue
		}
		if protected.torrentFileIn(dir.FilePath) {
			s.updateFileStatus(ctx, dir.ID, "skipped", "directory is now used by a torrent")
			continue
		}

		disp, err := safeDeleteEmptyDir(ctx, scanRoot, dir.FilePath, protected, deleteBackend)
		if err != nil {
			s.updateFileStatus(ctx, dir.ID, "failed", err.Error())
			log.Warn().Err(err).Str("path", dir.FilePath).Msg("orphanscan: failed to delete abandoned directory")
			failedDeletes++
			classifyFailure(err)
			continue
		}

		switch disp {
		case deleteDispositionSkippedMissing:
			s.updateFileStatus(ctx, dir.ID, "skipped", "directory no longer exists")
		case deleteDispositionSkippedNotDirectory:
			s.updateFileStatus(ctx, dir.ID, "skipped", "path is no longer a directory")
		case deleteDispositionSkippedDirInUseThroughAlias:
			s.updateFileStatus(ctx, dir.ID, "skipped", "a torrent now uses this directory through another path")
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

	var failureMessage string
	remote := mode == models.FilesystemModeRemote
	if failedDeletes > 0 {
		switch {
		case sawConnectionLost:
			failureMessage = fmt.Sprintf("Deletion failed for %d item(s): the connection to the remote instance was lost. Run a new scan to list what is left.", failedDeletes)
		case sawReadOnly && !remote:
			failureMessage = fmt.Sprintf("Deletion failed for %d item(s): filesystem is read-only. If running via Docker, remove ':ro' from the volume mapping for your downloads path.", failedDeletes)
		case sawPermissionDenied && remote:
			failureMessage = fmt.Sprintf("Deletion failed for %d item(s): permission denied. Check that the account qui uses on the remote instance has write access to the download directories.", failedDeletes)
		case sawPermissionDenied:
			failureMessage = fmt.Sprintf("Deletion failed for %d item(s): permission denied. Check that the qui process has write access to the download directories.", failedDeletes)
		default:
			failureMessage = fmt.Sprintf("Deletion failed for %d item(s). Check the details for specific errors.", failedDeletes)
		}
	}
	if sshChanged {
		failureMessage = strings.TrimSpace(failureMessage + "\n\n" + FilesystemModeChangedMessage)
	}

	if (failedDeletes > 0 || sshChanged) && filesDeleted == 0 && foldersDeleted == 0 {
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
	if failureMessage != "" {
		warningMessage = strings.TrimSpace(warningMessage + "\n\n" + failureMessage)
	}

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
	nPath := fsops.NormalizePath(d, path)
	nBase := fsops.NormalizePath(d, base)
	return nPath == nBase || isPathUnderNormalized(d, nPath, nBase)
}

func linkedPeerRoots(ctx context.Context, peerRoots, scanRoots, walkRoots []string, backend fsops.Backend) []string {
	d := backend.Paths()
	realDirs := map[string]bool{}
	isRealDir := func(p string) bool {
		ok, seen := realDirs[p]
		if !seen {
			info, err := backend.Lstat(ctx, p)
			ok = err == nil && info.IsDir && !info.IsSymlink
			realDirs[p] = ok
		}
		return ok
	}
	norm := func(p string) string { return fsops.NormalizePath(d, p) }
	var linked []string
	for _, root := range peerRoots {
		if slices.Contains(scanRoots, root) {
			continue
		}
		under := func(w string) bool { return isPathUnderNormalized(d, norm(root), norm(w)) }
		if !slices.ContainsFunc(walkRoots, under) {
			continue
		}
		reached := func(w string) bool {
			nw := norm(w)
			for dir := root; dir != w && dir != w+d.Separator(); dir = d.Dir(dir) {
				if !isRealDir(dir) {
					return false
				}
				if n := norm(dir); n == nw || n == nw+d.Separator() {
					return true
				}
				if d.Dir(dir) == dir {
					return false
				}
			}
			return true
		}
		if !slices.ContainsFunc(walkRoots, func(w string) bool { return under(w) && reached(w) }) {
			linked = append(linked, root)
		}
	}
	return linked
}

func ignoredSymlinkRoots(ctx context.Context, d fsops.PathDialect, roots, kept []string, backend fsops.Backend) []string {
	var links []string
	for _, root := range roots {
		if slices.Contains(kept, d.Clean(root)) {
			continue
		}
		if info, err := backend.Lstat(ctx, root); err == nil && info.IsSymlink {
			links = append(links, root)
		}
	}
	return links
}

func filterCoveredScanRoots(d fsops.PathDialect, scanRoots, covers []string) []string {
	filtered := make([]string, 0, len(scanRoots))
	for _, root := range scanRoots {
		covered := false
		for _, cover := range covers {
			if isAbsoluteFilesystemRoot(d, cover) {
				continue
			}
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

func canonicalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}

func isAbsoluteFilesystemRoot(d fsops.PathDialect, p string) bool {
	cleaned := d.Clean(p)
	sep := d.Separator()
	if sep == "" || cleaned == "" {
		return false
	}
	if strings.HasSuffix(cleaned, sep) {
		return true
	}
	if strings.HasPrefix(cleaned, sep+sep) {
		rest := cleaned[len(sep)*2:]
		server, share, found := strings.Cut(rest, sep)
		return found && server != "" && share != "" && !strings.Contains(share, sep)
	}
	return false
}

func isSameRoot(ctx context.Context, first, second string, backend fsops.Backend, instance *models.Instance) bool {
	d := backend.Paths()
	first = d.Clean(first)
	second = d.Clean(second)
	if first == second {
		return true
	}
	if fsops.NormalizePath(d, first) != fsops.NormalizePath(d, second) {
		return false
	}
	firstInfo, firstErr := backend.Lstat(ctx, first)
	secondInfo, secondErr := backend.Lstat(ctx, second)
	return firstErr == nil && secondErr == nil && sameRootIdentity(firstInfo, secondInfo, instance)
}

func sameRootIdentity(first, second *fsops.LstatInfo, instance *models.Instance) bool {
	return first.FileIDErr == nil && second.FileIDErr == nil &&
		fsops.SameFile(first.FileID, instance, second.FileID, instance)
}

func metadataIgnoreRoots(ctx context.Context, scanRoots, metadataRoots []string, backend fsops.Backend, instance *models.Instance) []string {
	d := backend.Paths()
	trusted := instance == nil || models.FilesystemCapabilitiesOf(instance).TrustedIdentity
	ignored := make([]string, 0, len(metadataRoots))
	for _, metadataRoot := range metadataRoots {
		normalizedMetadataRoot := fsops.NormalizePath(d, metadataRoot)
		nested := false
		for _, scanRoot := range scanRoots {
			normalizedScanRoot := fsops.NormalizePath(d, scanRoot)
			if d.Clean(metadataRoot) == d.Clean(scanRoot) || (trusted && isSameRoot(ctx, metadataRoot, scanRoot, backend, instance)) {
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

func scanIgnorePaths(ctx context.Context, configured, scanRoots []string, result *buildFileMapResult, backend fsops.Backend, instance *models.Instance) []string {
	ignored := slices.Concat(configured, result.skippedRoots, result.transferFolders)
	return append(ignored, metadataIgnoreRoots(ctx, scanRoots, result.metadataRoots, backend, instance)...)
}

func dedupeCaseVariantRoots(ctx context.Context, roots []string, backend fsops.Backend, instance *models.Instance) []string {
	if len(roots) < 2 {
		return roots
	}

	d := backend.Paths()
	kept := make([]string, 0, len(roots))
	seen := make(map[string]*fsops.LstatInfo, len(roots))
	exact := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		cleaned := d.Clean(root)
		if _, ok := exact[cleaned]; ok {
			log.Trace().Str("root", root).Msg("orphanscan: dropped scan root that repeats an earlier root")
			continue
		}
		exact[cleaned] = struct{}{}

		norm := fsops.NormalizePath(d, root)
		info, _ := backend.Lstat(ctx, root)

		if prev, ok := seen[norm]; ok && prev != nil && info != nil && sameRootIdentity(prev, info, instance) {
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

type buildFileMapResult struct {
	fileMap           *TorrentFileMap
	scanRoots         []string
	skippedRoots      []string
	metadataRoots     []string
	categoryPaths     []string
	declaredOnlyRoots []string
	torrentCount      int
	torrentPaths      []string
	peerRoots         []string
	transferFolders   []string
}

func (r *buildFileMapResult) mergeProtection(d fsops.PathDialect, other *buildFileMapResult) int {
	added := r.fileMap.MergeFrom(other.fileMap)
	r.torrentPaths = append(r.torrentPaths, other.torrentPaths...)
	r.peerRoots = mergeRootLists(d, r.peerRoots, other.scanRoots)
	r.skippedRoots = mergeRootLists(d, r.skippedRoots, other.skippedRoots)
	r.metadataRoots = mergeRootLists(d, r.metadataRoots, other.metadataRoots)
	r.transferFolders = mergeRootLists(d, r.transferFolders, other.transferFolders)
	r.scanRoots = filterCoveredScanRoots(d, r.scanRoots, r.skippedRoots)
	return added
}

func buildFileMapFromTorrents(d fsops.PathDialect, torrents []*qbt.Torrent, filesByHash map[string]qbt.TorrentFiles, wantPaths bool) (*buildFileMapResult, error) {
	tfm := NewTorrentFileMap(d)
	scanRoots := make(map[string]struct{})
	skippedRoots := make(map[string]struct{})
	metadataRoots := make(map[string]struct{})
	stableMissingFiles := 0
	var torrentPaths []string

	for _, torrent := range torrents {
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

		add := func(root string) {
			scanRoots[root] = struct{}{}
			for _, f := range files {
				p := d.Join(root, d.FromSlash(f.Name))
				if wantPaths {
					torrentPaths = append(torrentPaths, p)
				}
				tfm.Add(fsops.NormalizePath(d, p))
			}
		}
		add(savePath)

		actualSavePath := actualSavePathFromContentPath(d, savePath, torrent.ContentPath, files)
		if actualSavePath != "" && actualSavePath != savePath {
			add(actualSavePath)
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
		torrentPaths:  torrentPaths,
	}, nil
}

func (s *Service) getOverlapCandidateInstances(ctx context.Context, scanned *models.Instance, scannedPin []byte) ([]*models.Instance, error) {
	instances, err := s.instances.List(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]*models.Instance, 0, len(instances))
	for _, inst := range instances {
		if inst == nil || inst.ID == scanned.ID || !inst.IsActive {
			continue
		}
		if _, ok := s.overlapPeer(scanned, scannedPin, inst); ok {
			candidates = append(candidates, inst)
		}
	}
	return candidates, nil
}

func (s *Service) overlapPeer(scanned *models.Instance, scannedPin []byte, inst *models.Instance) (label string, ok bool) {
	switch {
	case inst.HasLocalFilesystemAccess && models.FilesystemAccessMode(scanned) != models.FilesystemModeRemote:
		return "local-access instance", true
	case fsops.SameSSHServer(scanned, scannedPin, inst, s.hostKeyPin):
		return "remote instance on the same host", true
	}
	return "", false
}

func (s *Service) hostKeyPin(inst *models.Instance) []byte {
	pin, err := s.instances.GetHostKeyPin(inst)
	if err != nil && !errors.Is(err, models.ErrSSHHostKeyNotPinned) {
		log.Warn().Err(err).Int("instance", inst.ID).Msg("orphanscan: cannot read the host key pin, pairing peers by host and port only")
	}
	return pin
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

	torrents, err := s.sync.GetTorrentRefsFresh(ctx, instanceID, qbittorrent.TorrentRefFilter{})
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}
	torrents = s.leaveOutHelpers(instanceID, torrents)

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

func (s *Service) buildInstanceFileMap(ctx context.Context, instanceID int, scanned *models.Instance, timeout time.Duration, backend fsops.Backend, wantPaths bool) (*buildFileMapResult, error) {
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

	transferFolders, err := s.runningTransferFolders(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	torrents, err := s.sync.GetTorrentRefsFresh(ctx, instanceID, qbittorrent.TorrentRefFilter{})
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}
	torrents = s.leaveOutHelpers(instanceID, torrents)

	filesCtx := qbittorrent.WithForceFilesRefresh(ctx)
	filesByHash, err := s.sync.GetTorrentFilesBatch(filesCtx, instanceID, torrentHashes(torrents))
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent files: %w", err)
	}

	result, err := buildFileMapFromTorrents(backend.Paths(), torrents, filesByHash, wantPaths)
	if err != nil {
		return nil, err
	}
	result.transferFolders = transferFolders
	result.scanRoots = dedupeCaseVariantRoots(ctx, result.scanRoots, backend, scanned)
	return result, nil
}

func (s *Service) buildFileMap(ctx context.Context, scanned *models.Instance, backend fsops.Backend, scope scanScope) (*buildFileMapResult, error) {
	instanceID := scanned.ID
	d := backend.Paths()
	result, err := s.buildInstanceFileMap(ctx, instanceID, scanned, 5*time.Minute, backend, scope.TorrentPaths)
	if err != nil {
		return nil, err
	}

	extraRoots, categoryPaths, err := s.declaredScanRoots(ctx, d, instanceID, scope)
	if err != nil {
		return nil, err
	}
	result.categoryPaths = categoryPaths
	result.declaredOnlyRoots = rootsNoTorrentPointsAt(d, extraRoots, result.scanRoots)
	extraRoots = append(extraRoots, scope.PersistedRoots...)
	if len(extraRoots) > 0 {
		result.scanRoots = dedupeCaseVariantRoots(ctx, append(result.scanRoots, extraRoots...), backend, scanned)
	}

	scannedPin := s.hostKeyPin(scanned)
	candidates, err := s.getOverlapCandidateInstances(ctx, scanned, scannedPin)
	if err != nil {
		return nil, err
	}
	for _, inst := range candidates {
		peer, _ := s.overlapPeer(scanned, scannedPin, inst)
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

		otherResult, err := s.buildInstanceFileMap(ctx, inst.ID, scanned, 2*time.Minute, backend, scope.TorrentPaths)
		if err != nil {
			return nil, fmt.Errorf("overlapping %s unavailable (id=%d name=%q): %w", peer, inst.ID, inst.Name, err)
		}

		added := result.mergeProtection(d, otherResult)
		log.Debug().
			Int("instance", inst.ID).
			Int("filesAdded", added).
			Str("rootsSource", source).
			Msg("orphanscan: merged cross-instance torrent files (overlapping scan roots)")
	}

	if err := s.mergeRouted(ctx, instanceID, d, scope.TorrentPaths, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) mergeRouted(ctx context.Context, instanceID int, d fsops.PathDialect, wantPaths bool, result *buildFileMapResult) error {
	if s.crossSeedRoutes == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	routed, err := s.crossSeedRoutes.LoadRouted(ctx, freshTorrents{s.sync}, instanceID)
	switch {
	case errors.Is(err, crossseedroute.ErrUnavailable):
		return fmt.Errorf("%w. Enable it in qui or clear the cross-seed route of this instance", crossseedroute.CrossSeedUnavailable(err))
	case err != nil:
		return fmt.Errorf("read the cross-seed route: %w", err)
	case routed == nil:
		return nil
	}
	routedRefs := make([]*qbt.Torrent, len(routed.Torrents))
	for i := range routed.Torrents {
		routedRefs[i] = &routed.Torrents[i]
	}
	other, err := buildFileMapFromTorrents(d, routedRefs, routed.Files, wantPaths)
	if err != nil {
		return fmt.Errorf("routed torrents of the Cross-seed instance: %w", err)
	}
	added := result.mergeProtection(d, other)
	log.Debug().Int("instance", instanceID).Int("filesAdded", added).Msg("orphanscan: merged routed torrent files of the Cross-seed instance")
	return nil
}

type freshTorrents struct{ sync syncReader }

func (r freshTorrents) GetAllTorrentRefs(ctx context.Context, instanceID int) ([]*qbt.Torrent, error) {
	return r.sync.GetTorrentRefsFresh(ctx, instanceID, qbittorrent.TorrentRefFilter{})
}

func (r freshTorrents) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	return r.sync.GetTorrentFilesBatch(qbittorrent.WithForceFilesRefresh(ctx), instanceID, hashes)
}

func torrentHashes(torrents []*qbt.Torrent) []string {
	hashes := make([]string, 0, len(torrents))
	for _, torrent := range torrents {
		hashes = append(hashes, torrent.Hash)
	}
	return hashes
}

func findScanRoot(d fsops.PathDialect, path string, scanRoots []string) string {
	nPath := fsops.NormalizePath(d, path)

	longest := ""
	for _, root := range scanRoots {
		nRoot := fsops.NormalizePath(d, root)
		if nPath != nRoot && isAbsoluteFilesystemRoot(d, nRoot) {
			continue
		}
		if nPath != nRoot && !isPathUnderNormalized(d, nPath, nRoot) {
			continue
		}
		if len(root) > len(longest) {
			longest = root
		}
	}
	return longest
}
