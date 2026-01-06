// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package automations enforces tracker-scoped speed/ratio rules per instance.
package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
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
	"github.com/autobrr/qui/pkg/hardlink"
)

// Config controls how often rules are re-applied and how long to debounce repeats.
type Config struct {
	ScanInterval          time.Duration
	SkipWithin            time.Duration
	MaxBatchHashes        int
	ActivityRetentionDays int
	ApplyTimeout          time.Duration // timeout for applying all actions per instance
}

// DefaultRuleInterval is the cadence for rules that don't specify their own interval.
const DefaultRuleInterval = 15 * time.Minute

// ruleKey identifies a rule within an instance for per-rule cadence tracking.
type ruleKey struct {
	instanceID int
	ruleID     int
}

func DefaultConfig() Config {
	return Config{
		ScanInterval:          20 * time.Second,
		SkipWithin:            2 * time.Minute,
		MaxBatchHashes:        50, // matches qBittorrent's max_concurrent_http_announces default
		ActivityRetentionDays: 7,
		ApplyTimeout:          60 * time.Second,
	}
}

// Service periodically applies automation rules to torrents for all active instances.
type Service struct {
	cfg                       Config
	instanceStore             *models.InstanceStore
	ruleStore                 *models.AutomationStore
	activityStore             *models.AutomationActivityStore
	trackerCustomizationStore *models.TrackerCustomizationStore
	syncManager               *qbittorrent.SyncManager

	// keep lightweight memory of recent applications to avoid hammering qBittorrent
	lastApplied map[int]map[string]time.Time // instanceID -> hash -> timestamp
	lastRuleRun map[ruleKey]time.Time        // per-rule cadence tracking
	mu          sync.RWMutex
}

func NewService(cfg Config, instanceStore *models.InstanceStore, ruleStore *models.AutomationStore, activityStore *models.AutomationActivityStore, trackerCustomizationStore *models.TrackerCustomizationStore, syncManager *qbittorrent.SyncManager) *Service {
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
		cfg:                       cfg,
		instanceStore:             instanceStore,
		ruleStore:                 ruleStore,
		activityStore:             activityStore,
		trackerCustomizationStore: trackerCustomizationStore,
		syncManager:               syncManager,
		lastApplied:               make(map[int]map[string]time.Time),
		lastRuleRun:               make(map[ruleKey]time.Time),
	}
}

