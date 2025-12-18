// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package trackerrules enforces tracker-scoped speed/ratio rules per instance.
package trackerrules

import (
	"context"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// Config controls how often rules are re-applied and how long to debounce repeats.
type Config struct {
	ScanInterval   time.Duration
	SkipWithin     time.Duration
	MaxBatchHashes int
}

func DefaultConfig() Config {
	return Config{
		ScanInterval:   20 * time.Second,
		SkipWithin:     2 * time.Minute,
		MaxBatchHashes: 150,
	}
}

// Service periodically applies tracker rules to torrents for all active instances.
type Service struct {
	cfg           Config
	instanceStore *models.InstanceStore
	ruleStore     *models.TrackerRuleStore
	syncManager   *qbittorrent.SyncManager

	// keep lightweight memory of recent applications to avoid hammering qBittorrent
	lastApplied map[int]map[string]time.Time // instanceID -> hash -> timestamp
	lastDeleted map[int]map[string]time.Time // instanceID -> hash -> timestamp (tracks deleted torrents)
	mu          sync.RWMutex
}

func NewService(cfg Config, instanceStore *models.InstanceStore, ruleStore *models.TrackerRuleStore, syncManager *qbittorrent.SyncManager) *Service {
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = DefaultConfig().ScanInterval
	}
	if cfg.SkipWithin <= 0 {
		cfg.SkipWithin = DefaultConfig().SkipWithin
	}
	if cfg.MaxBatchHashes <= 0 {
		cfg.MaxBatchHashes = DefaultConfig().MaxBatchHashes
	}
	return &Service{
		cfg:           cfg,
		instanceStore: instanceStore,
		ruleStore:     ruleStore,
		syncManager:   syncManager,
		lastApplied:   make(map[int]map[string]time.Time),
		lastDeleted:   make(map[int]map[string]time.Time),
	}
}

func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	go s.loop(ctx)
}

func (s *Service) loop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.applyAll(ctx)
		}
	}
}

func (s *Service) applyAll(ctx context.Context) {
	if s == nil || s.syncManager == nil || s.ruleStore == nil || s.instanceStore == nil {
		return
	}

	instances, err := s.instanceStore.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("tracker rules: failed to list instances")
		return
	}

	for _, instance := range instances {
		if !instance.IsActive {
			continue
		}
		if err := s.applyForInstance(ctx, instance.ID); err != nil {
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("tracker rules: apply failed")
		}
	}
}

// ApplyOnceForInstance allows manual triggering (API hook).
func (s *Service) ApplyOnceForInstance(ctx context.Context, instanceID int) error {
	return s.applyForInstance(ctx, instanceID)
}

