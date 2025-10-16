// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/torrentname"
)

var (
	// ErrInstanceBusy is returned when a backup is already running for the instance.
	ErrInstanceBusy = errors.New("backup already running for this instance")
)

// Config controls background backup scheduling.
type Config struct {
	DataDir      string
	PollInterval time.Duration
	WorkerCount  int
}

type BackupProgress struct {
	Current    int
	Total      int
	Percentage float64
}

type Service struct {
	store       *models.BackupStore
	syncManager *qbittorrent.SyncManager
	cfg         Config
	cacheDir    string

	jobs   chan job
	wg     sync.WaitGroup
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

func NewService(store *models.BackupStore, syncManager *qbittorrent.SyncManager, cfg Config) *Service {
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 1
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Minute
	}

	cacheDir := ""
	if strings.TrimSpace(cfg.DataDir) != "" {
		cacheDir = filepath.Join(cfg.DataDir, "backups", "torrents")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			log.Warn().Err(err).Str("cacheDir", cacheDir).Msg("Failed to prepare torrent cache directory")
		}
	}

	return &Service{
		store:       store,
		syncManager: syncManager,
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
	s.cancel = cancel

	for i := 0; i < s.cfg.WorkerCount; i++ {
		s.wg.Add(1)
		go s.worker(ctx)
	}

	s.wg.Add(1)
	go s.scheduler(ctx)
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
			run.ArchivePath = &result.archiveRelPath
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
	archiveRelPath  string
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

	archiveName := fmt.Sprintf("qui-backup_%s_%s_%s.zip", slug, j.kind, timestamp)
	archiveAbsPath := filepath.Join(baseAbs, archiveName)
	archiveRelPath := filepath.Join(baseRel, archiveName)

	manifestFileName := fmt.Sprintf("%s_manifest.json", strings.TrimSuffix(archiveName, ".zip"))
	manifestAbsPath := filepath.Join(baseAbs, manifestFileName)
	manifestRelPath := filepath.Join(baseRel, manifestFileName)

	archiveFile, err := os.Create(archiveAbsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create archive: %w", err)
	}
	defer func() {
		_ = archiveFile.Close()
	}()
	cleanupArchive := true
	defer func() {
		if cleanupArchive {
			_ = os.Remove(archiveAbsPath)
		}
	}()

	zipWriter := zip.NewWriter(archiveFile)
	defer func() {
		_ = zipWriter.Close()
	}()

	items := make([]models.BackupItem, 0, len(torrents))
	manifestItems := make([]ManifestItem, 0, len(torrents))
	usedPaths := make(map[string]int)
	categoryCounts := make(map[string]int)
	var totalBytes int64

	// Initialize progress tracking
	s.setProgress(j.runID, 0, len(torrents))

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
			var tracker string
			data, suggestedName, tracker, err = s.syncManager.ExportTorrent(ctx, j.instanceID, torrent.Hash)
			if err != nil {
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
			blobName := hex.EncodeToString(sum[:]) + ".torrent"
			absBlob := filepath.Join(s.cacheDir, blobName)
			if _, err := os.Stat(absBlob); errors.Is(err, os.ErrNotExist) {
				if err := os.WriteFile(absBlob, data, 0o644); err != nil && !errors.Is(err, os.ErrExist) {
					return nil, fmt.Errorf("cache torrent blob: %w", err)
				}
			}
			rel := filepath.ToSlash(filepath.Join("backups", "torrents", blobName))
			blobRelPath = &rel
		}

		header := &zip.FileHeader{
			Name:   uniquePath,
			Method: zip.Deflate,
		}
		header.Modified = s.now()

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("create zip entry: %w", err)
		}

		if _, err := writer.Write(data); err != nil {
			return nil, fmt.Errorf("write zip entry: %w", err)
		}

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
		s.setProgress(j.runID, idx+1, len(torrents))
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

	manifestHeader := &zip.FileHeader{
		Name:   "manifest.json",
		Method: zip.Deflate,
	}
	manifestHeader.Modified = s.now()
	manifestWriter, err := zipWriter.CreateHeader(manifestHeader)
	if err != nil {
		return nil, fmt.Errorf("create manifest entry: %w", err)
	}
	if _, err := manifestWriter.Write(manifestData); err != nil {
		return nil, fmt.Errorf("write manifest entry: %w", err)
	}

	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("finalize archive: %w", err)
	}

	if err := archiveFile.Close(); err != nil {
		return nil, fmt.Errorf("close archive: %w", err)
	}

	manifestPointer := &manifestRelPath
	if err := os.WriteFile(manifestAbsPath, manifestData, 0o644); err != nil {
		log.Warn().Err(err).Str("path", manifestAbsPath).Msg("Failed to write manifest to disk")
		manifestPointer = nil
	}

	cleanupArchive = false

	return &backupResult{
		archiveRelPath:  archiveRelPath,
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

func (s *Service) resolveBasePaths(ctx context.Context, settings *models.BackupSettings, instanceID int) (string, string, error) {
	if settings.CustomPath != nil {
		custom := strings.TrimSpace(*settings.CustomPath)
		if custom != "" {
			if filepath.IsAbs(custom) {
				return "", "", errors.New("custom backup path must be relative to data dir")
			}
			base := filepath.Clean(custom)
			if s.cfg.DataDir == "" {
				return "", "", errors.New("data directory not configured")
			}
			abs := filepath.Join(s.cfg.DataDir, base)
			return abs, base, nil
		}
	}

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

func (s *Service) setProgress(runID int64, current, total int) {
	percentage := 0.0
	if total > 0 {
		percentage = float64(current) / float64(total) * 100.0
	}

	s.progressMu.Lock()
	s.progress[runID] = &BackupProgress{
		Current:    current,
		Total:      total,
		Percentage: percentage,
	}
	s.progressMu.Unlock()
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

		evaluate := func(kind models.BackupRunKind, enabled bool, interval time.Duration, monthly bool) {
			if !enabled {
				return
			}
			lastRun, err := s.store.LatestRunByKind(ctx, cfg.InstanceID, kind)
			if err == nil {
				if lastRun.Status == models.BackupRunStatusRunning {
					return
				}
				ref := lastRun.CompletedAt
				if ref == nil {
					ref = &lastRun.RequestedAt
				}
				if monthly {
					next := ref.AddDate(0, 1, 0)
					if now.Before(next) {
						return
					}
				} else if ref.Add(interval).After(now) {
					return
				}
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				log.Warn().Err(err).Int("instanceID", cfg.InstanceID).Msg("Failed to check last backup run")
				return
			}

			if _, err := s.QueueRun(ctx, cfg.InstanceID, kind, "scheduler"); err != nil {
				if !errors.Is(err, ErrInstanceBusy) {
					log.Warn().Err(err).Int("instanceID", cfg.InstanceID).Msg("Failed to queue scheduled backup")
				}
			}
		}

		evaluate(models.BackupRunKindHourly, cfg.HourlyEnabled, time.Hour, false)
		evaluate(models.BackupRunKindDaily, cfg.DailyEnabled, 24*time.Hour, false)
		evaluate(models.BackupRunKindWeekly, cfg.WeeklyEnabled, 7*24*time.Hour, false)
		evaluate(models.BackupRunKindMonthly, cfg.MonthlyEnabled, 0, true)
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

	for _, runID := range runIDs {
		run, err := s.store.GetRun(ctx, runID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			log.Warn().Err(err).Int64("runID", runID).Msg("Failed to lookup run for cleanup")
			continue
		}

		items, err := s.store.ListItems(ctx, runID)
		if err != nil {
			log.Warn().Err(err).Int64("runID", runID).Msg("Failed to list backup items for cleanup")
		}

		if run.ManifestPath != nil {
			manifestPath := filepath.Join(s.cfg.DataDir, *run.ManifestPath)
			if err := os.Remove(manifestPath); err != nil && !os.IsNotExist(err) {
				log.Warn().
					Err(err).
					Int64("runID", runID).
					Str("path", manifestPath).
					Msg("Failed to remove backup manifest file during retention cleanup")
			}
		}
		if run.ArchivePath != nil {
			archivePath := filepath.Join(s.cfg.DataDir, *run.ArchivePath)
			if err := os.Remove(archivePath); err != nil && !os.IsNotExist(err) {
				log.Warn().
					Err(err).
					Int64("runID", runID).
					Str("path", archivePath).
					Msg("Failed to remove backup archive during retention cleanup")
			}
		}
		if err := s.store.CleanupRun(ctx, runID); err != nil {
			log.Warn().Err(err).Int64("runID", runID).Msg("Failed to cleanup run from database")
		}

		s.cleanupTorrentBlobs(ctx, items)
	}

	return nil
}

func (s *Service) GetSettings(ctx context.Context, instanceID int) (*models.BackupSettings, error) {
	settings, err := s.store.GetSettings(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	s.normalizeAndPersistSettings(ctx, settings)

	return settings, nil
}

func (s *Service) UpdateSettings(ctx context.Context, settings *models.BackupSettings) error {
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

	if run.ManifestPath != nil {
		manifestPath := filepath.Join(s.cfg.DataDir, *run.ManifestPath)
		if err := os.Remove(manifestPath); err != nil && !os.IsNotExist(err) {
			log.Warn().
				Err(err).
				Int64("runID", runID).
				Str("path", manifestPath).
				Msg("Failed to remove backup manifest file during run deletion")
		}
	}
	if run.ArchivePath != nil {
		archivePath := filepath.Join(s.cfg.DataDir, *run.ArchivePath)
		if err := os.Remove(archivePath); err != nil && !os.IsNotExist(err) {
			log.Warn().
				Err(err).
				Int64("runID", runID).
				Str("path", archivePath).
				Msg("Failed to remove backup archive during run deletion")
		}
	}

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

		count, err := s.store.CountBlobReferences(ctx, rel)
		if err != nil {
			log.Warn().Err(err).Str("blob", rel).Msg("Failed to count torrent blob references")
			continue
		}
		if count > 0 {
			continue
		}

		if s.cfg.DataDir == "" {
			log.Warn().Str("blob", rel).Msg("Cannot cleanup torrent blob without data directory")
			continue
		}

		abs := filepath.Join(s.cfg.DataDir, rel)
		if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("blob", rel).Msg("Failed to delete torrent blob")
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
