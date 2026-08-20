// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/pkg/torrentname"
)

var (
	// ErrInstanceBusy is returned when a backup is already running for the instance.
	ErrInstanceBusy = errors.New("backup already running for this instance")
	removeFile      = os.Remove
)

// Config controls background backup scheduling.
type Config struct {
	DataDir         string
	BackupDir       string // backup root; empty means <DataDir>/backups
	PollInterval    time.Duration
	WorkerCount     int
	FailureCooldown time.Duration
	ExportThrottle  time.Duration
}

type BackupProgress struct {
	Current    int
	Total      int
	Percentage float64
}

type missingTorrent struct {
	hash    string
	name    string
	relPath string
	absPath string
}

type Service struct {
	store          *models.BackupStore
	reader         backupReader
	tracker        backupTrackerSource
	categoryWriter backupCategoryMutator
	tagWriter      backupTagMutator
	torrentWriter  backupTorrentMutator
	jackettSvc     *jackett.Service
	notifier       notifications.Notifier
	cfg            Config
	root           string // backup root directory; stored paths resolve against it
	cacheDir       string

	jobs   chan job
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once

	inflight   map[int]int64
	inflightMu sync.Mutex

	progress   map[int64]*BackupProgress
	progressMu sync.RWMutex

	now func() time.Time

	activityPublisher activity.Publisher
}

type backupReader interface {
	GetAllTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error)
	GetCategories(ctx context.Context, instanceID int) (map[string]qbt.Category, error)
	GetTags(ctx context.Context, instanceID int) ([]string, error)
	GetInstanceWebAPIVersion(ctx context.Context, instanceID int) (string, error)
	ExportTorrent(ctx context.Context, instanceID int, hash string) ([]byte, string, string, error)
}

type backupCategoryMutator interface {
	CreateCategory(ctx context.Context, instanceID int, name string, path string) error
	EditCategory(ctx context.Context, instanceID int, name string, path string) error
	RemoveCategories(ctx context.Context, instanceID int, categories []string) error
}

type backupTagMutator interface {
	CreateTags(ctx context.Context, instanceID int, tags []string) error
	DeleteTags(ctx context.Context, instanceID int, tags []string) error
}

type backupTorrentMutator interface {
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error)
	SetCategory(ctx context.Context, instanceID int, hashes []string, category string) error
	SetTags(ctx context.Context, instanceID int, hashes []string, tags string) error
	ResumeWhenComplete(instanceID int, hashes []string, opts qbittorrent.ResumeWhenCompleteOptions)
	BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error
}

type job struct {
	runID      int64
	instanceID int
	kind       models.BackupRunKind
}

// Manifest captures details about a backup run and its contents for API responses and archived metadata.
type Manifest struct {
	InstanceID   int                                `json:"instanceId"`
	Kind         string                             `json:"kind"`
	GeneratedAt  time.Time                          `json:"generatedAt"`
	TorrentCount int                                `json:"torrentCount"`
	Categories   map[string]models.CategorySnapshot `json:"categories,omitempty"`
	Tags         []string                           `json:"tags,omitempty"`
	Items        []ManifestItem                     `json:"items"`
}