func (s *Service) applyForInstance(ctx context.Context, instanceID int) error {
	rules, err := s.ruleStore.ListByInstance(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("tracker rules: failed to load rules")
		return err
	}
	if len(rules) == 0 {
		return nil
	}

	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("tracker rules: unable to fetch torrents")
		return err
	}

	if len(torrents) == 0 {
		return nil
	}

	// snapshot lastApplied map for this instance under lock and ensure it's initialized
	s.mu.RLock()
	instLastApplied, ok := s.lastApplied[instanceID]
	s.mu.RUnlock()
	if !ok || instLastApplied == nil {
		s.mu.Lock()
		if s.lastApplied[instanceID] == nil { // re-check in case another goroutine initialized it
			s.lastApplied[instanceID] = make(map[string]time.Time)
		}
		instLastApplied = s.lastApplied[instanceID]
		s.mu.Unlock()
	}

	now := time.Now()

	type shareKey struct {
		ratio float64
		seed  int64
	}
	shareBatches := make(map[shareKey][]string)
	uploadBatches := make(map[int64][]string)
	downloadBatches := make(map[int64][]string)

	for _, torrent := range torrents {
		s.mu.RLock()
		ts, ok := instLastApplied[torrent.Hash]
		s.mu.RUnlock()
		if ok && now.Sub(ts) < s.cfg.SkipWithin {
			continue
		}

		rule := selectRule(torrent, rules, s.syncManager)
		if rule == nil {
			continue
		}

		if rule.UploadLimitKiB != nil {
			desired := *rule.UploadLimitKiB * 1024
			if torrent.UpLimit != desired {
				uploadBatches[*rule.UploadLimitKiB] = append(uploadBatches[*rule.UploadLimitKiB], torrent.Hash)
			}
		}
		if rule.DownloadLimitKiB != nil {
			desired := *rule.DownloadLimitKiB * 1024
			if torrent.DlLimit != desired {
				downloadBatches[*rule.DownloadLimitKiB] = append(downloadBatches[*rule.DownloadLimitKiB], torrent.Hash)
			}
		}
		if rule.RatioLimit != nil || rule.SeedingTimeLimitMinutes != nil {
			ratio := torrent.RatioLimit
			if rule.RatioLimit != nil {
				ratio = *rule.RatioLimit
			}
			seedMinutes := torrent.SeedingTimeLimit
			if rule.SeedingTimeLimitMinutes != nil {
				seedMinutes = *rule.SeedingTimeLimitMinutes
			}
			currentKey := shareKey{ratio: ratio, seed: seedMinutes}
			needsShareUpdate := (rule.RatioLimit != nil && torrent.RatioLimit != ratio) ||
				(rule.SeedingTimeLimitMinutes != nil && torrent.SeedingTimeLimit != seedMinutes)
			if needsShareUpdate {
				shareBatches[currentKey] = append(shareBatches[currentKey], torrent.Hash)
			}
		}

		s.mu.Lock()
		instLastApplied[torrent.Hash] = now
		s.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	for limit, hashes := range uploadBatches {
		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.SetTorrentUploadLimit(ctx, instanceID, batch, limit); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Int64("limitKiB", limit).Int("count", len(batch)).Msg("tracker rules: upload limit failed")
			}
		}
	}

	for limit, hashes := range downloadBatches {
		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.SetTorrentDownloadLimit(ctx, instanceID, batch, limit); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Int64("limitKiB", limit).Int("count", len(batch)).Msg("tracker rules: download limit failed")
			}
		}
	}

	for key, hashes := range shareBatches {
		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.SetTorrentShareLimit(ctx, instanceID, batch, key.ratio, key.seed, -1); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Float64("ratio", key.ratio).Int64("seedMinutes", key.seed).Int("count", len(batch)).Msg("tracker rules: share limit failed")
			}
		}
	}

	// Process deletions for torrents that have reached their limits
	s.mu.RLock()
	instDeletedMap, deletedMapOk := s.lastDeleted[instanceID]
	s.mu.RUnlock()
	if !deletedMapOk || instDeletedMap == nil {
		s.mu.Lock()
		if s.lastDeleted[instanceID] == nil {
			s.lastDeleted[instanceID] = make(map[string]time.Time)
		}
		instDeletedMap = s.lastDeleted[instanceID]
		s.mu.Unlock()
	}

	// Batch torrents for deletion by delete mode
	deleteHashesByMode := make(map[string][]string) // "delete" or "deleteWithFiles" -> hashes

	for _, torrent := range torrents {
		// Skip if recently processed for deletion (avoid duplicate attempts)
		s.mu.RLock()
		if ts, deleted := instDeletedMap[torrent.Hash]; deleted {
			if now.Sub(ts) < 5*time.Minute {
				s.mu.RUnlock()
				continue
			}
		}
		s.mu.RUnlock()

		rule := selectRule(torrent, rules, s.syncManager)
		if rule == nil {
			continue
		}

		if shouldDeleteTorrent(torrent, rule) {
			deleteMode := *rule.DeleteMode

			// Handle cross-seed aware deletion
			if deleteMode == "deleteWithFilesPreserveCrossSeeds" {
				if detectCrossSeeds(torrent, torrents) {
					// Cross-seed found: preserve files, only remove torrent entry
					log.Info().Str("hash", torrent.Hash).Str("name", torrent.Name).Msg("tracker rules: cross-seed detected, preserving files")
					deleteHashesByMode["delete"] = append(deleteHashesByMode["delete"], torrent.Hash)
				} else {
					// No cross-seed: safe to delete files
					deleteHashesByMode["deleteWithFiles"] = append(deleteHashesByMode["deleteWithFiles"], torrent.Hash)
				}
			} else {
				deleteHashesByMode[deleteMode] = append(deleteHashesByMode[deleteMode], torrent.Hash)
			}

			// Mark as processed for deletion
			s.mu.Lock()
			instDeletedMap[torrent.Hash] = now
			s.mu.Unlock()
		}
	}

	// Process unregistered torrents (separate from ratio/seeding time based deletions)
	healthCounts := s.syncManager.GetTrackerHealthCounts(instanceID)
	if healthCounts != nil && len(healthCounts.UnregisteredSet) > 0 {
		for _, torrent := range torrents {
			// Skip if not unregistered
			if _, isUnregistered := healthCounts.UnregisteredSet[torrent.Hash]; !isUnregistered {
				continue
			}

			// Skip if recently processed for deletion
			s.mu.RLock()
			if ts, deleted := instDeletedMap[torrent.Hash]; deleted {
				if now.Sub(ts) < 5*time.Minute {
					s.mu.RUnlock()
					continue
				}
			}
			s.mu.RUnlock()

			// Find matching rule with DeleteUnregistered enabled
			rule := selectRule(torrent, rules, s.syncManager)
			if rule == nil || !rule.DeleteUnregistered {
				continue
			}

			// Must have delete mode set
			if rule.DeleteMode == nil || *rule.DeleteMode == "" || *rule.DeleteMode == "none" {
				continue
			}

			deleteMode := *rule.DeleteMode

			// Handle cross-seed aware mode (reuse existing logic)
			if deleteMode == "deleteWithFilesPreserveCrossSeeds" {
				if detectCrossSeeds(torrent, torrents) {
					log.Info().Str("hash", torrent.Hash).Str("name", torrent.Name).Msg("tracker rules: unregistered torrent has cross-seed, preserving files")
					deleteHashesByMode["delete"] = append(deleteHashesByMode["delete"], torrent.Hash)
				} else {
					deleteHashesByMode["deleteWithFiles"] = append(deleteHashesByMode["deleteWithFiles"], torrent.Hash)
				}
			} else {
				deleteHashesByMode[deleteMode] = append(deleteHashesByMode[deleteMode], torrent.Hash)
			}

			log.Info().Str("hash", torrent.Hash).Str("name", torrent.Name).Str("mode", deleteMode).Msg("tracker rules: queuing unregistered torrent for deletion")

			// Mark as processed for deletion
			s.mu.Lock()
			instDeletedMap[torrent.Hash] = now
			s.mu.Unlock()
		}
	}

	// Execute deletions
	for mode, hashes := range deleteHashesByMode {
		if len(hashes) == 0 {
			continue
		}

		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.BulkAction(ctx, instanceID, batch, mode); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Str("action", mode).Int("count", len(batch)).Msg("tracker rules: delete failed")
			} else {
				log.Info().Int("instanceID", instanceID).Str("action", mode).Int("count", len(batch)).Msg("tracker rules: deleted torrents")
			}
		}
	}

	return nil
}

