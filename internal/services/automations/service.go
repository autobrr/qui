// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package automations enforces tracker-scoped speed/ratio rules per instance.
package automations

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/autobrr/qui/internal/services/externalprograms"
)

// Config controls how often rules are re-applied and how long to debounce repeats.
type Config struct {
	ScanInterval          time.Duration
	SkipWithin            time.Duration
	MaxBatchHashes        int
	ActivityRetentionDays int
	ActivityRunRetention  time.Duration
	ActivityRunMax        int
	ApplyTimeout          time.Duration // timeout for applying all actions per instance
}

// DefaultRuleInterval is the cadence for rules that don't specify their own interval.
const DefaultRuleInterval = 15 * time.Minute

// freeSpaceDeleteCooldown prevents FREE_SPACE delete rules from running too frequently.
// After a successful delete-with-files caused by a FREE_SPACE rule, the next run is
// delayed to allow qBittorrent to refresh its disk free space reading.
const freeSpaceDeleteCooldown = 5 * time.Minute

// Log messages for delete actions (reduces duplication)
const logMsgRemoveTorrentWithFiles = "automations: removing torrent with files"

// ruleKey identifies a rule within an instance for per-rule cadence tracking.
type ruleKey struct {
	instanceID int
	ruleID     int
}

type shareKey struct {
	ratio    float64
	seed     int64
	inactive int64
}

type tagChange struct {
	current  map[string]struct{}
	desired  map[string]struct{}
	toAdd    []string
	toRemove []string
}

type categoryMove struct {
	hash          string
	name          string
	trackerDomain string
	category      string
}

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