// cleanupStaleEntries removes entries from lastApplied and lastRuleRun maps
// that are older than the cutoff to prevent unbounded memory growth.
func (s *Service) cleanupStaleEntries() {
	cutoff := time.Now().Add(-10 * time.Minute)
	ruleCutoff := time.Now().Add(-24 * time.Hour) // 1 day for rule tracking
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, instMap := range s.lastApplied {
		for hash, ts := range instMap {
			if ts.Before(cutoff) {
				delete(instMap, hash)
			}
		}
	}

	for key, ts := range s.lastRuleRun {
		if ts.Before(ruleCutoff) {
			delete(s.lastRuleRun, key)
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
		if err := s.applyForInstance(ctx, instance.ID, false); err != nil {
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("automations: apply failed")
		}
	}
}

// ApplyOnceForInstance allows manual triggering (API hook).
// It bypasses per-rule interval checks (force=true).
func (s *Service) ApplyOnceForInstance(ctx context.Context, instanceID int) error {
	return s.applyForInstance(ctx, instanceID, true)
}

// PreviewResult contains torrents that would match a rule.
type PreviewResult struct {
	TotalMatches   int              `json:"totalMatches"`
	CrossSeedCount int              `json:"crossSeedCount,omitempty"` // Count of cross-seeds included (for category preview)
	Examples       []PreviewTorrent `json:"examples"`
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
	IsCrossSeed    bool    `json:"isCrossSeed,omitempty"` // For category preview

	// Additional fields for dynamic columns based on filter conditions
	NumSeeds      int64   `json:"numSeeds"`                // Active seeders (connected to)
	NumComplete   int64   `json:"numComplete"`             // Total seeders in swarm
	NumLeechs     int64   `json:"numLeechs"`               // Active leechers (connected to)
	NumIncomplete int64   `json:"numIncomplete"`           // Total leechers in swarm
	Progress      float64 `json:"progress"`                // Download progress (0-1)
	Availability  float64 `json:"availability"`            // Distributed copies
	TimeActive    int64   `json:"timeActive"`              // Total active time (seconds)
	LastActivity  int64   `json:"lastActivity"`            // Last activity timestamp
	CompletionOn  int64   `json:"completionOn"`            // Completion timestamp
	TotalSize     int64   `json:"totalSize"`               // Total torrent size
	HardlinkScope string  `json:"hardlinkScope,omitempty"` // none, torrents_only, outside_qbittorrent
}

// buildPreviewTorrent creates a PreviewTorrent from a qbt.Torrent with optional context flags.
func buildPreviewTorrent(torrent qbt.Torrent, tracker string, evalCtx *EvalContext, isCrossSeed bool) PreviewTorrent {
	pt := PreviewTorrent{
		Name:          torrent.Name,
		Hash:          torrent.Hash,
		Size:          torrent.Size,
		Ratio:         torrent.Ratio,
		SeedingTime:   torrent.SeedingTime,
		Tracker:       tracker,
		Category:      torrent.Category,
		Tags:          torrent.Tags,
		State:         string(torrent.State),
		AddedOn:       torrent.AddedOn,
		Uploaded:      torrent.Uploaded,
		Downloaded:    torrent.Downloaded,
		IsCrossSeed:   isCrossSeed,
		NumSeeds:      torrent.NumSeeds,
		NumComplete:   torrent.NumComplete,
		NumLeechs:     torrent.NumLeechs,
		NumIncomplete: torrent.NumIncomplete,
		Progress:      torrent.Progress,
		Availability:  torrent.Availability,
		TimeActive:    torrent.TimeActive,
		LastActivity:  torrent.LastActivity,
		CompletionOn:  torrent.CompletionOn,
		TotalSize:     torrent.TotalSize,
	}

	if evalCtx != nil {
		if evalCtx.UnregisteredSet != nil {
			_, pt.IsUnregistered = evalCtx.UnregisteredSet[torrent.Hash]
		}
		if evalCtx.HardlinkScopeByHash != nil {
			pt.HardlinkScope = evalCtx.HardlinkScopeByHash[torrent.Hash]
		}
	}

	return pt
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

	// Stable sort for deterministic pagination: oldest first, then by hash
	sort.Slice(torrents, func(i, j int) bool {
		if torrents[i].AddedOn != torrents[j].AddedOn {
			return torrents[i].AddedOn < torrents[j].AddedOn
		}
		return torrents[i].Hash < torrents[j].Hash
	})

	if limit <= 0 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}

	result := &PreviewResult{
		Examples: make([]PreviewTorrent, 0, limit),
	}

	// Get instance for hardlink context
	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get instance for preview, proceeding without hardlink context")
	}

	// Initialize evaluation context
	evalCtx := &EvalContext{}
	if instance != nil {
		evalCtx.InstanceHasLocalAccess = instance.HasLocalFilesystemAccess
	}

	// Build category index for EXISTS_IN/CONTAINS_IN operators
	evalCtx.CategoryIndex, evalCtx.CategoryNames = BuildCategoryIndex(torrents)

	// Get health counts for tracker health conditions (from background cache)
	if healthCounts := s.syncManager.GetTrackerHealthCounts(instanceID); healthCounts != nil {
		if len(healthCounts.UnregisteredSet) > 0 {
			evalCtx.UnregisteredSet = healthCounts.UnregisteredSet
		}
		if len(healthCounts.TrackerDownSet) > 0 {
			evalCtx.TrackerDownSet = healthCounts.TrackerDownSet
		}
	}

	// Check if rule uses hardlink conditions and populate context
	if instance != nil && instance.HasLocalFilesystemAccess && rule.Conditions != nil && rule.Conditions.Delete != nil {
		cond := rule.Conditions.Delete.Condition
		if ConditionUsesField(cond, FieldHardlinkScope) {
			evalCtx.HardlinkScopeByHash = s.detectHardlinkScope(ctx, instanceID, torrents)
		}
	}

	// Get free space on instance (only if rules use FREE_SPACE field)
	if instance != nil && rulesUseCondition([]*models.Automation{rule}, FieldFreeSpace) {
		freeSpace, err := s.syncManager.GetFreeSpace(ctx, instanceID)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get free space")
			return nil, err
		}
		evalCtx.FreeSpace = freeSpace
		evalCtx.FilesToClear = make(map[crossSeedKey]struct{})
	}

	matchIndex := 0
	for _, torrent := range torrents {
		// Check tracker match
		trackerDomains := collectTrackerDomains(torrent, s.syncManager)
		if !matchesTracker(rule.TrackerPattern, trackerDomains) {
			continue
		}

		// Check if torrent would be deleted
		wouldDelete := false

		if rule.Conditions != nil && rule.Conditions.Delete != nil && rule.Conditions.Delete.Enabled {
			// Evaluate condition (if no condition, match all)
			if rule.Conditions.Delete.Condition == nil {
				wouldDelete = true
			} else {
				wouldDelete = EvaluateConditionWithContext(rule.Conditions.Delete.Condition, torrent, evalCtx, 0)
			}
		}

		if wouldDelete {
			updateCumulativeFreeSpaceCleared(torrent, evalCtx)

			matchIndex++
			if matchIndex <= offset {
				continue
			}
			if len(result.Examples) < limit {
				tracker := ""
				if domains := collectTrackerDomains(torrent, s.syncManager); len(domains) > 0 {
					tracker = domains[0]
				}
				result.Examples = append(result.Examples, buildPreviewTorrent(torrent, tracker, evalCtx, false))
			}
		}
	}

	result.TotalMatches = matchIndex
	return result, nil
}

