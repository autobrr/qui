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
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/torrentname"
)

var (
	// ErrInstanceBusy is returned when a backup is already running for the instance.
	ErrInstanceBusy = errors.New("backup already running for this instance")
)

// Config controls background backup scheduling.
type Config struct {
	DataDir         string
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
	absPath string
}

type Service struct {
	store       *models.BackupStore
	syncManager *qbittorrent.SyncManager
	jackettSvc  *jackett.Service
	cfg         Config
	cacheDir    string

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
}

func NewService(store *models.BackupStore, syncManager *qbittorrent.SyncManager, jackettSvc interface{}, cfg Config) *Service {
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
		cfg.ExportThrottle = 200 * time.Millisecond
	}

	cacheDir := ""
	if strings.TrimSpace(cfg.DataDir) != "" {
		cacheDir = filepath.Join(cfg.DataDir, "backups", "torrents")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			log.Warn().Err(err).Str("cacheDir", cacheDir).Msg("Failed to prepare torrent cache directory")
		}
	}

	var jackettService *jackett.Service
	if svc, ok := jackettSvc.(*jackett.Service); ok {
		_ = svc // TODO: jackettService = svc when jackett fallback is re-enabled
	}

	return &Service{
		store:       store,
		syncManager: syncManager,
		jackettSvc:  jackettService,
		cfg:         cfg,
		cacheDir:    cacheDir,
		jobs:        make(chan job, cfg.WorkerCount*2),
		inflight:    make(map[int]int64),
		progress:    make(map[int64]*BackupProgress),
		now:         func() time.Time { return time.Now().UTC() },
	}
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

	for i := 0; i < s.cfg.WorkerCount; i++ {
		s.wg.Add(1)
		go s.worker(ctx)
	}

	// Check for missed backups and queue exactly one if applicable
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.checkMissedBackups(ctx); err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				log.Debug().Msg("Missed-backup check canceled")
			} else {
				log.Warn().Err(err).Msg("Failed to check for missed backups")
			}
		}
	}()

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
		end := i + chunkSize
		if end > len(runIDs) {
			end = len(runIDs)
		}
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
	if s.syncManager == nil {
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
	}

	s.clearInstance(j.instanceID, j.runID)
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