// ManifestItem describes a single torrent contained in a backup archive.
type ManifestItem struct {
	Hash        string   `json:"hash"`
	Name        string   `json:"name"`
	Category    *string  `json:"category,omitempty"`
	SizeBytes   int64    `json:"sizeBytes"`
	ArchivePath string   `json:"archivePath"`
	InfoHashV1  *string  `json:"infohashV1,omitempty"`
	InfoHashV2  *string  `json:"infohashV2,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	TorrentBlob string   `json:"torrentBlob,omitempty"`
	SavePath    string   `json:"savePath,omitempty"`
}

func NewService(store *models.BackupStore, reader backupReader, jackettSvc any, cfg Config, notifier notifications.Notifier) *Service {
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 1
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Minute
	}
	if cfg.FailureCooldown <= 0 {
		cfg.FailureCooldown = 10 * time.Minute
	}
	if cfg.ExportThrottle <= 0 {
		cfg.ExportThrottle = 100 * time.Millisecond
	}

	root := strings.TrimSpace(cfg.BackupDir)
	if root == "" && strings.TrimSpace(cfg.DataDir) != "" {
		root = filepath.Join(cfg.DataDir, "backups")
	}

	cacheDir := ""
	if root != "" {
		cacheDir = filepath.Join(root, "torrents")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			log.Warn().Err(err).Str("cacheDir", cacheDir).Msg("Failed to prepare torrent cache directory")
		} else {
			go sweepStaleBlobTemps(cacheDir)
		}
	}

	var jackettService *jackett.Service
	if svc, ok := jackettSvc.(*jackett.Service); ok {
		_ = svc // TODO: jackettService = svc when jackett fallback is re-enabled
	}

	svc := &Service{
		store:      store,
		reader:     reader,
		jackettSvc: jackettService,
		notifier:   notifier,
		cfg:        cfg,
		root:       root,
		cacheDir:   cacheDir,
		jobs:       make(chan job, cfg.WorkerCount*2),
		inflight:   make(map[int]int64),
		progress:   make(map[int64]*BackupProgress),
		now:        func() time.Time { return time.Now().UTC() },

		activityPublisher: activity.NopPublisher{},
	}
	if tracker, ok := reader.(backupTrackerSource); ok {
		svc.tracker = tracker
	}
	if writer, ok := reader.(backupCategoryMutator); ok {
		svc.categoryWriter = writer
	}
	if writer, ok := reader.(backupTagMutator); ok {
		svc.tagWriter = writer
	}
	if writer, ok := reader.(backupTorrentMutator); ok {
		svc.torrentWriter = writer
	}

	return svc
}

// SetActivityPublisher wires the qui server-event hub so backup run status
// changes are pushed to connected clients instead of polled. Safe to call once
// at startup.
func (s *Service) SetActivityPublisher(publisher activity.Publisher) {
	if s == nil || publisher == nil {
		return
	}
	s.activityPublisher = publisher
}

// emitRunActivity signals connected clients that a backup run's status changed
// so they refetch instead of polling. Must be called after the state transition
// is persisted and any held lock released.
func (s *Service) emitRunActivity(instanceID int, runID int64) {
	if s == nil || s.activityPublisher == nil {
		return
	}
	s.activityPublisher.Publish(activity.Event{
		Kind:       activity.KindBackupRun,
		InstanceID: instanceID,
		ResourceID: strconv.FormatInt(runID, 10),
	})
}

func normalizeBackupSettings(settings *models.BackupSettings) bool {
	if settings == nil {
		return false
	}

	changed := false

	if settings.CustomPath != nil {
		settings.CustomPath = nil
		changed = true
	}

	if settings.KeepHourly < 0 {
		settings.KeepHourly = 0
		changed = true
	}
	if settings.KeepDaily < 0 {
		settings.KeepDaily = 0
		changed = true
	}
	if settings.KeepWeekly < 0 {
		settings.KeepWeekly = 0
		changed = true
	}
	if settings.KeepMonthly < 0 {
		settings.KeepMonthly = 0
		changed = true
	}
	if settings.HourlyEnabled && settings.KeepHourly < 1 {
		settings.KeepHourly = 1
		changed = true
	}
	if settings.DailyEnabled && settings.KeepDaily < 1 {
		settings.KeepDaily = 1
		changed = true
	}
	if settings.WeeklyEnabled && settings.KeepWeekly < 1 {
		settings.KeepWeekly = 1
		changed = true
	}
	if settings.MonthlyEnabled && settings.KeepMonthly < 1 {
		settings.KeepMonthly = 1
		changed = true
	}

	return changed
}

func (s *Service) normalizeAndPersistSettings(ctx context.Context, settings *models.BackupSettings) bool {
	if settings == nil {
		return false
	}

	changed := normalizeBackupSettings(settings)
	if !changed {
		return false
	}

	if err := s.store.UpsertSettings(ctx, settings); err != nil {
		log.Warn().Err(err).Int("instanceID", settings.InstanceID).Msg("Failed to persist normalized backup settings")
	}

	return true
}

func (s *Service) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.ctx = ctx
	s.cancel = cancel

	// Recover any incomplete backup runs from previous session
	if err := s.recoverIncompleteRuns(ctx); err != nil {
		log.Warn().Err(err).Msg("Failed to recover incomplete backup runs")
	}

	// Reclaim cache blobs stranded by failed runs before workers start
	// writing, but off the startup path so a large cache cannot hold up the
	// HTTP listener. Workers only spawn once the sweep finishes; a canceled
	// ctx stops the sweep mid-walk, and the pre-registered wg count plus the
	// tracked sweep goroutine keep Stop waiting for both.
	s.wg.Add(s.cfg.WorkerCount)
	s.wg.Go(func() {
		s.cleanupOrphanedBlobs(ctx)
		for i := 0; i < s.cfg.WorkerCount; i++ {
			go s.worker(ctx)
		}
	})

	// Check for missed backups and queue exactly one if applicable
	s.wg.Go(func() {
		if err := s.checkMissedBackups(ctx); err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				log.Debug().Msg("Missed-backup check canceled")
			} else {
				log.Warn().Err(err).Msg("Failed to check for missed backups")
			}
		}
	})

	s.wg.Add(1)
	go s.scheduler(ctx)
}

// recoverIncompleteRuns marks any pending or running backup runs as failed.
// This handles the case where qui was restarted while backups were in progress.
func (s *Service) recoverIncompleteRuns(ctx context.Context) error {
	incompleteRuns, err := s.store.FindIncompleteRuns(ctx)
	if err != nil {
		return fmt.Errorf("failed to find incomplete runs: %w", err)
	}

	if len(incompleteRuns) == 0 {
		return nil
	}

	log.Info().Int("count", len(incompleteRuns)).Msg("Recovering incomplete backup runs from previous session")

	now := s.now()
	errorMsg := "Backup interrupted by application restart"

	// Collect all run IDs to update
	runIDs := make([]int64, len(incompleteRuns))
	for i, run := range incompleteRuns {
		runIDs[i] = run.ID
	}

	// Process runIDs in chunks to avoid SQLite bind parameter limits
	const chunkSize = 1000
	totalChunks := (len(runIDs) + chunkSize - 1) / chunkSize

	for i := 0; i < len(runIDs); i += chunkSize {
		end := min(i+chunkSize, len(runIDs))
		chunk := runIDs[i:end]
		chunkNum := (i / chunkSize) + 1

		log.Debug().
			Int("chunk", chunkNum).
			Int("total_chunks", totalChunks).
			Int("chunk_size", len(chunk)).
			Msg("Updating backup run status chunk")

		err = s.store.UpdateMultipleRunsStatus(ctx, chunk, models.BackupRunStatusFailed, &now, &errorMsg)
		if err != nil {
			return fmt.Errorf("failed to update incomplete runs (chunk %d/%d): %w", chunkNum, totalChunks, err)
		}
	}

	log.Info().Int("count", len(incompleteRuns)).Msg("Successfully recovered incomplete backup runs")

	// Notify connected clients that these runs transitioned to failed.
	for _, run := range incompleteRuns {
		s.emitRunActivity(run.InstanceID, run.ID)
	}
	return nil
}

func (s *Service) isBackupMissed(ctx context.Context, instanceID int, kind models.BackupRunKind, enabled bool, now time.Time) bool {
	if !enabled {
		return false
	}

	// We only consider the most recent successful run as the reference point. Failed/running/pending
	// runs do not count toward the schedule — i.e. a failed run doesn't reset the schedule.
	runs, err := s.store.ListRunsByKind(ctx, instanceID, kind, 10)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to list runs for missed backup check")
		}
		// On DB error treat as not missed to avoid accidental scheduling
		return false
	}

	// Short-circuit if the latest run is still in flight or within failure cooldown.
	for _, r := range runs {
		if r == nil {
			continue
		}
		switch r.Status {
		case models.BackupRunStatusPending, models.BackupRunStatusRunning:
			return false
		case models.BackupRunStatusFailed, models.BackupRunStatusCanceled:
			if s.cfg.FailureCooldown > 0 {
				ref := r.CompletedAt
				if ref == nil {
					ref = &r.RequestedAt
				}
				if ref != nil && now.Before(ref.Add(s.cfg.FailureCooldown)) {
					return false
				}
			}
		case models.BackupRunStatusSuccess:
			// Success is handled below when selecting schedule reference.
		}
		break
	}

	// Find the most recent successful run
	var refTime *time.Time
	var foundSuccess bool
	for _, r := range runs {
		if r == nil {
			continue
		}
		if strings.EqualFold(string(r.Status), string(models.BackupRunStatusSuccess)) {
			if r.CompletedAt != nil {
				refTime = r.CompletedAt
			} else {
				refTime = &r.RequestedAt
			}
			foundSuccess = true
			break
		}
	}

	// If we found no successful run, consider it missed (first-run semantics)
	if !foundSuccess || refTime == nil {
		return true
	}

	ref := *refTime

	var interval time.Duration
	switch kind {
	case models.BackupRunKindHourly:
		interval = time.Hour
	case models.BackupRunKindDaily:
		interval = 24 * time.Hour
	case models.BackupRunKindWeekly:
		interval = 7 * 24 * time.Hour
	case models.BackupRunKindMonthly:
		next := ref.AddDate(0, 1, 0)
		return !now.Before(next)
	default:
		// Unknown kind — don't consider it missed
		return false
	}

	return !ref.Add(interval).After(now)
}

func (s *Service) checkMissedBackups(ctx context.Context) error {
	settings, err := s.store.ListEnabledSettings(ctx)
	if err != nil {
		return err
	}

	now := s.now()

	for _, cfg := range settings {
		s.normalizeAndPersistSettings(ctx, cfg)

		if !cfg.Enabled {
			continue
		}

		var missedKinds []models.BackupRunKind

		if s.isBackupMissed(ctx, cfg.InstanceID, models.BackupRunKindHourly, cfg.HourlyEnabled, now) {
			missedKinds = append(missedKinds, models.BackupRunKindHourly)
		}
		if s.isBackupMissed(ctx, cfg.InstanceID, models.BackupRunKindDaily, cfg.DailyEnabled, now) {
			missedKinds = append(missedKinds, models.BackupRunKindDaily)
		}
		if s.isBackupMissed(ctx, cfg.InstanceID, models.BackupRunKindWeekly, cfg.WeeklyEnabled, now) {
			missedKinds = append(missedKinds, models.BackupRunKindWeekly)
		}
		if s.isBackupMissed(ctx, cfg.InstanceID, models.BackupRunKindMonthly, cfg.MonthlyEnabled, now) {
			missedKinds = append(missedKinds, models.BackupRunKindMonthly)
		}

		// Queue the first missed backup if any are missed
		if len(missedKinds) > 0 {
			kind := missedKinds[0]
			if _, err := s.QueueRun(ctx, cfg.InstanceID, kind, "startup-recovery"); err != nil {
				if !errors.Is(err, ErrInstanceBusy) {
					log.Warn().Err(err).Int("instanceID", cfg.InstanceID).Str("kind", string(kind)).Msg("Failed to queue missed backup on startup")
				}
			} else {
				log.Info().Int("instanceID", cfg.InstanceID).Str("kind", string(kind)).Msg("Queued missed backup on startup")
			}
		}
	}

	return nil
}

func (s *Service) Stop() {
	s.once.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		s.wg.Wait()
	})
}

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.jobs:
			s.handleJob(ctx, job)
		}
	}
}

func (s *Service) handleJob(ctx context.Context, j job) {
	if s.reader == nil {
		now := s.now()
		msg := "sync manager not configured"
		_ = s.store.UpdateRunMetadata(ctx, j.runID, func(run *models.BackupRun) error {
			run.Status = models.BackupRunStatusFailed
			run.CompletedAt = &now
			run.ErrorMessage = &msg
			return nil
		})
		s.clearInstance(j.instanceID, j.runID)
		log.Error().Int("instanceID", j.instanceID).Msg("Backup run failed: sync manager not configured")
		s.notify(ctx, notifications.Event{
			Type:         notifications.EventBackupFailed,
			InstanceID:   j.instanceID,
			BackupKind:   j.kind,
			BackupRunID:  j.runID,
			ErrorMessage: msg,
			CompletedAt:  &now,
		})
		s.emitRunActivity(j.instanceID, j.runID)
		return
	}

	start := s.now()
	err := s.store.UpdateRunMetadata(ctx, j.runID, func(run *models.BackupRun) error {
		run.Status = models.BackupRunStatusRunning
		run.ErrorMessage = nil
		run.StartedAt = &start
		return nil
	})
	if err != nil {
		s.clearInstance(j.instanceID, j.runID)
		log.Error().Err(err).Int("instanceID", j.instanceID).Msg("Failed to mark backup run as running")
		return
	}
	s.emitRunActivity(j.instanceID, j.runID)

	result, execErr := s.executeBackup(ctx, j)
	if execErr != nil {
		msg := execErr.Error()
		now := s.now()
		_ = s.store.UpdateRunMetadata(ctx, j.runID, func(run *models.BackupRun) error {
			run.Status = models.BackupRunStatusFailed
			run.CompletedAt = &now
			run.ErrorMessage = &msg
			return nil
		})
		log.Error().Err(execErr).Int("instanceID", j.instanceID).Int64("runID", j.runID).Msg("Backup run failed")
		s.notify(ctx, notifications.Event{
			Type:         notifications.EventBackupFailed,
			InstanceID:   j.instanceID,
			BackupKind:   j.kind,
			BackupRunID:  j.runID,
			ErrorMessage: msg,
			StartedAt:    &start,
			CompletedAt:  &now,
		})
		s.emitRunActivity(j.instanceID, j.runID)
	} else {
		now := s.now()
		_ = s.store.UpdateRunMetadata(ctx, j.runID, func(run *models.BackupRun) error {
			run.Status = models.BackupRunStatusSuccess
			run.CompletedAt = &now
			if result.manifestRelPath != nil {
				run.ManifestPath = result.manifestRelPath
			}
			run.TotalBytes = result.totalBytes
			run.TorrentCount = result.torrentCount
			run.CategoryCounts = result.categoryCounts
			run.Categories = result.categories
			run.Tags = result.tags
			run.ErrorMessage = nil
			return nil
		})

		if len(result.items) > 0 {
			if err := s.store.InsertItems(ctx, j.runID, result.items); err != nil {
				log.Warn().Err(err).Int64("runID", j.runID).Msg("Failed to persist backup manifest items")
			}
		}

		if result.settings != nil {
			if err := s.applyRetention(ctx, j.instanceID, result.settings); err != nil {
				log.Warn().Err(err).Int("instanceID", j.instanceID).Msg("Failed to apply backup retention")
			}
		}
		s.notify(ctx, notifications.Event{
			Type:               notifications.EventBackupSucceeded,
			InstanceID:         j.instanceID,
			BackupKind:         j.kind,
			BackupRunID:        j.runID,
			BackupTorrentCount: result.torrentCount,
			StartedAt:          &start,
			CompletedAt:        &now,
		})
		s.emitRunActivity(j.instanceID, j.runID)
	}

	s.clearInstance(j.instanceID, j.runID)
}

func (s *Service) notify(ctx context.Context, event notifications.Event) {
	if s == nil || s.notifier == nil {
		return
	}
	s.notifier.Notify(ctx, event)
}

type backupResult struct {
	manifestRelPath *string
	totalBytes      int64
	torrentCount    int
	categoryCounts  map[string]int
	items           []models.BackupItem
	settings        *models.BackupSettings
	categories      map[string]models.CategorySnapshot
	tags            []string
}

func shouldSkipLiveExportForBackup(torrent qbt.Torrent, hasCachedBlob bool, cacheErr error) bool {
	if hasCachedBlob || cacheErr != nil {
		return false
	}

	return strings.TrimSpace(torrent.InfohashV1) != "" && strings.TrimSpace(torrent.InfohashV2) != ""
}

func (s *Service) executeBackup(ctx context.Context, j job) (*backupResult, error) {
	settings, err := s.store.GetSettings(ctx, j.instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load backup settings: %w", err)
	}
	s.normalizeAndPersistSettings(ctx, settings)

	torrents, err := s.reader.GetAllTorrents(ctx, j.instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load torrents: %w", err)
	}

	if len(torrents) == 0 {
		return &backupResult{torrentCount: 0, totalBytes: 0, categoryCounts: map[string]int{}, items: nil, settings: settings}, nil
	}

	baseAbs, baseRel, err := s.resolveBasePaths(ctx, settings, j.instanceID)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(baseAbs, 0o755); err != nil {
		return nil, fmt.Errorf("failed to prepare backup directory: %w", err)
	}

	var snapshotCategories map[string]models.CategorySnapshot
	if settings.IncludeCategories {
		categories, err := s.reader.GetCategories(ctx, j.instanceID)
		if err != nil {
			return nil, fmt.Errorf("failed to load categories: %w", err)
		}
		if len(categories) > 0 {
			snapshotCategories = make(map[string]models.CategorySnapshot, len(categories))
			for name, cat := range categories {
				snapshotCategories[name] = models.CategorySnapshot{SavePath: strings.TrimSpace(cat.SavePath)}
			}
		}
	}

	var snapshotTags []string
	if settings.IncludeTags {
		tags, err := s.reader.GetTags(ctx, j.instanceID)
		if err != nil {
			return nil, fmt.Errorf("failed to load tags: %w", err)
		}
		if len(tags) > 0 {
			snapshotTags = append(snapshotTags, tags...)
		}
	}
	if len(snapshotTags) > 1 {
		sort.Strings(snapshotTags)
	}

	webAPIVersion := ""
	patchTrackers := false
	if version, err := s.reader.GetInstanceWebAPIVersion(ctx, j.instanceID); err != nil {
		log.Debug().Err(err).Int("instanceID", j.instanceID).Msg("Unable to determine qBittorrent API version for tracker patching")
	} else {
		webAPIVersion = version
		patchTrackers = shouldInjectTrackerMetadata(version)
	}

	timestamp := s.now().UTC().Format("20060102T150405Z")
	baseSegment := filepath.Base(baseRel)
	baseSegment = strings.TrimSpace(baseSegment)
	if baseSegment == "" || baseSegment == "." || baseSegment == string(filepath.Separator) {
		baseSegment = fmt.Sprintf("instance-%d", j.instanceID)
	}

	slug := safeSegment(baseSegment)
	if slug == "" || slug == "uncategorized" {
		slug = fmt.Sprintf("instance-%d", j.instanceID)
	}

	manifestFileName := fmt.Sprintf("qui-backup_%s_%s_%s_manifest.json", slug, j.kind, timestamp)
	manifestAbsPath := filepath.Join(baseAbs, manifestFileName)
	manifestRelPath := filepath.Join(baseRel, manifestFileName)

	items := make([]models.BackupItem, 0, len(torrents))
	manifestItems := make([]ManifestItem, 0, len(torrents))
	usedPaths := make(map[string]int)
	categoryCounts := make(map[string]int)
	var totalBytes int64

	// Initialize progress tracking
	s.progressMu.Lock()
	s.progress[j.runID] = &BackupProgress{
		Total:      len(torrents),
		Current:    0,
		Percentage: 0,
	}
	s.progressMu.Unlock()

	// Exports run concurrently: qBittorrent serves its WebAPI from a single
	// thread, so a handful of workers lets fast exports drain while one waits
	// out a stall behind another client's large response. Results land in a
	// slice indexed by input position; all order-dependent bookkeeping
	// (usedPaths, categoryCounts, items) happens afterwards in input order so
	// the output is identical to a serial run.
	results := make([]exportedTorrent, len(torrents))
	var exportedCount atomic.Int64

	g, gctx := errgroup.WithContext(ctx)
	indexes := make(chan int)
	g.Go(func() error {
		defer close(indexes)
		for i := range torrents {
			select {
			case indexes <- i:
			case <-gctx.Done():
				return gctx.Err()
			}
		}
		return nil
	})
	for range exportWorkers {
		g.Go(func() error {
			// Per-worker adaptive delay keeps the aggregate request rate
			// bounded by exportWorkers regardless of how the pool schedules.
			var lastExportElapsed time.Duration
			for idx := range indexes {
				res, err := s.exportBackupTorrent(gctx, j, torrents[idx], patchTrackers, webAPIVersion, &lastExportElapsed)
				if err != nil {
					return err
				}
				results[idx] = res
				s.updateProgress(j.runID, int(exportedCount.Add(1)))
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	for idx, torrent := range torrents {
		res := results[idx]
		if res.skipped {
			continue
		}

		category := strings.TrimSpace(torrent.Category)
		var categoryPtr *string
		if category != "" {
			categoryPtr = &category
			categoryCounts[category]++
		} else {
			categoryCounts["(uncategorized)"]++
		}

		rawTags := ""
		if settings.IncludeTags {
			rawTags = strings.TrimSpace(torrent.Tags)
		}

		archivePath := res.filename
		if settings.IncludeCategories && category != "" {
			archivePath = filepath.ToSlash(filepath.Join(safeSegment(category), res.filename))
		}

		uniquePath := ensureUniquePath(archivePath, usedPaths)
		blobRelPath := res.blobRelPath

		totalBytes += int64(res.dataLen)

		infohashV1 := strings.TrimSpace(torrent.InfohashV1)
		infohashV2 := strings.TrimSpace(torrent.InfohashV2)

		// Capture the per-torrent save path only when it diverges from the
		// category (cross-seed hardlinks, manual relocations, Auto TMM off).
		// Category-managed torrents store nothing here and are placed by their
		// recreated category on restore.
		storeSavePath := ""
		if settings.IncludeSavePaths {
			storeSavePath = resolveBackupSavePath(torrent.SavePath, category, snapshotCategories)
		}

		item := models.BackupItem{
			RunID:       j.runID,
			TorrentHash: torrent.Hash,
			Name:        torrent.Name,
			SizeBytes:   torrent.TotalSize,
		}
		if categoryPtr != nil {
			item.Category = categoryPtr
		}
		if uniquePath != "" {
			rel := uniquePath
			item.ArchiveRelPath = &rel
		}
		if infohashV1 != "" {
			item.InfoHashV1 = &infohashV1
		}
		if infohashV2 != "" {
			item.InfoHashV2 = &infohashV2
		}
		if rawTags != "" {
			item.Tags = &rawTags
		}
		if blobRelPath != nil {
			item.TorrentBlobPath = blobRelPath
		}
		if storeSavePath != "" {
			sp := storeSavePath
			item.SavePath = &sp
		}
		items = append(items, item)

		manifestItem := ManifestItem{
			Hash:        torrent.Hash,
			Name:        torrent.Name,
			ArchivePath: uniquePath,
			SizeBytes:   torrent.TotalSize,
		}
		if categoryPtr != nil {
			manifestItem.Category = categoryPtr
		}
		if infohashV1 != "" {
			manifestItem.InfoHashV1 = &infohashV1
		}
		if infohashV2 != "" {
			manifestItem.InfoHashV2 = &infohashV2
		}
		if rawTags != "" {
			manifestItem.Tags = splitTags(rawTags)
		}
		if blobRelPath != nil {
			manifestItem.TorrentBlob = *blobRelPath
		}
		if storeSavePath != "" {
			manifestItem.SavePath = storeSavePath
		}
		manifestItems = append(manifestItems, manifestItem)
	}

	manifest := Manifest{
		InstanceID:   j.instanceID,
		Kind:         string(j.kind),
		GeneratedAt:  s.now().UTC(),
		TorrentCount: len(manifestItems),
		Categories:   snapshotCategories,
		Tags:         snapshotTags,
		Items:        manifestItems,
	}

	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	manifestPointer := &manifestRelPath
	if err := os.WriteFile(manifestAbsPath, manifestData, 0o644); err != nil {
		log.Warn().Err(err).Str("path", manifestAbsPath).Msg("Failed to write manifest to disk")
		manifestPointer = nil
	}

	return &backupResult{
		manifestRelPath: manifestPointer,
		totalBytes:      totalBytes,
		torrentCount:    len(manifestItems),
		categoryCounts:  categoryCounts,
		categories:      snapshotCategories,
		tags:            snapshotTags,
		items:           items,
		settings:        settings,
	}, nil
}

// exportWorkers bounds concurrent torrents/export calls during a backup run.
// Kept small on purpose: enough to overlap stalls on qBittorrent's
// single-threaded WebAPI without flooding a struggling instance (#1101).
const exportWorkers = 4

// maxAdaptiveExportDelay caps the adaptive back-pressure delay. A slow export
// usually means the request queued behind another client's large response, not
// that qBittorrent is melting down; sleeping the full stall duration again
// would just double the cost of every stall.
const maxAdaptiveExportDelay = 500 * time.Millisecond

// exportedTorrent is the order-independent result of exporting one torrent.
type exportedTorrent struct {
	skipped     bool
	dataLen     int
	filename    string
	blobRelPath *string
}

// exportBackupTorrent produces the .torrent payload for one torrent (cached
// blob or live export), patches trackers if needed, and persists the blob to
// the cache. It touches no order-dependent backup state, so callers may run it
// concurrently for distinct torrents. lastExportElapsed carries the adaptive
// delay state between consecutive calls on the same worker.
func (s *Service) exportBackupTorrent(ctx context.Context, j job, torrent qbt.Torrent, patchTrackers bool, webAPIVersion string, lastExportElapsed *time.Duration) (exportedTorrent, error) {
	select {
	case <-ctx.Done():
		return exportedTorrent{}, ctx.Err()
	default:
	}

	var (
		data          []byte
		suggestedName string
		trackerDomain string
		blobRelPath   *string
	)

	cachedTorrent, cacheErr := s.loadCachedTorrent(ctx, j.instanceID, torrent.Hash)
	if cacheErr != nil {
		log.Warn().Err(cacheErr).Str("hash", torrent.Hash).Msg("Failed to load cached torrent blob")
	}
	if cachedTorrent != nil {
		data = cachedTorrent.data
		suggestedName = torrent.Name
		trackerDomain = trackerDomainFromTorrent(torrent)
		rel := cachedTorrent.relPath
		blobRelPath = &rel
	}

	if data == nil {
		if shouldSkipLiveExportForBackup(torrent, cachedTorrent != nil, cacheErr) {
			log.Warn().
				Str("hash", torrent.Hash).
				Str("name", torrent.Name).
				Int("instanceID", j.instanceID).
				Msg("Skipping torrent export; live qBittorrent export disabled for hybrid torrents")
			return exportedTorrent{skipped: true}, nil
		}
		if err := adaptiveExportDelay(ctx, s.cfg.ExportThrottle, *lastExportElapsed); err != nil {
			return exportedTorrent{}, err
		}
		exportStart := time.Now()
		var tracker string
		var err error
		data, suggestedName, tracker, err = s.reader.ExportTorrent(ctx, j.instanceID, torrent.Hash)
		*lastExportElapsed = time.Since(exportStart)
		if err != nil {
			if isExportMetadataUnavailable(err) {
				log.Warn().
					Err(err).
					Str("hash", torrent.Hash).
					Str("name", torrent.Name).
					Int("instanceID", j.instanceID).
					Msg("Skipping torrent export; metadata not downloaded yet")
				return exportedTorrent{skipped: true}, nil
			}
			return exportedTorrent{}, fmt.Errorf("export torrent %s: %w", torrent.Hash, err)
		}
		trackerDomain = tracker
	}

	if patchTrackers {
		trackers := gatherTrackerURLs(ctx, s.tracker, j.instanceID, torrent)
		if patched, changed, err := patchTorrentTrackers(data, trackers); err != nil {
			log.Warn().Err(err).Str("hash", torrent.Hash).Int("instanceID", j.instanceID).Msg("Failed to patch exported torrent trackers")
		} else if changed {
			data = patched
			// ensure cached entry is rebuilt with the corrected payload
			blobRelPath = nil
			log.Debug().Str("hash", torrent.Hash).Int("instanceID", j.instanceID).Str("webAPIVersion", webAPIVersion).Msg("Injected tracker metadata into exported torrent")
		}
	}

	if blobRelPath == nil && s.cacheDir != "" {
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		blobName := hash + ".torrent"
		subdir := ""
		if len(hash) >= 6 {
			subdir = filepath.Join(hash[0:2], hash[2:4], hash[4:6])
		}
		if err := cacheTorrentBlob(s.cacheDir, filepath.Join(subdir, blobName), data); err != nil {
			return exportedTorrent{}, err
		}
		rel := filepath.ToSlash(filepath.Join("backups", "torrents", subdir, blobName))
		blobRelPath = &rel
	}

	return exportedTorrent{
		dataLen:     len(data),
		filename:    torrentname.SanitizeExportFilename(suggestedName, torrent.Hash, trackerDomain, torrent.Hash),
		blobRelPath: blobRelPath,
	}, nil
}

// blobTmpSeq distinguishes temp file names when several writers cache the
// same payload at once.
var blobTmpSeq atomic.Int64

// blobTmpMinAge shields temp files a live writer is about to rename into
// place from the sweep; anything older is crash litter.
const blobTmpMinAge = time.Hour

// sweepStaleBlobTemps removes *.tmp-* files a crashed run may have left in
// the blob cache. They are never trusted or served; this is litter
// collection, so every failure is best-effort. Runs in the background so a
// large cache does not stall startup; the age guard keeps it clear of temp
// files concurrent writers are about to rename into place.
func sweepStaleBlobTemps(cacheDir string) {
	root, err := os.OpenRoot(cacheDir)
	if err != nil {
		log.Warn().Err(err).Str("cacheDir", cacheDir).Msg("Failed to open torrent cache for temp sweep")
		return
	}
	defer root.Close()

	cutoff := time.Now().Add(-blobTmpMinAge)
	removed := 0
	_ = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.Contains(d.Name(), ".tmp-") {
			return nil
		}
		if info, err := d.Info(); err != nil || info.ModTime().After(cutoff) {
			return nil
		}
		if root.Remove(filepath.FromSlash(p)) == nil {
			removed++
		}
		return nil
	})
	if removed > 0 {
		log.Info().Int("removed", removed).Str("cacheDir", cacheDir).Msg("Removed stale torrent cache temp files")
	}
}

// cacheTorrentBlob persists data at the content-addressed path relBlob inside
// rootDir via a temp file plus rename, so a crash mid-write can never leave a
// truncated file at a path later runs would trust (#2187). An existing
// destination is kept as-is: same address means same content. All access goes
// through os.Root, which guarantees the path cannot escape rootDir. Every
// writer of the blob cache (live export, temp-dir import, background import
// download) must go through this helper.
func cacheTorrentBlob(rootDir, relBlob string, data []byte) error {
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return fmt.Errorf("open torrent cache: %w", err)
	}
	defer root.Close()

	if _, err := root.Stat(relBlob); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cache torrent blob: %w", err)
	}
	if dir := filepath.Dir(relBlob); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create torrent cache subdir: %w", err)
		}
	}

	// 0o644 matches the manifest and archive writes in this file.
	tmpName := fmt.Sprintf("%s.tmp-%d-%d", relBlob, os.Getpid(), blobTmpSeq.Add(1))
	tmp, err := root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("cache torrent blob: %w", err)
	}
	// Best-effort cleanup: after a successful rename the temp name is gone
	// and this remove is a no-op.
	defer func() { _ = root.Remove(tmpName) }()

	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Close()
	} else {
		tmp.Close()
	}
	if err != nil {
		return fmt.Errorf("cache torrent blob: %w", err)
	}

	if err := root.Rename(tmpName, relBlob); err != nil {
		// The destination can only exist here if a concurrent worker
		// published the identical payload after the existence check above,
		// so losing the rename race is success. Rename atomically replaces
		// existing files on POSIX and Windows alike; the stat covers any
		// filesystem that refuses the replace regardless.
		if _, statErr := root.Stat(relBlob); statErr == nil {
			return nil
		}
		return fmt.Errorf("cache torrent blob: %w", err)
	}
	return nil
}

// orphanBlobMinAge shields just-written blobs from the startup orphan sweep.
const orphanBlobMinAge = 24 * time.Hour

// cleanupOrphanedBlobs removes cache files no backup item references. Blobs
// are written to the cache during export but only referenced in the database
// once the whole run succeeds, so every failed run strands its already-written
// blobs and nothing else ever deletes them. Runs in the background at startup,
// before any backup workers spawn; the age guard keeps it clear of files
// written around the sweep. All access goes through os.Root so paths cannot
// escape the cache.
func (s *Service) cleanupOrphanedBlobs(ctx context.Context) {
	if s.cacheDir == "" {
		return
	}

	refs, err := s.store.ListTorrentBlobPaths(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to list referenced torrent blobs; skipping cache cleanup")
		return
	}
	// Stored paths carry the legacy "backups/" prefix or not; ResolveBackupPath
	// maps both forms to the same absolute path under the backup root.
	referenced := make(map[string]struct{}, len(refs))
	for _, rel := range refs {
		if abs := s.ResolveBackupPath(rel); abs != "" {
			referenced[abs] = struct{}{}
		}
	}

	root, err := os.OpenRoot(s.cacheDir)
	if err != nil {
		log.Warn().Err(err).Str("cacheDir", s.cacheDir).Msg("Failed to open torrent cache for cleanup")
		return
	}
	defer root.Close()

	cutoff := s.now().Add(-orphanBlobMinAge)
	removed := 0
	var freed int64
	walkErr := fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, entryErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entryErr != nil || d.IsDir() {
			return nil
		}
		if _, ok := referenced[filepath.Join(s.cacheDir, filepath.FromSlash(p))]; ok {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().After(cutoff) {
			return nil
		}
		if root.Remove(filepath.FromSlash(p)) == nil {
			removed++
			freed += info.Size()
		}
		return nil
	})
	if walkErr != nil {
		log.Warn().Err(walkErr).Str("cacheDir", s.cacheDir).Msg("Torrent cache cleanup stopped early")
	}
	if removed > 0 {
		log.Info().Int("removed", removed).Int64("freedBytes", freed).Str("cacheDir", s.cacheDir).Msg("Removed orphaned torrent cache blobs")
	}
}

// adaptiveExportDelay waits between export API calls with back-pressure.
// The delay is at least minDelay and extends to match the previous export's
// response time when qBittorrent is under load, capped at
// maxAdaptiveExportDelay so a stalled request is not double-charged.
func adaptiveExportDelay(ctx context.Context, minDelay, lastExportDuration time.Duration) error {
	if minDelay <= 0 {
		return nil
	}

	delay := max(minDelay, min(lastExportDuration, maxAdaptiveExportDelay))
	t := time.NewTimer(delay)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (s *Service) resolveBasePaths(ctx context.Context, _ *models.BackupSettings, instanceID int) (string, string, error) {
	var baseSegment string
	if name, err := s.store.GetInstanceName(ctx, instanceID); err == nil {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			baseSegment = safeSegment(trimmed)
		}
	} else if !errors.Is(err, models.ErrInstanceNotFound) {
		return "", "", err
	}

	if baseSegment == "" {
		baseSegment = fmt.Sprintf("instance-%d", instanceID)
	}

	if s.root == "" {
		return "", "", errors.New("backup directory not configured")
	}

	base := filepath.Join("backups", baseSegment)
	abs := filepath.Join(s.root, baseSegment)
	return abs, base, nil
}

func ensureUniquePath(path string, used map[string]int) string {
	if _, exists := used[path]; !exists {
		used[path] = 1
		return path
	}

	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)

	idx := used[path]
	for {
		candidate := fmt.Sprintf("%s_%d%s", base, idx, ext)
		if _, exists := used[candidate]; !exists {
			used[path] = idx + 1
			used[candidate] = 1
			return candidate
		}
		idx++
	}
}

func safeSegment(input string) string {
	cleaned := strings.TrimSpace(input)
	if cleaned == "" {
		return "uncategorized"
	}

	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r == '/', r == '\\', r == ':', r == '*', r == '?', r == '"', r == '<', r == '>', r == '|':
			return '_'
		case r < 32 || r == 127:
			return -1
		}
		return r
	}, cleaned)

	sanitized = strings.Trim(sanitized, " .")
	if sanitized == "" {
		return "uncategorized"
	}

	sanitized = torrentname.TruncateUTF8(sanitized, 100)
	return sanitized
}

func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	if len(result) > 1 {
		sort.Strings(result)
	}
	return result
}

func (s *Service) clearInstance(instanceID int, runID int64) {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	if current, ok := s.inflight[instanceID]; ok && current == runID {
		delete(s.inflight, instanceID)
	}

	s.progressMu.Lock()
	delete(s.progress, runID)
	s.progressMu.Unlock()
}

// updateProgress updates the progress for a run. Progress only moves forward:
// concurrent export workers may report completion counts out of order.
func (s *Service) updateProgress(runID int64, current int) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if p := s.progress[runID]; p != nil && current > p.Current {
		p.Current = current
		p.Percentage = float64(current) / float64(p.Total) * 100
	}
}

func isExportMetadataUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, qbt.ErrTorrentMetadataNotDownloadedYet) {
		return true
	}
	return strings.Contains(err.Error(), "status code: 409")
}

func (s *Service) GetProgress(runID int64) *BackupProgress {
	s.progressMu.RLock()
	defer s.progressMu.RUnlock()
	if p, ok := s.progress[runID]; ok {
		return &BackupProgress{
			Current:    p.Current,
			Total:      p.Total,
			Percentage: p.Percentage,
		}
	}
	return nil
}

func (s *Service) markInstance(instanceID int, runID int64) bool {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	if _, exists := s.inflight[instanceID]; exists {
		return false
	}
	s.inflight[instanceID] = runID
	return true
}

func (s *Service) QueueRun(ctx context.Context, instanceID int, kind models.BackupRunKind, requestedBy string) (*models.BackupRun, error) {
	if !s.markInstance(instanceID, 0) {
		return nil, ErrInstanceBusy
	}

	run := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        kind,
		Status:      models.BackupRunStatusPending,
		RequestedBy: requestedBy,
		RequestedAt: s.now(),
	}

	if err := s.store.CreateRun(ctx, run); err != nil {
		s.clearInstance(instanceID, 0)
		return nil, err
	}

	s.inflightMu.Lock()
	s.inflight[instanceID] = run.ID
	s.inflightMu.Unlock()

	select {
	case <-ctx.Done():
		s.clearInstance(instanceID, run.ID)

		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.store.DeleteRun(cleanupCtx, run.ID); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Int64("runID", run.ID).Msg("Failed to remove canceled backup run")
		}
		cancelCleanup()
		return nil, ctx.Err()
	case s.jobs <- job{runID: run.ID, instanceID: instanceID, kind: kind}:
	}

	s.emitRunActivity(instanceID, run.ID)
	return run, nil
}

func (s *Service) scheduler(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.scheduleDueBackups(ctx); err != nil {
				log.Warn().Err(err).Msg("Backup scheduler tick failed")
			}
		}
	}
}

func (s *Service) scheduleDueBackups(ctx context.Context) error {
	settings, err := s.store.ListEnabledSettings(ctx)
	if err != nil {
		return err
	}

	now := s.now()

	for _, cfg := range settings {
		s.normalizeAndPersistSettings(ctx, cfg)

		if !cfg.Enabled {
			continue
		}

		evaluate := func(kind models.BackupRunKind, enabled bool) {
			if s.isBackupMissed(ctx, cfg.InstanceID, kind, enabled, now) {
				if _, err := s.QueueRun(ctx, cfg.InstanceID, kind, "scheduler"); err != nil {
					if !errors.Is(err, ErrInstanceBusy) {
						log.Warn().Err(err).Int("instanceID", cfg.InstanceID).Msg("Failed to queue scheduled backup")
					}
				}
			}
		}

		evaluate(models.BackupRunKindHourly, cfg.HourlyEnabled)
		evaluate(models.BackupRunKindDaily, cfg.DailyEnabled)
		evaluate(models.BackupRunKindWeekly, cfg.WeeklyEnabled)
		evaluate(models.BackupRunKindMonthly, cfg.MonthlyEnabled)
	}

	return nil
}

func (s *Service) applyRetention(ctx context.Context, instanceID int, settings *models.BackupSettings) error {
	kinds := []struct {
		kind models.BackupRunKind
		keep int
	}{
		{models.BackupRunKindHourly, settings.KeepHourly},
		{models.BackupRunKindDaily, settings.KeepDaily},
		{models.BackupRunKindWeekly, settings.KeepWeekly},
		{models.BackupRunKindMonthly, settings.KeepMonthly},
	}

	for _, cfg := range kinds {
		runIDs, err := s.store.DeleteRunsOlderThan(ctx, instanceID, cfg.kind, cfg.keep)
		if err != nil {
			return err
		}
		if err := s.cleanupRunFiles(ctx, runIDs); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to cleanup old backup files")
		}
	}

	return nil
}

func (s *Service) cleanupRunFiles(ctx context.Context, runIDs []int64) error {
	if len(runIDs) == 0 {
		return nil
	}

	// Get all runs in one query
	runs, err := s.store.GetRuns(ctx, runIDs)
	if err != nil {
		return err
	}

	// Create a map for quick lookup
	runMap := make(map[int64]*models.BackupRun)
	for _, run := range runs {
		if run != nil {
			runMap[run.ID] = run
		}
	}

	// Get all items for all runs in one query
	items, err := s.store.ListItemsForRuns(ctx, runIDs)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to list backup items for cleanup")
		items = nil
	}

	var filesToDelete []string
	var itemsToCleanup []*models.BackupItem

	for _, runID := range runIDs {
		run, exists := runMap[runID]
		if !exists {
			// Run was already deleted or not found
			continue
		}

		// Collect items for this run
		var runItems []*models.BackupItem
		for _, item := range items {
			if item.RunID == runID {
				runItems = append(runItems, item)
			}
		}
		itemsToCleanup = append(itemsToCleanup, runItems...)

		if run.ManifestPath != nil {
			if abs := s.ResolveBackupPath(*run.ManifestPath); abs != "" {
				filesToDelete = append(filesToDelete, abs)
			} else {
				log.Warn().Str("path", *run.ManifestPath).Msg("Skipping manifest with unresolvable path during cleanup")
			}
		}
		if run.ArchivePath != nil {
			if abs := s.ResolveBackupPath(*run.ArchivePath); abs != "" {
				filesToDelete = append(filesToDelete, abs)
			} else {
				log.Warn().Str("path", *run.ArchivePath).Msg("Skipping archive with unresolvable path during cleanup")
			}
		}
	}

	// Delete files in parallel
	s.deleteFilesParallel(ctx, filesToDelete)

	// Batch cleanup all runs in database
	if err := s.store.CleanupRuns(ctx, runIDs); err != nil {
		log.Warn().Err(err).Msg("Failed to cleanup runs from database")
	}

	s.cleanupTorrentBlobs(ctx, itemsToCleanup)

	return nil
}

func (s *Service) GetSettings(ctx context.Context, instanceID int) (*models.BackupSettings, error) {
	settings, err := s.store.GetSettings(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	s.normalizeAndPersistSettings(ctx, settings)
	settings.CustomPath = nil

	return settings, nil
}

func (s *Service) UpdateSettings(ctx context.Context, settings *models.BackupSettings) error {
	settings.CustomPath = nil
	normalizeBackupSettings(settings)
	return s.store.UpsertSettings(ctx, settings)
}

func (s *Service) ListRuns(ctx context.Context, instanceID int, limit, offset int) ([]*models.BackupRun, error) {
	return s.store.ListRuns(ctx, instanceID, limit, offset)
}

func (s *Service) GetRun(ctx context.Context, runID int64) (*models.BackupRun, error) {
	return s.store.GetRun(ctx, runID)
}

func (s *Service) GetItem(ctx context.Context, runID int64, hash string) (*models.BackupItem, error) {
	return s.store.GetItemByHash(ctx, runID, hash)
}

func (s *Service) DeleteRun(ctx context.Context, runID int64) error {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return err
	}

	items, err := s.store.ListItems(ctx, runID)
	if err != nil {
		log.Warn().Err(err).Int64("runID", runID).Msg("Failed to list backup items before delete")
		items = nil
	}

	var filesToDelete []string
	if run.ManifestPath != nil {
		if abs := s.ResolveBackupPath(*run.ManifestPath); abs != "" {
			filesToDelete = append(filesToDelete, abs)
		} else {
			log.Warn().Str("path", *run.ManifestPath).Msg("Skipping manifest with unresolvable path during delete")
		}
	}
	if run.ArchivePath != nil {
		if abs := s.ResolveBackupPath(*run.ArchivePath); abs != "" {
			filesToDelete = append(filesToDelete, abs)
		} else {
			log.Warn().Str("path", *run.ArchivePath).Msg("Skipping archive with unresolvable path during delete")
		}
	}

	s.deleteFilesParallel(ctx, filesToDelete)

	if err := s.store.CleanupRun(ctx, runID); err != nil {
		return err
	}

	s.cleanupTorrentBlobs(ctx, items)

	return nil
}

func (s *Service) DeleteAllRuns(ctx context.Context, instanceID int) error {
	runIDs, err := s.store.ListRunIDs(ctx, instanceID)
	if err != nil {
		return err
	}
	if len(runIDs) == 0 {
		return nil
	}
	for _, runID := range runIDs {
		if err := s.DeleteRun(ctx, runID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
	}
	return nil
}

func (s *Service) LoadManifest(ctx context.Context, runID int64) (*Manifest, error) {
	items, err := s.store.ListItems(ctx, runID)
	if err != nil {
		return nil, err
	}

	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	manifest := &Manifest{
		InstanceID:   run.InstanceID,
		Kind:         string(run.Kind),
		GeneratedAt:  run.RequestedAt,
		TorrentCount: len(items),
		Categories:   run.Categories,
		Tags:         run.Tags,
		Items:        make([]ManifestItem, 0, len(items)),
	}

	for _, item := range items {
		entry := ManifestItem{
			Hash:        item.TorrentHash,
			Name:        item.Name,
			ArchivePath: "",
			SizeBytes:   item.SizeBytes,
		}
		if item.Category != nil {
			entry.Category = item.Category
		}
		if item.ArchiveRelPath != nil {
			entry.ArchivePath = *item.ArchiveRelPath
		}
		if item.InfoHashV1 != nil {
			entry.InfoHashV1 = item.InfoHashV1
		}
		if item.InfoHashV2 != nil {
			entry.InfoHashV2 = item.InfoHashV2
		}
		if item.Tags != nil {
			entry.Tags = splitTags(*item.Tags)
		}
		if item.TorrentBlobPath != nil {
			entry.TorrentBlob = *item.TorrentBlobPath
		}
		if item.SavePath != nil {
			entry.SavePath = *item.SavePath
		}
		manifest.Items = append(manifest.Items, entry)
	}

	return manifest, nil
}

// ImportManifestFromDir imports a backup manifest with torrent files from temp paths.
// torrentPaths is a map of archivePath -> absolute temp file path on disk.
// The caller is responsible for cleaning up the temp files after this returns.
func (s *Service) ImportManifestFromDir(ctx context.Context, instanceID int, manifestData []byte, requestedBy string, torrentPaths map[string]string) (*models.BackupRun, error) {
	// Use local variable to avoid mutating shared config (thread-safety)
	rootDir := s.root

	// Normalize the backup root for Windows Git Bash paths
	if runtime.GOOS == "windows" && strings.HasPrefix(rootDir, "/c/") {
		rootDir = "C:" + strings.ReplaceAll(strings.TrimPrefix(rootDir, "/c"), "/", "\\")
		log.Info().Str("normalizedBackupDir", rootDir).Msg("Normalized backup directory for Windows")
	}

	log.Info().Int("instanceID", instanceID).Str("requestedBy", requestedBy).Int("dataSize", len(manifestData)).Int("torrentPaths", len(torrentPaths)).Str("backupDir", rootDir).Msg("Starting manifest import from dir")

	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		log.Error().Err(err).Msg("Failed to parse manifest JSON")
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	log.Info().Int("manifestItemCount", len(manifest.Items)).Int("manifestTorrentCount", manifest.TorrentCount).Msg("Manifest parsed successfully")

	// Create a backup run record for the import
	run := &models.BackupRun{
		InstanceID:   instanceID,
		Kind:         models.BackupRunKind(manifest.Kind),
		Status:       models.BackupRunStatusRunning,
		RequestedBy:  requestedBy,
		RequestedAt:  manifest.GeneratedAt,
		CompletedAt:  nil,
		TotalBytes:   0,
		TorrentCount: 0,
		Categories:   manifest.Categories,
		Tags:         manifest.Tags,
	}

	log.Info().Int("instanceID", instanceID).Str("kind", string(run.Kind)).Msg("Creating backup run for import")

	if err := s.store.CreateRun(ctx, run); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to create import run")
		return nil, fmt.Errorf("failed to create import run: %w", err)
	}

	log.Info().Int64("runID", run.ID).Msg("Backup run created successfully")
	s.emitRunActivity(instanceID, run.ID)

	// Convert manifest items to backup items
	items := make([]models.BackupItem, 0, len(manifest.Items))
	var totalBytes int64
	var totalTorrentFileBytes int64

	var missing []missingTorrent

	log.Info().Int("totalItems", len(manifest.Items)).Msg("Starting to process manifest items")

	for i, item := range manifest.Items {
		// Skip items with invalid required fields
		if strings.TrimSpace(item.Hash) == "" || strings.TrimSpace(item.Name) == "" {
			continue
		}

		// Log progress every 100 items
		if i > 0 && i%100 == 0 {
			log.Info().Int("processed", i).Int("total", len(manifest.Items)).Int("validSoFar", len(items)).Msg("Processing manifest items progress")
		}

		backupItem := models.BackupItem{
			RunID:       run.ID,
			TorrentHash: item.Hash,
			Name:        item.Name,
			SizeBytes:   item.SizeBytes,
		}

		if item.Category != nil {
			backupItem.Category = item.Category
		}

		if item.ArchivePath != "" {
			backupItem.ArchiveRelPath = &item.ArchivePath
		}

		if item.InfoHashV1 != nil {
			backupItem.InfoHashV1 = item.InfoHashV1
		}

		if item.InfoHashV2 != nil {
			backupItem.InfoHashV2 = item.InfoHashV2
		}

		if len(item.Tags) > 0 {
			tagsStr := strings.Join(item.Tags, ",")
			backupItem.Tags = &tagsStr
		}

		if savePath := strings.TrimSpace(item.SavePath); savePath != "" {
			backupItem.SavePath = &savePath
		}

		if item.TorrentBlob != "" {
			// Validate blob path to prevent directory traversal
			slashRel := backupRelPath(item.TorrentBlob)
			if slashRel == "" {
				log.Warn().Str("hash", item.Hash).Str("blob", item.TorrentBlob).Msg("Ignoring unsafe TorrentBlob path from manifest")
				items = append(items, backupItem)
				totalBytes += item.SizeBytes
				continue
			}

			stored := path.Join("backups", slashRel)
			backupItem.TorrentBlobPath = &stored
			rel := filepath.FromSlash(slashRel)
			absPath := filepath.Join(rootDir, rel)

			// Check if torrent file path was provided from temp directory
			if torrentPaths != nil && item.ArchivePath != "" {
				if tempPath, ok := torrentPaths[item.ArchivePath]; ok {
					if err := s.copyTorrentFromTemp(tempPath, rootDir, rel); err == nil {
						if info, statErr := os.Stat(absPath); statErr == nil {
							totalTorrentFileBytes += info.Size()
						}
						log.Debug().Str("hash", item.Hash).Str("archivePath", item.ArchivePath).Msg("Imported torrent from temp")
						items = append(items, backupItem)
						totalBytes += item.SizeBytes
						continue
					} else {
						log.Warn().Err(err).Str("hash", item.Hash).Msg("Failed to copy from temp, will try qBittorrent")
					}
				}
			}

			// Mark for background download from qBittorrent
			missing = append(missing, missingTorrent{hash: item.Hash, name: item.Name, relPath: rel, absPath: absPath})
		}

		items = append(items, backupItem)
		totalBytes += item.SizeBytes
	}

	log.Info().Int("validItems", len(items)).Int64("totalBytes", totalBytes).Msg("Finished processing manifest items")

	// Validate that we have valid items if the manifest claimed to have any
	if len(items) == 0 && len(manifest.Items) > 0 {
		log.Error().Int("manifestItems", len(manifest.Items)).Msg("Manifest contains items but none are valid")
		return nil, fmt.Errorf("manifest contains %d items but none are valid (missing required hash or name)", len(manifest.Items))
	}

	// Insert the items
	if len(items) > 0 {
		log.Info().Int("itemCount", len(items)).Int64("runID", run.ID).Msg("Inserting backup items into database")
		if err := s.store.InsertItems(ctx, run.ID, items); err != nil {
			log.Error().Err(err).Int64("runID", run.ID).Msg("Failed to insert backup items")
			return nil, fmt.Errorf("failed to insert backup items: %w", err)
		}
		log.Info().Int("insertedItems", len(items)).Int64("runID", run.ID).Msg("Successfully inserted backup items")
	}

	// Start background download of missing torrents
	if len(missing) > 0 {
		log.Info().Int("missingCount", len(missing)).Msg("Starting background download of missing torrent blobs")
		s.progressMu.Lock()
		s.progress[run.ID] = &BackupProgress{
			Current: 0,
			Total:   len(missing),
		}
		s.progressMu.Unlock()
		log.Info().Int64("runID", run.ID).Int("total", len(missing)).Msg("Initialized import progress")
		s.wg.Go(func() {
			s.downloadMissingTorrents(run.ID, instanceID, rootDir, missing)
		})
	} else {
		// No missing torrents, mark as completed immediately
		now := s.now()
		run.Status = models.BackupRunStatusSuccess
		run.CompletedAt = &now
		if err := s.store.UpdateRunMetadata(ctx, run.ID, func(r *models.BackupRun) error {
			r.Status = models.BackupRunStatusSuccess
			r.CompletedAt = &now
			return nil
		}); err != nil {
			log.Warn().Err(err).Int64("runID", run.ID).Msg("Failed to mark import run as completed")
		}
		s.emitRunActivity(instanceID, run.ID)
	}

	// Update the run with total bytes and torrent count
	run.TotalBytes = totalTorrentFileBytes
	run.TorrentCount = len(items)
	log.Info().Int64("runID", run.ID).Int("torrentCount", len(items)).Int64("totalTorrentFileBytes", totalTorrentFileBytes).Msg("Updating backup run metadata")
	if err := s.store.UpdateRunMetadata(ctx, run.ID, func(r *models.BackupRun) error {
		r.TotalBytes = totalTorrentFileBytes
		r.TorrentCount = len(items)
		return nil
	}); err != nil {
		log.Warn().Err(err).Int64("runID", run.ID).Msg("Failed to update total bytes and torrent count for imported run")
	} else {
		log.Info().Int64("runID", run.ID).Msg("Successfully updated backup run metadata")
	}

	log.Info().Int64("runID", run.ID).Msg("Manifest import from dir completed successfully")
	return run, nil
}

// copyTorrentFromTemp validates a torrent from the import temp dir and caches
// it at the final blob location through the same atomic write as live exports.
func (s *Service) copyTorrentFromTemp(srcPath, rootDir, relPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read temp file: %w", err)
	}
	// Valid torrents are at least ~50 bytes and start with 'd' (bencoded dict).
	if len(data) < 50 {
		return fmt.Errorf("invalid torrent data: too small (%d bytes)", len(data))
	}
	if data[0] != 'd' {
		return fmt.Errorf("invalid torrent data: not a bencoded dict")
	}

	return cacheTorrentBlob(rootDir, relPath, data)
}

// downloadMissingTorrents downloads torrent blobs in the background for
// imported manifests. rootDir is the import's normalized backup root, the
// same root missingTorrent.absPath was built from.
func (s *Service) downloadMissingTorrents(runID int64, instanceID int, rootDir string, missing []missingTorrent) {
	if s.reader == nil {
		log.Warn().Int64("runID", runID).Msg("No sync manager available for background torrent downloads")
		s.markImportComplete(instanceID, runID)
		return
	}

	total := len(missing)
	log.Info().Int("total", total).Int64("runID", runID).Int("instanceID", instanceID).Msg("Starting background download of missing torrent blobs")

	successCount := 0
	var totalTorrentBytes int64
	for i, mt := range missing {
		// Check for shutdown
		if s.ctx != nil {
			select {
			case <-s.ctx.Done():
				log.Info().Int64("runID", runID).Int("completed", successCount).Int("total", total).Msg("Background download cancelled due to shutdown")
				s.markImportComplete(instanceID, runID)
				return
			default:
			}
		}

		// Check if file already exists
		if info, err := os.Stat(mt.absPath); err == nil {
			sz := info.Size()
			log.Trace().Int("current", i+1).Int("total", total).Int64("runID", runID).Str("hash", mt.hash).Str("path", mt.absPath).Int64("size", sz).Msg("Torrent blob already exists, skipping download")
			totalTorrentBytes += sz
			successCount++
			s.updateProgress(runID, i+1)
			continue
		}

		log.Trace().Int("current", i+1).Int("total", total).Int64("runID", runID).Str("hash", mt.hash).Str("path", mt.absPath).Msg("Downloading missing torrent blob in background")
		ctx := s.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		if data, _, _, err := s.reader.ExportTorrent(ctx, instanceID, mt.hash); err == nil {
			if err := cacheTorrentBlob(rootDir, mt.relPath, data); err == nil {
				log.Trace().Int("downloaded", successCount+1).Int("total", total).Int64("runID", runID).Str("hash", mt.hash).Str("path", mt.absPath).Msg("Successfully cached missing torrent blob")
				totalTorrentBytes += int64(len(data))
				successCount++
				s.updateProgress(runID, i+1)
			} else {
				log.Error().Err(err).Int("downloaded", successCount).Int("total", total).Int64("runID", runID).Str("hash", mt.hash).Str("path", mt.absPath).Msg("Failed to cache missing torrent blob")
				s.updateProgress(runID, i+1)
			}
		} else {
			log.Warn().Err(err).Int("downloaded", successCount).Int("total", total).Int64("runID", runID).Str("hash", mt.hash).Msg("Failed to download missing torrent blob from client")
			// TODO: Torznab search fallback is disabled due to API changes
			/*
				// Attempt Torznab search fallback for missing torrents
				if s.jackettSvc != nil {
					ctx := context.Background()
					searchReq := &jackett.TorznabSearchRequest{
						Query: mt.name, // Search by torrent name
						Limit: 10,      // Get multiple results to find the right one
					}

					searchResp, searchErr := s.jackettSvc.SearchGeneric(ctx, searchReq)
					if searchErr != nil {
						log.Warn().Err(searchErr).Int64("runID", runID).Str("hash", mt.hash).Str("name", mt.name).Msg("Torznab search failed")
					} else if len(searchResp.Results) > 0 {
						// Look for a result that matches our infohash or name
						var matchingResult *jackett.SearchResult
						for _, result := range searchResp.Results {
							// Check if this result has the infohash we want
							// TODO: If InfoHashV1/InfoHashV2 are populated from RSS, check them here

							// For now, prefer exact name matches, then partial matches
							if result.Title == mt.name {
								matchingResult = &result
								break // Exact match, use this one
							} else if strings.Contains(strings.ToLower(result.Title), strings.ToLower(mt.name)) && matchingResult == nil {
								matchingResult = &result // Partial match, keep looking for exact
							}
						}

						// If no good name match, take the first result as a last resort
						if matchingResult == nil && len(searchResp.Results) > 0 {
							matchingResult = &searchResp.Results[0]
							log.Warn().Int64("runID", runID).Str("hash", mt.hash).Str("name", mt.name).Str("fallbackTitle", matchingResult.Title).Msg("No good name match found, using first result as fallback")
						}

						if matchingResult != nil {
							log.Info().Int64("runID", runID).Str("hash", mt.hash).Str("name", mt.name).Str("title", matchingResult.Title).Str("indexer", matchingResult.Indexer).Msg("Found potential torrent match via Torznab search, downloading")
			*/
			s.updateProgress(runID, i+1)
		}
	}

	log.Info().Int("completed", successCount).Int("total", total).Int64("runID", runID).Msg("Completed background download of missing torrent blobs")

	log.Info().Int64("totalTorrentBytes", totalTorrentBytes).Int64("runID", runID).Msg("Calculated total torrent file bytes")

	// Update run metadata with actual torrent file sizes
	ctx := context.Background()
	if err := s.store.UpdateRunMetadata(ctx, runID, func(r *models.BackupRun) error {
		r.TotalBytes = totalTorrentBytes
		return nil
	}); err != nil {
		log.Error().Err(err).Int64("runID", runID).Msg("Failed to update run with torrent file sizes")
	} else {
		log.Info().Int64("runID", runID).Int64("totalBytes", totalTorrentBytes).Msg("Updated run metadata with torrent file sizes")
	}

	s.markImportComplete(instanceID, runID)
}

// markImportComplete marks an import run as completed and cleans up progress
func (s *Service) markImportComplete(instanceID int, runID int64) {
	ctx := context.Background()
	now := time.Now().UTC()

	// Update run status in database
	if err := s.store.UpdateRunMetadata(ctx, runID, func(r *models.BackupRun) error {
		r.Status = models.BackupRunStatusSuccess
		r.CompletedAt = &now
		return nil
	}); err != nil {
		log.Error().Err(err).Int64("runID", runID).Msg("Failed to mark import run as completed")
	} else {
		log.Info().Int64("runID", runID).Msg("Marked import run as completed")
	}

	// Clean up progress
	s.progressMu.Lock()
	delete(s.progress, runID)
	s.progressMu.Unlock()

	// Notify connected clients after the progress lock is released.
	s.emitRunActivity(instanceID, runID)
}

// backupRelPath normalizes a stored backup-relative path to a slash form
// without the legacy "backups/" prefix. Returns "" for unsafe paths:
// absolute (POSIX or Windows), drive-prefixed, UNC, or containing a ".."
// path segment.
func backupRelPath(rel string) string {
	raw := strings.ReplaceAll(strings.TrimSpace(rel), `\`, "/")
	if slices.Contains(strings.Split(raw, "/"), "..") {
		return ""
	}
	slash := path.Clean(raw)
	if slash == "." || strings.HasPrefix(slash, "/") || (len(slash) >= 2 && slash[1] == ':') {
		return ""
	}
	return strings.TrimPrefix(slash, "backups/")
}

// ResolveBackupPath maps a stored backup-relative path (with or without the
// legacy "backups/" prefix) to an absolute path under the backup root.
// Returns "" when no root is configured or the path is unsafe.
func (s *Service) ResolveBackupPath(rel string) string {
	slash := backupRelPath(rel)
	if slash == "" || s.root == "" {
		return ""
	}
	return filepath.Join(s.root, filepath.FromSlash(slash))
}

type cachedTorrent struct {
	data    []byte
	relPath string
}

func (s *Service) loadCachedTorrent(ctx context.Context, instanceID int, hash string) (*cachedTorrent, error) {
	if s.cacheDir == "" {
		return nil, nil
	}

	rel, err := s.store.FindCachedTorrentBlob(ctx, instanceID, hash)
	if err != nil {
		return nil, err
	}
	if rel == nil {
		return nil, nil
	}

	absPath := s.ResolveBackupPath(*rel)
	if absPath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	// Keep the stored spelling so rows that share a blob agree on the rel
	// path; blob refcounts compare these strings.
	return &cachedTorrent{data: data, relPath: *rel}, nil
}

func (s *Service) cleanupTorrentBlobs(ctx context.Context, items []*models.BackupItem) {
	if len(items) == 0 {
		return
	}

	// Rows can reference the same blob under the canonical "backups/"-prefixed
	// spelling or the legacy unprefixed one; both resolve to the same file, so
	// count remaining references across both spellings before deleting.
	seen := make(map[string]struct{})
	var uniqueBlobs, lookup []string

	for _, item := range items {
		if item == nil || item.TorrentBlobPath == nil {
			continue
		}

		canon := backupRelPath(*item.TorrentBlobPath)
		if canon == "" {
			continue
		}
		if _, ok := seen[canon]; ok {
			continue
		}

		seen[canon] = struct{}{}
		uniqueBlobs = append(uniqueBlobs, canon)
		lookup = append(lookup, canon, "backups/"+canon)
	}

	if len(uniqueBlobs) == 0 {
		return
	}

	// Item rows are committed only at run end, so a live run's blob reuse is
	// invisible to the refcount, and blobs are content-addressed and shared
	// across instances. Defer to the startup sweep while any run is active.
	if active, err := s.store.FindIncompleteRuns(ctx); err != nil {
		log.Warn().Err(err).Msg("Failed to check for active backup runs; skipping blob cleanup")
		return
	} else if len(active) > 0 {
		log.Debug().Int("activeRuns", len(active)).Msg("Backup run in progress; deferring blob cleanup to the startup sweep")
		return
	}

	refCounts, err := s.store.CountBlobReferencesBatch(ctx, lookup)
	if err != nil {
		// Unknown counts must keep the blob: deleting on error can lose a file
		// another run still references. Startup's cleanupOrphanedBlobs sweeps
		// anything truly unreferenced later.
		log.Warn().Err(err).Msg("Failed to count torrent blob references; skipping blob cleanup")
		return
	}

	var blobsToDelete []string

	for _, canon := range uniqueBlobs {
		if refCounts[canon]+refCounts["backups/"+canon] > 0 {
			continue
		}

		abs := s.ResolveBackupPath(canon)
		if abs == "" {
			log.Warn().Str("blob", canon).Msg("Cannot cleanup torrent blob without backup directory")
			continue
		}
		blobsToDelete = append(blobsToDelete, abs)
	}

	s.deleteFilesParallel(ctx, blobsToDelete)
}

func (s *Service) deleteFilesParallel(ctx context.Context, paths []string) {
	if len(paths) == 0 {
		return
	}

	for _, path := range paths {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := removeFile(path); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", path).Msg("Failed to remove file during bulk cleanup")
		}
	}
}

func trackerDomainFromTorrent(t qbt.Torrent) string {
	if host := hostFromURL(t.Tracker); host != "" {
		return host
	}

	for _, tracker := range t.Trackers {
		if host := hostFromURL(tracker.Url); host != "" {
			return host
		}
	}

	return ""
}

func hostFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	return u.Hostname()
}