func DefaultConfig() Config {
	return Config{
		ScanInterval:          20 * time.Second,
		SkipWithin:            2 * time.Minute,
		MaxBatchHashes:        50, // matches qBittorrent's max_concurrent_http_announces default
		ActivityRetentionDays: 7,
		ActivityRunRetention:  24 * time.Hour,
		ActivityRunMax:        500,
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
	externalProgramService    *externalprograms.Service // for executing external programs
	activityRuns              *activityRunStore

	// keep lightweight memory of recent applications to avoid hammering qBittorrent
	lastApplied           map[int]map[string]time.Time // instanceID -> hash -> timestamp
	lastRuleRun           map[ruleKey]time.Time        // per-rule cadence tracking
	lastFreeSpaceDeleteAt map[int]time.Time            // instanceID -> last FREE_SPACE delete timestamp
	mu                    sync.RWMutex
}

func NewService(cfg Config, instanceStore *models.InstanceStore, ruleStore *models.AutomationStore, activityStore *models.AutomationActivityStore, trackerCustomizationStore *models.TrackerCustomizationStore, syncManager *qbittorrent.SyncManager, externalProgramService *externalprograms.Service) *Service {
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
	if cfg.ActivityRunRetention <= 0 {
		cfg.ActivityRunRetention = DefaultConfig().ActivityRunRetention
	}
	if cfg.ActivityRunMax <= 0 {
		cfg.ActivityRunMax = DefaultConfig().ActivityRunMax
	}
	return &Service{
		cfg:                       cfg,
		instanceStore:             instanceStore,
		ruleStore:                 ruleStore,
		activityStore:             activityStore,
		trackerCustomizationStore: trackerCustomizationStore,
		syncManager:               syncManager,
		externalProgramService:    externalProgramService,
		activityRuns:              newActivityRunStore(cfg.ActivityRunRetention, cfg.ActivityRunMax),
		lastApplied:               make(map[int]map[string]time.Time),
		lastRuleRun:               make(map[ruleKey]time.Time),
		lastFreeSpaceDeleteAt:     make(map[int]time.Time),
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

	// Clean up FREE_SPACE cooldown entries older than 10 minutes
	for instanceID, ts := range s.lastFreeSpaceDeleteAt {
		if ts.Before(cutoff) {
			delete(s.lastFreeSpaceDeleteAt, instanceID)
		}
	}

	if s.activityRuns != nil {
		s.activityRuns.Prune()
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
	ContentPath    string  `json:"contentPath,omitempty"`
	IsUnregistered bool    `json:"isUnregistered,omitempty"`
	IsCrossSeed    bool    `json:"isCrossSeed,omitempty"`    // For category preview
	IsHardlinkCopy bool    `json:"isHardlinkCopy,omitempty"` // Included via hardlink expansion (not ContentPath match)

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
func buildPreviewTorrent(torrent *qbt.Torrent, tracker string, evalCtx *EvalContext, isCrossSeed, isHardlinkCopy bool) PreviewTorrent {
	pt := PreviewTorrent{
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
		ContentPath:    torrent.ContentPath,
		IsCrossSeed:    isCrossSeed,
		IsHardlinkCopy: isHardlinkCopy,
		NumSeeds:       torrent.NumSeeds,
		NumComplete:    torrent.NumComplete,
		NumLeechs:      torrent.NumLeechs,
		NumIncomplete:  torrent.NumIncomplete,
		Progress:       torrent.Progress,
		Availability:   torrent.Availability,
		TimeActive:     torrent.TimeActive,
		LastActivity:   torrent.LastActivity,
		CompletionOn:   torrent.CompletionOn,
		TotalSize:      torrent.TotalSize,
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

// previewConfig holds common preview configuration.
type previewConfig struct {
	limit  int
	offset int
}

// normalize applies default values to preview config.
func (c *previewConfig) normalize() {
	if c.limit <= 0 {
		c.limit = 25
	}
	if c.offset < 0 {
		c.offset = 0
	}
}

// sortTorrentsStable sorts torrents by AddedOn (oldest first), then by hash for deterministic pagination.
func sortTorrentsStable(torrents []qbt.Torrent) {
	sort.Slice(torrents, func(i, j int) bool {
		if torrents[i].AddedOn != torrents[j].AddedOn {
			return torrents[i].AddedOn < torrents[j].AddedOn
		}
		return torrents[i].Hash < torrents[j].Hash
	})
}

// initPreviewEvalContext initializes an EvalContext for preview with common setup.
func (s *Service) initPreviewEvalContext(ctx context.Context, instanceID int, torrents []qbt.Torrent) (*EvalContext, *models.Instance) {
	evalCtx := &EvalContext{}

	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get instance for preview, proceeding without instance context")
	}

	if instance != nil {
		evalCtx.InstanceHasLocalAccess = instance.HasLocalFilesystemAccess
	}

	// Build category index for EXISTS_IN/CONTAINS_IN operators
	evalCtx.CategoryIndex, evalCtx.CategoryNames = BuildCategoryIndex(torrents)

	// Get health counts from background cache
	if healthCounts := s.syncManager.GetTrackerHealthCounts(instanceID); healthCounts != nil {
		if len(healthCounts.UnregisteredSet) > 0 {
			evalCtx.UnregisteredSet = healthCounts.UnregisteredSet
		}
		if len(healthCounts.TrackerDownSet) > 0 {
			evalCtx.TrackerDownSet = healthCounts.TrackerDownSet
		}
	}

	return evalCtx, instance
}

// setupFreeSpaceContext initializes FREE_SPACE context if needed by the rule.
func (s *Service) setupFreeSpaceContext(ctx context.Context, instanceID int, rule *models.Automation, evalCtx *EvalContext, instance *models.Instance) error {
	if instance == nil || !rulesUseCondition([]*models.Automation{rule}, FieldFreeSpace) {
		return nil
	}

	freeSpace, err := GetFreeSpaceBytesForSource(ctx, s.syncManager, instance, rule.FreeSpaceSource)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get free space")
		return fmt.Errorf("failed to get free space: %w", err)
	}
	evalCtx.FreeSpace = freeSpace
	evalCtx.FilesToClear = make(map[crossSeedKey]struct{})
	return nil
}

// getTrackerForTorrent returns the first tracker domain for a torrent.
func getTrackerForTorrent(torrent *qbt.Torrent, sm *qbittorrent.SyncManager) string {
	if domains := collectTrackerDomains(*torrent, sm); len(domains) > 0 {
		return domains[0]
	}
	return ""
}

// PreviewDeleteRule returns torrents that would be deleted by the given rule.
// This is used to show users what a rule would affect before saving.
// For "include cross-seeds" mode, also shows expanded cross-seeds that would be deleted.
// previewView controls the view mode:
//   - "needed" (default): Show minimal deletions needed to reach FREE_SPACE target (stops early)
//   - "eligible": Show all torrents matching the rule filters (ignores cumulative stop-when-satisfied)
func (s *Service) PreviewDeleteRule(ctx context.Context, instanceID int, rule *models.Automation, limit, offset int, previewView string) (*PreviewResult, error) {
	if s == nil || s.syncManager == nil {
		return &PreviewResult{}, nil
	}

	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	sortTorrentsStable(torrents)

	cfg := previewConfig{limit: limit, offset: offset}
	cfg.normalize()

	evalCtx, instance := s.initPreviewEvalContext(ctx, instanceID, torrents)
	hardlinkIndex := s.setupDeleteHardlinkContext(ctx, instanceID, rule, torrents, evalCtx, instance)
	s.setupMissingFilesContext(ctx, instanceID, rule, torrents, evalCtx, instance)

	if err := s.setupFreeSpaceContext(ctx, instanceID, rule, evalCtx, instance); err != nil {
		return nil, err
	}

	deleteMode := getDeleteMode(rule)
	eligibleMode := previewView == "eligible"

	if deleteMode == DeleteModeWithFilesIncludeCrossSeeds {
		return s.previewDeleteIncludeCrossSeeds(ctx, instanceID, rule, torrents, evalCtx, hardlinkIndex, cfg.limit, cfg.offset, eligibleMode)
	}

	return s.previewDeleteStandard(rule, torrents, evalCtx, deleteMode, eligibleMode, cfg)
}

// setupDeleteHardlinkContext sets up hardlink index if needed for delete preview.
func (s *Service) setupDeleteHardlinkContext(ctx context.Context, instanceID int, rule *models.Automation, torrents []qbt.Torrent, evalCtx *EvalContext, instance *models.Instance) *HardlinkIndex {
	if instance == nil || !instance.HasLocalFilesystemAccess {
		return nil
	}
	if rule.Conditions == nil || rule.Conditions.Delete == nil {
		return nil
	}

	cond := rule.Conditions.Delete.Condition
	needsHardlinkScope := ConditionUsesField(cond, FieldHardlinkScope) || rule.Conditions.Delete.IncludeHardlinks
	if !needsHardlinkScope {
		return nil
	}

	hardlinkIndex := s.GetHardlinkIndex(ctx, instanceID, torrents)
	if hardlinkIndex != nil {
		evalCtx.HardlinkScopeByHash = hardlinkIndex.ScopeByHash
	}
	return hardlinkIndex
}

// setupMissingFilesContext sets up missing files detection if needed for delete preview.
func (s *Service) setupMissingFilesContext(ctx context.Context, instanceID int, rule *models.Automation, torrents []qbt.Torrent, evalCtx *EvalContext, instance *models.Instance) {
	if instance == nil || !instance.HasLocalFilesystemAccess {
		return
	}
	if rule.Conditions == nil || rule.Conditions.Delete == nil {
		return
	}

	cond := rule.Conditions.Delete.Condition
	if !ConditionUsesField(cond, FieldHasMissingFiles) {
		return
	}

	evalCtx.HasMissingFilesByHash = s.detectMissingFiles(ctx, instanceID, torrents)
}

// getDeleteMode returns the delete mode from rule or default.
func getDeleteMode(rule *models.Automation) string {
	if rule.Conditions != nil && rule.Conditions.Delete != nil && rule.Conditions.Delete.Mode != "" {
		return rule.Conditions.Delete.Mode
	}
	return DeleteModeKeepFiles
}

// shouldDeleteTorrent checks if a torrent matches the delete condition.
func shouldDeleteTorrent(rule *models.Automation, torrent *qbt.Torrent, evalCtx *EvalContext) bool {
	if rule.Conditions == nil || rule.Conditions.Delete == nil || !rule.Conditions.Delete.Enabled {
		return false
	}
	if rule.Conditions.Delete.Condition == nil {
		return true
	}
	return EvaluateConditionWithContext(rule.Conditions.Delete.Condition, *torrent, evalCtx, 0)
}

// previewDeleteStandard handles standard (non-include-cross-seeds) delete preview.
func (s *Service) previewDeleteStandard(
	rule *models.Automation,
	torrents []qbt.Torrent,
	evalCtx *EvalContext,
	deleteMode string,
	eligibleMode bool,
	cfg previewConfig,
) (*PreviewResult, error) {
	result := &PreviewResult{
		Examples: make([]PreviewTorrent, 0, cfg.limit),
	}

	matchIndex := 0
	for i := range torrents {
		torrent := &torrents[i]

		trackerDomains := collectTrackerDomains(*torrent, s.syncManager)
		if !matchesTracker(rule.TrackerPattern, trackerDomains) {
			continue
		}

		if !shouldDeleteTorrent(rule, torrent, evalCtx) {
			continue
		}

		if !eligibleMode {
			updateCumulativeFreeSpaceCleared(*torrent, evalCtx, deleteMode, torrents)
		}

		matchIndex++
		if matchIndex <= cfg.offset {
			continue
		}
		if len(result.Examples) < cfg.limit {
			tracker := getTrackerForTorrent(torrent, s.syncManager)
			result.Examples = append(result.Examples, buildPreviewTorrent(torrent, tracker, evalCtx, false, false))
		}
	}

	result.TotalMatches = matchIndex
	return result, nil
}

// crossSeedExpansionState tracks state during cross-seed preview expansion.
type crossSeedExpansionState struct {
	expandedSet           map[string]struct{}
	crossSeedSet          map[string]struct{}
	hardlinkCopySet       map[string]struct{}
	processedContentPaths map[string]struct{}
}

func newCrossSeedExpansionState() *crossSeedExpansionState {
	return &crossSeedExpansionState{
		expandedSet:           make(map[string]struct{}),
		crossSeedSet:          make(map[string]struct{}),
		hardlinkCopySet:       make(map[string]struct{}),
		processedContentPaths: make(map[string]struct{}),
	}
}

// isAlreadyExpanded returns true if the torrent hash is already in the expanded set.
func (s *crossSeedExpansionState) isAlreadyExpanded(hash string) bool {
	_, included := s.expandedSet[hash]
	return included
}

// isContentPathProcessed returns true if the content path was already processed.
func (s *crossSeedExpansionState) isContentPathProcessed(contentPath string) bool {
	_, processed := s.processedContentPaths[contentPath]
	return processed
}

// markContentPathProcessed marks a content path as processed.
func (s *crossSeedExpansionState) markContentPathProcessed(contentPath string) {
	s.processedContentPaths[contentPath] = struct{}{}
}

// addHardlinkCopies adds hardlink copies to the expanded set.
func (s *crossSeedExpansionState) addHardlinkCopies(hardlinkIndex *HardlinkIndex, triggerHash string) {
	if hardlinkIndex == nil {
		return
	}
	for _, hlHash := range hardlinkIndex.GetHardlinkCopies(triggerHash) {
		if _, exists := s.expandedSet[hlHash]; !exists {
			s.expandedSet[hlHash] = struct{}{}
			s.hardlinkCopySet[hlHash] = struct{}{}
		}
	}
}

// previewDeleteIncludeCrossSeeds handles preview for "include cross-seeds" delete mode.
// It evaluates torrents incrementally, expanding with cross-seeds and updating FREE_SPACE
// projection after each group so that stop-when-satisfied behavior works correctly.
// When eligibleMode is true, it shows all matching torrents without cumulative stop-when-satisfied.
// If IncludeHardlinks is enabled, also expands with hardlink copies (same physical files).
func (s *Service) previewDeleteIncludeCrossSeeds(
	ctx context.Context,
	instanceID int,
	rule *models.Automation,
	torrents []qbt.Torrent,
	evalCtx *EvalContext,
	hardlinkIndex *HardlinkIndex,
	limit, offset int,
	eligibleMode bool,
) (*PreviewResult, error) {
	if rule.Conditions == nil || rule.Conditions.Delete == nil || !rule.Conditions.Delete.Enabled {
		return &PreviewResult{Examples: make([]PreviewTorrent, 0)}, nil
	}

	state := newCrossSeedExpansionState()
	deleteCond := rule.Conditions.Delete
	includeHardlinks := deleteCond.IncludeHardlinks

	s.setupHardlinkSignatureContext(evalCtx, hardlinkIndex, deleteCond.Condition, eligibleMode, includeHardlinks)

	for i := range torrents {
		torrent := &torrents[i]
		if state.isAlreadyExpanded(torrent.Hash) {
			continue
		}

		if !s.torrentMatchesDeleteRule(rule, torrent, evalCtx) {
			continue
		}

		contentPath := normalizePath(torrent.ContentPath)
		if state.isContentPathProcessed(contentPath) {
			continue
		}

		crossSeedGroup := findCrossSeedGroup(*torrent, torrents)
		state.markContentPathProcessed(contentPath)

		if !s.expandGroupForPreview(ctx, instanceID, torrent, crossSeedGroup, state.expandedSet, state.crossSeedSet) {
			continue
		}

		if includeHardlinks {
			state.addHardlinkCopies(hardlinkIndex, torrent.Hash)
		}

		if !eligibleMode {
			updateCumulativeFreeSpaceCleared(*torrent, evalCtx, DeleteModeWithFilesIncludeCrossSeeds, torrents)
		}
	}

	return s.buildCrossSeedPreviewResult(torrents, state, evalCtx, limit, offset), nil
}

// setupHardlinkSignatureContext sets up hardlink signature tracking for FREE_SPACE projection.
func (s *Service) setupHardlinkSignatureContext(evalCtx *EvalContext, hardlinkIndex *HardlinkIndex, cond *RuleCondition, eligibleMode, includeHardlinks bool) {
	if !includeHardlinks || hardlinkIndex == nil || eligibleMode {
		return
	}
	if !ConditionUsesField(cond, FieldFreeSpace) {
		return
	}
	evalCtx.HardlinkSignatureByHash = hardlinkIndex.SignatureByHash
	evalCtx.HardlinkSignaturesToClear = make(map[string]struct{})
}

// torrentMatchesDeleteRule checks if a torrent matches the tracker pattern and delete condition.
func (s *Service) torrentMatchesDeleteRule(rule *models.Automation, torrent *qbt.Torrent, evalCtx *EvalContext) bool {
	trackerDomains := collectTrackerDomains(*torrent, s.syncManager)
	if !matchesTracker(rule.TrackerPattern, trackerDomains) {
		return false
	}

	cond := rule.Conditions.Delete.Condition
	return cond == nil || EvaluateConditionWithContext(cond, *torrent, evalCtx, 0)
}

// buildCrossSeedPreviewResult builds the paginated preview result from expansion state.
func (s *Service) buildCrossSeedPreviewResult(
	torrents []qbt.Torrent,
	state *crossSeedExpansionState,
	evalCtx *EvalContext,
	limit, offset int,
) *PreviewResult {
	result := &PreviewResult{
		TotalMatches:   len(state.expandedSet),
		CrossSeedCount: len(state.crossSeedSet),
		Examples:       make([]PreviewTorrent, 0, limit),
	}

	matchIndex := 0
	for i := range torrents {
		torrent := &torrents[i]
		if !state.isAlreadyExpanded(torrent.Hash) {
			continue
		}

		matchIndex++
		if matchIndex <= offset {
			continue
		}
		if len(result.Examples) >= limit {
			break
		}

		_, isCrossSeed := state.crossSeedSet[torrent.Hash]
		_, isHardlinkCopy := state.hardlinkCopySet[torrent.Hash]
		tracker := getTrackerForTorrent(torrent, s.syncManager)
		result.Examples = append(result.Examples, buildPreviewTorrent(torrent, tracker, evalCtx, isCrossSeed, isHardlinkCopy))
	}

	return result
}

// verifyGroupForPreview validates an ambiguous cross-seed group for preview.
// Returns (true, hashes) if all verifications pass, (false, nil) if any fail.
// Safety: if ANY verification fails, the entire group should be skipped.
func (s *Service) verifyGroupForPreview(
	ctx context.Context,
	instanceID int,
	trigger *qbt.Torrent,
	crossSeedGroup []qbt.Torrent,
	alreadyIncluded map[string]struct{},
) (ok bool, hashes []string) {
	verifiedHashes := []string{trigger.Hash}
	for i := range crossSeedGroup {
		other := &crossSeedGroup[i]
		if other.Hash == trigger.Hash {
			continue
		}
		if _, exists := alreadyIncluded[other.Hash]; exists {
			continue
		}
		hasOverlap, err := s.verifyFileOverlap(ctx, instanceID, *trigger, *other)
		if err != nil || !hasOverlap {
			// Any failure means skip the entire group
			return false, nil
		}
		verifiedHashes = append(verifiedHashes, other.Hash)
	}
	return true, verifiedHashes
}

// expandGroupForPreview expands a trigger torrent with its cross-seed group for preview.
// Returns true if group was added, false if skipped (e.g., verification failure).
func (s *Service) expandGroupForPreview(
	ctx context.Context,
	instanceID int,
	trigger *qbt.Torrent,
	crossSeedGroup []qbt.Torrent,
	expandedSet, crossSeedSet map[string]struct{},
) bool {
	// No cross-seeds, just add the trigger
	if len(crossSeedGroup) <= 1 {
		expandedSet[trigger.Hash] = struct{}{}
		return true
	}

	// Ambiguous group requires verification
	if isContentPathAmbiguous(*trigger) {
		return s.expandAmbiguousGroup(ctx, instanceID, trigger, crossSeedGroup, expandedSet, crossSeedSet)
	}

	// Unambiguous group - include all cross-seeds
	expandUnambiguousCrossSeeds(trigger, crossSeedGroup, expandedSet, crossSeedSet)
	return true
}

// expandAmbiguousGroup verifies and expands an ambiguous cross-seed group.
func (s *Service) expandAmbiguousGroup(
	ctx context.Context,
	instanceID int,
	trigger *qbt.Torrent,
	crossSeedGroup []qbt.Torrent,
	expandedSet, crossSeedSet map[string]struct{},
) bool {
	valid, verifiedHashes := s.verifyGroupForPreview(ctx, instanceID, trigger, crossSeedGroup, expandedSet)
	if !valid {
		return false
	}
	for _, h := range verifiedHashes {
		expandedSet[h] = struct{}{}
		if h != trigger.Hash {
			crossSeedSet[h] = struct{}{}
		}
	}
	return true
}

// expandUnambiguousCrossSeeds adds all cross-seeds from an unambiguous group.
func expandUnambiguousCrossSeeds(trigger *qbt.Torrent, crossSeedGroup []qbt.Torrent, expandedSet, crossSeedSet map[string]struct{}) {
	expandedSet[trigger.Hash] = struct{}{}
	for i := range crossSeedGroup {
		other := &crossSeedGroup[i]
		if other.Hash == trigger.Hash {
			continue
		}
		if _, exists := expandedSet[other.Hash]; exists {
			continue
		}
		expandedSet[other.Hash] = struct{}{}
		crossSeedSet[other.Hash] = struct{}{}
	}
}

// categoryPreviewState tracks state during category preview processing.
type categoryPreviewState struct {
	directMatchSet map[string]struct{}
	crossSeedSet   map[string]struct{}
	matchedKeys    map[crossSeedKey]struct{}
	targetCategory string
}

func newCategoryPreviewState(targetCategory string) *categoryPreviewState {
	return &categoryPreviewState{
		directMatchSet: make(map[string]struct{}),
		crossSeedSet:   make(map[string]struct{}),
		matchedKeys:    make(map[crossSeedKey]struct{}),
		targetCategory: targetCategory,
	}
}

// PreviewCategoryRule returns torrents that would have their category changed by the given rule.
// If IncludeCrossSeeds is enabled, also includes cross-seeds that share files with matched torrents.
func (s *Service) PreviewCategoryRule(ctx context.Context, instanceID int, rule *models.Automation, limit, offset int) (*PreviewResult, error) {
	if s == nil || s.syncManager == nil {
		return &PreviewResult{}, nil
	}

	torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	sortTorrentsStable(torrents)
	crossSeedIndex := buildCrossSeedIndex(torrents)

	cfg := previewConfig{limit: limit, offset: offset}
	cfg.normalize()

	evalCtx, instance := s.initPreviewEvalContext(ctx, instanceID, torrents)
	s.setupCategoryHardlinkContext(ctx, instanceID, rule, torrents, evalCtx, instance)

	if err := s.setupFreeSpaceContext(ctx, instanceID, rule, evalCtx, instance); err != nil {
		return nil, err
	}

	catAction := getCategoryAction(rule)
	state := newCategoryPreviewState(catAction.targetCategory)

	s.findDirectCategoryMatches(rule, torrents, evalCtx, crossSeedIndex, catAction, state)
	s.findCategoryCrossSeeds(torrents, catAction, state)

	return s.buildCategoryPreviewResult(torrents, state, evalCtx, cfg), nil
}

// categoryActionConfig holds category action configuration.
type categoryActionConfig struct {
	targetCategory    string
	includeCrossSeeds bool
	blockCategories   []string
	condition         *RuleCondition
	enabled           bool
}

// getCategoryAction extracts category action configuration from rule.
func getCategoryAction(rule *models.Automation) categoryActionConfig {
	if rule.Conditions == nil || rule.Conditions.Category == nil {
		return categoryActionConfig{}
	}
	cat := rule.Conditions.Category
	return categoryActionConfig{
		targetCategory:    cat.Category,
		includeCrossSeeds: cat.IncludeCrossSeeds,
		blockCategories:   cat.BlockIfCrossSeedInCategories,
		condition:         cat.Condition,
		enabled:           cat.Enabled,
	}
}

// setupCategoryHardlinkContext sets up hardlink index if needed for category preview.
func (s *Service) setupCategoryHardlinkContext(ctx context.Context, instanceID int, rule *models.Automation, torrents []qbt.Torrent, evalCtx *EvalContext, instance *models.Instance) {
	if instance == nil || !instance.HasLocalFilesystemAccess {
		return
	}
	if rule.Conditions == nil || rule.Conditions.Category == nil {
		return
	}

	cond := rule.Conditions.Category.Condition
	if !ConditionUsesField(cond, FieldHardlinkScope) {
		return
	}

	hardlinkIndex := s.GetHardlinkIndex(ctx, instanceID, torrents)
	if hardlinkIndex != nil {
		evalCtx.HardlinkScopeByHash = hardlinkIndex.ScopeByHash
	}
}

// shouldApplyCategoryAction checks if category action should apply to torrent.
func shouldApplyCategoryAction(torrent *qbt.Torrent, catAction categoryActionConfig, evalCtx *EvalContext, crossSeedIndex map[crossSeedKey][]qbt.Torrent) bool {
	if !catAction.enabled {
		return false
	}
	if catAction.condition != nil && !EvaluateConditionWithContext(catAction.condition, *torrent, evalCtx, 0) {
		return false
	}
	return !shouldBlockCategoryChangeForCrossSeeds(*torrent, catAction.blockCategories, crossSeedIndex)
}

// findDirectCategoryMatches finds torrents that directly match the category rule.
func (s *Service) findDirectCategoryMatches(
	rule *models.Automation,
	torrents []qbt.Torrent,
	evalCtx *EvalContext,
	crossSeedIndex map[crossSeedKey][]qbt.Torrent,
	catAction categoryActionConfig,
	state *categoryPreviewState,
) {
	for i := range torrents {
		torrent := &torrents[i]

		trackerDomains := collectTrackerDomains(*torrent, s.syncManager)
		if !matchesTracker(rule.TrackerPattern, trackerDomains) {
			continue
		}

		if torrent.Category == state.targetCategory {
			continue
		}

		if !shouldApplyCategoryAction(torrent, catAction, evalCtx, crossSeedIndex) {
			continue
		}

		state.directMatchSet[torrent.Hash] = struct{}{}
		if catAction.includeCrossSeeds {
			if key, ok := makeCrossSeedKey(*torrent); ok {
				state.matchedKeys[key] = struct{}{}
			}
		}
	}
}

// findCategoryCrossSeeds finds cross-seeds for matched torrents.
func (s *Service) findCategoryCrossSeeds(torrents []qbt.Torrent, catAction categoryActionConfig, state *categoryPreviewState) {
	if !catAction.includeCrossSeeds || len(state.matchedKeys) == 0 {
		return
	}

	for i := range torrents {
		torrent := &torrents[i]
		if _, isDirectMatch := state.directMatchSet[torrent.Hash]; isDirectMatch {
			continue
		}
		if torrent.Category == state.targetCategory {
			continue
		}
		if key, ok := makeCrossSeedKey(*torrent); ok {
			if _, matched := state.matchedKeys[key]; matched {
				state.crossSeedSet[torrent.Hash] = struct{}{}
			}
		}
	}
}

// buildCategoryPreviewResult builds the paginated preview result for category preview.
func (s *Service) buildCategoryPreviewResult(
	torrents []qbt.Torrent,
	state *categoryPreviewState,
	evalCtx *EvalContext,
	cfg previewConfig,
) *PreviewResult {
	allMatches := make(map[string]struct{}, len(state.directMatchSet)+len(state.crossSeedSet))
	for h := range state.directMatchSet {
		allMatches[h] = struct{}{}
	}
	for h := range state.crossSeedSet {
		allMatches[h] = struct{}{}
	}

	result := &PreviewResult{
		TotalMatches:   len(allMatches),
		CrossSeedCount: len(state.crossSeedSet),
		Examples:       make([]PreviewTorrent, 0, cfg.limit),
	}

	matchIndex := 0
	for i := range torrents {
		torrent := &torrents[i]
		if _, included := allMatches[torrent.Hash]; !included {
			continue
		}

		matchIndex++
		if matchIndex <= cfg.offset {
			continue
		}
		if len(result.Examples) >= cfg.limit {
			break
		}

		_, isCrossSeed := state.crossSeedSet[torrent.Hash]
		tracker := getTrackerForTorrent(torrent, s.syncManager)
		result.Examples = append(result.Examples, buildPreviewTorrent(torrent, tracker, evalCtx, isCrossSeed, false))
	}

	return result
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

	var liveRules []*models.Automation
	var dryRunRules []*models.Automation
	for _, rule := range rules {
		if rule.DryRun {
			dryRunRules = append(dryRunRules, rule)
		} else {
			liveRules = append(liveRules, rule)
		}
	}

	if err := s.applyRulesForInstance(ctx, instanceID, force, dryRunRules, true); err != nil {
		return err
	}
	if err := s.applyRulesForInstance(ctx, instanceID, force, liveRules, false); err != nil {
		return err
	}

	return nil
}

func (s *Service) applyRulesForInstance(ctx context.Context, instanceID int, force bool, rules []*models.Automation, dryRun bool) error {
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

	// Check FREE_SPACE delete cooldown for this instance
	// This prevents overly aggressive deletion while qBittorrent updates its disk free space reading
	s.mu.RLock()
	lastFSDelete := s.lastFreeSpaceDeleteAt[instanceID]
	s.mu.RUnlock()
	inFreeSpaceCooldown := !lastFSDelete.IsZero() && now.Sub(lastFSDelete) < freeSpaceDeleteCooldown

	// If in cooldown, filter out delete rules that use FREE_SPACE
	if inFreeSpaceCooldown {
		filtered := make([]*models.Automation, 0, len(eligibleRules))
		for _, rule := range eligibleRules {
			// Skip delete rules that use FREE_SPACE condition
			if rule.Conditions != nil && rule.Conditions.Delete != nil && rule.Conditions.Delete.Enabled {
				if ConditionUsesField(rule.Conditions.Delete.Condition, FieldFreeSpace) {
					log.Debug().
						Int("instanceID", instanceID).
						Int("ruleID", rule.ID).
						Str("ruleName", rule.Name).
						Dur("cooldownRemaining", freeSpaceDeleteCooldown-now.Sub(lastFSDelete)).
						Msg("automations: skipping FREE_SPACE delete rule due to cooldown")
					continue
				}
			}
			filtered = append(filtered, rule)
		}
		eligibleRules = filtered
		if len(eligibleRules) == 0 {
			return nil
		}
	}

	// Build set of rule IDs whose delete action uses FREE_SPACE condition
	// Used to determine if we should start the cooldown after successful deletions
	freeSpaceDeleteRuleIDs := make(map[int]struct{})
	for _, rule := range eligibleRules {
		if rule.Conditions != nil && rule.Conditions.Delete != nil && rule.Conditions.Delete.Enabled {
			if ConditionUsesField(rule.Conditions.Delete.Condition, FieldFreeSpace) {
				freeSpaceDeleteRuleIDs[rule.ID] = struct{}{}
			}
		}
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

	// On-demand hardlink index (if rules use HARDLINK_SCOPE condition OR includeHardlinks)
	// The cached index provides scope detection AND hardlink grouping in a single build.
	var hardlinkIndex *HardlinkIndex
	needsHardlinkScope := rulesUseCondition(eligibleRules, FieldHardlinkScope) || rulesUseIncludeHardlinks(eligibleRules)
	if instance.HasLocalFilesystemAccess && needsHardlinkScope {
		hardlinkIndex = s.GetHardlinkIndex(ctx, instanceID, torrents)
		if hardlinkIndex != nil {
			evalCtx.HardlinkScopeByHash = hardlinkIndex.ScopeByHash
		}
	}

	// On-demand missing files detection (only if rules use HAS_MISSING_FILES and instance has local access)
	if instance.HasLocalFilesystemAccess && rulesUseCondition(eligibleRules, FieldHasMissingFiles) {
		evalCtx.HasMissingFilesByHash = s.detectMissingFiles(ctx, instanceID, torrents)
	}

	// Get free space on instance (only if rules use FREE_SPACE field)
	// Also pre-compute hardlink groups for FREE_SPACE projection if needed
	if rulesUseCondition(eligibleRules, FieldFreeSpace) {
		// Initialize per-rule free space states.
		// Each rule gets its own projection state (keyed by source + rule ID),
		// ensuring rules with different thresholds on the same disk don't interfere.
		evalCtx.FreeSpaceStates = make(map[string]*FreeSpaceSourceState)

		// First, cache free space by source key to avoid redundant disk reads
		freeSpaceBySourceKey := make(map[string]int64)
		for _, r := range eligibleRules {
			if !ruleUsesCondition(r, FieldFreeSpace) {
				continue
			}
			sourceKey := GetFreeSpaceSourceKey(r.FreeSpaceSource)
			if _, cached := freeSpaceBySourceKey[sourceKey]; cached {
				continue
			}

			// Get free space for this source
			freeSpace, err := GetFreeSpaceBytesForSource(ctx, s.syncManager, instance, r.FreeSpaceSource)
			if err != nil {
				log.Error().Err(err).Int("instanceID", instanceID).Str("sourceKey", sourceKey).Msg("automations: failed to get free space for source")
				return fmt.Errorf("failed to get free space for source %s: %w", sourceKey, err)
			}
			freeSpaceBySourceKey[sourceKey] = freeSpace
		}

		// Now create per-rule states using cached free space values
		for _, r := range eligibleRules {
			if !ruleUsesCondition(r, FieldFreeSpace) {
				continue
			}
			ruleKey := GetFreeSpaceRuleKey(r)
			sourceKey := GetFreeSpaceSourceKey(r.FreeSpaceSource)
			evalCtx.FreeSpaceStates[ruleKey] = &FreeSpaceSourceState{
				FreeSpace:                 freeSpaceBySourceKey[sourceKey],
				SpaceToClear:              0,
				FilesToClear:              make(map[crossSeedKey]struct{}),
				HardlinkSignaturesToClear: make(map[string]struct{}),
			}
		}

		// Build hardlink signature map for FREE_SPACE dedupe if any rule needs it.
		// Must happen BEFORE processTorrents() so SpaceToClear is correctly deduplicated.
		if rulesNeedHardlinkSignatureMap(eligibleRules) && hardlinkIndex != nil {
			evalCtx.HardlinkSignatureByHash = hardlinkIndex.SignatureByHash
		}
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
				Int("moveNoMatch", stats.MoveConditionNotMet).
				Int("moveAlreadyAtDest", stats.MoveAlreadyAtDestination).
				Int("moveBlockedByCrossSeed", stats.MoveBlockedByCrossSeed).
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
	shareBatches := make(map[shareKey][]string)
	uploadBatches := make(map[int64][]string)
	downloadBatches := make(map[int64][]string)
	pauseHashes := make([]string, 0)
	resumeHashes := make([]string, 0)

	tagChanges := make(map[string]*tagChange)
	categoryBatches := make(map[string][]string) // category name -> hashes
	moveBatches := make(map[string][]string)     // path -> hashes

	// External program execution tracking
	var programExecutions []pendingProgramExec
	deleteHashesByMode := make(map[string][]string)
	pendingByHash := make(map[string]pendingDeletion)

	// Track hashes that have been processed for "include cross-seeds" expansion
	// to avoid double-processing
	includedCrossSeedHashes := make(map[string]struct{})

	// Track hashes that have been processed for hardlink expansion
	includedHardlinkHashes := make(map[string]struct{})

	for hash, state := range states {
		torrent := torrentByHash[hash]

		// If torrent is marked for deletion, skip all other actions
		if state.shouldDelete {
			deleteMode := state.deleteMode
			var actualMode string
			var keepingFiles bool
			var logMsg string
			var hashesToDelete []string

			switch deleteMode {
			case DeleteModeWithFilesIncludeCrossSeeds:
				// Find all cross-seeds sharing the same ContentPath
				crossSeedGroup := findCrossSeedGroup(torrent, torrents)
				if len(crossSeedGroup) <= 1 {
					// No cross-seeds, just delete this torrent
					hashesToDelete = []string{hash}
					actualMode = DeleteModeWithFiles
					logMsg = logMsgRemoveTorrentWithFiles
					keepingFiles = false
				} else if isContentPathAmbiguous(torrent) {
					// ContentPath is ambiguous (equals SavePath), need to verify file overlap for ALL members.
					// Safety: if ANY verification fails, skip the entire group to avoid leaving broken torrents.
					verifiedHashes := []string{hash}
					skipGroup := false
					for _, other := range crossSeedGroup {
						if other.Hash == hash {
							continue
						}
						// Skip if already processed in a previous iteration
						if _, processed := includedCrossSeedHashes[other.Hash]; processed {
							continue
						}
						hasOverlap, err := s.verifyFileOverlap(ctx, instanceID, torrent, other)
						if err != nil {
							log.Warn().Err(err).
								Int("instanceID", instanceID).Int("ruleID", state.deleteRuleID).Str("ruleName", state.deleteRuleName).
								Str("hash", hash).Str("otherHash", other.Hash).
								Msg("automations: skipping entire group due to verification error")
							skipGroup = true
							break
						}
						if !hasOverlap {
							log.Warn().
								Int("instanceID", instanceID).Int("ruleID", state.deleteRuleID).Str("ruleName", state.deleteRuleName).
								Str("hash", hash).Str("otherHash", other.Hash).
								Msg("automations: skipping entire group due to low file overlap")
							skipGroup = true
							break
						}
						verifiedHashes = append(verifiedHashes, other.Hash)
					}
					if skipGroup {
						// Skip this torrent entirely - don't delete trigger or cross-seeds
						continue
					}
					// All verified - mark cross-seeds and proceed
					for _, h := range verifiedHashes {
						if h != hash {
							includedCrossSeedHashes[h] = struct{}{}
						}
					}
					hashesToDelete = verifiedHashes
					actualMode = DeleteModeWithFiles
					logMsg = "automations: removing torrent with files (include cross-seeds - verified)"
					keepingFiles = false
				} else {
					// ContentPath is unambiguous, include all cross-seeds
					hashesToDelete = make([]string, 0, len(crossSeedGroup))
					for _, t := range crossSeedGroup {
						// Skip if already processed in a previous iteration
						if _, processed := includedCrossSeedHashes[t.Hash]; processed {
							continue
						}
						hashesToDelete = append(hashesToDelete, t.Hash)
						if t.Hash != hash {
							includedCrossSeedHashes[t.Hash] = struct{}{}
						}
					}
					actualMode = DeleteModeWithFiles
					logMsg = "automations: removing torrent with files (include cross-seeds)"
					keepingFiles = false
				}

				// Expand with hardlink copies if enabled (O(1) lookup via cached index)
				if state.deleteIncludeHardlinks && hardlinkIndex != nil {
					hlCopies := hardlinkIndex.GetHardlinkCopies(hash)
					if len(hlCopies) > 0 {
						// Build set from hashesToDelete for O(1) membership check
						toDeleteSet := make(map[string]struct{}, len(hashesToDelete))
						for _, h := range hashesToDelete {
							toDeleteSet[h] = struct{}{}
						}
						for _, hlHash := range hlCopies {
							// Skip if already in hashesToDelete or already processed
							if _, inDelete := toDeleteSet[hlHash]; inDelete {
								continue
							}
							if _, processed := includedHardlinkHashes[hlHash]; processed {
								continue
							}
							hashesToDelete = append(hashesToDelete, hlHash)
							includedHardlinkHashes[hlHash] = struct{}{}
						}
						logMsg = "automations: removing torrent with files (include cross-seeds + hardlinks)"
					}
				}
			case DeleteModeWithFilesPreserveCrossSeeds:
				hashesToDelete = []string{hash}
				if detectCrossSeeds(torrent, torrents) {
					actualMode = DeleteModeKeepFiles
					logMsg = "automations: removing torrent (cross-seed detected - keeping files)"
					keepingFiles = true
				} else {
					actualMode = DeleteModeWithFiles
					logMsg = logMsgRemoveTorrentWithFiles
					keepingFiles = false
				}
			case DeleteModeKeepFiles:
				hashesToDelete = []string{hash}
				actualMode = DeleteModeKeepFiles
				logMsg = "automations: removing torrent (keeping files)"
				keepingFiles = true
			default:
				hashesToDelete = []string{hash}
				actualMode = deleteMode
				logMsg = logMsgRemoveTorrentWithFiles
				keepingFiles = false
			}

			// Add all hashes to delete batch (with deduplication)
			for _, h := range hashesToDelete {
				// Skip if already queued for deletion (dedup)
				if _, alreadyQueued := pendingByHash[h]; alreadyQueued {
					continue
				}

				// Look up actual torrent info for proper logging
				torrentName := state.name
				trackerDomain := ""
				if len(state.trackerDomains) > 0 {
					trackerDomain = state.trackerDomains[0]
				}
				if h != hash {
					// Expanded cross-seed - use its own name/tracker info
					if t, exists := torrentByHash[h]; exists {
						torrentName = t.Name
						if domains := collectTrackerDomains(t, s.syncManager); len(domains) > 0 {
							trackerDomain = domains[0]
						}
					}
				}

				// Log with correct name for each hash
				isTrigger := h == hash
				log.Info().Str("hash", h).Str("name", torrentName).Bool("isTrigger", isTrigger).Str("reason", state.deleteReason).Bool("filesKept", keepingFiles).Msg(logMsg)
				deleteHashesByMode[actualMode] = append(deleteHashesByMode[actualMode], h)

				// Determine activity action type
				action := models.ActivityActionDeletedCondition
				if state.deleteReason == "unregistered" {
					action = models.ActivityActionDeletedUnregistered
				} else if state.deleteReason == "ratio limit reached" {
					action = models.ActivityActionDeletedRatio
				} else if state.deleteReason == "seeding time limit reached" || state.deleteReason == "ratio and seeding time limits reached" {
					action = models.ActivityActionDeletedSeeding
				}

				pendingByHash[h] = pendingDeletion{
					hash:          h,
					torrentName:   torrentName,
					trackerDomain: trackerDomain,
					action:        action,
					ruleID:        state.deleteRuleID,
					ruleName:      state.deleteRuleName,
					reason:        state.deleteReason,
					details:       map[string]any{"filesKept": keepingFiles, "deleteMode": deleteMode, "isTrigger": isTrigger},
				}

				// Mark as processed
				if !dryRun {
					s.mu.Lock()
					instLastApplied[h] = now
					s.mu.Unlock()
				}
			}
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
			// Start with torrent's current values
			ratio := torrent.RatioLimit
			seedMinutes := torrent.SeedingTimeLimit
			inactiveMinutes := torrent.InactiveSeedingTimeLimit // Preserve inactive limit

			// Apply desired values if set
			if state.ratioLimit != nil {
				ratio = *state.ratioLimit
			}
			if state.seedingMinutes != nil {
				seedMinutes = *state.seedingMinutes
			}

			// Normalize ratio to 2 decimal places to match qBittorrent/go-qbittorrent precision
			// This prevents perpetual reapplication due to floating point differences
			normalizeRatio := func(r float64) float64 {
				if r >= 0 {
					return float64(int(r*100+0.5)) / 100
				}
				return r // Keep sentinel values (-1, -2) unchanged
			}
			ratio = normalizeRatio(ratio)
			currentRatio := normalizeRatio(torrent.RatioLimit)

			// Check if update is needed (comparing normalized values)
			needsUpdate := (state.ratioLimit != nil && currentRatio != ratio) ||
				(state.seedingMinutes != nil && torrent.SeedingTimeLimit != seedMinutes)
			if needsUpdate {
				key := shareKey{ratio: ratio, seed: seedMinutes, inactive: inactiveMinutes}
				shareBatches[key] = append(shareBatches[key], hash)
			}
		}

		// Pause
		if state.shouldPause {
			pauseHashes = append(pauseHashes, hash)
		}

		// Resume
		if state.shouldResume {
			resumeHashes = append(resumeHashes, hash)
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

		if state.shouldMove {
			moveBatches[state.movePath] = append(moveBatches[state.movePath], hash)
		}

		// External program execution
		if state.externalProgramID != nil {
			programExecutions = append(programExecutions, pendingProgramExec{
				hash:      hash,
				torrent:   torrent,
				programID: *state.externalProgramID,
				ruleID:    state.programRuleID,
				ruleName:  state.programRuleName,
			})
		}

		// Mark as processed
		if !dryRun {
			s.mu.Lock()
			instLastApplied[hash] = now
			s.mu.Unlock()
		}
	}

	if dryRun {
		s.recordDryRunActivities(
			ctx,
			instanceID,
			uploadBatches,
			downloadBatches,
			shareBatches,
			pauseHashes,
			resumeHashes,
			tagChanges,
			categoryBatches,
			moveBatches,
			pendingByHash,
			programExecutions,
			torrentByHash,
			torrents,
			states,
		)
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.cfg.ApplyTimeout)
	defer cancel()

	// Apply speed limits and track success
	uploadSuccess := s.applySpeedLimits(ctx, instanceID, uploadBatches, "upload", s.syncManager.SetTorrentUploadLimit)
	downloadSuccess := s.applySpeedLimits(ctx, instanceID, downloadBatches, "download", s.syncManager.SetTorrentDownloadLimit)

	// Record aggregated speed limit activity
	if s.activityStore != nil && (len(uploadSuccess) > 0 || len(downloadSuccess) > 0) {
		speedLimits := make(map[string]int) // "upload:1024" -> count, "download:2048" -> count
		for limit, hashes := range uploadSuccess {
			speedLimits[fmt.Sprintf("upload:%d", limit)] = len(hashes)
		}
		for limit, hashes := range downloadSuccess {
			speedLimits[fmt.Sprintf("download:%d", limit)] = len(hashes)
		}
		detailsJSON, _ := json.Marshal(map[string]any{"limits": speedLimits})
		activityID, err := s.activityStore.CreateWithID(ctx, &models.AutomationActivity{
			InstanceID: instanceID,
			Hash:       "",
			Action:     models.ActivityActionSpeedLimitsChanged,
			Outcome:    models.ActivityOutcomeSuccess,
			Details:    detailsJSON,
		})
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record speed limit activity")
		} else if s.activityRuns != nil {
			items := buildSpeedLimitRunItems(uploadSuccess, downloadSuccess, torrentByHash, s.syncManager)
			if len(items) > 0 {
				s.activityRuns.Put(activityID, instanceID, items)
			}
		}
	}

	// Apply share limits and track success
	shareLimitSuccess := make(map[shareKey][]string) // "ratio:seed:inactive" -> hashes
	for key, hashes := range shareBatches {
		limited := limitHashBatch(hashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			err := s.syncManager.SetTorrentShareLimit(ctx, instanceID, batch, key.ratio, key.seed, key.inactive)
			if err == nil {
				shareLimitSuccess[key] = append(shareLimitSuccess[key], batch...)
				continue
			}
			log.Warn().Err(err).Int("instanceID", instanceID).Float64("ratio", key.ratio).Int64("seedMinutes", key.seed).Int64("inactiveMinutes", key.inactive).Int("count", len(batch)).Msg("automations: share limit failed")
			if s.activityStore == nil {
				continue
			}
			detailsJSON, marshalErr := json.Marshal(map[string]any{"ratio": key.ratio, "seedMinutes": key.seed, "inactiveMinutes": key.inactive, "count": len(batch), "type": "share"})
			if marshalErr != nil {
				log.Warn().Err(marshalErr).Int("instanceID", instanceID).Msg("automations: failed to marshal share limit details")
				continue
			}
			if actErr := s.activityStore.Create(ctx, &models.AutomationActivity{
				InstanceID: instanceID,
				Hash:       strings.Join(batch, ","),
				Action:     models.ActivityActionLimitFailed,
				Outcome:    models.ActivityOutcomeFailed,
				Reason:     "share limit failed: " + err.Error(),
				Details:    detailsJSON,
			}); actErr != nil {
				log.Warn().Err(actErr).Int("instanceID", instanceID).Msg("automations: failed to record activity")
			}
		}
	}

	// Record aggregated share limit activity
	if s.activityStore != nil && len(shareLimitSuccess) > 0 {
		limitCounts := make(map[string]int)
		for key, hashes := range shareLimitSuccess {
			limitKey := fmt.Sprintf("%.2f:%d:%d", key.ratio, key.seed, key.inactive)
			limitCounts[limitKey] = len(hashes)
		}
		detailsJSON, _ := json.Marshal(map[string]any{"limits": limitCounts})
		activityID, err := s.activityStore.CreateWithID(ctx, &models.AutomationActivity{
			InstanceID: instanceID,
			Hash:       "",
			Action:     models.ActivityActionShareLimitsChanged,
			Outcome:    models.ActivityOutcomeSuccess,
			Details:    detailsJSON,
		})
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record share limit activity")
		} else if s.activityRuns != nil {
			items := buildShareLimitRunItems(shareLimitSuccess, torrentByHash, s.syncManager)
			if len(items) > 0 {
				s.activityRuns.Put(activityID, instanceID, items)
			}
		}
	}

	// Execute pause actions for expression-based rules
	pausedCount := 0
	pausedHashesSuccess := make([]string, 0)
	if len(pauseHashes) > 0 {
		limited := limitHashBatch(pauseHashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.BulkAction(ctx, instanceID, batch, "pause"); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: pause action failed")
			} else {
				log.Info().Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: paused torrents")
				pausedCount += len(batch)
				pausedHashesSuccess = append(pausedHashesSuccess, batch...)
			}
		}
	}

	// Record aggregated pause activity
	if s.activityStore != nil && pausedCount > 0 {
		detailsJSON, _ := json.Marshal(map[string]any{"count": pausedCount})
		activityID, err := s.activityStore.CreateWithID(ctx, &models.AutomationActivity{
			InstanceID: instanceID,
			Hash:       "",
			Action:     models.ActivityActionPaused,
			Outcome:    models.ActivityOutcomeSuccess,
			Details:    detailsJSON,
		})
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record pause activity")
		} else if s.activityRuns != nil {
			items := buildRunItemsFromHashes(pausedHashesSuccess, torrentByHash, s.syncManager)
			if len(items) > 0 {
				s.activityRuns.Put(activityID, instanceID, items)
			}
		}
	}

	// Execute resume actions for expression-based rules
	resumedCount := 0
	resumedHashesSuccess := make([]string, 0)
	if len(resumeHashes) > 0 {
		limited := limitHashBatch(resumeHashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if err := s.syncManager.BulkAction(ctx, instanceID, batch, "resume"); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: resume action failed")
			} else {
				log.Info().Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: resumed torrents")
				resumedCount += len(batch)
				resumedHashesSuccess = append(resumedHashesSuccess, batch...)
			}
		}
	}

	// Record aggregated resume activity
	if s.activityStore != nil && resumedCount > 0 {
		detailsJSON, _ := json.Marshal(map[string]any{"count": resumedCount})
		activityID, err := s.activityStore.CreateWithID(ctx, &models.AutomationActivity{
			InstanceID: instanceID,
			Hash:       "",
			Action:     models.ActivityActionResumed,
			Outcome:    models.ActivityOutcomeSuccess,
			Details:    detailsJSON,
		})
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record resume activity")
		} else if s.activityRuns != nil {
			items := buildRunItemsFromHashes(resumedHashesSuccess, torrentByHash, s.syncManager)
			if len(items) > 0 {
				s.activityRuns.Put(activityID, instanceID, items)
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
				activityID, err := s.activityStore.CreateWithID(ctx, &models.AutomationActivity{
					InstanceID: instanceID,
					Hash:       "", // No single hash for batch operations
					Action:     models.ActivityActionTagsChanged,
					Outcome:    models.ActivityOutcomeSuccess,
					Details:    detailsJSON,
				})
				if err != nil {
					log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record tag activity")
				} else if s.activityRuns != nil {
					items := buildTagRunItems(tagChanges, torrentByHash, s.syncManager)
					if len(items) > 0 {
						s.activityRuns.Put(activityID, instanceID, items)
					}
				}
			}
		}
	}

	// Execute category changes - expand with cross-seeds where winning rule requested it
	// Sort keys for deterministic execution order
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
		activityID, err := s.activityStore.CreateWithID(ctx, &models.AutomationActivity{
			InstanceID: instanceID,
			Hash:       "", // No single hash for batch operations
			Action:     models.ActivityActionCategoryChanged,
			Outcome:    models.ActivityOutcomeSuccess,
			Details:    detailsJSON,
		})
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record category activity")
		} else if s.activityRuns != nil {
			items := buildCategoryRunItems(successfulMoves, torrentByHash, s.syncManager)
			if len(items) > 0 {
				s.activityRuns.Put(activityID, instanceID, items)
			}
		}
	}

	// Execute moves - sort paths for deterministic processing order
	sortedPaths := make([]string, 0, len(moveBatches))
	for path := range moveBatches {
		sortedPaths = append(sortedPaths, path)
	}
	sort.Strings(sortedPaths)

	movedHashes := make(map[string]struct{})
	successfulMovesByPath := make(map[string]int)
	failedMovesByPath := make(map[string]int)
	successfulMoveHashesByPath := make(map[string][]string)
	for _, path := range sortedPaths {
		hashes := moveBatches[path]
		successfulMovesForPath := 0
		failedMovesForPath := 0

		// Before moving, we need to get all of the cross-seeds for each torrent to avoid breaking cross-seeds
		var expandedHashes []string
		for _, hash := range hashes {
			if _, exists := movedHashes[hash]; exists {
				continue // Already moved
			}
			expandedHashes = append(expandedHashes, hash)
			movedHashes[hash] = struct{}{}
		}

		keysToExpand := make(map[crossSeedKey]struct{})
		for _, hash := range hashes {
			if t, exists := torrentByHash[hash]; exists {
				if key, ok := makeCrossSeedKey(t); ok {
					keysToExpand[key] = struct{}{}
				}
			}
		}

		if len(keysToExpand) > 0 {
			for _, t := range torrents {
				if normalizePath(t.SavePath) == normalizePath(path) {
					continue // Already in target path
				}
				if _, exists := movedHashes[t.Hash]; exists {
					continue // Already moved
				}
				if key, ok := makeCrossSeedKey(t); ok {
					if _, matched := keysToExpand[key]; matched {
						expandedHashes = append(expandedHashes, t.Hash)
						movedHashes[t.Hash] = struct{}{}
					}
				}
			}
		}

		limited := limitHashBatch(expandedHashes, s.cfg.MaxBatchHashes)
		for _, batch := range limited {
			if len(batch) == 0 {
				continue
			}

			if err := s.syncManager.SetLocation(ctx, instanceID, batch, path); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Str("path", path).Strs("hashes", batch).Msg("automations: move failed")
				failedMovesForPath += len(batch)
			} else {
				log.Debug().Int("instanceID", instanceID).Str("path", path).Strs("hashes", batch).Msg("automations: moved torrent")
				successfulMovesForPath += len(batch)
				successfulMoveHashesByPath[path] = append(successfulMoveHashesByPath[path], batch...)
			}
		}

		successfulMovesByPath[path] = successfulMovesForPath
		failedMovesByPath[path] = failedMovesForPath
	}

	// Record aggregated move activity
	if s.activityStore != nil {
		var hasSuccesses, hasFailures bool
		for _, count := range successfulMovesByPath {
			if count > 0 {
				hasSuccesses = true
				break
			}
		}
		for _, count := range failedMovesByPath {
			if count > 0 {
				hasFailures = true
				break
			}
		}

		if hasSuccesses {
			detailsJSON, _ := json.Marshal(map[string]any{"paths": successfulMovesByPath})
			activityID, err := s.activityStore.CreateWithID(ctx, &models.AutomationActivity{
				InstanceID: instanceID,
				Hash:       "",
				Action:     models.ActivityActionMoved,
				Outcome:    models.ActivityOutcomeSuccess,
				Details:    detailsJSON,
			})
			if err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record move activity")
			} else if s.activityRuns != nil {
				items := buildMoveRunItems(successfulMoveHashesByPath, torrentByHash, s.syncManager)
				if len(items) > 0 {
					s.activityRuns.Put(activityID, instanceID, items)
				}
			}
		}
		if hasFailures {
			detailsJSON, _ := json.Marshal(map[string]any{"paths": failedMovesByPath})
			if err := s.activityStore.Create(ctx, &models.AutomationActivity{
				InstanceID: instanceID,
				Hash:       "",
				Action:     models.ActivityActionMoved,
				Outcome:    models.ActivityOutcomeFailed,
				Details:    detailsJSON,
			}); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to record move activity")
			}
		}
	}

	// Execute external programs (async, fire-and-forget)
	s.executeExternalProgramsFromAutomation(ctx, instanceID, programExecutions)

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

					// Start FREE_SPACE cooldown if files were deleted by a FREE_SPACE rule
					// This allows qBittorrent time to refresh its disk free space reading
					if len(freeSpaceDeleteRuleIDs) > 0 {
						for _, hash := range batch {
							if pending, ok := pendingByHash[hash]; ok {
								if _, isFSRule := freeSpaceDeleteRuleIDs[pending.ruleID]; isFSRule {
									s.mu.Lock()
									s.lastFreeSpaceDeleteAt[instanceID] = now
									s.mu.Unlock()
									log.Debug().
										Int("instanceID", instanceID).
										Int("ruleID", pending.ruleID).
										Dur("cooldown", freeSpaceDeleteCooldown).
										Msg("automations: started FREE_SPACE delete cooldown")
									break // Only need to set once per batch
								}
							}
						}
					}
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