func limitHashBatch(hashes []string, max int) [][]string {
	if max <= 0 || len(hashes) <= max {
		return [][]string{hashes}
	}
	var batches [][]string
	for len(hashes) > 0 {
		end := max
		if len(hashes) < max {
			end = len(hashes)
		}
		batches = append(batches, slices.Clone(hashes[:end]))
		hashes = hashes[end:]
	}
	return batches
}

func selectRule(torrent qbt.Torrent, rules []*models.TrackerRule, sm *qbittorrent.SyncManager) *models.TrackerRule {
	trackerDomains := collectTrackerDomains(torrent, sm)

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if !matchesTracker(rule.TrackerPattern, trackerDomains) {
			continue
		}
		if rule.Category != nil && strings.TrimSpace(*rule.Category) != "" {
			if !strings.EqualFold(torrent.Category, strings.TrimSpace(*rule.Category)) {
				continue
			}
		}
		if rule.Tag != nil && strings.TrimSpace(*rule.Tag) != "" {
			if !torrentHasTag(torrent.Tags, strings.TrimSpace(*rule.Tag)) {
				continue
			}
		}
		return rule
	}

	return nil
}

func matchesTracker(pattern string, domains []string) bool {
	if pattern == "" {
		return false
	}

	tokens := strings.FieldsFunc(pattern, func(r rune) bool {
		return r == ',' || r == ';' || r == '|'
	})

	for _, token := range tokens {
		normalized := strings.ToLower(strings.TrimSpace(token))
		if normalized == "" {
			continue
		}
		isGlob := strings.ContainsAny(normalized, "*?")

		for _, domain := range domains {
			d := strings.ToLower(domain)
			if isGlob {
				ok, err := path.Match(normalized, d)
				if err != nil {
					log.Error().Err(err).Str("pattern", normalized).Msg("tracker rules: invalid glob pattern")
					continue
				}
				if ok {
					return true
				}
			} else if d == normalized {
				return true
			} else if strings.HasPrefix(normalized, ".") && strings.HasSuffix(d, normalized) {
				return true
			}
		}
	}

	return false
}