// PreviewCategoryRule returns torrents that would have their category changed by the given rule.
// If IncludeCrossSeeds is enabled, also includes cross-seeds that share files with matched torrents.
func (s *Service) PreviewCategoryRule(ctx context.Context, instanceID int, rule *models.Automation, limit int, offset int) (*PreviewResult, error) {
	if s == nil || s.syncManager == nil {
		return &PreviewResult{}, nil
	}

	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	// Stable sort for deterministic pagination: oldest first, then by hash
	sort.Slice(torrents, func(i, j int) bool {
		if torrents[i].AddedOn != torrents[j].AddedOn {
			return torrents[i].AddedOn < torrents[j].AddedOn
		}
		return torrents[i].Hash < torrents[j].Hash
	})

	crossSeedIndex := buildCrossSeedIndex(torrents)

	if limit <= 0 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}

	result := &PreviewResult{
		Examples: make([]PreviewTorrent, 0, limit),
	}

	// Get instance for local access context
	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get instance for preview")
	}

	// Initialize evaluation context
	evalCtx := &EvalContext{}
	if instance != nil {
		evalCtx.InstanceHasLocalAccess = instance.HasLocalFilesystemAccess
	}

	// Build category index for EXISTS_IN/CONTAINS_IN operators
	evalCtx.CategoryIndex, evalCtx.CategoryNames = BuildCategoryIndex(torrents)

	// Get health counts for unregistered condition evaluation
	if healthCounts := s.syncManager.GetTrackerHealthCounts(instanceID); healthCounts != nil {
		evalCtx.UnregisteredSet = healthCounts.UnregisteredSet
		evalCtx.TrackerDownSet = healthCounts.TrackerDownSet
	}

	// Check if rule uses hardlink conditions
	if instance != nil && instance.HasLocalFilesystemAccess && rule.Conditions != nil && rule.Conditions.Category != nil {
		cond := rule.Conditions.Category.Condition
		if ConditionUsesField(cond, FieldHardlinkScope) {
			evalCtx.HardlinkScopeByHash = s.detectHardlinkScope(ctx, instanceID, torrents)
		}
	}

	// Get free space on instance (only if rules use FREE_SPACE field)
	if instance != nil && rulesUseCondition([]*models.Automation{rule}, FieldFreeSpace) {
		freeSpace, err := s.syncManager.GetFreeSpace(ctx, instanceID)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get free space")
			return nil, err
		}
		evalCtx.FreeSpace = freeSpace
		evalCtx.FilesToClear = make(map[crossSeedKey]struct{})
	}

	targetCategory := ""
	includeCrossSeeds := false
	if rule.Conditions != nil && rule.Conditions.Category != nil {
		targetCategory = rule.Conditions.Category.Category
		includeCrossSeeds = rule.Conditions.Category.IncludeCrossSeeds
	}

	// Phase 1: Find direct matches - use SET for membership, iterate slice for stable order
	directMatchSet := make(map[string]struct{})
	matchedKeys := make(map[crossSeedKey]struct{}) // for cross-seed lookup

	for _, torrent := range torrents {
		// Check tracker match
		trackerDomains := collectTrackerDomains(torrent, s.syncManager)
		if !matchesTracker(rule.TrackerPattern, trackerDomains) {
			continue
		}

		// Skip if already in target category (no-op - won't "pull" cross-seeds)
		if torrent.Category == targetCategory {
			continue
		}

		// Check if category action applies
		if rule.Conditions != nil && rule.Conditions.Category != nil && rule.Conditions.Category.Enabled {
			shouldApply := rule.Conditions.Category.Condition == nil ||
				EvaluateConditionWithContext(rule.Conditions.Category.Condition, torrent, evalCtx, 0)
			if shouldApply {
				if shouldBlockCategoryChangeForCrossSeeds(torrent, rule.Conditions.Category.BlockIfCrossSeedInCategories, crossSeedIndex) {
					continue
				}
				directMatchSet[torrent.Hash] = struct{}{}
				if includeCrossSeeds {
					if key, ok := makeCrossSeedKey(torrent); ok {
						matchedKeys[key] = struct{}{}
					}
				}
			}
		}
	}

	// Phase 2: Find cross-seeds (if enabled) - require BOTH ContentPath AND SavePath match
	crossSeedSet := make(map[string]struct{})
	if includeCrossSeeds && len(matchedKeys) > 0 {
		for _, torrent := range torrents {
			if _, isDirectMatch := directMatchSet[torrent.Hash]; isDirectMatch {
				continue // Skip direct matches
			}
			if torrent.Category == targetCategory {
				continue // Already in target category
			}
			if key, ok := makeCrossSeedKey(torrent); ok {
				if _, matched := matchedKeys[key]; matched {
					crossSeedSet[torrent.Hash] = struct{}{}
				}
			}
		}
	}

	// Build result - iterate torrents slice for STABLE pagination order
	result.TotalMatches = len(directMatchSet) + len(crossSeedSet)
	result.CrossSeedCount = len(crossSeedSet)

	matchIndex := 0
	for _, torrent := range torrents {
		_, isDirectMatch := directMatchSet[torrent.Hash]
		_, isCrossSeed := crossSeedSet[torrent.Hash]

		if !isDirectMatch && !isCrossSeed {
			continue
		}

		matchIndex++
		if matchIndex <= offset {
			continue
		}
		if len(result.Examples) >= limit {
			break
		}

		tracker := ""
		if domains := collectTrackerDomains(torrent, s.syncManager); len(domains) > 0 {
			tracker = domains[0]
		}

		result.Examples = append(result.Examples, buildPreviewTorrent(torrent, tracker, evalCtx, isCrossSeed))
	}

	return result, nil
}