// isContentPathAmbiguous returns true if the ContentPath cannot reliably identify
// files unique to this torrent. This happens when ContentPath == SavePath, meaning
// the torrent uses the SavePath directly (common for shared download directories).
func isContentPathAmbiguous(t qbt.Torrent) bool {
	contentPath := normalizePath(t.ContentPath)
	savePath := normalizePath(t.SavePath)
	return contentPath == savePath
}

// findCrossSeedGroup returns all torrents (including the target) that share
// the same normalized ContentPath. Returns nil if ContentPath is empty.
func findCrossSeedGroup(target qbt.Torrent, allTorrents []qbt.Torrent) []qbt.Torrent {
	targetPath := normalizePath(target.ContentPath)
	if targetPath == "" {
		return nil
	}
	var group []qbt.Torrent
	for _, t := range allTorrents {
		if normalizePath(t.ContentPath) == targetPath {
			group = append(group, t)
		}
	}
	return group
}

// fileOverlapKey represents a unique file identity for overlap comparison.
// Uses lowercase normalized path + size to identify matching files.
type fileOverlapKey struct {
	name string // normalized lowercase path
	size int64
}

// minFileOverlapPercent is the minimum percentage of file overlap required
// to consider two torrents as sharing the same files when ContentPath is ambiguous.
// 90% tolerates small differences (extra NFO/sample/metadata files) while preventing
// accidental grouping of unrelated torrents that happen to share the same SavePath.
const minFileOverlapPercent = 90

