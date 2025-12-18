// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package trackerrules enforces tracker-scoped speed/ratio rules per instance.
package trackerrules

import (
	"context"
	"encoding/json"
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
	ScanInterval          time.Duration
	SkipWithin            time.Duration
	MaxBatchHashes        int
	ActivityRetentionDays int
}

func DefaultConfig() Config {
	return Config{
		ScanInterval:          20 * time.Second,
		SkipWithin:            2 * time.Minute,
		MaxBatchHashes:        50, // matches qBittorrent's max_concurrent_http_announces default
		ActivityRetentionDays: 7,
	}
}

// Service periodically applies tracker rules to torrents for all active instances.
type Service struct {
	cfg           Config
	instanceStore *models.InstanceStore
	ruleStore     *models.TrackerRuleStore
	activityStore *models.TrackerRuleActivityStore
	syncManager   *qbittorrent.SyncManager

	// keep lightweight memory of recent applications to avoid hammering qBittorrent
	lastApplied map[int]map[string]time.Time // instanceID -> hash -> timestamp
	lastDeleted map[int]map[string]time.Time // instanceID -> hash -> timestamp (tracks deleted torrents)
	mu          sync.RWMutex
}

func NewService(cfg Config, instanceStore *models.InstanceStore, ruleStore *models.TrackerRuleStore, activityStore *models.TrackerRuleActivityStore, syncManager *qbittorrent.SyncManager) *Service {
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = DefaultConfig().ScanInterval
	}
	if cfg.SkipWithin <= 0 {
		cfg.SkipWithin = DefaultConfig().SkipWithin
	}
	if cfg.MaxBatchHashes <= 0 {
		cfg.MaxBatchHashes = DefaultConfig().MaxBatchHashes
	}
	if cfg.ActivityRetentionDays <= 0 {
		cfg.ActivityRetentionDays = DefaultConfig().ActivityRetentionDays
	}
	return &Service{
		cfg:           cfg,
		instanceStore: instanceStore,
		ruleStore:     ruleStore,
		activityStore: activityStore,
		syncManager:   syncManager,
		lastApplied:   make(map[int]map[string]time.Time),
		lastDeleted:   make(map[int]map[string]time.Time),
	}
}