func (s *Service) applyForInstance(ctx context.Context, instanceID int, force bool) error {
	rules, err := s.ruleStore.ListByInstance(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to load rules")
		return err
	}
	if len(rules) == 0 {
		return nil
	}

	// Pre-filter rules by interval eligibility
	now := time.Now()
	eligibleRules := make([]*models.Automation, 0, len(rules))
	for _, rule := range rules {
		if !force {
			interval := DefaultRuleInterval
			if rule.IntervalSeconds != nil {
				interval = time.Duration(*rule.IntervalSeconds) * time.Second
			}
			key := ruleKey{instanceID, rule.ID}
			s.mu.RLock()
			lastRun := s.lastRuleRun[key]
			s.mu.RUnlock()
			if now.Sub(lastRun) < interval {
				continue // skip, interval not elapsed
			}
		}
		eligibleRules = append(eligibleRules, rule)
	}
	if len(eligibleRules) == 0 {
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

	// Get instance for local filesystem access check
	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get instance")
		return err
	}

	// Initialize evaluation context
	evalCtx := &EvalContext{
		InstanceHasLocalAccess: instance.HasLocalFilesystemAccess,
	}

	// Build category index for EXISTS_IN/CONTAINS_IN operators
	evalCtx.CategoryIndex, evalCtx.CategoryNames = BuildCategoryIndex(torrents)

	// Get health counts for isUnregistered condition evaluation
	if healthCounts := s.syncManager.GetTrackerHealthCounts(instanceID); healthCounts != nil {
		evalCtx.UnregisteredSet = healthCounts.UnregisteredSet
		evalCtx.TrackerDownSet = healthCounts.TrackerDownSet
	}

	// On-demand hardlink detection (only if rules use HARDLINK_SCOPE and instance has local access)
	if instance.HasLocalFilesystemAccess && rulesUseCondition(eligibleRules, FieldHardlinkScope) {
		evalCtx.HardlinkScopeByHash = s.detectHardlinkScope(ctx, instanceID, torrents)
	}

	// Get free space on instance (only if rules use FREE_SPACE field)
	if rulesUseCondition(eligibleRules, FieldFreeSpace) {
		freeSpace, err := s.syncManager.GetFreeSpace(ctx, instanceID)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get free space")
			return fmt.Errorf("failed to get free space: %w", err)
		}
		evalCtx.FreeSpace = freeSpace
		evalCtx.FilesToClear = make(map[crossSeedKey]struct{})
	}

	// Load tracker display names if any rule uses UseTrackerAsTag with UseDisplayName
	if rulesUseTrackerDisplayName(eligibleRules) && s.trackerCustomizationStore != nil {
		customizations, err := s.trackerCustomizationStore.List(ctx)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to load tracker customizations for display names")
		} else {
			evalCtx.TrackerDisplayNameByDomain = buildTrackerDisplayNameMap(customizations)
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

	// Skip checker for recently processed torrents
	skipCheck := func(hash string) bool {
		s.mu.RLock()
		ts, exists := instLastApplied[hash]
		s.mu.RUnlock()
		return exists && now.Sub(ts) < s.cfg.SkipWithin
	}

	// Compute which rules actually have matching torrents that won't be skipped.
	// This must happen after skipCheck is defined so we only stamp lastRuleRun
	// for rules that will actually process at least one torrent.
	rulesUsed := make(map[int]struct{})
	for _, torrent := range torrents {
		if skipCheck(torrent.Hash) {
			continue
		}
		for _, rule := range selectMatchingRules(torrent, eligibleRules, s.syncManager) {
			rulesUsed[rule.ID] = struct{}{}
		}
	}

	// Process all torrents through all eligible rules
	ruleStats := make(map[int]*ruleRunStats)
	states := processTorrents(torrents, eligibleRules, evalCtx, s.syncManager, skipCheck, ruleStats)

	if len(states) == 0 {
		log.Debug().
			Int("instanceID", instanceID).
			Int("eligibleRules", len(eligibleRules)).
			Int("torrents", len(torrents)).
			Int("matchedRules", len(rulesUsed)).
			Msg("automations: no actions to apply")

		for _, rule := range eligibleRules {
			stats := ruleStats[rule.ID]
			if stats == nil || stats.MatchedTrackers == 0 {
				continue
			}
			if stats.totalApplied() > 0 {
				continue
			}

			log.Debug().
				Int("instanceID", instanceID).
				Int("ruleID", rule.ID).
				Str("ruleName", rule.Name).
				Int("matchedTrackers", stats.MatchedTrackers).
				Int("speedNoMatch", stats.SpeedConditionNotMet).
				Int("shareNoMatch", stats.ShareConditionNotMet).
				Int("pauseNoMatch", stats.PauseConditionNotMet).
				Int("tagNoMatch", stats.TagConditionNotMet).
				Int("tagMissingUnregisteredSet", stats.TagSkippedMissingUnregisteredSet).
				Int("categoryNoMatchOrBlocked", stats.CategoryConditionNotMetOrBlocked).
				Int("deleteNoMatch", stats.DeleteConditionNotMet).
				Msg("automations: rule matched trackers but applied no actions")
		}
	}

	// Update lastRuleRun only for rules that matched at least one non-skipped torrent
	s.mu.Lock()
	for ruleID := range rulesUsed {
		key := ruleKey{instanceID, ruleID}
		s.lastRuleRun[key] = now
	}
	s.mu.Unlock()

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
	categoryBatches := make(map[string][]string) // category name -> hashes

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

			trackerDomain := ""
			if len(state.trackerDomains) > 0 {
				trackerDomain = state.trackerDomains[0]
			}
			pendingByHash[hash] = pendingDeletion{
				hash:          hash,
				torrentName:   state.name,
				trackerDomain: trackerDomain,
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

		// Category - filter no-ops by comparing desired vs current
		if state.category != nil {
			if torrent.Category != *state.category {
				categoryBatches[*state.category] = append(categoryBatches[*state.category], hash)
			}
		}

		// Mark as processed
		s.mu.Lock()
		instLastApplied[hash] = now
		s.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(ctx, s.cfg.ApplyTimeout)
	defer cancel()

	// Apply speed limits and track success
	uploadSuccess := s.applySpeedLimits(ctx, instanceID, uploadBatches, "upload", s.syncManager.SetTorrentUploadLimit)
	downloadSuccess := s.applySpeedLimits(ctx, instanceID, downloadBatches, "download", s.syncManager.SetTorrentDownloadLimit)

	// Record aggregated speed limit activity
	if s.activityStore != nil && (len(uploadSuccess) > 0 || len(downloadSuccess) > 0) {
		speedLimits := make(map[string]int) // "upload:1024" -> count, "download:2048" -> count
		for limit, count := range uploadSuccess {
			speedLimits[fmt.Sprintf("upload:%d", limit)] = count
		}
		for limit, count := range downloadSuccess {
			speedLimits[fmt.Sprintf("download:%d", limit)] = count
		}
		detailsJSON, _ := json.Marshal(map[string]any{"limits": speedLimits})
		if err := s.activityStore.Create(ctx, &models.AutomationActivity{
			InstanceID: instanceID,
			Hash:       "",
			Action:     models.ActivityActionSpeedLimitsChanged,
			Outcome:    models.ActivityOutcomeSuccess,
			Details:    detailsJSON,
		}); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record speed limit activity")
		}
	}

	// Apply share limits and track success
	shareLimitSuccess := make(map[string]int) // "ratio:seed" -> count
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
			} else {
				limitKey := fmt.Sprintf("%.2f:%d", key.ratio, key.seed)
				shareLimitSuccess[limitKey] += len(batch)
			}
		}
	}

	// Record aggregated share limit activity
	if s.activityStore != nil && len(shareLimitSuccess) > 0 {
		detailsJSON, _ := json.Marshal(map[string]any{"limits": shareLimitSuccess})
		if err := s.activityStore.Create(ctx, &models.AutomationActivity{
			InstanceID: instanceID,
			Hash:       "",
			Action:     models.ActivityActionShareLimitsChanged,
			Outcome:    models.ActivityOutcomeSuccess,
			Details:    detailsJSON,
		}); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record share limit activity")
		}
	}

	// Execute pause actions for expression-based rules
	pausedCount := 0
	if len(pauseHashes) > 0 {
		limited := limitHashBatch(pauseHashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.BulkAction(ctx, instanceID, batch, "pause"); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: pause action failed")
			} else {
				log.Info().Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: paused torrents")
				pausedCount += len(batch)
			}
		}
	}

	// Record aggregated pause activity
	if s.activityStore != nil && pausedCount > 0 {
		detailsJSON, _ := json.Marshal(map[string]any{"count": pausedCount})
		if err := s.activityStore.Create(ctx, &models.AutomationActivity{
			InstanceID: instanceID,
			Hash:       "",
			Action:     models.ActivityActionPaused,
			Outcome:    models.ActivityOutcomeSuccess,
			Details:    detailsJSON,
		}); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record pause activity")
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

	// Execute category changes - expand with cross-seeds where winning rule requested it
	// Sort keys for deterministic execution order
	type categoryMove struct {
		hash          string
		name          string
		trackerDomain string
		category      string
	}
	var successfulMoves []categoryMove

	sortedCategories := make([]string, 0, len(categoryBatches))
	for cat := range categoryBatches {
		sortedCategories = append(sortedCategories, cat)
	}
	sort.Strings(sortedCategories)

	for _, category := range sortedCategories {
		hashes := categoryBatches[category]
		expandedHashes := hashes

		// Find torrents whose winning category rule had IncludeCrossSeeds=true
		// and expand with their cross-seeds (require BOTH ContentPath AND SavePath match)
		keysToExpand := make(map[crossSeedKey]struct{})
		for _, hash := range hashes {
			if state, exists := states[hash]; exists && state.categoryIncludeCrossSeeds {
				if t, exists := torrentByHash[hash]; exists {
					if key, ok := makeCrossSeedKey(t); ok {
						keysToExpand[key] = struct{}{}
					}
				}
			}
		}

		if len(keysToExpand) > 0 {
			expandedSet := make(map[string]struct{})
			for _, h := range expandedHashes {
				expandedSet[h] = struct{}{}
			}

			for _, t := range torrents {
				if t.Category == category {
					continue // Already in target category
				}
				if _, exists := expandedSet[t.Hash]; exists {
					continue // Already in batch
				}
				// CRITICAL: Don't override torrent's own computed desired category
				// If this torrent has its own category set by rules, respect "last rule wins"
				if state, hasState := states[t.Hash]; hasState && state.category != nil {
					if *state.category != category {
						continue // Torrent's winning rule chose a different category
					}
				}
				if key, ok := makeCrossSeedKey(t); ok {
					if _, shouldExpand := keysToExpand[key]; shouldExpand {
						expandedHashes = append(expandedHashes, t.Hash)
						expandedSet[t.Hash] = struct{}{}
					}
				}
			}
		}

		limited := limitHashBatch(expandedHashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.SetCategory(ctx, instanceID, batch, category); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Str("category", category).Int("count", len(batch)).Msg("automations: set category failed")
			} else {
				log.Debug().Int("instanceID", instanceID).Str("category", category).Int("count", len(batch)).Msg("automations: set category on torrents")
				// Track individual successes for activity logging
				for _, hash := range batch {
					move := categoryMove{
						hash:     hash,
						category: category,
					}
					if t, exists := torrentByHash[hash]; exists {
						move.name = t.Name
						if domains := collectTrackerDomains(t, s.syncManager); len(domains) > 0 {
							move.trackerDomain = domains[0]
						}
					}
					successfulMoves = append(successfulMoves, move)
				}
			}
		}
	}

	// Record aggregated category activity (like tags)
	if s.activityStore != nil && len(successfulMoves) > 0 {
		categoryCounts := make(map[string]int) // category -> count of torrents moved
		for _, move := range successfulMoves {
			categoryCounts[move.category]++
		}

		detailsJSON, _ := json.Marshal(map[string]any{
			"categories": categoryCounts,
		})
		if err := s.activityStore.Create(ctx, &models.AutomationActivity{
			InstanceID: instanceID,
			Hash:       "", // No single hash for batch operations
			Action:     models.ActivityActionCategoryChanged,
			Outcome:    models.ActivityOutcomeSuccess,
			Details:    detailsJSON,
		}); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record category activity")
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

// crossSeedKey identifies torrents at the same on-disk location.
// Both ContentPath and SavePath must match for category cross-seed detection.
type crossSeedKey struct {
	contentPath string
	savePath    string
}

// makeCrossSeedKey returns the key for a torrent, and ok=false if paths are empty.
func makeCrossSeedKey(t qbt.Torrent) (crossSeedKey, bool) {
	contentPath := normalizePath(t.ContentPath)
	savePath := normalizePath(t.SavePath)
	if contentPath == "" || savePath == "" {
		return crossSeedKey{}, false
	}
	return crossSeedKey{contentPath, savePath}, true
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

// rulesUseCondition checks if any enabled rule uses the given field.
func rulesUseCondition(rules []*models.Automation, field ConditionField) bool {
	for _, rule := range rules {
		if rule.Conditions == nil || !rule.Enabled {
			continue
		}
		ac := rule.Conditions
		if ac.SpeedLimits != nil && ConditionUsesField(ac.SpeedLimits.Condition, field) {
			return true
		}
		if ac.ShareLimits != nil && ConditionUsesField(ac.ShareLimits.Condition, field) {
			return true
		}
		if ac.Pause != nil && ConditionUsesField(ac.Pause.Condition, field) {
			return true
		}
		if ac.Delete != nil && ConditionUsesField(ac.Delete.Condition, field) {
			return true
		}
		if ac.Tag != nil && ConditionUsesField(ac.Tag.Condition, field) {
			return true
		}
		if ac.Category != nil && ConditionUsesField(ac.Category.Condition, field) {
			return true
		}
	}
	return false
}

// rulesUseTrackerDisplayName checks if any enabled rule uses UseTrackerAsTag with UseDisplayName.
func rulesUseTrackerDisplayName(rules []*models.Automation) bool {
	for _, rule := range rules {
		if rule.Conditions == nil || !rule.Enabled {
			continue
		}
		tag := rule.Conditions.Tag
		if tag != nil && tag.Enabled && tag.UseTrackerAsTag && tag.UseDisplayName {
			return true
		}
	}
	return false
}

// buildTrackerDisplayNameMap builds a map from lowercase domain to display name.
func buildTrackerDisplayNameMap(customizations []*models.TrackerCustomization) map[string]string {
	result := make(map[string]string)
	for _, c := range customizations {
		for _, domain := range c.Domains {
			result[strings.ToLower(domain)] = c.DisplayName
		}
	}
	return result
}

// fileIDInfo holds file identity and link count for hardlink scope detection.
type fileIDInfo struct {
	nlink uint64
	paths map[string]struct{}
}

// detectHardlinkScope computes the hardlink scope for each torrent.
// Returns a map of torrent hash to scope value (none, torrents_only, outside_qbittorrent).
func (s *Service) detectHardlinkScope(ctx context.Context, instanceID int, torrents []qbt.Torrent) map[string]string {
	result := make(map[string]string)

	hashes := make([]string, 0, len(torrents))
	torrentByHash := make(map[string]qbt.Torrent)
	for _, t := range torrents {
		hashes = append(hashes, t.Hash)
		torrentByHash[t.Hash] = t
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).
			Msg("automations: failed to fetch files for hardlink scope detection")
		// Return empty map - scope defaults to "none" in evaluator
		return result
	}

	// Build fileID accounting map across ALL torrents first
	// Key: fileID (unique physical file identifier)
	// Value: link count and set of paths pointing to this file
	fileIDMap := make(map[string]*fileIDInfo)

	for hash, files := range filesByHash {
		torrent := torrentByHash[hash]
		for _, f := range files {
			fullPath := buildFullPath(torrent.SavePath, f.Name)

			info, err := os.Lstat(fullPath)
			if err != nil {
				continue // Skip inaccessible files
			}
			if !info.Mode().IsRegular() {
				continue // Skip directories and non-regular files
			}

			fileID, nlink, err := hardlink.LinkInfo(info, fullPath)
			if err != nil {
				continue // Skip files we can't get info for
			}

			if fileIDMap[fileID] == nil {
				fileIDMap[fileID] = &fileIDInfo{
					nlink: nlink,
					paths: make(map[string]struct{}),
				}
			}
			fileIDMap[fileID].paths[fullPath] = struct{}{}
		}
	}

	// Now compute scope for each torrent
	for hash, files := range filesByHash {
		torrent := torrentByHash[hash]
		scope := HardlinkScopeNone
		filesAccessible := 0

		for _, f := range files {
			fullPath := buildFullPath(torrent.SavePath, f.Name)

			info, err := os.Lstat(fullPath)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}

			fileID, nlink, err := hardlink.LinkInfo(info, fullPath)
			if err != nil {
				continue // Skip files we can't get info for
			}

			filesAccessible++

			if nlink <= 1 {
				continue // Not hardlinked, but file was accessible
			}

			// File has hardlinks (nlink > 1)
			idInfo := fileIDMap[fileID]
			if idInfo == nil {
				continue
			}

			inTorrentSetCount := uint64(len(idInfo.paths))

			if nlink > inTorrentSetCount {
				// At least one link is outside the torrent set
				scope = HardlinkScopeOutsideQBitTorrent
				break // outside_qbittorrent wins - no need to check more files
			}

			// All links are within the torrent set
			if scope != HardlinkScopeOutsideQBitTorrent {
				scope = HardlinkScopeTorrentsOnly
			}
		}

		// Only add to result if at least one file was accessible
		// Unknown scope (no accessible files) = not added = won't match any condition
		if filesAccessible > 0 {
			result[hash] = scope
		}
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("totalTorrents", len(torrents)).
		Int("scopeComputed", len(result)).
		Msg("automations: hardlink scope detection completed")

	return result
}

// buildFullPath constructs the full path for a torrent file.
// qBittorrent always returns forward slashes, so we normalize using filepath.FromSlash.
func buildFullPath(basePath, filePath string) string {
	// Normalize forward slashes to OS-native path separators
	normalizedFile := filepath.FromSlash(filePath)
	normalizedBase := filepath.FromSlash(basePath)

	cleaned := filepath.Clean(normalizedFile)
	if filepath.IsAbs(cleaned) {
		return cleaned
	}
	return filepath.Join(normalizedBase, cleaned)
}

// applySpeedLimits applies upload or download limits in batches, logging and recording failures.
// Returns a map of limit (KiB) -> count of successfully updated torrents.
func (s *Service) applySpeedLimits(
	ctx context.Context,
	instanceID int,
	batches map[int64][]string,
	limitType string,
	setLimit func(ctx context.Context, instanceID int, hashes []string, limit int64) error,
) map[int64]int {
	successCounts := make(map[int64]int)
	for limit, hashes := range batches {
		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := setLimit(ctx, instanceID, batch, limit); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Int64("limitKiB", limit).Int("count", len(batch)).Str("limitType", limitType).Msg("automations: speed limit failed")
				if s.activityStore != nil {
					detailsJSON, marshalErr := json.Marshal(map[string]any{"limitKiB": limit, "count": len(batch), "type": limitType})
					if marshalErr != nil {
						log.Warn().Err(marshalErr).Int("instanceID", instanceID).Msg("automations: failed to marshal activity details")
					}
					if err := s.activityStore.Create(ctx, &models.AutomationActivity{
						InstanceID: instanceID,
						Hash:       strings.Join(batch, ","),
						Action:     models.ActivityActionLimitFailed,
						Outcome:    models.ActivityOutcomeFailed,
						Reason:     limitType + " limit failed: " + err.Error(),
						Details:    detailsJSON,
					}); err != nil {
						log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record activity")
					}
				}
			} else {
				successCounts[limit] += len(batch)
			}
		}
	}
	return successCounts
}