// verifyFileOverlap checks if two torrents share at least minFileOverlapPercent of their files.
// Returns true if verification passes, false if not enough overlap or verification failed.
// This is used as a safety check when ContentPath matching is ambiguous.
func (s *Service) verifyFileOverlap(ctx context.Context, instanceID int, torrent1, torrent2 qbt.Torrent) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// Get files for both torrents
	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, []string{torrent1.Hash, torrent2.Hash})
	if err != nil {
		return false, fmt.Errorf("failed to fetch files: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}

	files1, ok1 := filesByHash[torrent1.Hash]
	files2, ok2 := filesByHash[torrent2.Hash]
	if !ok1 || !ok2 || len(files1) == 0 || len(files2) == 0 {
		return false, fmt.Errorf("missing file lists for torrents")
	}

	// Build set of file keys from first torrent and compute total bytes
	fileSet1 := make(map[fileOverlapKey]struct{}, len(files1))
	var totalBytes1 int64
	for _, f := range files1 {
		key := fileOverlapKey{
			name: strings.ToLower(normalizePath(f.Name)),
			size: f.Size,
		}
		fileSet1[key] = struct{}{}
		totalBytes1 += f.Size
	}

	// Compute total bytes for second torrent and sum matched bytes
	var totalBytes2, matchedBytes int64
	for _, f := range files2 {
		totalBytes2 += f.Size
		key := fileOverlapKey{
			name: strings.ToLower(normalizePath(f.Name)),
			size: f.Size,
		}
		if _, exists := fileSet1[key]; exists {
			matchedBytes += f.Size
		}
	}

	// Calculate overlap percentage based on bytes of the smaller torrent
	smallerBytes := totalBytes1
	if totalBytes2 < smallerBytes {
		smallerBytes = totalBytes2
	}
	if smallerBytes == 0 {
		return false, fmt.Errorf("cannot compute overlap: zero-size torrents")
	}

	overlapPercent := (matchedBytes * 100) / smallerBytes
	return overlapPercent >= minFileOverlapPercent, nil
}

// deleteFreesSpace returns true if deleting this torrent with the given mode
// will actually free disk space. This is used to determine whether to count
// the torrent's size toward cumulative free space projection.
//
// Returns false for:
//   - DeleteModeKeepFiles: files are retained on disk
//   - DeleteModeWithFilesPreserveCrossSeeds when cross-seeds exist: files are kept
//   - Unknown/invalid modes: don't count toward projection to avoid false early-stop
//
// Returns true for:
//   - DeleteModeWithFiles: files are always deleted
//   - DeleteModeWithFilesPreserveCrossSeeds when no cross-seeds exist: files will be deleted
//   - DeleteModeWithFilesIncludeCrossSeeds: always frees disk space (deletes entire group)
func deleteFreesSpace(mode string, torrent qbt.Torrent, allTorrents []qbt.Torrent) bool {
	switch mode {
	case DeleteModeKeepFiles, DeleteModeNone, "":
		// Keep-files mode never frees disk space
		return false
	case DeleteModeWithFilesPreserveCrossSeeds:
		// Only frees space if no cross-seeds share the files
		return !detectCrossSeeds(torrent, allTorrents)
	case DeleteModeWithFiles, DeleteModeWithFilesIncludeCrossSeeds:
		// Always frees disk space (include mode deletes the whole group)
		return true
	default:
		// Unknown mode - don't count toward projection to avoid false early-stop
		log.Warn().Str("mode", mode).Msg("automations: unknown delete mode, not counting toward free space projection")
		return false
	}
}

func ruleUsesCondition(rule *models.Automation, field ConditionField) bool {
	if rule == nil || rule.Conditions == nil || !rule.Enabled {
		return false
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
	if ac.Resume != nil && ConditionUsesField(ac.Resume.Condition, field) {
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
	if ac.Move != nil && ConditionUsesField(ac.Move.Condition, field) {
		return true
	}
	if ac.ExternalProgram != nil && ConditionUsesField(ac.ExternalProgram.Condition, field) {
		return true
	}
	return false
}

// rulesUseCondition checks if any enabled rule uses the given field.
func rulesUseCondition(rules []*models.Automation, field ConditionField) bool {
	for _, rule := range rules {
		if ruleUsesCondition(rule, field) {
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

// rulesUseIncludeHardlinks checks if any enabled delete rule has IncludeHardlinks enabled
// with the include-cross-seeds mode (the only mode that can actually expand hardlink groups).
func rulesUseIncludeHardlinks(rules []*models.Automation) bool {
	for _, rule := range rules {
		if rule.Conditions == nil || !rule.Enabled {
			continue
		}
		del := rule.Conditions.Delete
		// IncludeHardlinks only makes sense with the include-cross-seeds delete mode
		if del != nil && del.Enabled && del.IncludeHardlinks && del.Mode == DeleteModeWithFilesIncludeCrossSeeds {
			return true
		}
	}
	return false
}

// rulesNeedHardlinkSignatureMap checks if any rule uses FREE_SPACE + includeHardlinks
// with the include-cross-seeds delete mode. This determines if we need to build
// the hardlink signature map for accurate FREE_SPACE projection.
func rulesNeedHardlinkSignatureMap(rules []*models.Automation) bool {
	for _, rule := range rules {
		if rule.Conditions == nil || !rule.Enabled {
			continue
		}
		del := rule.Conditions.Delete
		if del == nil || !del.Enabled || !del.IncludeHardlinks {
			continue
		}
		// Only include-cross-seeds mode can actually delete hardlink groups
		if del.Mode != DeleteModeWithFilesIncludeCrossSeeds {
			continue
		}
		if ConditionUsesField(del.Condition, FieldFreeSpace) {
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
// Returns a map of limit (KiB) -> hashes of successfully updated torrents.
func (s *Service) applySpeedLimits(
	ctx context.Context,
	instanceID int,
	batches map[int64][]string,
	limitType string,
	setLimit func(ctx context.Context, instanceID int, hashes []string, limit int64) error,
) map[int64][]string {
	successHashes := make(map[int64][]string)
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
				successHashes[limit] = append(successHashes[limit], batch...)
			}
		}
	}
	return successHashes
}

func (s *Service) recordDryRunActivities(
	ctx context.Context,
	instanceID int,
	uploadBatches map[int64][]string,
	downloadBatches map[int64][]string,
	shareBatches map[shareKey][]string,
	pauseHashes []string,
	resumeHashes []string,
	tagChanges map[string]*tagChange,
	categoryBatches map[string][]string,
	moveBatches map[string][]string,
	pendingByHash map[string]pendingDeletion,
	programExecutions []pendingProgramExec,
	torrentByHash map[string]qbt.Torrent,
	torrents []qbt.Torrent,
	states map[string]*torrentDesiredState,
) {
	if s.activityStore == nil {
		return
	}

	createActivity := func(action string, details map[string]any, buildItems func() []ActivityRunTorrent) {
		detailsJSON, _ := json.Marshal(details)
		activityID, err := s.activityStore.CreateWithID(ctx, &models.AutomationActivity{
			InstanceID: instanceID,
			Hash:       "",
			Action:     action,
			Outcome:    models.ActivityOutcomeDryRun,
			Details:    detailsJSON,
		})
		if err != nil || s.activityRuns == nil || buildItems == nil {
			return
		}
		items := buildItems()
		if len(items) > 0 {
			s.activityRuns.Put(activityID, instanceID, items)
		}
	}

	// Speed limits
	if len(uploadBatches) > 0 || len(downloadBatches) > 0 {
		limitCounts := make(map[string]int)
		for limit, hashes := range uploadBatches {
			limitCounts[fmt.Sprintf("upload:%d", limit)] = len(dedupeHashes(hashes))
		}
		for limit, hashes := range downloadBatches {
			limitCounts[fmt.Sprintf("download:%d", limit)] = len(dedupeHashes(hashes))
		}
		createActivity(models.ActivityActionSpeedLimitsChanged, map[string]any{"limits": limitCounts}, func() []ActivityRunTorrent {
			return buildSpeedLimitRunItems(uploadBatches, downloadBatches, torrentByHash, s.syncManager)
		})
	}

	// Share limits
	if len(shareBatches) > 0 {
		limitCounts := make(map[string]int)
		for key, hashes := range shareBatches {
			limitKey := fmt.Sprintf("%.2f:%d:%d", key.ratio, key.seed, key.inactive)
			limitCounts[limitKey] = len(dedupeHashes(hashes))
		}
		createActivity(models.ActivityActionShareLimitsChanged, map[string]any{"limits": limitCounts}, func() []ActivityRunTorrent {
			return buildShareLimitRunItems(shareBatches, torrentByHash, s.syncManager)
		})
	}

	for _, a := range []struct {
		action string
		hashes []string
	}{
		{action: models.ActivityActionPaused, hashes: pauseHashes},
		{action: models.ActivityActionResumed, hashes: resumeHashes},
	} {
		if len(a.hashes) == 0 {
			continue
		}
		uniqueHashes := dedupeHashes(a.hashes)
		createActivity(a.action, map[string]any{"count": len(uniqueHashes)}, func() []ActivityRunTorrent {
			return buildRunItemsFromHashes(uniqueHashes, torrentByHash, s.syncManager)
		})
	}

	// Tags
	if len(tagChanges) > 0 {
		addCounts := make(map[string]int)
		removeCounts := make(map[string]int)
		for _, change := range tagChanges {
			for _, tag := range change.toAdd {
				addCounts[tag]++
			}
			for _, tag := range change.toRemove {
				removeCounts[tag]++
			}
		}
		if len(addCounts) > 0 || len(removeCounts) > 0 {
			createActivity(models.ActivityActionTagsChanged, map[string]any{
				"added":   addCounts,
				"removed": removeCounts,
			}, func() []ActivityRunTorrent {
				return buildTagRunItems(tagChanges, torrentByHash, s.syncManager)
			})
		}
	}

	// Categories (include cross-seed expansion)
	if len(categoryBatches) > 0 && len(states) > 0 {
		var plannedMoves []categoryMove
		sortedCategories := make([]string, 0, len(categoryBatches))
		for cat := range categoryBatches {
			sortedCategories = append(sortedCategories, cat)
		}
		sort.Strings(sortedCategories)

		for _, category := range sortedCategories {
			hashes := categoryBatches[category]
			expandedHashes := hashes

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
						continue
					}
					if _, exists := expandedSet[t.Hash]; exists {
						continue
					}
					if state, hasState := states[t.Hash]; hasState && state.category != nil {
						if *state.category != category {
							continue
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

			for _, hash := range expandedHashes {
				move := categoryMove{hash: hash, category: category}
				if t, exists := torrentByHash[hash]; exists {
					move.name = t.Name
					if domains := collectTrackerDomains(t, s.syncManager); len(domains) > 0 {
						move.trackerDomain = domains[0]
					}
				}
				plannedMoves = append(plannedMoves, move)
			}
		}

		if len(plannedMoves) > 0 {
			categoryCounts := make(map[string]int)
			for _, move := range plannedMoves {
				categoryCounts[move.category]++
			}
			createActivity(models.ActivityActionCategoryChanged, map[string]any{"categories": categoryCounts}, func() []ActivityRunTorrent {
				return buildCategoryRunItems(plannedMoves, torrentByHash, s.syncManager)
			})
		}
	}

	// Moves (include cross-seed expansion)
	if len(moveBatches) > 0 {
		sortedPaths := make([]string, 0, len(moveBatches))
		for path := range moveBatches {
			sortedPaths = append(sortedPaths, path)
		}
		sort.Strings(sortedPaths)

		movedHashes := make(map[string]struct{})
		plannedCounts := make(map[string]int)
		plannedHashesByPath := make(map[string][]string)

		for _, path := range sortedPaths {
			hashes := moveBatches[path]
			var expandedHashes []string
			for _, hash := range hashes {
				if _, exists := movedHashes[hash]; exists {
					continue
				}
				expandedHashes = append(expandedHashes, hash)
				movedHashes[hash] = struct{}{}
			}

			keysToExpand := make(map[crossSeedKey]struct{})
			for _, hash := range hashes {
				if t, exists := torrentByHash[hash]; exists {
					if key, ok := makeCrossSeedKey(t); ok {
						keysToExpand[key] = struct{}{}
					}
				}
			}

			if len(keysToExpand) > 0 {
				for _, t := range torrents {
					if normalizePath(t.SavePath) == normalizePath(path) {
						continue
					}
					if _, exists := movedHashes[t.Hash]; exists {
						continue
					}
					if key, ok := makeCrossSeedKey(t); ok {
						if _, matched := keysToExpand[key]; matched {
							expandedHashes = append(expandedHashes, t.Hash)
							movedHashes[t.Hash] = struct{}{}
						}
					}
				}
			}

			expandedHashes = dedupeHashes(expandedHashes)
			if len(expandedHashes) > 0 {
				plannedHashesByPath[path] = expandedHashes
				plannedCounts[path] = len(expandedHashes)
			}
		}

		if len(plannedHashesByPath) > 0 {
			createActivity(models.ActivityActionMoved, map[string]any{"paths": plannedCounts}, func() []ActivityRunTorrent {
				return buildMoveRunItems(plannedHashesByPath, torrentByHash, s.syncManager)
			})
		}
	}

	// External programs
	if len(programExecutions) > 0 {
		hashesByProgram := make(map[int][]string)
		for _, exec := range programExecutions {
			hashesByProgram[exec.programID] = append(hashesByProgram[exec.programID], exec.hash)
		}
		for programID, hashes := range hashesByProgram {
			uniqueHashes := dedupeHashes(hashes)
			createActivity(externalprograms.ActivityActionExternalProgram, map[string]any{"programId": programID, "count": len(uniqueHashes)}, func() []ActivityRunTorrent {
				return buildRunItemsFromHashes(uniqueHashes, torrentByHash, s.syncManager)
			})
		}
	}

	// Deletes
	if len(pendingByHash) > 0 {
		hashesByAction := make(map[string][]string)
		for hash, pending := range pendingByHash {
			hashesByAction[pending.action] = append(hashesByAction[pending.action], hash)
		}
		for action, hashes := range hashesByAction {
			uniqueHashes := dedupeHashes(hashes)
			createActivity(action, map[string]any{"count": len(uniqueHashes)}, func() []ActivityRunTorrent {
				return buildRunItemsFromHashes(uniqueHashes, torrentByHash, s.syncManager)
			})
		}
	}
}

func buildRunItemFromHash(hash string, torrentByHash map[string]qbt.Torrent, sm *qbittorrent.SyncManager) ActivityRunTorrent {
	item := ActivityRunTorrent{Hash: hash}
	if t, ok := torrentByHash[hash]; ok {
		item.Name = t.Name
		size := t.Size
		ratio := t.Ratio
		addedOn := t.AddedOn
		item.Size = &size
		item.Ratio = &ratio
		item.AddedOn = &addedOn
		if sm != nil {
			if domains := collectTrackerDomains(t, sm); len(domains) > 0 {
				item.TrackerDomain = domains[0]
			}
		}
	}
	return item
}

func buildRunItemsFromHashes(hashes []string, torrentByHash map[string]qbt.Torrent, sm *qbittorrent.SyncManager) []ActivityRunTorrent {
	seen := make(map[string]struct{})
	items := make([]ActivityRunTorrent, 0, len(hashes))
	for _, hash := range hashes {
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		items = append(items, buildRunItemFromHash(hash, torrentByHash, sm))
	}
	sortActivityRunItems(items)
	return items
}

func buildTagRunItems(tagChanges map[string]*tagChange, torrentByHash map[string]qbt.Torrent, sm *qbittorrent.SyncManager) []ActivityRunTorrent {
	items := make([]ActivityRunTorrent, 0, len(tagChanges))
	for hash, change := range tagChanges {
		if len(change.toAdd) == 0 && len(change.toRemove) == 0 {
			continue
		}
		item := buildRunItemFromHash(hash, torrentByHash, sm)
		item.TagsAdded = slices.Clone(change.toAdd)
		item.TagsRemoved = slices.Clone(change.toRemove)
		slices.Sort(item.TagsAdded)
		slices.Sort(item.TagsRemoved)
		items = append(items, item)
	}
	sortActivityRunItems(items)
	return items
}

func buildCategoryRunItems(moves []categoryMove, torrentByHash map[string]qbt.Torrent, sm *qbittorrent.SyncManager) []ActivityRunTorrent {
	items := make([]ActivityRunTorrent, 0, len(moves))
	for _, move := range moves {
		item := buildRunItemFromHash(move.hash, torrentByHash, sm)
		if item.Name == "" {
			item.Name = move.name
		}
		if item.TrackerDomain == "" {
			item.TrackerDomain = move.trackerDomain
		}
		item.Category = move.category
		items = append(items, item)
	}
	sortActivityRunItems(items)
	return items
}

func buildSpeedLimitRunItems(
	uploadSuccess map[int64][]string,
	downloadSuccess map[int64][]string,
	torrentByHash map[string]qbt.Torrent,
	sm *qbittorrent.SyncManager,
) []ActivityRunTorrent {
	itemMap := make(map[string]*ActivityRunTorrent)

	getItem := func(hash string) *ActivityRunTorrent {
		if item, ok := itemMap[hash]; ok {
			return item
		}
		item := buildRunItemFromHash(hash, torrentByHash, sm)
		itemMap[hash] = &item
		return &item
	}

	for limit, hashes := range uploadSuccess {
		for _, hash := range hashes {
			item := getItem(hash)
			limitValue := limit
			item.UploadLimitKiB = &limitValue
		}
	}

	for limit, hashes := range downloadSuccess {
		for _, hash := range hashes {
			item := getItem(hash)
			limitValue := limit
			item.DownloadLimitKiB = &limitValue
		}
	}

	items := make([]ActivityRunTorrent, 0, len(itemMap))
	for _, item := range itemMap {
		items = append(items, *item)
	}
	sortActivityRunItems(items)
	return items
}

func buildShareLimitRunItems(
	shareLimitSuccess map[shareKey][]string,
	torrentByHash map[string]qbt.Torrent,
	sm *qbittorrent.SyncManager,
) []ActivityRunTorrent {
	itemMap := make(map[string]*ActivityRunTorrent)

	getItem := func(hash string) *ActivityRunTorrent {
		if item, ok := itemMap[hash]; ok {
			return item
		}
		item := buildRunItemFromHash(hash, torrentByHash, sm)
		itemMap[hash] = &item
		return &item
	}

	for key, hashes := range shareLimitSuccess {
		for _, hash := range hashes {
			item := getItem(hash)
			ratioValue := key.ratio
			seedValue := key.seed
			item.RatioLimit = &ratioValue
			item.SeedingMinutes = &seedValue
		}
	}

	items := make([]ActivityRunTorrent, 0, len(itemMap))
	for _, item := range itemMap {
		items = append(items, *item)
	}
	sortActivityRunItems(items)
	return items
}

func buildMoveRunItems(
	successfulMoveHashesByPath map[string][]string,
	torrentByHash map[string]qbt.Torrent,
	sm *qbittorrent.SyncManager,
) []ActivityRunTorrent {
	itemMap := make(map[string]*ActivityRunTorrent)

	getItem := func(hash string) *ActivityRunTorrent {
		if item, ok := itemMap[hash]; ok {
			return item
		}
		item := buildRunItemFromHash(hash, torrentByHash, sm)
		itemMap[hash] = &item
		return &item
	}

	for path, hashes := range successfulMoveHashesByPath {
		for _, hash := range hashes {
			item := getItem(hash)
			item.MovePath = path
		}
	}

	items := make([]ActivityRunTorrent, 0, len(itemMap))
	for _, item := range itemMap {
		items = append(items, *item)
	}
	sortActivityRunItems(items)
	return items
}

func sortActivityRunItems(items []ActivityRunTorrent) {
	sort.Slice(items, func(i, j int) bool {
		nameA := strings.ToLower(items[i].Name)
		nameB := strings.ToLower(items[j].Name)
		if nameA == "" && nameB != "" {
			return false
		}
		if nameA != "" && nameB == "" {
			return true
		}
		if nameA != nameB {
			return nameA < nameB
		}
		return items[i].Hash < items[j].Hash
	})
}

func dedupeHashes(hashes []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		result = append(result, hash)
	}
	return result
}

// pendingProgramExec tracks a pending external program execution
type pendingProgramExec struct {
	hash      string
	torrent   qbt.Torrent
	programID int
	ruleID    int
	ruleName  string
}

// executeExternalProgramsFromAutomation executes external programs for matching torrents.
// Programs are executed asynchronously (fire-and-forget) to avoid blocking the automation run.
//
// WARNING: No rate limiting or process count limits are applied. If many torrents match a rule
// with an external program action, many processes will be spawned concurrently. Long-running
// or stuck programs can exhaust system resources.
func (s *Service) executeExternalProgramsFromAutomation(_ context.Context, instanceID int, executions []pendingProgramExec) {
	if len(executions) == 0 {
		return
	}

	if s.externalProgramService == nil {
		log.Error().
			Int("instanceID", instanceID).
			Int("pendingExecutions", len(executions)).
			Msg("external program service not initialized, skipping executions")

		// Log activity entries so users can see what happened
		if s.activityStore != nil {
			for _, exec := range executions {
				ruleID := exec.ruleID
				if err := s.activityStore.Create(context.Background(), &models.AutomationActivity{
					InstanceID:  instanceID,
					Hash:        exec.hash,
					TorrentName: exec.torrent.Name,
					Action:      externalprograms.ActivityActionExternalProgram,
					RuleID:      &ruleID,
					RuleName:    exec.ruleName,
					Outcome:     models.ActivityOutcomeFailed,
					Reason:      "External program service not configured",
				}); err != nil {
					log.Warn().Err(err).Str("hash", exec.hash).Msg("failed to log external program activity")
				}
			}
		}
		return
	}

	// Group by program ID to log summary
	programCounts := make(map[int]int)
	for _, exec := range executions {
		programCounts[exec.programID]++
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("executions", len(executions)).
		Interface("programCounts", programCounts).
		Msg("automations: executing external programs")

	for _, exec := range executions {
		// Copy to avoid closure issues
		torrent := exec.torrent
		ruleID := exec.ruleID
		programID := exec.programID
		ruleName := exec.ruleName

		// Execute asynchronously - the service handles its own activity logging
		// Use context.Background() since parent context may be cancelled before execution completes
		go func() {
			result := s.externalProgramService.Execute(context.Background(), externalprograms.ExecuteRequest{
				ProgramID:  programID,
				Torrent:    &torrent,
				InstanceID: instanceID,
				RuleID:     &ruleID,
				RuleName:   ruleName,
			})
			if !result.Success {
				log.Error().
					Err(result.Error).
					Int("programID", programID).
					Str("ruleName", ruleName).
					Str("torrentHash", torrent.Hash).
					Msg("automation: external program execution failed")
			}
		}()
	}
}