// cleanupStaleEntries removes entries from lastApplied and lastDeleted maps
// that are older than 10 minutes to prevent unbounded memory growth.
func (s *Service) cleanupStaleEntries() {
	cutoff := time.Now().Add(-10 * time.Minute)
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, instMap := range s.lastApplied {
		for hash, ts := range instMap {
			if ts.Before(cutoff) {
				delete(instMap, hash)
			}
		}
	}
	for _, instMap := range s.lastDeleted {
		for hash, ts := range instMap {
			if ts.Before(cutoff) {
				delete(instMap, hash)
			}
		}
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

	// Prune old activity on startup
	if s.activityStore != nil {
		if pruned, err := s.activityStore.Prune(ctx, s.cfg.ActivityRetentionDays); err != nil {
			log.Warn().Err(err).Msg("tracker rules: failed to prune old activity")
		} else if pruned > 0 {
			log.Info().Int64("count", pruned).Msg("tracker rules: pruned old activity entries")
		}
	}

	lastPrune := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.applyAll(ctx)

			// Prune hourly
			if time.Since(lastPrune) > time.Hour {
				if s.activityStore != nil {
					if pruned, err := s.activityStore.Prune(ctx, s.cfg.ActivityRetentionDays); err != nil {
						log.Warn().Err(err).Msg("tracker rules: failed to prune old activity")
					} else if pruned > 0 {
						log.Info().Int64("count", pruned).Msg("tracker rules: pruned old activity entries")
					}
				}
				s.cleanupStaleEntries()
				lastPrune = time.Now()
			}
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
				if s.activityStore != nil {
					detailsJSON, _ := json.Marshal(map[string]any{"limitKiB": limit, "count": len(batch), "type": "upload"})
					if err := s.activityStore.Create(ctx, &models.TrackerRuleActivity{
						InstanceID: instanceID,
						Hash:       strings.Join(batch, ","),
						Action:     models.ActivityActionLimitFailed,
						Outcome:    models.ActivityOutcomeFailed,
						Reason:     "upload limit failed: " + err.Error(),
						Details:    detailsJSON,
					}); err != nil {
						log.Warn().Err(err).Int("instanceID", instanceID).Msg("tracker rules: failed to record activity")
					}
				}
			}
		}
	}

	for limit, hashes := range downloadBatches {
		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.SetTorrentDownloadLimit(ctx, instanceID, batch, limit); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Int64("limitKiB", limit).Int("count", len(batch)).Msg("tracker rules: download limit failed")
				if s.activityStore != nil {
					detailsJSON, _ := json.Marshal(map[string]any{"limitKiB": limit, "count": len(batch), "type": "download"})
					if err := s.activityStore.Create(ctx, &models.TrackerRuleActivity{
						InstanceID: instanceID,
						Hash:       strings.Join(batch, ","),
						Action:     models.ActivityActionLimitFailed,
						Outcome:    models.ActivityOutcomeFailed,
						Reason:     "download limit failed: " + err.Error(),
						Details:    detailsJSON,
					}); err != nil {
						log.Warn().Err(err).Int("instanceID", instanceID).Msg("tracker rules: failed to record activity")
					}
				}
			}
		}
	}

	for key, hashes := range shareBatches {
		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.SetTorrentShareLimit(ctx, instanceID, batch, key.ratio, key.seed, -1); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Float64("ratio", key.ratio).Int64("seedMinutes", key.seed).Int("count", len(batch)).Msg("tracker rules: share limit failed")
				if s.activityStore != nil {
					detailsJSON, _ := json.Marshal(map[string]any{"ratio": key.ratio, "seedMinutes": key.seed, "count": len(batch), "type": "share"})
					if err := s.activityStore.Create(ctx, &models.TrackerRuleActivity{
						InstanceID: instanceID,
						Hash:       strings.Join(batch, ","),
						Action:     models.ActivityActionLimitFailed,
						Outcome:    models.ActivityOutcomeFailed,
						Reason:     "share limit failed: " + err.Error(),
						Details:    detailsJSON,
					}); err != nil {
						log.Warn().Err(err).Int("instanceID", instanceID).Msg("tracker rules: failed to record activity")
					}
				}
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

	// Pending deletion info for activity recording
	type pendingDeletion struct {
		hash          string
		torrentName   string
		trackerDomain string
		action        string // "deleted_ratio", "deleted_seeding", "deleted_unregistered"
		ruleID        int
		ruleName      string
		reason        string
		details       map[string]any
	}

	// Batch torrents for deletion by delete mode
	deleteHashesByMode := make(map[string][]string)   // "delete" or "deleteWithFiles" -> hashes
	pendingByHash := make(map[string]pendingDeletion) // hash -> deletion context
	queuedForDeletion := make(map[string]struct{})    // track hashes already queued

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

			// Determine which criterion triggered the deletion
			var reason string
			hasRatioLimit := rule.RatioLimit != nil && *rule.RatioLimit > 0
			hasSeedingTimeLimit := rule.SeedingTimeLimitMinutes != nil && *rule.SeedingTimeLimitMinutes > 0
			ratioMet := hasRatioLimit && torrent.Ratio >= *rule.RatioLimit
			seedingTimeMet := hasSeedingTimeLimit && torrent.SeedingTime >= *rule.SeedingTimeLimitMinutes*60

			if ratioMet && seedingTimeMet {
				reason = "ratio and seeding time limits reached"
			} else if ratioMet {
				reason = "ratio limit reached"
			} else {
				reason = "seeding time limit reached"
			}

			// Handle cross-seed aware deletion
			var actualMode string
			var logMsg string
			var keepingFiles bool

			if deleteMode == "deleteWithFilesPreserveCrossSeeds" {
				if detectCrossSeeds(torrent, torrents) {
					actualMode = "delete"
					logMsg = "tracker rules: removing torrent (cross-seed detected - keeping files)"
					keepingFiles = true
				} else {
					actualMode = "deleteWithFiles"
					logMsg = "tracker rules: removing torrent with files"
					keepingFiles = false
				}
			} else if deleteMode == "delete" {
				actualMode = "delete"
				logMsg = "tracker rules: removing torrent (keeping files)"
				keepingFiles = true
			} else {
				actualMode = deleteMode
				logMsg = "tracker rules: removing torrent with files"
				keepingFiles = false
			}

			logEvent := log.Info().Str("hash", torrent.Hash).Str("name", torrent.Name).Str("reason", reason)
			if ratioMet {
				logEvent = logEvent.Float64("ratio", torrent.Ratio).Float64("ratioLimit", *rule.RatioLimit)
			}
			if seedingTimeMet {
				logEvent = logEvent.Int64("seedingMinutes", torrent.SeedingTime/60).Int64("seedingLimitMinutes", *rule.SeedingTimeLimitMinutes)
			}
			logEvent.Bool("filesKept", keepingFiles).Msg(logMsg)
			deleteHashesByMode[actualMode] = append(deleteHashesByMode[actualMode], torrent.Hash)
			queuedForDeletion[torrent.Hash] = struct{}{}

			// Track pending deletion for activity recording
			action := models.ActivityActionDeletedRatio
			if seedingTimeMet && !ratioMet {
				action = models.ActivityActionDeletedSeeding
			}
			details := map[string]any{"filesKept": keepingFiles, "deleteMode": deleteMode}
			if ratioMet {
				details["ratio"] = torrent.Ratio
				details["ratioLimit"] = *rule.RatioLimit
			}
			if seedingTimeMet {
				details["seedingMinutes"] = torrent.SeedingTime / 60
				details["seedingLimitMinutes"] = *rule.SeedingTimeLimitMinutes
			}
			// Get primary tracker domain for activity record
			trackerDomain := ""
			if domains := collectTrackerDomains(torrent, s.syncManager); len(domains) > 0 {
				trackerDomain = domains[0]
			}
			pendingByHash[torrent.Hash] = pendingDeletion{
				hash:          torrent.Hash,
				torrentName:   torrent.Name,
				trackerDomain: trackerDomain,
				action:        action,
				ruleID:        rule.ID,
				ruleName:      rule.Name,
				reason:        reason,
				details:       details,
			}
		}
	}

	// Process unregistered torrents (separate from ratio/seeding time based deletions)
	healthCounts := s.syncManager.GetTrackerHealthCounts(instanceID)
	if healthCounts != nil && len(healthCounts.UnregisteredSet) > 0 {
		for _, torrent := range torrents {
			// Skip if already queued for deletion (e.g., by ratio/seeding limits)
			if _, queued := queuedForDeletion[torrent.Hash]; queued {
				continue
			}

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
			var actualMode string
			var logMsg string
			var keepingFiles bool

			if deleteMode == "deleteWithFilesPreserveCrossSeeds" {
				if detectCrossSeeds(torrent, torrents) {
					actualMode = "delete"
					logMsg = "tracker rules: removing unregistered torrent (cross-seed detected - keeping files)"
					keepingFiles = true
				} else {
					actualMode = "deleteWithFiles"
					logMsg = "tracker rules: removing unregistered torrent with files"
					keepingFiles = false
				}
			} else if deleteMode == "delete" {
				actualMode = "delete"
				logMsg = "tracker rules: removing unregistered torrent (keeping files)"
				keepingFiles = true
			} else {
				actualMode = deleteMode
				logMsg = "tracker rules: removing unregistered torrent with files"
				keepingFiles = false
			}

			log.Info().Str("hash", torrent.Hash).Str("name", torrent.Name).Str("reason", "unregistered").Bool("filesKept", keepingFiles).Msg(logMsg)
			deleteHashesByMode[actualMode] = append(deleteHashesByMode[actualMode], torrent.Hash)

			// Track pending deletion for activity recording
			trackerDomain := ""
			if domains := collectTrackerDomains(torrent, s.syncManager); len(domains) > 0 {
				trackerDomain = domains[0]
			}
			pendingByHash[torrent.Hash] = pendingDeletion{
				hash:          torrent.Hash,
				torrentName:   torrent.Name,
				trackerDomain: trackerDomain,
				action:        models.ActivityActionDeletedUnregistered,
				ruleID:        rule.ID,
				ruleName:      rule.Name,
				reason:        "unregistered",
				details:       map[string]any{"filesKept": keepingFiles, "deleteMode": deleteMode},
			}
		}
	}

	// Execute deletions
	//
	// Note on tracker announces: No explicit pause/reannounce step is needed before
	// deletion. When qBittorrent's DeleteTorrents API is called, libtorrent automatically
	// sends a "stopped" announce to all trackers with the final uploaded/downloaded stats.
	//
	// References:
	// - libtorrent/src/torrent.cpp:stop_announcing() - sends stopped event to all trackers
	// - qBittorrent/src/base/bittorrent/sessionimpl.cpp:removeTorrent() - triggers libtorrent removal
	// - stop_tracker_timeout setting (default 2s) controls how long to wait for tracker ack
	//
	// This behavior is identical for both BitTorrent v1 and v2 torrents.
	for mode, hashes := range deleteHashesByMode {
		if len(hashes) == 0 {
			continue
		}

		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.BulkAction(ctx, instanceID, batch, mode); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Str("action", mode).Int("count", len(batch)).Strs("hashes", batch).Msg("tracker rules: delete failed")

				// Record failed deletion activity
				if s.activityStore != nil {
					for _, hash := range batch {
						if pending, ok := pendingByHash[hash]; ok {
							detailsJSON, _ := json.Marshal(pending.details)
							if err := s.activityStore.Create(ctx, &models.TrackerRuleActivity{
								InstanceID:    instanceID,
								Hash:          hash,
								TorrentName:   pending.torrentName,
								TrackerDomain: pending.trackerDomain,
								Action:        models.ActivityActionDeleteFailed,
								RuleID:        &pending.ruleID,
								RuleName:      pending.ruleName,
								Outcome:       models.ActivityOutcomeFailed,
								Reason:        err.Error(),
								Details:       detailsJSON,
							}); err != nil {
								log.Warn().Err(err).Str("hash", hash).Int("instanceID", instanceID).Msg("tracker rules: failed to record activity")
							}
						}
					}
				}
			} else {
				// Mark as processed only after successful deletion
				s.mu.Lock()
				for _, hash := range batch {
					instDeletedMap[hash] = now
				}
				s.mu.Unlock()

				if mode == "delete" {
					log.Info().Int("instanceID", instanceID).Int("count", len(batch)).Msg("tracker rules: removed torrents (files kept)")
				} else {
					log.Info().Int("instanceID", instanceID).Int("count", len(batch)).Msg("tracker rules: removed torrents with files")
				}

				// Record successful deletion activity
				if s.activityStore != nil {
					for _, hash := range batch {
						if pending, ok := pendingByHash[hash]; ok {
							detailsJSON, _ := json.Marshal(pending.details)
							if err := s.activityStore.Create(ctx, &models.TrackerRuleActivity{
								InstanceID:    instanceID,
								Hash:          hash,
								TorrentName:   pending.torrentName,
								TrackerDomain: pending.trackerDomain,
								Action:        pending.action,
								RuleID:        &pending.ruleID,
								RuleName:      pending.ruleName,
								Outcome:       models.ActivityOutcomeSuccess,
								Reason:        pending.reason,
								Details:       detailsJSON,
							}); err != nil {
								log.Warn().Err(err).Str("hash", hash).Int("instanceID", instanceID).Msg("tracker rules: failed to record activity")
							}
						}
					}
				}
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
		// Check if torrent's category matches ANY of the rule's categories
		if len(rule.Categories) > 0 {
			matched := false
			for _, cat := range rule.Categories {
				if strings.EqualFold(torrent.Category, cat) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		// Check if torrent has the rule's tags based on match mode
		if len(rule.Tags) > 0 {
			if rule.TagMatchMode == models.TagMatchModeAll {
				// ALL: torrent must have every tag in the rule
				allMatched := true
				for _, tag := range rule.Tags {
					if !torrentHasTag(torrent.Tags, tag) {
						allMatched = false
						break
					}
				}
				if !allMatched {
					continue
				}
			} else {
				// ANY (default): torrent must have at least one tag
				matched := false
				for _, tag := range rule.Tags {
					if torrentHasTag(torrent.Tags, tag) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}
		}
		return rule
	}

	return nil
}

func matchesTracker(pattern string, domains []string) bool {
	if pattern == "*" {
		return true // Match all trackers
	}
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