func (s *Service) executeBackup(ctx context.Context, j job) (*backupResult, error) {
	settings, err := s.store.GetSettings(ctx, j.instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load backup settings: %w", err)
	}
	s.normalizeAndPersistSettings(ctx, settings)

	torrents, err := s.syncManager.GetAllTorrents(ctx, j.instanceID)
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

	var exportTicker *time.Ticker
	var exportThrottle <-chan time.Time
	if s.cfg.ExportThrottle > 0 {
		exportTicker = time.NewTicker(s.cfg.ExportThrottle)
		exportThrottle = exportTicker.C
	}
	if exportTicker != nil {
		defer exportTicker.Stop()
	}

	var snapshotCategories map[string]models.CategorySnapshot
	if settings.IncludeCategories {
		categories, err := s.syncManager.GetCategories(ctx, j.instanceID)
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
		tags, err := s.syncManager.GetTags(ctx, j.instanceID)
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
	if version, err := s.syncManager.GetInstanceWebAPIVersion(ctx, j.instanceID); err != nil {
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

	for idx, torrent := range torrents {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var (
			data          []byte
			suggestedName string
			trackerDomain string
			blobRelPath   *string
		)

		cachedTorrent, err := s.loadCachedTorrent(ctx, j.instanceID, torrent.Hash)
		if err != nil {
			log.Warn().Err(err).Str("hash", torrent.Hash).Msg("Failed to load cached torrent blob")
		}
		if cachedTorrent != nil {
			data = cachedTorrent.data
			suggestedName = torrent.Name
			trackerDomain = trackerDomainFromTorrent(torrent)
			rel := cachedTorrent.relPath
			blobRelPath = &rel
		}

		if data == nil {
			if err := waitForExportThrottle(ctx, exportThrottle); err != nil {
				return nil, err
			}
			var tracker string
			data, suggestedName, tracker, err = s.syncManager.ExportTorrent(ctx, j.instanceID, torrent.Hash)
			if err != nil {
				if isExportMetadataUnavailable(err) {
					log.Warn().
						Err(err).
						Str("hash", torrent.Hash).
						Str("name", torrent.Name).
						Int("instanceID", j.instanceID).
						Msg("Skipping torrent export; metadata not downloaded yet")
					s.updateProgress(j.runID, idx+1)
					continue
				}
				return nil, fmt.Errorf("export torrent %s: %w", torrent.Hash, err)
			}
			trackerDomain = tracker
		}

		if patchTrackers {
			trackers := gatherTrackerURLs(ctx, s.syncManager, j.instanceID, torrent)
			if patched, changed, err := patchTorrentTrackers(data, trackers); err != nil {
				log.Warn().Err(err).Str("hash", torrent.Hash).Int("instanceID", j.instanceID).Msg("Failed to patch exported torrent trackers")
			} else if changed {
				data = patched
				// ensure cached entry is rebuilt with the corrected payload
				blobRelPath = nil
				log.Debug().Str("hash", torrent.Hash).Int("instanceID", j.instanceID).Str("webAPIVersion", webAPIVersion).Msg("Injected tracker metadata into exported torrent")
			}
		}

		filename := torrentname.SanitizeExportFilename(suggestedName, torrent.Hash, trackerDomain, torrent.Hash)
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

		archivePath := filename
		if settings.IncludeCategories && category != "" {
			archivePath = filepath.ToSlash(filepath.Join(safeSegment(category), filename))
		}

		uniquePath := ensureUniquePath(archivePath, usedPaths)

		if blobRelPath == nil && s.cacheDir != "" {
			sum := sha256.Sum256(data)
			hash := hex.EncodeToString(sum[:])
			blobName := hash + ".torrent"
			subdir := ""
			if len(hash) >= 6 {
				subdir = filepath.Join(hash[0:2], hash[2:4], hash[4:6])
			}
			absBlob := filepath.Join(s.cacheDir, subdir, blobName)
			if _, err := os.Stat(absBlob); errors.Is(err, os.ErrNotExist) {
				if subdir != "" {
					if err := os.MkdirAll(filepath.Dir(absBlob), 0o755); err != nil {
						return nil, fmt.Errorf("create torrent cache subdir: %w", err)
					}
				}
				if err := os.WriteFile(absBlob, data, 0o644); err != nil && !errors.Is(err, os.ErrExist) {
					return nil, fmt.Errorf("cache torrent blob: %w", err)
				}
			}
			rel := filepath.ToSlash(filepath.Join("backups", "torrents", subdir, blobName))
			blobRelPath = &rel
		}

		// Note: Removed zip header creation for streaming interface

		totalBytes += int64(len(data))

		infohashV1 := strings.TrimSpace(torrent.InfohashV1)
		infohashV2 := strings.TrimSpace(torrent.InfohashV2)

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
		manifestItems = append(manifestItems, manifestItem)

		// Update progress after processing each torrent
		s.updateProgress(j.runID, idx+1)
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

func waitForExportThrottle(ctx context.Context, throttle <-chan time.Time) error {
	if throttle == nil {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-throttle:
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

	base := filepath.Join("backups", baseSegment)

	if s.cfg.DataDir == "" {
		return "", "", errors.New("data directory not configured")
	}

	abs := filepath.Join(s.cfg.DataDir, base)
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

// updateProgress updates the progress for a run
func (s *Service) updateProgress(runID int64, current int) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if p := s.progress[runID]; p != nil {
		p.Current = current
		p.Percentage = float64(current) / float64(p.Total) * 100
	}
}

func isExportMetadataUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, qbt.ErrTorrentMetdataNotDownloadedYet) {
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
			manifestPath := filepath.Join(s.cfg.DataDir, *run.ManifestPath)
			filesToDelete = append(filesToDelete, manifestPath)
		}
		if run.ArchivePath != nil {
			archivePath := filepath.Join(s.cfg.DataDir, *run.ArchivePath)
			filesToDelete = append(filesToDelete, archivePath)
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
		manifestPath := filepath.Join(s.cfg.DataDir, *run.ManifestPath)
		filesToDelete = append(filesToDelete, manifestPath)
	}
	if run.ArchivePath != nil {
		archivePath := filepath.Join(s.cfg.DataDir, *run.ArchivePath)
		filesToDelete = append(filesToDelete, archivePath)
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
		manifest.Items = append(manifest.Items, entry)
	}

	return manifest, nil
}

// ImportManifestFromDir imports a backup manifest with torrent files from temp paths.
// torrentPaths is a map of archivePath -> absolute temp file path on disk.
// The caller is responsible for cleaning up the temp files after this returns.
func (s *Service) ImportManifestFromDir(ctx context.Context, instanceID int, manifestData []byte, requestedBy string, torrentPaths map[string]string) (*models.BackupRun, error) {
	// Use local variable to avoid mutating shared config (thread-safety)
	dataDir := s.cfg.DataDir

	// Normalize DataDir for Windows Git Bash paths
	if runtime.GOOS == "windows" && strings.HasPrefix(dataDir, "/c/") {
		dataDir = "C:" + strings.ReplaceAll(strings.TrimPrefix(dataDir, "/c"), "/", "\\")
		log.Info().Str("normalizedDataDir", dataDir).Msg("Normalized DataDir for Windows")
	}

	log.Info().Int("instanceID", instanceID).Str("requestedBy", requestedBy).Int("dataSize", len(manifestData)).Int("torrentPaths", len(torrentPaths)).Str("dataDir", dataDir).Msg("Starting manifest import from dir")

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

		if item.TorrentBlob != "" {
			// Validate blob path to prevent directory traversal
			rel := filepath.Clean(item.TorrentBlob)
			if filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
				log.Warn().Str("hash", item.Hash).Str("blob", item.TorrentBlob).Msg("Ignoring unsafe TorrentBlob path from manifest")
				items = append(items, backupItem)
				totalBytes += item.SizeBytes
				continue
			}

			backupItem.TorrentBlobPath = &rel
			absPath := filepath.Join(dataDir, rel)

			// Check if torrent file path was provided from temp directory
			if torrentPaths != nil && item.ArchivePath != "" {
				if tempPath, ok := torrentPaths[item.ArchivePath]; ok {
					if err := s.copyTorrentFromTemp(tempPath, absPath); err == nil {
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
			missing = append(missing, missingTorrent{hash: item.Hash, name: item.Name, absPath: absPath})
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
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.downloadMissingTorrents(run.ID, instanceID, missing)
		}()
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

// copyTorrentFromTemp copies a torrent from temp file to final blob cache location.
func (s *Service) copyTorrentFromTemp(srcPath, destPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	defer srcFile.Close()

	// Check minimum file size (valid torrents are at least ~50 bytes)
	info, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat temp file: %w", err)
	}
	if info.Size() < 50 {
		return fmt.Errorf("invalid torrent data: too small (%d bytes)", info.Size())
	}

	// Validate torrent starts with 'd' (bencoded dict)
	header := make([]byte, 1)
	if _, err := srcFile.Read(header); err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	if header[0] != 'd' {
		return fmt.Errorf("invalid torrent data: not a bencoded dict")
	}
	if _, err := srcFile.Seek(0, io.SeekStart); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("copy: %w", err)
	}

	return nil
}

// downloadMissingTorrents downloads torrent blobs in the background for imported manifests
func (s *Service) downloadMissingTorrents(runID int64, instanceID int, missing []missingTorrent) {
	if s.syncManager == nil {
		log.Warn().Int64("runID", runID).Msg("No sync manager available for background torrent downloads")
		s.markImportComplete(runID)
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
				s.markImportComplete(runID)
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
		if data, _, _, err := s.syncManager.ExportTorrent(ctx, instanceID, mt.hash); err == nil {
			// Ensure directory exists
			if err := os.MkdirAll(filepath.Dir(mt.absPath), 0o755); err != nil {
				log.Error().Err(err).Int("downloaded", successCount).Int("total", total).Int64("runID", runID).Str("hash", mt.hash).Str("path", mt.absPath).Msg("Failed to create directory for torrent blob")
				s.updateProgress(runID, i+1)
				continue
			}
			if err := os.WriteFile(mt.absPath, data, 0o644); err == nil {
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

	s.markImportComplete(runID)
}

// markImportComplete marks an import run as completed and cleans up progress
func (s *Service) markImportComplete(runID int64) {
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
}

// DataDir returns the base data directory used for backups.
func (s *Service) DataDir() string {
	return s.cfg.DataDir
}

type cachedTorrent struct {
	data    []byte
	relPath string
}

func (s *Service) loadCachedTorrent(ctx context.Context, instanceID int, hash string) (*cachedTorrent, error) {
	if s.cacheDir == "" || strings.TrimSpace(s.cfg.DataDir) == "" {
		return nil, nil
	}

	rel, err := s.store.FindCachedTorrentBlob(ctx, instanceID, hash)
	if err != nil {
		return nil, err
	}
	if rel == nil {
		return nil, nil
	}

	absPath := filepath.Join(s.cfg.DataDir, *rel)
	data, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			altRel := filepath.ToSlash(filepath.Join("backups", *rel))
			altAbs := filepath.Join(s.cfg.DataDir, altRel)
			if altData, altErr := os.ReadFile(altAbs); altErr == nil {
				return &cachedTorrent{data: altData, relPath: altRel}, nil
			} else if errors.Is(altErr, os.ErrNotExist) {
				return nil, nil
			} else {
				return nil, altErr
			}
		}
		return nil, err
	}

	return &cachedTorrent{data: data, relPath: *rel}, nil
}

func (s *Service) cleanupTorrentBlobs(ctx context.Context, items []*models.BackupItem) {
	if len(items) == 0 {
		return
	}

	seen := make(map[string]struct{})
	var uniqueBlobs []string

	for _, item := range items {
		if item == nil || item.TorrentBlobPath == nil {
			continue
		}

		rel := strings.TrimSpace(*item.TorrentBlobPath)
		if rel == "" {
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}

		seen[rel] = struct{}{}
		uniqueBlobs = append(uniqueBlobs, rel)
	}

	if len(uniqueBlobs) == 0 {
		return
	}

	// Batch count references for all blobs
	refCounts, err := s.store.CountBlobReferencesBatch(ctx, uniqueBlobs)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to count torrent blob references")
		// Fall back to individual counts if batch fails
		refCounts = make(map[string]int)
		for _, blob := range uniqueBlobs {
			count, err := s.store.CountBlobReferences(ctx, blob)
			if err != nil {
				log.Warn().Err(err).Str("blob", blob).Msg("Failed to count torrent blob references")
				continue
			}
			refCounts[blob] = count
		}
	}

	var blobsToDelete []string

	for _, rel := range uniqueBlobs {
		count, exists := refCounts[rel]
		if !exists {
			count = 0 // If not in map, assume 0 references
		}
		if count > 0 {
			continue
		}

		if s.cfg.DataDir == "" {
			log.Warn().Str("blob", rel).Msg("Cannot cleanup torrent blob without data directory")
			continue
		}

		abs := filepath.Join(s.cfg.DataDir, rel)
		blobsToDelete = append(blobsToDelete, abs)
	}

	s.deleteFilesParallel(ctx, blobsToDelete)
}

func (s *Service) deleteFilesParallel(ctx context.Context, paths []string) {
	if len(paths) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, p := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				log.Warn().Err(err).Str("path", path).Msg("Failed to remove file during bulk cleanup")
			}
		}(p)
	}
	wg.Wait()
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
