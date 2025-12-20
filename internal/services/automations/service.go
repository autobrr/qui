// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package automations enforces tracker-scoped speed/ratio rules per instance.
package automations

import (
	"context"
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"sort"
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

// Service periodically applies automation rules to torrents for all active instances.
type Service struct {
	cfg           Config
	instanceStore *models.InstanceStore
	ruleStore     *models.AutomationStore
	activityStore *models.AutomationActivityStore
	syncManager   *qbittorrent.SyncManager

	// keep lightweight memory of recent applications to avoid hammering qBittorrent
	lastApplied map[int]map[string]time.Time // instanceID -> hash -> timestamp
	mu          sync.RWMutex
}

func NewService(cfg Config, instanceStore *models.InstanceStore, ruleStore *models.AutomationStore, activityStore *models.AutomationActivityStore, syncManager *qbittorrent.SyncManager) *Service {
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
	}
}

// cleanupStaleEntries removes entries from lastApplied map
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
			log.Warn().Err(err).Msg("automations: failed to prune old activity")
		} else if pruned > 0 {
			log.Info().Int64("count", pruned).Msg("automations: pruned old activity entries")
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
						log.Warn().Err(err).Msg("automations: failed to prune old activity")
					} else if pruned > 0 {
						log.Info().Int64("count", pruned).Msg("automations: pruned old activity entries")
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
		log.Error().Err(err).Msg("automations: failed to list instances")
		return
	}

	for _, instance := range instances {
		if !instance.IsActive {
			continue
		}
		if err := s.applyForInstance(ctx, instance.ID); err != nil {
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("automations: apply failed")
		}
	}
}

// ApplyOnceForInstance allows manual triggering (API hook).
func (s *Service) ApplyOnceForInstance(ctx context.Context, instanceID int) error {
	return s.applyForInstance(ctx, instanceID)
}

// PreviewResult contains torrents that would match a rule.
type PreviewResult struct {
	TotalMatches int              `json:"totalMatches"`
	Examples     []PreviewTorrent `json:"examples"`
}

// PreviewTorrent is a simplified torrent for preview display.
type PreviewTorrent struct {
	Name           string  `json:"name"`
	Hash           string  `json:"hash"`
	Size           int64   `json:"size"`
	Ratio          float64 `json:"ratio"`
	SeedingTime    int64   `json:"seedingTime"`
	Tracker        string  `json:"tracker"`
	Category       string  `json:"category"`
	Tags           string  `json:"tags"`
	State          string  `json:"state"`
	AddedOn        int64   `json:"addedOn"`
	Uploaded       int64   `json:"uploaded"`
	Downloaded     int64   `json:"downloaded"`
	IsUnregistered bool    `json:"isUnregistered,omitempty"`
}