func collectTrackerDomains(t qbt.Torrent, sm *qbittorrent.SyncManager) []string {
	domainSet := make(map[string]struct{})

	if t.Tracker != "" {
		if domain := sm.ExtractDomainFromURL(t.Tracker); domain != "" && domain != "Unknown" {
			domainSet[domain] = struct{}{}
		}
	}

	for _, tr := range t.Trackers {
		if tr.Url == "" {
			continue
		}
		if domain := sm.ExtractDomainFromURL(tr.Url); domain != "" && domain != "Unknown" {
			domainSet[domain] = struct{}{}
		}
	}

	if len(domainSet) == 0 && t.Tracker != "" {
		if domain := sanitizeTrackerHost(t.Tracker); domain != "" {
			domainSet[domain] = struct{}{}
		}
	}

	var domains []string
	for d := range domainSet {
		domains = append(domains, d)
	}
	slices.Sort(domains)
	return domains
}

func sanitizeTrackerHost(urlOrHost string) string {
	clean := strings.TrimSpace(urlOrHost)
	if clean == "" {
		return ""
	}
	if strings.Contains(clean, "://") {
		return ""
	}
	// Remove URL-like path pieces
	clean = strings.Split(clean, "/")[0]
	clean = strings.Split(clean, ":")[0]
	re := regexp.MustCompile(`[^a-zA-Z0-9\.-]`)
	clean = re.ReplaceAllString(clean, "")
	return clean
}

func torrentHasTag(tags string, candidate string) bool {
	if tags == "" {
		return false
	}
	for _, tag := range strings.Split(tags, ",") {
		if strings.EqualFold(strings.TrimSpace(tag), candidate) {
			return true
		}
	}
	return false
}

// normalizePath standardizes a file path for comparison by lowercasing,
// converting backslashes to forward slashes, and removing trailing slashes.
func normalizePath(p string) string {
	if p == "" {
		return ""
	}
	// Lowercase for case-insensitive comparison
	p = strings.ToLower(p)
	// Normalize path separators (Windows backslashes to forward slashes)
	p = strings.ReplaceAll(p, "\\", "/")
	// Remove trailing slash
	p = strings.TrimSuffix(p, "/")
	return p
}

// detectCrossSeeds checks if any other torrent shares the same ContentPath,
// indicating they are cross-seeds sharing the same data files.
func detectCrossSeeds(target qbt.Torrent, allTorrents []qbt.Torrent) bool {
	targetPath := normalizePath(target.ContentPath)
	if targetPath == "" {
		return false
	}
	for _, other := range allTorrents {
		if other.Hash == target.Hash {
			continue // skip self
		}
		if normalizePath(other.ContentPath) == targetPath {
			return true // cross-seed found
		}
	}
	return false
}

func shouldDeleteTorrent(torrent qbt.Torrent, rule *models.TrackerRule) bool {
	// Only delete completed torrents
	if torrent.Progress < 1.0 {
		return false
	}

	// Must have delete mode enabled
	if rule.DeleteMode == nil || *rule.DeleteMode == "" || *rule.DeleteMode == "none" {
		return false
	}

	// Must have at least one limit configured
	hasRatioLimit := rule.RatioLimit != nil && *rule.RatioLimit > 0
	hasSeedingTimeLimit := rule.SeedingTimeLimitMinutes != nil && *rule.SeedingTimeLimitMinutes > 0

	if !hasRatioLimit && !hasSeedingTimeLimit {
		return false
	}

	// Check ratio limit (if set)
	if hasRatioLimit && torrent.Ratio >= *rule.RatioLimit {
		return true
	}

	// Check seeding time limit (if set) - torrent.SeedingTime is in seconds
	if hasSeedingTimeLimit {
		limitSeconds := *rule.SeedingTimeLimitMinutes * 60
		if torrent.SeedingTime >= limitSeconds {
			return true
		}
	}

	return false
}