// PreviewDeleteRule returns torrents that would be deleted by the given rule.
// This is used to show users what a rule would affect before saving.
func (s *Service) PreviewDeleteRule(ctx context.Context, instanceID int, rule *models.Automation, limit int, offset int) (*PreviewResult, error) {
	if s == nil || s.syncManager == nil {
		return &PreviewResult{}, nil
	}

	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}

	result := &PreviewResult{
		Examples: make([]PreviewTorrent, 0, limit),
	}

	// Get health counts for unregistered torrent preview and isUnregistered condition evaluation
	var unregisteredSet map[string]struct{}
	var evalCtx *EvalContext
	if healthCounts := s.syncManager.GetTrackerHealthCounts(instanceID); healthCounts != nil && len(healthCounts.UnregisteredSet) > 0 {
		unregisteredSet = healthCounts.UnregisteredSet
		evalCtx = &EvalContext{
			UnregisteredSet: healthCounts.UnregisteredSet,
		}
	}

	matchIndex := 0
	for _, torrent := range torrents {
		// Check tracker match
		trackerDomains := collectTrackerDomains(torrent, s.syncManager)
		if !matchesTracker(rule.TrackerPattern, trackerDomains) {
			continue
		}

		// For legacy mode, also check category/tag filters
		if !rule.UsesExpressions() {
			// Check category filter
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

			// Check tag filter
			if len(rule.Tags) > 0 {
				if rule.TagMatchMode == models.TagMatchModeAll {
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
		}

		// Check if torrent would be deleted
		wouldDelete := false

		if rule.UsesExpressions() {
			// Advanced mode: evaluate delete condition
			if rule.Conditions.Delete != nil && rule.Conditions.Delete.Enabled {
				// Must be completed to delete
				if torrent.Progress >= 1.0 {
					// Evaluate condition (if no condition, match all completed)
					if rule.Conditions.Delete.Condition == nil {
						wouldDelete = true
					} else {
						wouldDelete = EvaluateConditionWithContext(rule.Conditions.Delete.Condition, torrent, evalCtx, 0)
					}
				}
			}
		} else {
			// Legacy mode: use existing shouldDeleteTorrentLegacy logic
			wouldDelete = shouldDeleteTorrentLegacy(torrent, rule)
		}

		// Check for unregistered torrent deletion (legacy mode only - expression mode handles via isUnregistered condition)
		if !wouldDelete && !rule.UsesExpressions() && rule.DeleteUnregistered && unregisteredSet != nil {
			if _, isUnregistered := unregisteredSet[torrent.Hash]; isUnregistered {
				// Must have delete mode set for unregistered deletion
				if rule.DeleteMode != nil && *rule.DeleteMode != "" && *rule.DeleteMode != DeleteModeNone {
					wouldDelete = true
				}
			}
		}

		if wouldDelete {
			matchIndex++
			if matchIndex <= offset {
				continue
			}
			if len(result.Examples) < limit {
				// Get primary tracker domain for display
				tracker := ""
				if domains := collectTrackerDomains(torrent, s.syncManager); len(domains) > 0 {
					tracker = domains[0]
				}
				// Check if torrent is unregistered (safe: nil map returns false)
				var isUnregistered bool
				if unregisteredSet != nil {
					_, isUnregistered = unregisteredSet[torrent.Hash]
				}
				result.Examples = append(result.Examples, PreviewTorrent{
					Name:           torrent.Name,
					Hash:           torrent.Hash,
					Size:           torrent.Size,
					Ratio:          torrent.Ratio,
					SeedingTime:    torrent.SeedingTime,
					Tracker:        tracker,
					Category:       torrent.Category,
					Tags:           torrent.Tags,
					State:          string(torrent.State),
					AddedOn:        torrent.AddedOn,
					Uploaded:       torrent.Uploaded,
					Downloaded:     torrent.Downloaded,
					IsUnregistered: isUnregistered,
				})
			}
		}
	}

	result.TotalMatches = matchIndex
	return result, nil
}

func (s *Service) applyForInstance(ctx context.Context, instanceID int) error {
	rules, err := s.ruleStore.ListByInstance(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to load rules")
		return err
	}
	if len(rules) == 0 {
		return nil
	}

	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("automations: unable to fetch torrents")
		return err
	}

	if len(torrents) == 0 {
		return nil
	}

	// Get health counts for isUnregistered condition evaluation
	var evalCtx *EvalContext
	healthCounts := s.syncManager.GetTrackerHealthCounts(instanceID)
	if healthCounts != nil {
		evalCtx = &EvalContext{
			UnregisteredSet: healthCounts.UnregisteredSet,
		}
	}

	// Ensure lastApplied map is initialized for this instance
	s.mu.RLock()
	instLastApplied, ok := s.lastApplied[instanceID]
	s.mu.RUnlock()
	if !ok || instLastApplied == nil {
		s.mu.Lock()
		if s.lastApplied[instanceID] == nil {
			s.lastApplied[instanceID] = make(map[string]time.Time)
		}
		instLastApplied = s.lastApplied[instanceID]
		s.mu.Unlock()
	}

	now := time.Now()

	// Skip checker for recently processed torrents
	skipCheck := func(hash string) bool {
		s.mu.RLock()
		ts, exists := instLastApplied[hash]
		s.mu.RUnlock()
		return exists && now.Sub(ts) < s.cfg.SkipWithin
	}

	// Process all torrents through all matching rules
	states := processTorrents(torrents, rules, evalCtx, s.syncManager, skipCheck)

	// Build torrent lookup for cross-seed detection
	torrentByHash := make(map[string]qbt.Torrent, len(torrents))
	for _, t := range torrents {
		torrentByHash[t.Hash] = t
	}

	// Build batches from desired states
	type shareKey struct {
		ratio float64
		seed  int64
	}
	shareBatches := make(map[shareKey][]string)
	uploadBatches := make(map[int64][]string)
	downloadBatches := make(map[int64][]string)
	pauseHashes := make([]string, 0)

	type tagChange struct {
		current  map[string]struct{}
		desired  map[string]struct{}
		toAdd    []string
		toRemove []string
	}
	tagChanges := make(map[string]*tagChange)

	type pendingDeletion struct {
		hash          string
		torrentName   string
		trackerDomain string
		action        string
		ruleID        int
		ruleName      string
		reason        string
		details       map[string]any
	}
	deleteHashesByMode := make(map[string][]string)
	pendingByHash := make(map[string]pendingDeletion)

	for hash, state := range states {
		torrent := torrentByHash[hash]

		// If torrent is marked for deletion, skip all other actions
		if state.shouldDelete {
			deleteMode := state.deleteMode
			var actualMode string
			var keepingFiles bool
			var logMsg string

			switch deleteMode {
			case DeleteModeWithFilesPreserveCrossSeeds:
				if detectCrossSeeds(torrent, torrents) {
					actualMode = DeleteModeKeepFiles
					logMsg = "automations: removing torrent (cross-seed detected - keeping files)"
					keepingFiles = true
				} else {
					actualMode = DeleteModeWithFiles
					logMsg = "automations: removing torrent with files"
					keepingFiles = false
				}
			case DeleteModeKeepFiles:
				actualMode = DeleteModeKeepFiles
				logMsg = "automations: removing torrent (keeping files)"
				keepingFiles = true
			default:
				actualMode = deleteMode
				logMsg = "automations: removing torrent with files"
				keepingFiles = false
			}

			log.Info().Str("hash", hash).Str("name", state.name).Str("reason", state.deleteReason).Bool("filesKept", keepingFiles).Msg(logMsg)
			deleteHashesByMode[actualMode] = append(deleteHashesByMode[actualMode], hash)

			// Determine activity action type
			action := models.ActivityActionDeletedCondition
			if state.deleteReason == "unregistered" {
				action = models.ActivityActionDeletedUnregistered
			} else if state.deleteReason == "ratio limit reached" {
				action = models.ActivityActionDeletedRatio
			} else if state.deleteReason == "seeding time limit reached" || state.deleteReason == "ratio and seeding time limits reached" {
				action = models.ActivityActionDeletedSeeding
			}

			pendingByHash[hash] = pendingDeletion{
				hash:          hash,
				torrentName:   state.name,
				trackerDomain: state.trackerDomain,
				action:        action,
				ruleID:        state.deleteRuleID,
				ruleName:      state.deleteRuleName,
				reason:        state.deleteReason,
				details:       map[string]any{"filesKept": keepingFiles, "deleteMode": deleteMode},
			}

			// Mark as processed
			s.mu.Lock()
			instLastApplied[hash] = now
			s.mu.Unlock()
			continue
		}

		// Speed limits - only add to batch if current doesn't match desired
		if state.uploadLimitKiB != nil {
			desired := *state.uploadLimitKiB * 1024
			if torrent.UpLimit != desired {
				uploadBatches[*state.uploadLimitKiB] = append(uploadBatches[*state.uploadLimitKiB], hash)
			}
		}
		if state.downloadLimitKiB != nil {
			desired := *state.downloadLimitKiB * 1024
			if torrent.DlLimit != desired {
				downloadBatches[*state.downloadLimitKiB] = append(downloadBatches[*state.downloadLimitKiB], hash)
			}
		}

		// Share limits
		if state.ratioLimit != nil || state.seedingMinutes != nil {
			ratio := torrent.RatioLimit
			if state.ratioLimit != nil {
				ratio = *state.ratioLimit
			}
			seedMinutes := torrent.SeedingTimeLimit
			if state.seedingMinutes != nil {
				seedMinutes = *state.seedingMinutes
			}
			needsUpdate := (state.ratioLimit != nil && torrent.RatioLimit != ratio) ||
				(state.seedingMinutes != nil && torrent.SeedingTimeLimit != seedMinutes)
			if needsUpdate {
				key := shareKey{ratio: ratio, seed: seedMinutes}
				shareBatches[key] = append(shareBatches[key], hash)
			}
		}

		// Pause
		if state.shouldPause {
			pauseHashes = append(pauseHashes, hash)
		}

		// Tags
		if len(state.tagActions) > 0 {
			var toAdd, toRemove []string
			desired := make(map[string]struct{})
			for t := range state.currentTags {
				desired[t] = struct{}{}
			}
			for tag, action := range state.tagActions {
				if action == "add" {
					toAdd = append(toAdd, tag)
					desired[tag] = struct{}{}
				} else if action == "remove" {
					toRemove = append(toRemove, tag)
					delete(desired, tag)
				}
			}
			if len(toAdd) > 0 || len(toRemove) > 0 {
				tagChanges[hash] = &tagChange{
					current:  state.currentTags,
					desired:  desired,
					toAdd:    toAdd,
					toRemove: toRemove,
				}
			}
		}

		// Mark as processed
		s.mu.Lock()
		instLastApplied[hash] = now
		s.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	for limit, hashes := range uploadBatches {
		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.SetTorrentUploadLimit(ctx, instanceID, batch, limit); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Int64("limitKiB", limit).Int("count", len(batch)).Msg("automations: upload limit failed")
				if s.activityStore != nil {
					detailsJSON, _ := json.Marshal(map[string]any{"limitKiB": limit, "count": len(batch), "type": "upload"})
					if err := s.activityStore.Create(ctx, &models.AutomationActivity{
						InstanceID: instanceID,
						Hash:       strings.Join(batch, ","),
						Action:     models.ActivityActionLimitFailed,
						Outcome:    models.ActivityOutcomeFailed,
						Reason:     "upload limit failed: " + err.Error(),
						Details:    detailsJSON,
					}); err != nil {
						log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record activity")
					}
				}
			}
		}
	}

	for limit, hashes := range downloadBatches {
		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.SetTorrentDownloadLimit(ctx, instanceID, batch, limit); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Int64("limitKiB", limit).Int("count", len(batch)).Msg("automations: download limit failed")
				if s.activityStore != nil {
					detailsJSON, _ := json.Marshal(map[string]any{"limitKiB": limit, "count": len(batch), "type": "download"})
					if err := s.activityStore.Create(ctx, &models.AutomationActivity{
						InstanceID: instanceID,
						Hash:       strings.Join(batch, ","),
						Action:     models.ActivityActionLimitFailed,
						Outcome:    models.ActivityOutcomeFailed,
						Reason:     "download limit failed: " + err.Error(),
						Details:    detailsJSON,
					}); err != nil {
						log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record activity")
					}
				}
			}
		}
	}

	for key, hashes := range shareBatches {
		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.SetTorrentShareLimit(ctx, instanceID, batch, key.ratio, key.seed, -1); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Float64("ratio", key.ratio).Int64("seedMinutes", key.seed).Int("count", len(batch)).Msg("automations: share limit failed")
				if s.activityStore != nil {
					detailsJSON, _ := json.Marshal(map[string]any{"ratio": key.ratio, "seedMinutes": key.seed, "count": len(batch), "type": "share"})
					if err := s.activityStore.Create(ctx, &models.AutomationActivity{
						InstanceID: instanceID,
						Hash:       strings.Join(batch, ","),
						Action:     models.ActivityActionLimitFailed,
						Outcome:    models.ActivityOutcomeFailed,
						Reason:     "share limit failed: " + err.Error(),
						Details:    detailsJSON,
					}); err != nil {
						log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record activity")
					}
				}
			}
		}
	}

	// Execute pause actions for expression-based rules
	if len(pauseHashes) > 0 {
		limited := limitHashBatch(pauseHashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.BulkAction(ctx, instanceID, batch, "pause"); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: pause action failed")
			} else {
				log.Info().Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: paused torrents")
			}
		}
	}

	// Execute tag actions for expression-based rules
	if len(tagChanges) > 0 {
		// Try SetTags first (more efficient for qBit 5.1+)
		// Group by desired tag set for batching
		setTagsBatches := make(map[string][]string) // key = sorted tags, value = hashes

		for hash, change := range tagChanges {
			// Build sorted tag list for batching key
			tags := make([]string, 0, len(change.desired))
			for t := range change.desired {
				tags = append(tags, t)
			}
			sort.Strings(tags)
			key := strings.Join(tags, ",")
			setTagsBatches[key] = append(setTagsBatches[key], hash)
		}

		// Try SetTags first (qBit 5.1+)
		useSetTags := true
		for tagSet, hashes := range setTagsBatches {
			var tags []string
			if tagSet != "" {
				tags = strings.Split(tagSet, ",")
			}
			batches := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
			for _, batch := range batches {
				err := s.syncManager.SetTorrentTags(ctx, instanceID, batch, tags)
				if err != nil {
					// Check if it's an unsupported version error
					if strings.Contains(err.Error(), "requires qBittorrent") {
						useSetTags = false
						break
					}
					log.Warn().Err(err).Int("instanceID", instanceID).Strs("tags", tags).Int("count", len(batch)).Msg("automations: set tags failed")
				} else {
					log.Debug().Int("instanceID", instanceID).Strs("tags", tags).Int("count", len(batch)).Msg("automations: set tags on torrents")
				}
			}
			if !useSetTags {
				break
			}
		}

		// Fallback to Add/Remove for older clients
		if !useSetTags {
			log.Debug().Int("instanceID", instanceID).Msg("automations: falling back to add/remove tags (older qBittorrent)")

			// Group by tags to add/remove
			addBatches := make(map[string][]string)    // key = tag, value = hashes
			removeBatches := make(map[string][]string) // key = tag, value = hashes

			for hash, change := range tagChanges {
				for _, tag := range change.toAdd {
					addBatches[tag] = append(addBatches[tag], hash)
				}
				for _, tag := range change.toRemove {
					removeBatches[tag] = append(removeBatches[tag], hash)
				}
			}

			for tag, hashes := range addBatches {
				batches := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
				for _, batch := range batches {
					if err := s.syncManager.AddTorrentTags(ctx, instanceID, batch, []string{tag}); err != nil {
						log.Warn().Err(err).Int("instanceID", instanceID).Str("tag", tag).Int("count", len(batch)).Msg("automations: add tags failed")
					} else {
						log.Debug().Int("instanceID", instanceID).Str("tag", tag).Int("count", len(batch)).Msg("automations: added tag to torrents")
					}
				}
			}

			for tag, hashes := range removeBatches {
				batches := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
				for _, batch := range batches {
					if err := s.syncManager.RemoveTorrentTags(ctx, instanceID, batch, []string{tag}); err != nil {
						log.Warn().Err(err).Int("instanceID", instanceID).Str("tag", tag).Int("count", len(batch)).Msg("automations: remove tags failed")
					} else {
						log.Debug().Int("instanceID", instanceID).Str("tag", tag).Int("count", len(batch)).Msg("automations: removed tag from torrents")
					}
				}
			}
		}

		// Record tag activity summary
		if s.activityStore != nil {
			// Aggregate counts per tag
			addCounts := make(map[string]int)    // tag -> count of torrents
			removeCounts := make(map[string]int) // tag -> count of torrents

			for _, change := range tagChanges {
				for _, tag := range change.toAdd {
					addCounts[tag]++
				}
				for _, tag := range change.toRemove {
					removeCounts[tag]++
				}
			}

			// Only record if there were actual changes
			if len(addCounts) > 0 || len(removeCounts) > 0 {
				detailsJSON, _ := json.Marshal(map[string]any{
					"added":   addCounts,
					"removed": removeCounts,
				})
				if err := s.activityStore.Create(ctx, &models.AutomationActivity{
					InstanceID: instanceID,
					Hash:       "", // No single hash for batch operations
					Action:     models.ActivityActionTagsChanged,
					Outcome:    models.ActivityOutcomeSuccess,
					Details:    detailsJSON,
				}); err != nil {
					log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record tag activity")
				}
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
				log.Warn().Err(err).Int("instanceID", instanceID).Str("action", mode).Int("count", len(batch)).Strs("hashes", batch).Msg("automations: delete failed")

				// Record failed deletion activity
				if s.activityStore != nil {
					for _, hash := range batch {
						if pending, ok := pendingByHash[hash]; ok {
							detailsJSON, _ := json.Marshal(pending.details)
							if err := s.activityStore.Create(ctx, &models.AutomationActivity{
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
								log.Warn().Err(err).Str("hash", hash).Int("instanceID", instanceID).Msg("automations: failed to record activity")
							}
						}
					}
				}
			} else {
				if mode == DeleteModeKeepFiles {
					log.Info().Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: removed torrents (files kept)")
				} else {
					log.Info().Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: removed torrents with files")
				}

				// Record successful deletion activity
				if s.activityStore != nil {
					for _, hash := range batch {
						if pending, ok := pendingByHash[hash]; ok {
							detailsJSON, _ := json.Marshal(pending.details)
							if err := s.activityStore.Create(ctx, &models.AutomationActivity{
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
								log.Warn().Err(err).Str("hash", hash).Int("instanceID", instanceID).Msg("automations: failed to record activity")
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
					log.Error().Err(err).Str("pattern", normalized).Msg("automations: invalid glob pattern")
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
