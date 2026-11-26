// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/externalprograms"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/pkg/releases"
)

type Config struct {
	ScanInterval          time.Duration
	SkipWithin            time.Duration
	MaxBatchHashes        int
	ActivityRetentionDays int
	ActivityRunRetention  time.Duration
	ActivityRunMax        int
	ApplyTimeout          time.Duration
}

const DefaultRuleInterval = 15 * time.Minute

const freeSpaceDeleteCooldown = 5 * time.Minute

const logMsgRemoveTorrentWithFiles = "automations: removing torrent with files"

var automationActionLabels = map[string]string{
	models.ActivityActionDeletedRatio:        "Deleted torrent (ratio rule)",
	models.ActivityActionDeletedSeeding:      "Deleted torrent (seeding rule)",
	models.ActivityActionDeletedUnregistered: "Deleted torrent (unregistered)",
	models.ActivityActionDeletedCondition:    "Deleted torrent (rule)",
	models.ActivityActionDeleteFailed:        "Delete failed",
	models.ActivityActionLimitFailed:         "Speed/share limit failed",
	models.ActivityActionTagsChanged:         "Tags updated",
	models.ActivityActionCategoryChanged:     "Category updated",
	models.ActivityActionSpeedLimitsChanged:  "Speed limits updated",
	models.ActivityActionShareLimitsChanged:  "Share limits updated",
	models.ActivityActionPaused:              "Paused torrents",
	models.ActivityActionResumed:             "Resumed torrents",
	models.ActivityActionRechecked:           "Rechecked torrents",
	models.ActivityActionReannounced:         "Reannounced torrents",
	models.ActivityActionMoved:               "Moved torrents",
	models.ActivityActionExportedToInstance:  "Export to instance",
	models.ActivityActionDryRunNoMatch:       "Dry-run: no matches",

	models.ActivityActionTransferredToInstance: "Transfer to instance",
}

type automationSummary struct {
	applied          int
	failed           int
	appliedByAction  map[string]int
	failedByAction   map[string]int
	rules            map[string]*automationRuleSummary
	tagAddedByName   map[string]int
	tagRemovedByName map[string]int
	tagSamples       []string
	sampleTorrents   []string
	sampleErrors     []string
	sampleSeen       map[string]struct{}
	tagSampleSeen    map[string]struct{}
	activities       []*models.AutomationActivity
}

type automationRuleSummary struct {
	ruleID   int
	ruleName string
	applied  int
	failed   int
	actions  map[string]*automationActionCounts
}

type automationActionCounts struct {
	applied int
	failed  int
}

func newAutomationSummary() *automationSummary {
	return &automationSummary{
		appliedByAction:  make(map[string]int),
		failedByAction:   make(map[string]int),
		rules:            make(map[string]*automationRuleSummary),
		tagAddedByName:   make(map[string]int),
		tagRemovedByName: make(map[string]int),
		sampleSeen:       make(map[string]struct{}),
		tagSampleSeen:    make(map[string]struct{}),
	}
}

func (s *automationSummary) add(action, outcome string, count int) {
	if s == nil || count <= 0 {
		return
	}
	switch outcome {
	case models.ActivityOutcomeSuccess:
		s.applied += count
		s.appliedByAction[action] += count
	case models.ActivityOutcomeFailed:
		s.failed += count
		s.failedByAction[action] += count
	}
}

func (s *automationSummary) hasActivity() bool {
	if s == nil {
		return false
	}
	return s.applied > 0 || s.failed > 0
}

func (s *automationSummary) message() string {
	if s == nil {
		return ""
	}
	lines := []string{fmt.Sprintf("Applied: %d", s.applied)}
	if s.failed > 0 {
		lines = append(lines, fmt.Sprintf("Failed: %d", s.failed))
	}
	if formatted := formatActionCounts(s.appliedByAction, 3); formatted != "" {
		lines = append(lines, "Top actions: "+formatted)
	}
	if formatted := formatActionCounts(s.failedByAction, 3); formatted != "" {
		lines = append(lines, "Top failures: "+formatted)
	}
	if formatted := formatRuleCounts(s.ruleTotalsByName(), 3); formatted != "" {
		lines = append(lines, "Rules: "+formatted)
	}
	if formatted := formatTagCounts(s.tagAddedByName, s.tagRemovedByName, 3); formatted != "" {
		lines = append(lines, "Tags: "+formatted)
	}
	if len(s.tagSamples) > 0 {
		lines = append(lines, "Tag samples: "+strings.Join(s.tagSamples, "; "))
	}
	if len(s.sampleTorrents) > 0 {
		lines = append(lines, "Samples: "+strings.Join(s.sampleTorrents, "; "))
	}
	if len(s.sampleErrors) > 0 {
		lines = append(lines, "Errors: "+strings.Join(s.sampleErrors, "; "))
	}
	return strings.Join(lines, "\n")
}

func (s *automationSummary) recordActivity(activity *models.AutomationActivity, count int) {
	if s == nil || activity == nil {
		return
	}
	if count <= 0 {
		count = 1
	}
	if activity.CreatedAt.IsZero() {
		activity.CreatedAt = time.Now().UTC()
	}
	s.activities = append(s.activities, activity)
	s.add(activity.Action, activity.Outcome, count)
	s.addRuleAction(activity, count)
	s.addSamplesFromActivity(activity)
}

func (s *automationSummary) addSamplesFromActivity(activity *models.AutomationActivity) {
	if s == nil || activity == nil {
		return
	}
	if activity.TorrentName != "" {
		s.addSample(&s.sampleTorrents, activity.TorrentName, 3)
	}
	if activity.Outcome == models.ActivityOutcomeFailed && strings.TrimSpace(activity.Reason) != "" {
		s.addSample(&s.sampleErrors, activity.Reason, 2)
	}
}

func (s *automationSummary) addTorrentSamples(names []string, limit int) {
	if s == nil || limit <= 0 {
		return
	}
	for _, name := range names {
		s.addSample(&s.sampleTorrents, name, limit)
	}
}

func (s *automationSummary) addRuleAction(activity *models.AutomationActivity, count int) {
	if s == nil || activity == nil || count <= 0 {
		return
	}

	ruleName := normalizeRuleName(activity.RuleID, activity.RuleName)
	ruleID := 0
	if activity.RuleID != nil {
		ruleID = *activity.RuleID
	}
	if ruleID <= 0 && ruleName == "" {
		return
	}

	key := ruleName
	if ruleID > 0 {
		key = fmt.Sprintf("%d:%s", ruleID, ruleName)
	}

	rule, ok := s.rules[key]
	if !ok {
		rule = &automationRuleSummary{
			ruleID:   ruleID,
			ruleName: ruleName,
			actions:  make(map[string]*automationActionCounts),
		}
		s.rules[key] = rule
	}

	action := strings.TrimSpace(activity.Action)
	if action == "" {
		action = "unknown"
	}

	counts, ok := rule.actions[action]
	if !ok {
		counts = &automationActionCounts{}
		rule.actions[action] = counts
	}

	switch activity.Outcome {
	case models.ActivityOutcomeSuccess:
		rule.applied += count
		counts.applied += count
	case models.ActivityOutcomeFailed:
		rule.failed += count
		counts.failed += count
	}
}

func (s *automationSummary) ruleTotalsByName() map[string]int {
	if s == nil || len(s.rules) == 0 {
		return nil
	}

	out := make(map[string]int, len(s.rules))
	for _, rule := range s.rules {
		if rule == nil {
			continue
		}
		name := normalizeRuleName(intPtrForRule(rule.ruleID), rule.ruleName)
		if name == "" {
			continue
		}
		out[name] += rule.applied + rule.failed
	}
	return out
}

func (s *automationSummary) recordRuleCounts(action string, outcome string, counts map[ruleRef]int) {
	if s == nil || len(counts) == 0 {
		return
	}
	for ref, count := range counts {
		if count <= 0 {
			continue
		}
		refName := strings.TrimSpace(ref.name)
		refID := ref.id
		var ruleID *int
		if refID > 0 {
			ruleID = &refID
		}
		s.addRuleAction(&models.AutomationActivity{
			Action:   action,
			Outcome:  outcome,
			RuleID:   ruleID,
			RuleName: refName,
		}, count)
	}
}

func (s *automationSummary) addTagCounts(added map[string]int, removed map[string]int) {
	if s == nil {
		return
	}
	for tag, count := range added {
		trimmedTag := strings.TrimSpace(tag)
		if trimmedTag == "" || count <= 0 {
			continue
		}
		s.tagAddedByName[trimmedTag] += count
	}
	for tag, count := range removed {
		trimmedTag := strings.TrimSpace(tag)
		if trimmedTag == "" || count <= 0 {
			continue
		}
		s.tagRemovedByName[trimmedTag] += count
	}
}

func (s *automationSummary) addTagSamples(names []string, limit int) {
	if s == nil || limit <= 0 {
		return
	}
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, seen := s.tagSampleSeen[trimmed]; seen {
			continue
		}
		if len(s.tagSamples) >= limit {
			return
		}
		s.tagSampleSeen[trimmed] = struct{}{}
		s.tagSamples = append(s.tagSamples, trimmed)
	}
}

func (s *automationSummary) addSample(list *[]string, value string, limit int) {
	if s == nil || list == nil || limit <= 0 {
		return
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	if _, exists := s.sampleSeen[trimmed]; exists {
		return
	}
	if len(*list) >= limit {
		return
	}
	s.sampleSeen[trimmed] = struct{}{}
	*list = append(*list, trimmed)
}

type countItem struct {
	key   string
	count int
}

func sortedCountItems(counts map[string]int) []countItem {
	if len(counts) == 0 {
		return nil
	}

	items := make([]countItem, 0, len(counts))
	for key, count := range counts {
		items = append(items, countItem{key: key, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].key < items[j].key
		}
		return items[i].count > items[j].count
	})
	return items
}

func formatActionCounts(counts map[string]int, limit int) string {
	items := sortedCountItems(counts)
	if len(items) == 0 {
		return ""
	}

	maxItems := limit
	if maxItems <= 0 || maxItems > len(items) {
		maxItems = len(items)
	}

	parts := make([]string, 0, maxItems)
	for i := 0; i < maxItems; i++ {
		label := automationActionLabel(items[i].key)
		parts = append(parts, fmt.Sprintf("%s=%d", label, items[i].count))
	}
	return strings.Join(parts, "; ")
}

func formatRuleCounts(counts map[string]int, limit int) string {
	items := sortedCountItems(counts)
	if len(items) == 0 {
		return ""
	}

	maxItems := limit
	if maxItems <= 0 || maxItems > len(items) {
		maxItems = len(items)
	}

	parts := make([]string, 0, maxItems)
	for i := 0; i < maxItems; i++ {
		parts = append(parts, items[i].key)
	}
	return strings.Join(parts, "; ")
}

func formatTagCounts(added map[string]int, removed map[string]int, limit int) string {
	parts := make([]string, 0, limit*2)

	for _, item := range sortedCountItems(added) {
		if len(parts) >= limit {
			break
		}
		parts = append(parts, fmt.Sprintf("+%s=%d", item.key, item.count))
	}

	for _, item := range sortedCountItems(removed) {
		if len(parts) >= limit*2 {
			break
		}
		parts = append(parts, fmt.Sprintf("-%s=%d", item.key, item.count))
	}

	return strings.Join(parts, "; ")
}

func automationActionLabel(action string) string {
	if label, ok := automationActionLabels[action]; ok {
		return label
	}
	return strings.ReplaceAll(action, "_", " ")
}

func normalizeRuleName(ruleID *int, ruleName string) string {
	name := strings.TrimSpace(ruleName)
	if name != "" {
		return name
	}
	if ruleID != nil && *ruleID > 0 {
		return fmt.Sprintf("Rule #%d", *ruleID)
	}
	return ""
}

func intPtrForRule(ruleID int) *int {
	if ruleID <= 0 {
		return nil
	}
	id := ruleID
	return &id
}

type ruleKey struct {
	instanceID int
	ruleID     int
}

type shareKey struct {
	ratio    float64
	seed     int64
	inactive int64
	action   string
	mode     string
}

func normalizeShareLimitEnum(value string) string {
	v := strings.TrimSpace(value)
	if v == "" || strings.EqualFold(v, "Default") {
		return ""
	}
	return v
}

func shareLimitRuleRef(primary, fallback ruleRef) ruleRef {
	if primary.id != 0 {
		return primary
	}
	return fallback
}

type tagChange struct {
	current   map[string]struct{}
	desired   map[string]struct{}
	toAdd     []string
	toRemove  []string
	ruleByTag map[string]ruleRef
}

func (s *Service) applyTagChanges(ctx context.Context, instanceID int, tagChanges map[string]*tagChange) map[string]*tagChange {
	client, err := s.syncManager.GetClient(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: get client for tags failed")
		return nil
	}

	if client.Supports(qbittorrent.CapSetTags) {
		return s.setTagsByTagSet(ctx, instanceID, tagChanges)
	}

	log.Debug().Int("instanceID", instanceID).Msg("automations: falling back to add/remove tags (older qBittorrent)")
	return s.editTagsByTag(ctx, instanceID, tagChanges)
}

func (s *Service) setTagsByTagSet(ctx context.Context, instanceID int, tagChanges map[string]*tagChange) map[string]*tagChange {
	setTagsBatches := make(map[string][]string)
	for hash, change := range tagChanges {
		key := strings.Join(slices.Sorted(maps.Keys(change.desired)), ",")
		setTagsBatches[key] = append(setTagsBatches[key], hash)
	}

	accepted := make(map[string]*tagChange, len(tagChanges))
	for tagSet, hashes := range setTagsBatches {
		var tags []string
		if tagSet != "" {
			tags = strings.Split(tagSet, ",")
		}
		for _, batch := range limitHashBatch(hashes, s.cfg.MaxBatchHashes) {
			if err := s.syncManager.SetTags(ctx, instanceID, batch, tagSet); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Strs("tags", tags).Int("count", len(batch)).Msg("automations: set tags failed")
				continue
			}
			log.Debug().Int("instanceID", instanceID).Strs("tags", tags).Int("count", len(batch)).Msg("automations: set tags on torrents")
			for _, hash := range batch {
				accepted[hash] = tagChanges[hash]
			}
		}
	}
	return accepted
}

func (s *Service) editTagsByTag(ctx context.Context, instanceID int, tagChanges map[string]*tagChange) map[string]*tagChange {
	addBatches := make(map[string][]string)
	removeBatches := make(map[string][]string)
	for hash, change := range tagChanges {
		for _, tag := range change.toAdd {
			addBatches[tag] = append(addBatches[tag], hash)
		}
		for _, tag := range change.toRemove {
			removeBatches[tag] = append(removeBatches[tag], hash)
		}
	}

	refused := make(map[string]struct{})
	for tag, hashes := range addBatches {
		for _, batch := range limitHashBatch(hashes, s.cfg.MaxBatchHashes) {
			if err := s.syncManager.AddTags(ctx, instanceID, batch, tag); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Str("tag", tag).Int("count", len(batch)).Msg("automations: add tags failed")
				for _, hash := range batch {
					refused[hash] = struct{}{}
				}
			} else {
				log.Debug().Int("instanceID", instanceID).Str("tag", tag).Int("count", len(batch)).Msg("automations: added tag to torrents")
			}
		}
	}

	for tag, hashes := range removeBatches {
		for _, batch := range limitHashBatch(hashes, s.cfg.MaxBatchHashes) {
			if err := s.syncManager.RemoveTags(ctx, instanceID, batch, tag); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Str("tag", tag).Int("count", len(batch)).Msg("automations: remove tags failed")
				for _, hash := range batch {
					refused[hash] = struct{}{}
				}
			} else {
				log.Debug().Int("instanceID", instanceID).Str("tag", tag).Int("count", len(batch)).Msg("automations: removed tag from torrents")
			}
		}
	}

	accepted := maps.Clone(tagChanges)
	for hash := range refused {
		delete(accepted, hash)
	}
	return accepted
}

func DefaultConfig() Config {
	return Config{
		ScanInterval:          20 * time.Second,
		SkipWithin:            2 * time.Minute,
		MaxBatchHashes:        50,
		ActivityRetentionDays: 7,
		ActivityRunRetention:  24 * time.Hour,
		ActivityRunMax:        500,
		ApplyTimeout:          60 * time.Second,
	}
}

type CrossMatchNeeds = crossseed.CrossMatchNeeds

type CrossMatchResult = crossseed.CrossMatchResult

type CrossMatchSession = crossseed.CrossMatchSession

type ResolveRequest = crossseed.ResolveRequest

type filesReader interface {
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error)
	GetTorrentRefs(ctx context.Context, instanceID int, filter qbittorrent.TorrentRefFilter) ([]*qbt.Torrent, error)
}

type torrentDeleter interface {
	DeleteTorrents(ctx context.Context, instanceID int, hashes []string, deleteFiles bool, actor qbittorrent.DeleteActor) error
}

type CrossMatcher interface {
	OpenCrossMatchSession(ctx context.Context, currentInstanceID int, needs CrossMatchNeeds) (CrossMatchSession, error)
}

type Service struct {
	cfg                       Config
	instanceStore             *models.InstanceStore
	ruleStore                 *models.AutomationStore
	activityStore             *models.AutomationActivityStore
	trackerCustomizationStore *models.TrackerCustomizationStore
	syncManager               *qbittorrent.SyncManager
	filesReader               filesReader
	targetLookup              targetLookup
	deleter                   torrentDeleter
	notifier                  notifications.Notifier
	externalProgramService    *externalprograms.Service
	crossMatcher              CrossMatcher
	backendPool               *fsops.Pool
	previewUsageMu            sync.Mutex
	previewUsage              map[previewUsageKey]previewUsageReading
	activityRuns              *activityRunStore
	releaseParser             *releases.Parser

	lastApplied           map[int]map[string]time.Time
	lastRuleRun           map[ruleKey]time.Time
	reportedSkips         map[reportedSkipKey]RuleSkip
	lastFreeSpaceDeleteAt map[int]time.Time
	inFlightExports       map[string]struct{}
	runningInstances      map[int]struct{}
	mu                    sync.RWMutex

	activityPublisher activity.Publisher
	crossSeedRoutes   *crossseedroute.Service
	routedTorrents    crossseedroute.TorrentReader
	claimed           claimedTorrents
	exportRunner      exportRunner
	transferRunner    transferRunner
	transferHolds     map[string]time.Time
	clock             func() time.Time
}

func NewService(cfg Config, instanceStore *models.InstanceStore, ruleStore *models.AutomationStore, activityStore *models.AutomationActivityStore, trackerCustomizationStore *models.TrackerCustomizationStore, syncManager *qbittorrent.SyncManager, notifier notifications.Notifier, externalProgramService *externalprograms.Service, crossMatcher CrossMatcher, backendPool *fsops.Pool) *Service {
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
		filesReader:               syncManager,
		targetLookup:              syncManager,
		deleter:                   syncManager,
		routedTorrents:            routedFileOwners{syncManager},
		notifier:                  notifier,
		externalProgramService:    externalProgramService,
		crossMatcher:              crossMatcher,
		backendPool:               backendPool,
		activityRuns:              newActivityRunStore(cfg.ActivityRunRetention, cfg.ActivityRunMax),
		releaseParser:             releases.NewDefaultParser(),
		lastRuleRun:               make(map[ruleKey]time.Time),
		reportedSkips:             make(map[reportedSkipKey]RuleSkip),
		lastFreeSpaceDeleteAt:     make(map[int]time.Time),
		inFlightExports:           make(map[string]struct{}),
		activityPublisher:         activity.NopPublisher{},
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

func (s *Service) cleanupStaleEntries() {
	cutoff := time.Now().Add(-10 * time.Minute)
	ruleCutoff := time.Now().Add(-24 * time.Hour)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dropOldSkipStamps(cutoff)

	for key, ts := range s.lastRuleRun {
		if ts.Before(ruleCutoff) {
			delete(s.lastRuleRun, key)
		}
	}

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
		if err := s.applyForInstance(ctx, instance.ID, false); errors.Is(err, ErrRunInProgress) {
			log.Debug().Int("instanceID", instance.ID).Msg("automations: run in progress, skipping instance until the next check")
		} else if err != nil {
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("automations: apply failed")
		}
	}
}

func (s *Service) ApplyOnceForInstance(ctx context.Context, instanceID int) error {
	return s.applyForInstance(context.WithoutCancel(ctx), instanceID, true)
}

var ErrRunInProgress = errors.New("automations: a run is in progress on this instance")

func (s *Service) tryLockRun(instanceID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, running := s.runningInstances[instanceID]; running {
		return false
	}
	if s.runningInstances == nil {
		s.runningInstances = make(map[int]struct{})
	}
	s.runningInstances[instanceID] = struct{}{}
	return true
}

func (s *Service) unlockRun(instanceID int) {
	s.mu.Lock()
	delete(s.runningInstances, instanceID)
	s.mu.Unlock()
}

func (s *Service) ApplyRuleNow(ctx context.Context, instanceID int, rule *models.Automation) ([]*models.AutomationActivity, error) {
	if s.syncManager == nil || s.instanceStore == nil {
		return nil, nil
	}
	if rule.DryRun {
		activities, err := s.ApplyRuleDryRun(ctx, instanceID, rule)
		s.publishActivity(instanceID, activities)
		return activities, err
	}

	if !s.tryLockRun(instanceID) {
		return nil, ErrRunInProgress
	}
	defer s.unlockRun(instanceID)

	liveRule := *rule
	liveRule.Enabled = true
	activities, err := s.applyRulesForInstance(context.WithoutCancel(ctx), instanceID, true, []*models.Automation{&liveRule}, false)
	if err != nil {
		return nil, err
	}
	s.publishActivity(instanceID, activities)
	return activities, nil
}

func (s *Service) publishActivity(instanceID int, activities []*models.AutomationActivity) {
	if len(activities) == 0 || s.activityPublisher == nil {
		return
	}
	s.activityPublisher.Publish(activity.Event{
		Kind:       activity.KindAutomationActivity,
		InstanceID: instanceID,
	})
}

func (s *Service) ApplyRuleDryRun(ctx context.Context, instanceID int, rule *models.Automation) ([]*models.AutomationActivity, error) {
	if s == nil || rule == nil {
		return nil, nil
	}
	if s.syncManager == nil || s.instanceStore == nil {
		return nil, nil
	}

	dryRunRule := prepareRuleForDryRun(rule, instanceID)
	activities, err := s.applyRulesForInstance(ctx, instanceID, true, []*models.Automation{dryRunRule}, true)
	if err != nil {
		return nil, err
	}
	if len(activities) == 0 {
		return s.recordDryRunNoMatch(ctx, instanceID), nil
	}
	return activities, nil
}

const dryRunEphemeralRuleIDBase = 1_000_000_000

func runtimeRuleID(ruleID int, instanceID int) int {
	if ruleID > 0 {
		return ruleID
	}
	if instanceID < 0 {
		instanceID = -instanceID
	}
	return dryRunEphemeralRuleIDBase + instanceID
}

func prepareRuleForPreview(rule *models.Automation, instanceID int) *models.Automation {
	if rule == nil {
		return nil
	}
	if rule.ID > 0 {
		return rule
	}
	cloned := *rule
	cloned.ID = runtimeRuleID(cloned.ID, instanceID)
	return &cloned
}

func prepareRuleForDryRun(rule *models.Automation, instanceID int) *models.Automation {
	cloned := *rule
	cloned.ID = runtimeRuleID(cloned.ID, instanceID)
	cloned.InstanceID = instanceID
	cloned.Enabled = true
	cloned.DryRun = true
	return &cloned
}

type PreviewResult struct {
	TotalMatches   int              `json:"totalMatches"`
	CrossSeedCount int              `json:"crossSeedCount,omitempty"`
	Examples       []PreviewTorrent `json:"examples"`
	Skipped        *RuleSkip        `json:"skipped,omitempty"`
}

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
	IsCrossSeed    bool    `json:"isCrossSeed,omitempty"`
	IsHardlinkCopy bool    `json:"isHardlinkCopy,omitempty"`

	NumSeeds           int64    `json:"numSeeds"`
	NumComplete        int64    `json:"numComplete"`
	NumLeechs          int64    `json:"numLeechs"`
	NumIncomplete      int64    `json:"numIncomplete"`
	Progress           float64  `json:"progress"`
	Availability       float64  `json:"availability"`
	TimeActive         int64    `json:"timeActive"`
	LastActivity       int64    `json:"lastActivity"`
	CompletionOn       int64    `json:"completionOn"`
	TotalSize          int64    `json:"totalSize"`
	HardlinkScope      string   `json:"hardlinkScope,omitempty"`
	HardlinkCrossScope string   `json:"hardlinkCrossScope,omitempty"`
	Score              *float64 `json:"score,omitempty"`
}

func buildPreviewTorrent(torrent *qbt.Torrent, tracker string, evalCtx *EvalContext, isCrossSeed, isHardlinkCopy bool, score *float64) PreviewTorrent {
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
		CompletionOn:   qbittorrent.NormalizeCompletionTimestamp(torrent.CompletionOn),
		TotalSize:      torrent.TotalSize,
		Score:          score,
	}

	if evalCtx != nil {
		pt.IsUnregistered = evalCtx.TrackerHealth.Health(torrent.Hash) == qbittorrent.TrackerHealthUnregistered
		if evalCtx.HardlinkScopeByHash != nil {
			pt.HardlinkScope = evalCtx.HardlinkScopeByHash[torrent.Hash]
		}
		if evalCtx.HardlinkCrossScopeByHash != nil {
			pt.HardlinkCrossScope = evalCtx.HardlinkCrossScopeByHash[torrent.Hash]
		}
	}

	return pt
}

type previewConfig struct {
	limit  int
	offset int
}

func (c *previewConfig) normalize() {
	if c.limit <= 0 {
		c.limit = 25
	}
	if c.offset < 0 {
		c.offset = 0
	}
}

func (s *Service) buildEvalContext(ctx context.Context, instanceID int, instance *models.Instance, torrents []*qbt.Torrent, needs RuleNeeds, trackers qbittorrent.TrackerLookup) (*EvalContext, *HardlinkIndex, error) {
	evalCtx := &EvalContext{ReleaseParser: s.releaseParser, trackers: trackers}
	if instance != nil {
		evalCtx.setFilesystemAccess(instance)
	}

	evalCtx.CategoryIndex, evalCtx.CategoryNames = BuildCategoryIndex(torrents)

	if s.syncManager != nil {
		evalCtx.TrackerHealth = s.syncManager.GetTrackerHealthSnapshot(instanceID)
	}

	var hardlinkIndex *HardlinkIndex
	if evalCtx.InstanceHasTrustedIdentity && (needs.HardlinkScope || needs.HardlinkCrossScope || needs.HardlinkSignature) {
		hardlinkIndex = s.GetHardlinkIndex(ctx, instanceID, torrents)
		if hardlinkIndex != nil {
			evalCtx.HardlinkScopeByHash = hardlinkIndex.ScopeByHash
			if needs.HardlinkSignature {
				evalCtx.HardlinkSignatureByHash = hardlinkIndex.SignatureByHash
			}
			if needs.HardlinkCrossScope {
				hardlinkIndex.crossScopeMu.Lock()
				if hardlinkIndex.CrossScopeByHash == nil && hardlinkIndex.buildState != nil {
					s.augmentCrossInstanceScope(ctx, instanceID, hardlinkIndex)
				}
				evalCtx.HardlinkCrossScopeByHash = hardlinkIndex.CrossScopeByHash
				hardlinkIndex.crossScopeMu.Unlock()
			}
		}
	}

	if evalCtx.InstanceCanRead && needs.MissingFiles {
		missing, err := s.detectMissingFiles(ctx, instanceID, torrents)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: missing files detection failed")
		} else {
			evalCtx.HasMissingFilesByHash = missing
		}
	}

	if needs.SkippedFiles {
		skipped, err := s.detectSkippedFiles(ctx, instanceID, torrents)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: skipped files detection failed")
		} else {
			evalCtx.HasSkippedFilesByHash = skipped
		}
	}

	if needs.CrossMatch != (CrossMatchNeeds{}) && s.crossMatcher != nil {
		if err := s.openRunSession(ctx, instanceID, torrents, needs, evalCtx); err != nil {
			return nil, nil, fmt.Errorf("failed to build cross-seed snapshot: %w", err)
		}
	}

	if needs.SeasonPack {
		evalCtx.SeasonPackSet = buildSeasonPackSet(s.releaseParser, torrents)
	}
	if needs.SeasonPackAnyInstance {
		evalCtx.SeasonPackSetAnyInstance = s.buildAnyInstanceSeasonPackSet(ctx)
	}

	if needs.TrackerNames && s.trackerCustomizationStore != nil {
		customizations, err := s.trackerCustomizationStore.List(ctx)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to load tracker customizations for display names")
		} else {
			evalCtx.TrackerDisplayNameByDomain = buildTrackerDisplayNameMap(customizations)
		}
	}

	if needs.SavePaths && s.syncManager != nil {
		evalCtx.CategorySavePaths = s.loadCategorySavePaths(ctx, instanceID)
	}

	evalCtx.hardlinkIndex = hardlinkIndex
	return evalCtx, hardlinkIndex, nil
}

func (s *Service) previewEvalContext(ctx context.Context, instanceID int, rule *models.Automation, torrents []*qbt.Torrent, scope *ruleScope) (*EvalContext, *HardlinkIndex, *models.Instance, error) {
	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get instance for preview, proceeding without instance context")
	}
	evalCtx, hardlinkIndex, err := s.buildEvalContext(ctx, instanceID, instance, torrents, previewNeeds(rule), scope.trackers)
	if err != nil {
		return nil, nil, instance, err
	}
	askRuleFacts([]*models.Automation{rule}, torrents, evalCtx, scope)
	return evalCtx, hardlinkIndex, instance, nil
}

func (s *Service) PreviewDeleteRule(ctx context.Context, instanceID int, rule *models.Automation, limit, offset int, previewView string) (*PreviewResult, error) {
	if s == nil || s.syncManager == nil {
		return &PreviewResult{}, nil
	}
	rule = prepareRuleForPreview(rule, instanceID)
	if rule == nil {
		return &PreviewResult{}, nil
	}

	torrents, err := s.syncManager.GetAllTorrentRefs(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}
	torrents = s.leaveOutClaimed(instanceID, torrents)
	if skip, ok := ruleSkipOf(rule, s.instanceSupports(ctx, instanceID)); ok {
		return &PreviewResult{Examples: []PreviewTorrent{}, Skipped: &skip}, nil
	}
	trackers, err := s.trackersForRule(ctx, instanceID, torrents, rule)
	if err != nil {
		return nil, err
	}

	cfg := previewConfig{limit: limit, offset: offset}
	cfg.normalize()

	scope := newRuleScope([]*models.Automation{rule}, nil, trackers)
	evalCtx, hardlinkIndex, instance, err := s.previewEvalContext(ctx, instanceID, rule, torrents, scope)
	if err != nil {
		return nil, err
	}
	evalCtx.freeSpaceLedgers, err = s.buildFreeSpaceLedgers(ctx, instance, []*models.Automation{rule}, previewView == "eligible", s.previewUsageUnder)
	if err != nil {
		return nil, err
	}
	enterRule(evalCtx, rule, torrents, scope)

	scoreByHash := buildPreviewScoreMap(torrents, rule, evalCtx)

	crossSeeds := buildCrossSeedGroups(torrents, evalCtx)
	if ruleNeedsCrossSeedFiles(rule) {
		var projectionHardlinks *HardlinkIndex
		if ruleUsesIncludeCrossSeedsFreeSpace(rule) && rule.Conditions.Delete.IncludeHardlinks {
			projectionHardlinks = hardlinkIndex
		}
		s.loadCrossSeedFiles(ctx, instanceID, crossSeeds, evalCtx, projectionHardlinks)
	}

	selection := s.selectDeletions(ctx, instanceID, deletionSelectionInput{
		rules:      []*models.Automation{rule},
		torrents:   torrents,
		evalCtx:    evalCtx,
		crossSeeds: crossSeeds,
		scope:      scope,
	})
	if err := selection.failedRules[rule.ID]; err != nil {
		return nil, err
	}
	return buildDeletePreviewResult(torrents, selection, evalCtx, cfg, rule, scoreByHash, scope), nil
}

func buildPreviewScoreMap(torrents []*qbt.Torrent, rule *models.Automation, evalCtx *EvalContext) map[string]float64 {
	if rule == nil || rule.SortingConfig == nil || rule.SortingConfig.Type != models.SortingTypeScore {
		return nil
	}

	scoreByHash := make(map[string]float64, len(torrents))
	for _, torrent := range torrents {
		scoreByHash[torrent.Hash] = CalculateScore(*torrent, rule.SortingConfig, evalCtx)
	}

	return scoreByHash
}

func computePreviewScore(torrent *qbt.Torrent, rule *models.Automation, evalCtx *EvalContext, scoreByHash map[string]float64) *float64 {
	if score, ok := scoreByHash[torrent.Hash]; ok {
		scoreCopy := score
		return &scoreCopy
	}
	if rule != nil && rule.SortingConfig != nil && rule.SortingConfig.Type == models.SortingTypeScore {
		score := CalculateScore(*torrent, rule.SortingConfig, evalCtx)
		return &score
	}
	return nil
}

func getDeleteMode(rule *models.Automation) string {
	if rule.Conditions != nil && rule.Conditions.Delete != nil && rule.Conditions.Delete.Mode != "" {
		return rule.Conditions.Delete.Mode
	}
	return DeleteModeKeepFiles
}

func shouldDeleteTorrent(rule *models.Automation, torrent *qbt.Torrent, evalCtx *EvalContext) bool {
	if rule.Conditions == nil || rule.Conditions.Delete == nil || !rule.Conditions.Delete.Enabled {
		return false
	}
	if matchesStateValue(*torrent, "checking", evalCtx) {
		return false
	}
	if rule.Conditions.Delete.Condition == nil {
		return true
	}
	return EvaluateConditionWithContext(rule.Conditions.Delete.Condition, *torrent, evalCtx, 0)
}

type torrentFilesFetcher func(hashes []string) (map[string]qbt.TorrentFiles, error)

type crossSeedExpansionState struct {
	expandedSet     map[string]struct{}
	hardlinkCopySet map[string]struct{}
}

func newCrossSeedExpansionState() *crossSeedExpansionState {
	return &crossSeedExpansionState{
		expandedSet:     make(map[string]struct{}),
		hardlinkCopySet: make(map[string]struct{}),
	}
}

func deleteCrossSeedGroup(
	trigger qbt.Torrent,
	torrents map[string]*qbt.Torrent,
	crossSeeds *crossSeedGroups,
	hardlinks *HardlinkIndex,
	alreadyIncluded map[string]struct{},
	fetchFiles torrentFilesFetcher,
) (*crossSeedExpansionState, bool) {
	state := newCrossSeedExpansionState()
	state.expandedSet[trigger.Hash] = struct{}{}
	queue := []*qbt.Torrent{&trigger}
	add := func(hash string, linked bool) bool {
		if _, exists := alreadyIncluded[hash]; exists {
			return true
		}
		if _, exists := state.expandedSet[hash]; exists {
			return true
		}
		torrent, exists := torrents[hash]
		if !exists {
			return false
		}
		state.expandedSet[hash] = struct{}{}
		if linked {
			state.hardlinkCopySet[hash] = struct{}{}
		}
		queue = append(queue, torrent)
		return true
	}
	contentChecked := make(map[string]struct{})
	linkChecked := make(map[string]struct{})
	for i := 0; i < len(queue); i++ {
		member := queue[i]
		if _, checked := contentChecked[member.Hash]; !checked {
			group, ok := crossSeedGroupMembers(*member, crossSeeds.members(member.Hash), alreadyIncluded, fetchFiles)
			if !ok {
				return nil, false
			}
			for _, hash := range group {
				contentChecked[hash] = struct{}{}
				if !add(hash, false) {
					return nil, false
				}
			}
		}
		if hardlinks == nil {
			continue
		}
		signature := hardlinks.DeleteSafeSignatureByHash[member.Hash]
		if _, checked := linkChecked[signature]; checked {
			continue
		}
		linkChecked[signature] = struct{}{}
		for _, hash := range hardlinks.GetHardlinkCopies(member.Hash) {
			if !add(hash, true) {
				return nil, false
			}
		}
	}
	return state, true
}

func buildDeletePreviewResult(
	torrents []*qbt.Torrent,
	selection *deletionSelection,
	evalCtx *EvalContext,
	cfg previewConfig,
	rule *models.Automation,
	scoreByHash map[string]float64,
	scope *ruleScope,
) *PreviewResult {
	result := &PreviewResult{
		TotalMatches: len(selection.byHash),
		Examples:     make([]PreviewTorrent, 0, cfg.limit),
	}

	matchIndex := 0
	for _, torrent := range torrents {
		selected, ok := selection.byHash[torrent.Hash]
		if !ok {
			continue
		}
		isCrossSeed := !selected.trigger && !selected.hardlinkCopy
		if isCrossSeed && selected.deleteMode == DeleteModeWithFilesIncludeCrossSeeds {
			result.CrossSeedCount++
		}

		matchIndex++
		if matchIndex <= cfg.offset || len(result.Examples) >= cfg.limit {
			continue
		}
		score := computePreviewScore(torrent, rule, evalCtx, scoreByHash)
		result.Examples = append(result.Examples, buildPreviewTorrent(torrent, scope.tracker(torrent), evalCtx, isCrossSeed, selected.hardlinkCopy, score))
	}

	return result
}

func (s *Service) PreviewCategoryRule(ctx context.Context, instanceID int, rule *models.Automation, limit, offset int) (*PreviewResult, error) {
	if rule == nil {
		return &PreviewResult{}, nil
	}
	return s.previewPlanPart(ctx, instanceID, rule, models.ActionConditions{Category: rule.Conditions.Category}, limit, offset, func(plan runPlan) []string {
		var hashes []string
		for _, targetHashes := range plan.category.byTarget {
			hashes = append(hashes, targetHashes...)
		}
		return hashes
	})
}

func (s *Service) PreviewTagRule(ctx context.Context, instanceID int, rule *models.Automation, limit, offset int) (*PreviewResult, error) {
	if rule == nil {
		return &PreviewResult{}, nil
	}
	return s.previewPlanPart(ctx, instanceID, rule, models.ActionConditions{Tag: rule.Conditions.Tag, Tags: rule.Conditions.Tags}, limit, offset, func(plan runPlan) []string {
		return slices.Collect(maps.Keys(plan.tags))
	})
}

func (s *Service) previewPlanPart(ctx context.Context, instanceID int, rule *models.Automation, keep models.ActionConditions, limit, offset int, part func(runPlan) []string) (*PreviewResult, error) {
	if s == nil || s.syncManager == nil {
		return &PreviewResult{}, nil
	}
	rule = prepareRuleForPreview(rule, instanceID)

	torrents, err := s.syncManager.GetAllTorrentRefs(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}
	torrents = s.leaveOutClaimed(instanceID, torrents)
	if skip, ok := ruleSkipOf(rule, s.instanceSupports(ctx, instanceID)); ok {
		return &PreviewResult{Examples: []PreviewTorrent{}, Skipped: &skip}, nil
	}
	trackers, err := s.trackersForRule(ctx, instanceID, torrents, rule)
	if err != nil {
		return nil, err
	}

	cfg := previewConfig{limit: limit, offset: offset}
	cfg.normalize()

	scope := newRuleScope([]*models.Automation{rule}, nil, trackers)
	evalCtx, _, instance, err := s.previewEvalContext(ctx, instanceID, rule, torrents, scope)
	if err != nil {
		return nil, err
	}
	crossSeeds := buildCrossSeedGroups(torrents, evalCtx)

	evalCtx.freeSpaceLedgers, err = s.buildFreeSpaceLedgers(ctx, instance, []*models.Automation{rule}, false, s.previewUsageUnder)
	if err != nil {
		return nil, err
	}

	plan, own := s.planPreview(ctx, instanceID, rule, keep, torrents, evalCtx, crossSeeds, scope)
	scoreByHash := buildPreviewScoreMap(torrents, rule, evalCtx)

	directSet := make(map[string]struct{})
	crossSeedSet := make(map[string]struct{})
	for _, hash := range part(plan) {
		if _, ok := own[hash]; ok {
			directSet[hash] = struct{}{}
		} else {
			crossSeedSet[hash] = struct{}{}
		}
	}

	return buildMatchPreviewResult(torrents, directSet, crossSeedSet, evalCtx, cfg, rule, scoreByHash, scope), nil
}

type categoryActionConfig struct {
	includeCrossSeeds bool
	blockCategories   []string
	condition         *RuleCondition
	enabled           bool
}

func getCategoryAction(rule *models.Automation) categoryActionConfig {
	if rule.Conditions == nil {
		return categoryActionConfig{}
	}
	return categoryActionOf(rule.Conditions.Category)
}

func categoryActionOf(cat *models.CategoryAction) categoryActionConfig {
	if cat == nil {
		return categoryActionConfig{}
	}
	groupID := strings.TrimSpace(cat.GroupID)
	return categoryActionConfig{
		includeCrossSeeds: groupID == "" && cat.IncludeCrossSeeds,
		blockCategories:   cat.BlockIfCrossSeedInCategories,
		condition:         cat.Condition,
		enabled:           cat.Enabled,
	}
}

func (c categoryActionConfig) readsCrossSeedGroups() bool {
	return c.enabled && (c.includeCrossSeeds || len(c.blockCategories) > 0)
}

func shouldApplyCategoryAction(torrent *qbt.Torrent, catAction categoryActionConfig, evalCtx *EvalContext, crossSeeds *crossSeedGroups) bool {
	if !catAction.enabled {
		return false
	}
	if catAction.condition != nil && !EvaluateConditionWithContext(catAction.condition, *torrent, evalCtx, 0) {
		return false
	}
	if catAction.includeCrossSeeds && !crossSeedGroupKnown(*torrent, crossSeeds, evalCtx) {
		return false
	}
	return !shouldBlockCategoryChangeForCrossSeeds(*torrent, catAction.blockCategories, crossSeeds)
}

func buildMatchPreviewResult(
	torrents []*qbt.Torrent,
	directSet, crossSeedSet map[string]struct{},
	evalCtx *EvalContext,
	cfg previewConfig,
	rule *models.Automation,
	scoreByHash map[string]float64,
	scope *ruleScope,
) *PreviewResult {
	allMatches := make(map[string]struct{}, len(directSet)+len(crossSeedSet))
	for h := range directSet {
		allMatches[h] = struct{}{}
	}
	for h := range crossSeedSet {
		allMatches[h] = struct{}{}
	}

	result := &PreviewResult{
		TotalMatches:   len(allMatches),
		CrossSeedCount: len(crossSeedSet),
		Examples:       make([]PreviewTorrent, 0, cfg.limit),
	}

	matchIndex := 0
	for _, torrent := range torrents {
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

		_, isCrossSeed := crossSeedSet[torrent.Hash]

		score := computePreviewScore(torrent, rule, evalCtx, scoreByHash)

		result.Examples = append(result.Examples, buildPreviewTorrent(torrent, scope.tracker(torrent), evalCtx, isCrossSeed, false, score))
	}

	return result
}

type hardlinkUse struct {
	single bool
	cross  bool
}

func deleteUsesHardlinkData(rule *models.Automation) hardlinkUse {
	if rule == nil || rule.Conditions == nil || rule.Conditions.Delete == nil {
		return hardlinkUse{}
	}
	deleteOnly := *rule
	deleteOnly.Enabled = true
	deleteOnly.Conditions = &models.ActionConditions{Delete: rule.Conditions.Delete}
	needs := NeedsFor([]*models.Automation{&deleteOnly})
	return hardlinkUse{
		single: rule.Conditions.Delete.IncludeHardlinks || needs.HardlinkScope || ruleUsesHardlinkSignatureGrouping(rule),
		cross:  needs.HardlinkCrossScope,
	}
}

func (s *Service) blockedDeleteCandidates(
	ctx context.Context,
	instanceID int,
	hardlinkIndex *HardlinkIndex,
	torrentByHash map[string]*qbt.Torrent,
	deletions *deletionSelection,
	ruleByID map[int]*models.Automation,
) map[string]string {
	candidates := make(map[string]hardlinkUse)
	for hash, pending := range deletions.byHash {
		if use := deleteUsesHardlinkData(ruleByID[pending.rule.id]); use != (hardlinkUse{}) {
			candidates[hash] = use
		}
	}

	return s.verifyDeleteCandidates(ctx, instanceID, hardlinkIndex, torrentByHash, candidates)
}

func (s *Service) applyForInstance(ctx context.Context, instanceID int, force bool) error {
	if !s.tryLockRun(instanceID) {
		return ErrRunInProgress
	}
	defer s.unlockRun(instanceID)

	rules, err := s.ruleStore.ListByInstance(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to load rules")
		s.notifyAutomationFailure(ctx, instanceID, err)
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

	dryActivity, err := s.applyRulesForInstance(ctx, instanceID, force, dryRunRules, true)
	if err != nil {
		return err
	}
	liveActivity, err := s.applyRulesForInstance(ctx, instanceID, force, liveRules, false)
	if err != nil {
		return err
	}

	s.publishActivity(instanceID, append(dryActivity, liveActivity...))
	return nil
}

var actionRunOrder = []string{
	"speedLimits",
	"shareLimits",
	"pause",
	"resume",
	"recheck",
	"reannounce",
	"autoManagement",
	"tag",
	"category",
	"move",
	"externalProgram",
	"exportToInstance",
	"delete",
	"transferToInstance",
}

func (s *Service) applyRulesForInstance(ctx context.Context, instanceID int, force bool, rules []*models.Automation, dryRun bool) ([]*models.AutomationActivity, error) {
	if len(rules) == 0 {
		return nil, nil
	}

	now := time.Now()
	eligibleRules := make([]*models.Automation, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
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
				continue
			}
		}
		eligibleRules = append(eligibleRules, rule)
	}
	if len(eligibleRules) == 0 {
		return nil, nil
	}

	s.mu.RLock()
	lastFSDelete := s.lastFreeSpaceDeleteAt[instanceID]
	s.mu.RUnlock()

	if !dryRun && !lastFSDelete.IsZero() && now.Sub(lastFSDelete) < freeSpaceDeleteCooldown {
		filtered := make([]*models.Automation, 0, len(eligibleRules))
		for _, rule := range eligibleRules {
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
			return nil, nil
		}
	}

	freeSpaceDeleteRuleIDs := make(map[int]struct{})
	for _, rule := range eligibleRules {
		if rule.Conditions != nil && rule.Conditions.Delete != nil && rule.Conditions.Delete.Enabled {
			if ConditionUsesField(rule.Conditions.Delete.Condition, FieldFreeSpace) {
				freeSpaceDeleteRuleIDs[rule.ID] = struct{}{}
			}
		}
	}

	torrents, err := s.syncManager.GetAllTorrentRefs(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("automations: unable to fetch torrents")
		s.notifyAutomationFailure(ctx, instanceID, err)
		return nil, err
	}
	torrents = s.leaveOutClaimed(instanceID, torrents)

	eligibleRules, skipped := s.skipRulesWithUnknownFields(ctx, instanceID, eligibleRules, now)
	skipActivities := s.recordRuleSkips(ctx, instanceID, skipped, force, dryRun)
	if len(torrents) == 0 || len(eligibleRules) == 0 {
		return skipActivities, nil
	}

	needs := NeedsFor(eligibleRules)
	var trackers qbittorrent.TrackerLookup
	if needs.TrackerEntries || needs.TrackerURLs {
		trackers, err = s.readTrackers(ctx, instanceID, torrents, needs)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", instanceID).Msg("automations: tracker data incomplete, skipping run")
			s.notifyAutomationFailure(ctx, instanceID, err)
			return nil, err
		}
		if trackerDataMissing(torrents, trackers) {
			log.Debug().
				Int("instanceID", instanceID).
				Int("torrents", len(torrents)).
				Msg("automations: no tracker data for any torrent, tracker conditions will not match")
		}
	}

	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get instance")
		s.notifyAutomationFailure(ctx, instanceID, err)
		return nil, err
	}

	evalCtx, hardlinkIndex, err := s.buildEvalContext(ctx, instanceID, instance, torrents, needs, trackers)
	if err != nil {
		s.notifyAutomationFailure(ctx, instanceID, err)
		return nil, err
	}

	if needs.FreeSpace {
		evalCtx.freeSpaceLedgers, err = s.freeSpaceLedgers(ctx, instance, eligibleRules, false)
		if err != nil {
			s.notifyAutomationFailure(ctx, instanceID, err)
			return nil, err
		}
	}

	crossSeeds := buildCrossSeedGroups(torrents, evalCtx)
	if rulesNeedCrossSeedFiles(eligibleRules) {
		var projectionHardlinks *HardlinkIndex
		if rulesNeedHardlinkSignatureMap(eligibleRules) {
			projectionHardlinks = hardlinkIndex
		}
		s.loadCrossSeedFiles(ctx, instanceID, crossSeeds, evalCtx, projectionHardlinks)
	}

	var skipWindow map[string]struct{}
	if !dryRun {
		skipWindow = s.skipWindow(instanceID, now)
	}
	scope := newRuleScope(eligibleRules, skipWindow, trackers)

	askRuleFacts(eligibleRules, torrents, evalCtx, scope)

	rulesUsed, consideredTorrents := rulesWithTorrents(eligibleRules, torrents, scope)

	ruleByID := make(map[int]*models.Automation, len(eligibleRules))
	for _, r := range eligibleRules {
		ruleByID[r.ID] = r
	}
	ruleStats := make(map[int]*ruleRunStats)
	deletions := s.selectDeletions(ctx, instanceID, deletionSelectionInput{
		rules:      eligibleRules,
		torrents:   torrents,
		evalCtx:    evalCtx,
		crossSeeds: crossSeeds,
		scope:      scope,
		stats:      ruleStats,
	})
	for ruleID, err := range deletions.failedRules {
		log.Warn().Err(err).Int("instanceID", instanceID).Int("ruleID", ruleID).Str("ruleName", ruleByID[ruleID].Name).
			Msg("automations: skipping free-space delete rule")
	}

	states := make(map[string]*torrentDesiredState)

	buildAndExecuteBatches(instanceID, eligibleRules, torrents, evalCtx, crossSeeds, scope, ruleStats, states)

	if len(states) == 0 && len(deletions.triggers) == 0 {
		log.Trace().
			Int("instanceID", instanceID).
			Int("eligibleRules", len(eligibleRules)).
			Int("torrents", len(torrents)).
			Int("matchedRules", len(rulesUsed)).
			Msg("automations: no actions to apply")
	}

	for _, rule := range eligibleRules {
		stats := ruleStats[rule.ID]
		if stats == nil || stats.MatchedTrackers == 0 {
			if consideredTorrents > 0 {
				log.Debug().
					Int("instanceID", instanceID).
					Int("ruleID", rule.ID).
					Str("ruleName", rule.Name).
					Str("trackerPattern", rule.TrackerPattern).
					Int("consideredTorrents", consideredTorrents).
					Msg("automations: rule matched no torrents")
			}
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
			Int("resumeNoMatch", stats.ResumeConditionNotMet).
			Int("recheckNoMatch", stats.RecheckConditionNotMet).
			Int("reannounceNoMatch", stats.ReannounceConditionNotMet).
			Int("tagNoMatch", stats.TagConditionNotMet).
			Int("tagMissingTrackerHealth", stats.TagSkippedMissingTrackerHealth).
			Int("categoryNoMatchOrBlocked", stats.CategoryConditionNotMetOrBlocked).
			Int("deleteNoMatch", stats.DeleteConditionNotMet).
			Int("moveNoMatch", stats.MoveConditionNotMet).
			Int("moveAlreadyAtDest", stats.MoveAlreadyAtDestination).
			Int("moveBlockedByCrossSeed", stats.MoveBlockedByCrossSeed).
			Int("exportToInstanceNoMatch", stats.ExportToInstanceConditionNotMet).
			Int("transferToInstanceNoMatch", stats.TransferConditionNotMet).
			Msg("automations: rule matched trackers but applied no actions")
	}

	s.mu.Lock()
	for ruleID := range rulesUsed {
		key := ruleKey{instanceID, ruleID}
		s.lastRuleRun[key] = now
	}
	s.mu.Unlock()

	torrentByHash := evalCtx.torrentsByHash
	if torrentByHash == nil {
		torrentByHash = make(map[string]*qbt.Torrent, len(torrents))
		for _, t := range torrents {
			torrentByHash[t.Hash] = t
		}
	}

	for _, hash := range deletions.order {
		pending := deletions.byHash[hash]
		log.Info().Str("hash", hash).Str("name", torrentByHash[hash].Name).Bool("isTrigger", pending.trigger).Str("reason", "condition matched").Bool("filesKept", pending.filesKept()).Msg(pending.logMsg)
	}
	if !dryRun {
		s.startSkipWindow(instanceID, deletions.order, now)
	}

	if !dryRun {
		recheckCtx, cancelRecheck := context.WithTimeout(ctx, s.cfg.ApplyTimeout)
		blocked := s.blockedDeleteCandidates(recheckCtx, instanceID, hardlinkIndex, torrentByHash, deletions, ruleByID)
		cancelRecheck()
		for hash, reason := range blocked {
			pending := deletions.byHash[hash]
			delete(deletions.byHash, hash)
			log.Warn().
				Int("instanceID", instanceID).
				Str("hash", hash).
				Str("name", torrentByHash[hash].Name).
				Str("ruleName", pending.rule.name).
				Str("reason", reason).
				Msg("automations: skipped delete, hardlink state changed since the rule decided")
		}
	}
	deletes := groupDeletesByRule(deletions)

	if !dryRun {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.cfg.ApplyTimeout)
		defer cancel()
		crossSeeds.setContext(ctx)
	}

	plan := s.buildRunPlan(ctx, runPlanInput{
		instanceID:    instanceID,
		states:        states,
		deletions:     deletions,
		torrents:      torrents,
		torrentByHash: torrentByHash,
		ruleByID:      ruleByID,
		crossSeeds:    crossSeeds,
		evalCtx:       evalCtx,
		scope:         scope,
	})

	if dryRun {
		return append(s.recordDryRunActivities(ctx, instanceID, plan, torrentByHash, scope, force), skipActivities...), nil
	}

	summary := newAutomationSummary()
	steps := &stepRun{
		s:             s,
		instanceID:    instanceID,
		torrentByHash: torrentByHash,
		scope:         scope,
		summary:       summary,
		resetTags:     collectManagedTagsForClientReset(eligibleRules),
	}
	steps.runSteps(ctx, &plan)

	s.executeExternalProgramsFromAutomation(ctx, instanceID, plan.programs)

	exportResults := s.executeExportToInstance(ctx, instanceID, s.reserveExports(plan.exports))

	deleteActivity := func(hash string, pending selectedDeletion, action, outcome, reason string) *models.AutomationActivity {
		detailsJSON, _ := json.Marshal(struct { //nolint:errchkjson // string and bool fields cannot fail to encode
			DeleteMode string `json:"deleteMode"`
			FilesKept  bool   `json:"filesKept"`
			IsTrigger  bool   `json:"isTrigger"`
		}{pending.deleteMode, pending.filesKept(), pending.trigger})
		torrent := torrentByHash[hash]
		return &models.AutomationActivity{
			InstanceID:    instanceID,
			Hash:          hash,
			TorrentName:   torrent.Name,
			TrackerDomain: scope.tracker(torrent),
			Action:        action,
			RuleID:        &pending.rule.id,
			RuleName:      pending.rule.name,
			Outcome:       outcome,
			Reason:        reason,
			Details:       detailsJSON,
		}
	}
	for _, group := range deletes {
		actor := qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorAutomation, RuleName: group.rule.name}
		deleteFiles, modeErr := deleteFilesForMode(group.mode)
		for _, batch := range limitHashBatch(group.hashes, s.cfg.MaxBatchHashes) {
			err := modeErr
			if err == nil {
				err = s.deleter.DeleteTorrents(ctx, instanceID, batch, deleteFiles, actor)
			}
			if err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Str("action", group.mode).Int("count", len(batch)).Strs("hashes", batch).Msg("automations: delete failed")

				for _, hash := range batch {
					if pending, ok := deletions.byHash[hash]; ok {
						activity := deleteActivity(hash, pending, models.ActivityActionDeleteFailed, models.ActivityOutcomeFailed, err.Error())
						summary.recordActivity(activity, 1)
						if s.activityStore != nil {
							if err := s.activityStore.Create(ctx, activity); err != nil {
								log.Warn().Err(err).Str("hash", hash).Int("instanceID", instanceID).Msg("automations: failed to record activity")
							}
						}
					}
				}
			} else {
				if group.mode == DeleteModeKeepFiles {
					log.Info().Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: removed torrents (files kept)")
				} else {
					log.Info().Int("instanceID", instanceID).Int("count", len(batch)).Msg("automations: removed torrents with files")

					if len(freeSpaceDeleteRuleIDs) > 0 {
						for _, hash := range batch {
							if pending, ok := deletions.byHash[hash]; ok {
								if _, isFSRule := freeSpaceDeleteRuleIDs[pending.rule.id]; isFSRule {
									s.mu.Lock()
									s.lastFreeSpaceDeleteAt[instanceID] = now
									s.mu.Unlock()
									log.Debug().
										Int("instanceID", instanceID).
										Int("ruleID", pending.rule.id).
										Dur("cooldown", freeSpaceDeleteCooldown).
										Msg("automations: started FREE_SPACE delete cooldown")
									break
								}
							}
						}
					}
				}

				for _, hash := range batch {
					if pending, ok := deletions.byHash[hash]; ok {
						activity := deleteActivity(hash, pending, models.ActivityActionDeletedCondition, models.ActivityOutcomeSuccess, "condition matched")
						summary.recordActivity(activity, 1)
						if s.activityStore != nil {
							if err := s.activityStore.Create(ctx, activity); err != nil {
								log.Warn().Err(err).Str("hash", hash).Int("instanceID", instanceID).Msg("automations: failed to record activity")
							}
						}
					}
				}
			}
		}
	}

	if len(plan.transfers) > 0 {
		s.startTransfers(ctx, instanceID, plan.transfers, summary)
	}

	type exportBatch struct {
		activities []*models.AutomationActivity
	}
	exportBatchCh := make(chan exportBatch, 1)
	stopDrain := make(chan struct{})
	go func() {
		var collected []*models.AutomationActivity
		for {
			select {
			case activity, ok := <-exportResults:
				if !ok {
					exportBatchCh <- exportBatch{activities: collected}
					return
				}
				collected = append(collected, activity)
			case <-stopDrain:
				for {
					select {
					case activity, ok := <-exportResults:
						if !ok {
							exportBatchCh <- exportBatch{activities: collected}
							return
						}
						collected = append(collected, activity)
					default:
						exportBatchCh <- exportBatch{activities: collected}
						return
					}
				}
			}
		}
	}()

	drainTimer := time.NewTimer(3 * time.Minute)
	defer drainTimer.Stop()

	mergePartial := func(reason string) {
		close(stopDrain)
		batch := <-exportBatchCh
		for _, activity := range batch.activities {
			summary.recordActivity(activity, 1)
		}
		log.Warn().Int("instanceID", instanceID).Str("reason", reason).Msg("automations: export drain interrupted, notifying with partial summary")
	}

	select {
	case batch := <-exportBatchCh:
		for _, activity := range batch.activities {
			summary.recordActivity(activity, 1)
		}
	case <-drainTimer.C:
		mergePartial("timeout")
	case <-ctx.Done():
		mergePartial("context cancelled")
	}

	notifyCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		notifyCtx, cancel = context.WithTimeout(context.Background(), s.cfg.ApplyTimeout)
		defer cancel()
	}
	s.notifyAutomationSummary(notifyCtx, instanceID, summary, eligibleRules)
	return append(summary.activities, skipActivities...), nil
}

func (s *Service) notifyAutomationSummary(ctx context.Context, instanceID int, summary *automationSummary, rules []*models.Automation) {
	if s == nil || s.notifier == nil || summary == nil || !summary.hasActivity() {
		return
	}

	if !shouldNotifyAutomationSummary(summary, rules) {
		return
	}

	notifiedSummary := buildNotifiedAutomationSummary(summary, rules)
	if notifiedSummary == nil || !notifiedSummary.hasActivity() {
		return
	}

	var errorMessage string
	var errorMessages []string
	if notifiedSummary.failed > 0 && len(notifiedSummary.sampleErrors) > 0 {
		errorMessages = append([]string(nil), notifiedSummary.sampleErrors...)
		errorMessage = notifiedSummary.sampleErrors[0]
	}

	s.notifier.Notify(ctx, notifications.Event{
		Type:       notifications.EventAutomationsActionsApplied,
		InstanceID: instanceID,
		Message:    notifiedSummary.message(),
		Automations: &notifications.AutomationsEventData{
			Applied: notifiedSummary.applied,
			Failed:  notifiedSummary.failed,
			Rules:   buildAutomationRuleSummaries(notifiedSummary),
			Samples: append([]string(nil), notifiedSummary.sampleTorrents...),
		},
		ErrorMessage:  errorMessage,
		ErrorMessages: errorMessages,
	})
}

func shouldNotifyAutomationSummary(summary *automationSummary, rules []*models.Automation) bool {
	if summary == nil || len(rules) == 0 {
		return false
	}

	notifyByRuleID := make(map[int]bool, len(rules))
	for _, rule := range rules {
		notifyByRuleID[rule.ID] = rule.Notify
	}

	for _, ruleSummary := range summary.rules {
		if ruleSummary == nil {
			continue
		}
		if notifyByRuleID[ruleSummary.ruleID] {
			return true
		}
	}

	return false
}

func buildAutomationRuleSummaries(summary *automationSummary) []notifications.AutomationRuleSummary {
	if summary == nil || len(summary.rules) == 0 {
		return nil
	}

	type ruleItem struct {
		key   string
		rule  *automationRuleSummary
		total int
	}
	items := make([]ruleItem, 0, len(summary.rules))
	for key, rule := range summary.rules {
		if rule == nil {
			continue
		}
		items = append(items, ruleItem{
			key:   key,
			rule:  rule,
			total: rule.applied + rule.failed,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].total == items[j].total {
			return items[i].key < items[j].key
		}
		return items[i].total > items[j].total
	})

	out := make([]notifications.AutomationRuleSummary, 0, len(items))
	for _, item := range items {
		rule := item.rule

		actions := make([]notifications.AutomationActionSummary, 0, len(rule.actions))
		for action, counts := range rule.actions {
			if counts == nil {
				continue
			}
			if counts.applied == 0 && counts.failed == 0 {
				continue
			}
			actions = append(actions, notifications.AutomationActionSummary{
				Action:  action,
				Label:   automationActionLabel(action),
				Applied: counts.applied,
				Failed:  counts.failed,
			})
		}
		sort.Slice(actions, func(i, j int) bool {
			ai := actions[i].Applied + actions[i].Failed
			aj := actions[j].Applied + actions[j].Failed
			if ai == aj {
				return actions[i].Action < actions[j].Action
			}
			return ai > aj
		})

		out = append(out, notifications.AutomationRuleSummary{
			RuleID:   rule.ruleID,
			RuleName: normalizeRuleName(intPtrForRule(rule.ruleID), rule.ruleName),
			Applied:  rule.applied,
			Failed:   rule.failed,
			Actions:  actions,
		})
	}

	return out
}

func (s *Service) notifyAutomationFailure(ctx context.Context, instanceID int, err error) {
	if s == nil || s.notifier == nil || err == nil {
		return
	}

	errorMessage := strings.TrimSpace(err.Error())

	s.notifier.Notify(ctx, notifications.Event{
		Type:         notifications.EventAutomationsRunFailed,
		InstanceID:   instanceID,
		Message:      err.Error(),
		ErrorMessage: errorMessage,
		ErrorMessages: func() []string {
			if errorMessage == "" {
				return nil
			}
			return []string{errorMessage}
		}(),
	})
}

func limitHashBatch(hashes []string, batchSize int) [][]string {
	if batchSize <= 0 || len(hashes) <= batchSize {
		return [][]string{hashes}
	}
	var batches [][]string
	for len(hashes) > 0 {
		end := min(len(hashes), batchSize)
		batches = append(batches, slices.Clone(hashes[:end]))
		hashes = hashes[end:]
	}
	return batches
}

func buildRuleCountsFromHashes(hashes []string, ruleByHash map[string]ruleRef) map[ruleRef]int {
	if len(hashes) == 0 || len(ruleByHash) == 0 {
		return nil
	}
	counts := make(map[ruleRef]int)
	for _, hash := range hashes {
		ref, ok := ruleByHash[hash]
		if !ok {
			continue
		}
		ref.name = strings.TrimSpace(ref.name)
		if ref.id <= 0 && ref.name == "" {
			continue
		}
		counts[ref]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func buildNotifiedAutomationSummary(summary *automationSummary, rules []*models.Automation) *automationSummary {
	if summary == nil || len(summary.rules) == 0 || len(rules) == 0 {
		return nil
	}

	notifyByRuleID := make(map[int]bool, len(rules))
	for _, rule := range rules {
		notifyByRuleID[rule.ID] = rule.Notify
	}

	filtered := newAutomationSummary()
	totalRules := 0
	notifiedRules := 0

	for key, rule := range summary.rules {
		if rule == nil {
			continue
		}
		totalRules++
		if !notifyByRuleID[rule.ruleID] {
			continue
		}
		notifiedRules++

		clonedRule := &automationRuleSummary{
			ruleID:   rule.ruleID,
			ruleName: rule.ruleName,
			applied:  rule.applied,
			failed:   rule.failed,
			actions:  make(map[string]*automationActionCounts, len(rule.actions)),
		}

		for action, counts := range rule.actions {
			if counts == nil {
				continue
			}
			clonedRule.actions[action] = &automationActionCounts{
				applied: counts.applied,
				failed:  counts.failed,
			}
			filtered.appliedByAction[action] += counts.applied
			filtered.failedByAction[action] += counts.failed
		}

		filtered.applied += rule.applied
		filtered.failed += rule.failed
		filtered.rules[key] = clonedRule
	}

	if notifiedRules == 0 {
		return nil
	}

	if notifiedRules == totalRules {
		filtered.sampleTorrents = append([]string(nil), summary.sampleTorrents...)
		filtered.sampleErrors = append([]string(nil), summary.sampleErrors...)
		filtered.tagSamples = append([]string(nil), summary.tagSamples...)
		maps.Copy(filtered.tagAddedByName, summary.tagAddedByName)
		maps.Copy(filtered.tagRemovedByName, summary.tagRemovedByName)
	}

	return filtered
}

func buildRuleCountsFromHashMaps(hashes []string, ruleByHashMaps ...map[string]ruleRef) map[ruleRef]int {
	if len(hashes) == 0 || len(ruleByHashMaps) == 0 {
		return nil
	}

	counts := make(map[ruleRef]int)
	for _, hash := range hashes {
		refsForHash := make(map[ruleRef]struct{})
		for _, ruleByHash := range ruleByHashMaps {
			if len(ruleByHash) == 0 {
				continue
			}
			ref, ok := ruleByHash[hash]
			if !ok {
				continue
			}
			ref.name = strings.TrimSpace(ref.name)
			if ref.id <= 0 && ref.name == "" {
				continue
			}
			refsForHash[ref] = struct{}{}
		}
		for ref := range refsForHash {
			counts[ref]++
		}
	}

	if len(counts) == 0 {
		return nil
	}
	return counts
}

func collectTorrentNamesForHashes(hashes []string, torrentByHash map[string]*qbt.Torrent) []string {
	if len(hashes) == 0 || len(torrentByHash) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		torrent, ok := torrentByHash[hash]
		if !ok {
			continue
		}
		name := strings.TrimSpace(torrent.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sanitizeTrackerHost(urlOrHost string) string {
	clean := strings.TrimSpace(urlOrHost)
	if clean == "" {
		return ""
	}
	if strings.Contains(clean, "://") {
		return ""
	}
	clean = strings.Split(clean, "/")[0]
	clean = strings.Split(clean, ":")[0]
	clean = trackerHostSanitizeRegexp.ReplaceAllString(clean, "")
	return clean
}

func normalizePath(p string) string {
	return pathComparisonNormalizer.Normalize(p)
}

type crossSeedGroups struct {
	byHash  map[string][]*qbt.Torrent
	session *runSession
}

func (g *crossSeedGroups) members(hash string) []*qbt.Torrent {
	if g == nil {
		return nil
	}
	if g.session != nil {
		g.session.ensureGroups(hash)
	}
	return g.byHash[hash]
}

func buildCrossSeedGroups(torrents []*qbt.Torrent, evalCtx *EvalContext) *crossSeedGroups {
	if evalCtx != nil && evalCtx.crossSeeds != nil {
		return evalCtx.crossSeeds
	}
	if evalCtx == nil || len(evalCtx.SameInstanceContentByHash) == 0 {
		return nil
	}
	sharing := evalCtx.SameInstanceContentByHash
	byHash := make(map[string]*qbt.Torrent, len(torrents))
	for _, t := range torrents {
		byHash[t.Hash] = t
	}

	groups := make(map[string][]*qbt.Torrent)
	for _, t := range torrents {
		if _, grouped := groups[t.Hash]; grouped || len(sharing[t.Hash]) == 0 {
			continue
		}
		var members []*qbt.Torrent
		seen := map[string]struct{}{t.Hash: {}}
		queue := []string{t.Hash}
		for len(queue) > 0 {
			hash := queue[0]
			queue = queue[1:]
			if member, ok := byHash[hash]; ok {
				members = append(members, member)
			}
			for _, next := range sharing[hash] {
				if _, ok := seen[next]; !ok {
					seen[next] = struct{}{}
					queue = append(queue, next)
				}
			}
		}
		for _, member := range members {
			groups[member.Hash] = members
		}
	}
	return &crossSeedGroups{byHash: groups}
}

func (g *crossSeedGroups) key(t qbt.Torrent) string {
	if members := g.members(t.Hash); len(members) > 0 {
		return members[0].Hash
	}
	return t.Hash
}

func crossSeedRuleRefsByKey(triggerHashes []string, torrentByHash map[string]*qbt.Torrent, ruleByHash map[string]ruleRef, groups *crossSeedGroups) map[string]ruleRef {
	if len(triggerHashes) == 0 || len(torrentByHash) == 0 || len(ruleByHash) == 0 {
		return nil
	}
	sortedHashes := slices.Clone(triggerHashes)
	sort.Strings(sortedHashes)

	byKey := make(map[string]ruleRef)
	for _, hash := range sortedHashes {
		ref, hasRef := ruleByHash[hash]
		if !hasRef {
			continue
		}
		torrent, hasTorrent := torrentByHash[hash]
		if !hasTorrent {
			continue
		}
		key := groups.key(*torrent)
		if _, exists := byKey[key]; exists {
			continue
		}
		byKey[key] = ref
	}
	if len(byKey) == 0 {
		return nil
	}
	return byKey
}

func inheritRuleRefForCrossSeed(expandedHash, key string, ruleByHash map[string]ruleRef, ruleByCrossSeedKey map[string]ruleRef) {
	if strings.TrimSpace(expandedHash) == "" || len(ruleByHash) == 0 || len(ruleByCrossSeedKey) == 0 {
		return
	}
	if _, exists := ruleByHash[expandedHash]; exists {
		return
	}
	ref, ok := ruleByCrossSeedKey[key]
	if !ok {
		return
	}
	ruleByHash[expandedHash] = ref
}

func inheritRuleRefForMoveGroup(expandedHash, triggerHash string, ruleByHash map[string]ruleRef) {
	if strings.TrimSpace(expandedHash) == "" || strings.TrimSpace(triggerHash) == "" || len(ruleByHash) == 0 {
		return
	}
	ref, ok := ruleByHash[triggerHash]
	if !ok {
		return
	}
	ruleByHash[expandedHash] = ref
}

func (s *Service) loadCrossSeedFiles(ctx context.Context, instanceID int, groups *crossSeedGroups, evalCtx *EvalContext, hardlinks *HardlinkIndex) {
	if groups != nil && groups.session != nil {
		groups.session.loadFiles(ctx, hardlinks)
		return
	}
	if groups == nil && hardlinks == nil {
		return
	}
	var hashes []string
	if groups != nil {
		hashes = slices.Sorted(maps.Keys(groups.byHash))
	}
	if hardlinks != nil {
		for _, group := range hardlinks.DeleteSafeGroupBySignature {
			hashes = append(hashes, group...)
		}
		slices.Sort(hashes)
		hashes = slices.Compact(hashes)
	}

	filesByHash, err := s.filesReader.GetTorrentFilesBatch(ctx, instanceID, hashes)
	evalCtx.CrossSeedFilesByHash = filesByHash
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).
			Msg("automations: some cross-seed files could not be loaded")
	}
}

type categoryExpansion struct {
	hash    string
	trigger string
}

func categoryCrossSeedExpansion(category string, batch []string, states map[string]*torrentDesiredState, crossSeeds *crossSeedGroups, deleting map[string]selectedDeletion) []categoryExpansion {
	taken := make(map[string]struct{}, len(batch))
	for _, hash := range batch {
		taken[hash] = struct{}{}
	}
	var added []categoryExpansion
	for _, hash := range batch {
		state := states[hash]
		if state == nil || !state.categoryIncludeCrossSeeds || state.categoryGroupID != "" {
			continue
		}
		for _, member := range crossSeeds.members(hash) {
			if _, ok := taken[member.Hash]; ok || member.Category == category {
				continue
			}
			if _, ok := deleting[member.Hash]; ok {
				continue
			}
			if own := states[member.Hash]; own != nil && own.category != nil && *own.category != category {
				continue
			}
			taken[member.Hash] = struct{}{}
			added = append(added, categoryExpansion{hash: member.Hash, trigger: hash})
		}
	}
	return added
}

func cachedTorrentFilesFetcher(filesByHash map[string]qbt.TorrentFiles) torrentFilesFetcher {
	return func(_ []string) (map[string]qbt.TorrentFiles, error) {
		return filesByHash, nil
	}
}

type fileOverlapIndex struct {
	sizeByPath map[string]int64
	totalBytes int64
}

const minFileOverlapPercent = 90

const overlapUnknown = -1

func resolvedTorrentFilePath(torrent qbt.Torrent, fileName string) string {
	savePath := normalizePath(torrent.SavePath)
	relativePath := strings.TrimPrefix(normalizePath(fileName), "/")
	if savePath == "" {
		return relativePath
	}
	if relativePath == "" {
		return savePath
	}
	return savePath + "/" + relativePath
}

func buildFileOverlapIndex(torrent qbt.Torrent, files qbt.TorrentFiles) fileOverlapIndex {
	index := fileOverlapIndex{sizeByPath: make(map[string]int64, len(files))}
	for _, file := range files {
		index.sizeByPath[resolvedTorrentFilePath(torrent, file.Name)] = file.Size
		index.totalBytes += file.Size
	}
	return index
}

func fileOverlapPercent(triggerFiles fileOverlapIndex, candidate qbt.Torrent, candidateFiles qbt.TorrentFiles) int64 {
	if len(triggerFiles.sizeByPath) == 0 || len(candidateFiles) == 0 {
		return overlapUnknown
	}

	var candidateBytes, matchedBytes int64
	for _, file := range candidateFiles {
		candidateBytes += file.Size
		if triggerSize, exists := triggerFiles.sizeByPath[resolvedTorrentFilePath(candidate, file.Name)]; exists {
			matchedBytes += min(triggerSize, file.Size)
		}
	}

	smallerBytes := min(triggerFiles.totalBytes, candidateBytes)
	if smallerBytes <= 0 {
		return overlapUnknown
	}
	if matchedBytes > 0 {
		return max(1, (matchedBytes*100)/smallerBytes)
	}
	return 0
}

func crossSeedGroupMembers(
	trigger qbt.Torrent,
	group []*qbt.Torrent,
	alreadyIncludedHashes map[string]struct{},
	fetchFiles func(hashes []string) (map[string]qbt.TorrentFiles, error),
) ([]string, bool) {
	candidates := make([]*qbt.Torrent, 0, len(group))
	for _, other := range group {
		if other.Hash == trigger.Hash {
			continue
		}
		if _, processed := alreadyIncludedHashes[other.Hash]; processed {
			continue
		}
		candidates = append(candidates, other)
	}
	if len(candidates) == 0 {
		return []string{trigger.Hash}, true
	}

	hashes := make([]string, 0, len(candidates)+1)
	hashes = append(hashes, trigger.Hash)
	for _, candidate := range candidates {
		hashes = append(hashes, candidate.Hash)
	}
	filesByHash, err := fetchFiles(hashes)
	if err != nil {
		log.Warn().Err(err).Str("hash", trigger.Hash).
			Msg("automations: skipping cross-seed group, could not read its files")
		return nil, false
	}

	verified := []string{trigger.Hash}
	verifiedFiles := []fileOverlapIndex{buildFileOverlapIndex(trigger, filesByHash[trigger.Hash])}
	for joined := true; joined; {
		joined = false
		pending := candidates[:0]
		for _, candidate := range candidates {
			shares := false
			for _, memberFiles := range verifiedFiles {
				percent := fileOverlapPercent(memberFiles, *candidate, filesByHash[candidate.Hash])
				switch {
				case percent >= minFileOverlapPercent:
					shares = true
				case percent != 0:
					log.Warn().Str("hash", trigger.Hash).Str("otherHash", candidate.Hash).Int64("overlapPercent", percent).
						Msg("automations: skipping entire group due to partial file overlap")
					return nil, false
				}
			}
			if !shares {
				pending = append(pending, candidate)
				continue
			}
			verified = append(verified, candidate.Hash)
			verifiedFiles = append(verifiedFiles, buildFileOverlapIndex(*candidate, filesByHash[candidate.Hash]))
			joined = true
		}
		candidates = pending
	}
	for _, candidate := range candidates {
		log.Debug().Str("hash", trigger.Hash).Str("otherHash", candidate.Hash).
			Str("contentPath", trigger.ContentPath).
			Msg("automations: excluding torrent that shares the content path but no files")
	}
	return verified, true
}

func shouldPreserveCrossSeedFiles(torrent qbt.Torrent, crossSeeds *crossSeedGroups, filesByHash map[string]qbt.TorrentFiles) bool {
	verifiedHashes, ok := crossSeedGroupMembers(
		torrent,
		crossSeeds.members(torrent.Hash),
		nil,
		cachedTorrentFilesFetcher(filesByHash),
	)
	return !ok || len(verifiedHashes) > 1
}

func (s *Service) verifyFileOverlap(ctx context.Context, instanceID int, torrent1, torrent2 qbt.Torrent, minOverlapPercent int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	filesByHash, err := s.filesReader.GetTorrentFilesBatch(ctx, instanceID, []string{torrent1.Hash, torrent2.Hash})
	if err != nil {
		return false, fmt.Errorf("failed to fetch files: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}

	if minOverlapPercent <= 0 {
		minOverlapPercent = minFileOverlapPercent
	}
	percent := fileOverlapPercent(buildFileOverlapIndex(torrent1, filesByHash[torrent1.Hash]), torrent2, filesByHash[torrent2.Hash])
	if percent == overlapUnknown {
		return false, errors.New("cannot compute overlap: missing or zero-size file lists")
	}
	return percent >= int64(minOverlapPercent), nil
}

func expandTagStatesForCrossSeeds(states map[string]*torrentDesiredState, ruleByID map[int]*models.Automation, crossSeeds *crossSeedGroups) {
	var triggerHashes []string
	for hash, state := range states {
		if state != nil && len(state.tagExpandByTag) > 0 {
			triggerHashes = append(triggerHashes, hash)
		}
	}
	if len(triggerHashes) == 0 {
		return
	}
	sort.Strings(triggerHashes)

	exp := tagExpansion{states: states, rulePos: rulePositions(ruleByID), injected: make(map[tagInjectionKey]tagInjection)}
	for _, hash := range triggerHashes {
		state := states[hash]
		for _, tag := range slices.Sorted(maps.Keys(state.tagExpandByTag)) {
			intent := state.tagExpandByTag[tag]
			if ruleByID[intent.rule.id] == nil {
				continue
			}
			for _, sibTorrent := range crossSeeds.members(hash) {
				if sibTorrent.Hash != hash {
					exp.inject(*sibTorrent, tag, intent)
				}
			}
		}
	}
}

func rulePositions(ruleByID map[int]*models.Automation) map[int]int {
	ordered := slices.SortedFunc(maps.Values(ruleByID), func(a, b *models.Automation) int {
		if a.SortOrder != b.SortOrder {
			return a.SortOrder - b.SortOrder
		}
		return a.ID - b.ID
	})
	pos := make(map[int]int, len(ordered))
	for i, rule := range ordered {
		pos[rule.ID] = i
	}
	return pos
}

type tagInjectionKey struct {
	hash string
	tag  string
}

type tagInjection struct {
	pos    int
	action string
}

type tagExpansion struct {
	states   map[string]*torrentDesiredState
	rulePos  map[int]int
	injected map[tagInjectionKey]tagInjection
}

func (e *tagExpansion) inject(sibTorrent qbt.Torrent, tag string, intent tagExpandIntent) {
	sibState := e.states[sibTorrent.Hash]
	if sibState == nil {
		sibState = &torrentDesiredState{
			hash:         sibTorrent.Hash,
			name:         sibTorrent.Name,
			currentTags:  parseTorrentTags(sibTorrent.Tags),
			tagActions:   make(map[string]string),
			tagRuleByTag: make(map[string]ruleRef),
		}
		e.states[sibTorrent.Hash] = sibState
	}
	key := tagInjectionKey{hash: sibTorrent.Hash, tag: tag}
	pos := e.rulePos[intent.rule.id]
	prev, wasInjected := e.injected[key]
	if wasInjected && pos < prev.pos {
		return
	}
	if wasInjected && pos == prev.pos && prev.action == "add" && intent.action == "remove" {
		return
	}
	if ownRule, hasOwn := sibState.tagRuleByTag[tag]; hasOwn && !wasInjected {
		if ownRule.id != intent.rule.id {
			return
		}
		if sibState.tagActions[tag] == intent.action || intent.action == "remove" {
			return
		}
	}
	e.injected[key] = tagInjection{pos: pos, action: intent.action}

	_, sibHasTag := sibState.currentTags[tag]
	_, resetPending := sibState.tagResetTags[tag]
	reset := intent.resetFromClient || resetPending
	if reset {
		sibHasTag = false
	}
	if (intent.action == "add") == sibHasTag {
		delete(sibState.tagActions, tag)
		delete(sibState.tagRuleByTag, tag)
		return
	}
	sibState.tagActions[tag] = intent.action
	sibState.tagRuleByTag[tag] = intent.rule
	markTagReset(sibState, tag, reset && intent.action == "add")
}

func (s *Service) shouldExpandGroupWithAmbiguityPolicy(
	ctx context.Context,
	instanceID int,
	rule *models.Automation,
	groupID string,
	idx *groupIndex,
	triggerHash string,
	torrentByHash map[string]*qbt.Torrent,
) bool {
	expand, verifyOverlap, minPercent := s.groupAmbiguityPolicy(rule, groupID, idx, triggerHash)
	if !expand || !verifyOverlap {
		return expand
	}
	return s.triggerOverlapsGroup(ctx, instanceID, idx, triggerHash, torrentByHash, minPercent)
}

func (s *Service) groupAmbiguityPolicy(rule *models.Automation, groupID string, idx *groupIndex, triggerHash string) (expand, verifyOverlap bool, minPercent int) {
	if idx == nil || triggerHash == "" {
		return true, false, 0
	}
	if !idx.IsAmbiguousForHash(triggerHash) {
		return true, false, 0
	}

	def := (*models.GroupDefinition)(nil)
	if rule != nil && rule.Conditions != nil && rule.Conditions.Grouping != nil {
		def = findGroupDefinition(rule.Conditions.Grouping, groupID)
	}
	if def == nil {
		def = builtinGroupDefinition(groupID)
	}
	if def == nil || !containsKey(def.Keys, groupKeyContentPath) {
		return true, false, 0
	}

	policy := strings.TrimSpace(def.AmbiguousPolicy)
	if policy == "" {
		policy = groupAmbiguousVerifyOverlap
	}
	if policy == groupAmbiguousSkip {
		return false, false, 0
	}

	if s == nil || s.filesReader == nil {
		return false, false, 0
	}

	minPercent = def.MinFileOverlapPercent
	if minPercent <= 0 {
		minPercent = minFileOverlapPercent
	}
	return true, true, minPercent
}

func (s *Service) triggerOverlapsGroup(ctx context.Context, instanceID int, idx *groupIndex, triggerHash string, torrentByHash map[string]*qbt.Torrent, minPercent int) bool {
	triggerTorrent, ok := torrentByHash[triggerHash]
	if !ok {
		return false
	}
	for _, otherHash := range idx.MembersForHash(triggerHash) {
		if otherHash == triggerHash {
			continue
		}
		otherTorrent, ok := torrentByHash[otherHash]
		if !ok {
			return false
		}
		hasOverlap, err := s.verifyFileOverlap(ctx, instanceID, *triggerTorrent, *otherTorrent, minPercent)
		if err != nil || !hasOverlap {
			return false
		}
	}
	return true
}

func groupHoldsSharedFiles(members []string, crossSeeds *crossSeedGroups) bool {
	inGroup := make(map[string]struct{}, len(members))
	for _, hash := range members {
		inGroup[hash] = struct{}{}
	}
	for _, hash := range members {
		for _, peer := range crossSeeds.members(hash) {
			if _, ok := inGroup[peer.Hash]; !ok {
				return false
			}
		}
	}
	return true
}

func allGroupMembersMatchCondition(members []string, torrentByHash map[string]*qbt.Torrent, cond *models.RuleCondition, evalCtx *EvalContext) bool {
	if len(members) == 0 {
		return false
	}
	for _, memberHash := range members {
		memberTorrent, ok := torrentByHash[memberHash]
		if !ok {
			return false
		}
		if cond != nil && !EvaluateConditionWithContext(cond, *memberTorrent, evalCtx, 0) {
			return false
		}
	}
	return true
}

func allGroupMembersMatchCategoryAction(
	members []string,
	torrentByHash map[string]*qbt.Torrent,
	catAction categoryActionConfig,
	evalCtx *EvalContext,
	crossSeeds *crossSeedGroups,
) bool {
	if len(members) == 0 {
		return false
	}
	for _, memberHash := range members {
		memberTorrent, ok := torrentByHash[memberHash]
		if !ok {
			return false
		}
		if !shouldApplyCategoryAction(memberTorrent, catAction, evalCtx, crossSeeds) {
			return false
		}
	}
	return true
}

func deleteFreesSpace(
	mode string,
	torrent qbt.Torrent,
	crossSeeds *crossSeedGroups,
	filesByHash map[string]qbt.TorrentFiles,
) bool {
	switch mode {
	case DeleteModeKeepFiles, DeleteModeNone, "":
		return false
	case DeleteModeWithFilesPreserveCrossSeeds:
		return !shouldPreserveCrossSeedFiles(torrent, crossSeeds, filesByHash)
	case DeleteModeWithFiles, DeleteModeWithFilesIncludeCrossSeeds:
		return true
	default:
		log.Warn().Str("mode", mode).Msg("automations: unknown delete mode, not counting toward free space projection")
		return false
	}
}

func ruleUsesCondition(rule *models.Automation, field ConditionField) bool {
	if rule == nil {
		return false
	}
	if actionConditionsUseField(rule.Conditions, field) {
		return true
	}

	return sortingConfigUsesField(rule.SortingConfig, field)
}

func actionConditionsUseField(ac *models.ActionConditions, field ConditionField) bool {
	for c := range ac.Conditions() {
		if c.Enabled && conditionTreeUsesField(c.Condition, field) {
			return true
		}
	}
	return false
}

func conditionTreeUsesField(cond *models.RuleCondition, field ConditionField) bool {
	return cond != nil && ConditionUsesField(cond, field)
}

func sortingConfigUsesField(config *models.SortingConfig, field ConditionField) bool {
	if config == nil {
		return false
	}

	if config.Type == models.SortingTypeSimple {
		return config.Field == field
	}

	if config.Type != models.SortingTypeScore {
		return false
	}

	for i := range config.ScoreRules {
		if scoreRuleUsesField(config.ScoreRules[i], field) {
			return true
		}
	}

	return false
}

func scoreRuleUsesField(rule models.ScoreRule, field ConditionField) bool {
	if rule.FieldMultiplier != nil && rule.FieldMultiplier.Field == field {
		return true
	}
	if rule.Conditional != nil && ConditionUsesField(rule.Conditional.Condition, field) {
		return true
	}

	return false
}

func (s *Service) loadCategorySavePaths(ctx context.Context, instanceID int) map[string]string {
	categories, err := s.syncManager.GetCategories(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to load categories for category save paths")
		return nil
	}
	prefs, err := s.syncManager.GetAppPreferences(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to load default save path for category save paths")
		return nil
	}
	rule, err := s.syncManager.CategoryPathRule(ctx, instanceID, &prefs)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to check subcategory paths for category save paths")
		return nil
	}
	return buildCategorySavePaths(categories, prefs.SavePath, rule)
}

func (s *Service) trackersForRule(ctx context.Context, instanceID int, torrents []*qbt.Torrent, rule *models.Automation) (qbittorrent.TrackerLookup, error) {
	if s == nil || s.syncManager == nil || rule == nil {
		return nil, nil
	}
	needs := previewNeeds(rule)
	if !needs.TrackerEntries && !needs.TrackerURLs {
		return nil, nil
	}

	trackers, err := s.readTrackers(ctx, instanceID, torrents, needs)
	if err != nil {
		return nil, err
	}
	if trackerDataMissing(torrents, trackers) {
		log.Debug().
			Int("instanceID", instanceID).
			Str("rule", rule.Name).
			Int("torrents", len(torrents)).
			Msg("automations: no tracker data for any torrent, tracker conditions will not match")
	}
	return trackers, nil
}

func (s *Service) readTrackers(ctx context.Context, instanceID int, torrents []*qbt.Torrent, needs RuleNeeds) (qbittorrent.TrackerLookup, error) {
	if needs.TrackerEntries {
		return s.syncManager.TrackerDetails(ctx, instanceID, torrents)
	}
	return s.syncManager.TrackerURLs(ctx, instanceID), nil
}

func trackerDataMissing(torrents []*qbt.Torrent, trackers qbittorrent.TrackerLookup) bool {
	return len(torrents) > 0 && !slices.ContainsFunc(torrents, func(t *qbt.Torrent) bool { return len(trackers.Trackers(t.Hash)) > 0 })
}

func ruleUsesIncludeCrossSeedsDelete(rule *models.Automation) bool {
	if rule == nil || rule.Conditions == nil {
		return false
	}
	deleteAction := rule.Conditions.Delete
	return deleteAction != nil && deleteAction.Enabled && deleteAction.Mode == DeleteModeWithFilesIncludeCrossSeeds
}

func ruleUsesIncludeCrossSeedsFreeSpace(rule *models.Automation) bool {
	return ruleUsesIncludeCrossSeedsDelete(rule) && ConditionUsesField(rule.Conditions.Delete.Condition, FieldFreeSpace)
}

func ruleNeedsCrossSeedFiles(rule *models.Automation) bool {
	if rule == nil || rule.Conditions == nil {
		return false
	}
	deleteAction := rule.Conditions.Delete
	if deleteAction == nil || !deleteAction.Enabled {
		return false
	}
	return deleteAction.Mode == DeleteModeWithFilesIncludeCrossSeeds ||
		deleteAction.Mode == DeleteModeWithFilesPreserveCrossSeeds ||
		(deleteAction.Mode == DeleteModeWithFiles && ConditionUsesField(deleteAction.Condition, FieldFreeSpace))
}

func rulesNeedCrossSeedFiles(rules []*models.Automation) bool {
	return slices.ContainsFunc(rules, ruleNeedsCrossSeedFiles)
}

func otherInstanceKeepsFiles(evalCtx *EvalContext, hash string) (logMsg string, keep bool) {
	if _, shared := evalCtx.CrossInstanceSharedHashSet[hash]; shared {
		return "automations: removing torrent (a torrent on another instance shares the files - keeping files)", true
	}
	if _, unknown := evalCtx.CrossInstanceSharedUnknown[hash]; !unknown {
		return "", false
	}
	if len(evalCtx.OfflineLocalInstances) == 0 {
		return "automations: removing torrent (could not check other instances - keeping files)", true
	}
	return "automations: removing torrent (could not check instance " + strings.Join(evalCtx.OfflineLocalInstances, ", ") + " - keeping files)", true
}

func rulesNeedHardlinkSignatureMap(rules []*models.Automation) bool {
	for _, rule := range rules {
		if rule.Conditions == nil || !rule.Enabled {
			continue
		}
		del := rule.Conditions.Delete
		if del == nil || !del.Enabled || !del.IncludeHardlinks {
			continue
		}
		if del.Mode != DeleteModeWithFilesIncludeCrossSeeds {
			continue
		}
		if ConditionUsesField(del.Condition, FieldFreeSpace) {
			return true
		}
	}
	return false
}

func ruleUsesHardlinkSignatureGrouping(rule *models.Automation) bool {
	if rule == nil || rule.Conditions == nil {
		return false
	}

	grouping := rule.Conditions.Grouping

	groupUsesHardlinkSignature := func(groupID string) bool {
		id := strings.TrimSpace(groupID)
		if id == "" {
			return false
		}
		if id == GroupHardlinkSignature {
			return true
		}
		def := findGroupDefinition(grouping, id)
		return def != nil && containsKey(def.Keys, groupKeyHardlinkSignature)
	}

	if grouping != nil && groupUsesHardlinkSignature(grouping.DefaultGroupID) {
		return true
	}

	if rule.Conditions.Delete != nil && groupUsesHardlinkSignature(rule.Conditions.Delete.GroupID) {
		return true
	}
	if rule.Conditions.Category != nil && groupUsesHardlinkSignature(rule.Conditions.Category.GroupID) {
		return true
	}
	if rule.Conditions.Move != nil && groupUsesHardlinkSignature(rule.Conditions.Move.GroupID) {
		return true
	}

	var conditionTreeUsesHardlinkSignature func(cond *models.RuleCondition) bool
	conditionTreeUsesHardlinkSignature = func(cond *models.RuleCondition) bool {
		if cond == nil {
			return false
		}
		if (cond.Field == models.FieldGroupSize || cond.Field == models.FieldIsGrouped) &&
			groupUsesHardlinkSignature(cond.GroupID) {
			return true
		}
		return slices.ContainsFunc(cond.Conditions, conditionTreeUsesHardlinkSignature)
	}

	return slices.ContainsFunc(conditionTreesForRule(rule), conditionTreeUsesHardlinkSignature)
}

func buildTrackerDisplayNameMap(customizations []*models.TrackerCustomization) map[string]string {
	result := make(map[string]string)
	for _, c := range customizations {
		for _, domain := range c.Domains {
			result[normalizeLower(domain)] = c.DisplayName
		}
	}
	return result
}

func buildFullPath(d fsops.PathDialect, basePath, fileName string) (string, bool) {
	base := d.FromSlash(basePath)
	if base == "" || !d.IsAbs(base) {
		return "", false
	}
	if fileName == "" || strings.ContainsRune(fileName, '\\') ||
		strings.HasPrefix(fileName, "/") || hasWindowsDrivePrefix(fileName) {
		return "", false
	}
	cleaned := path.Clean(fileName)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return d.Join(base, d.FromSlash(cleaned)), true
}

func hasWindowsDrivePrefix(p string) bool {
	return len(p) >= 2 && p[1] == ':' &&
		(('a' <= p[0] && p[0] <= 'z') || ('A' <= p[0] && p[0] <= 'Z'))
}

func (s *Service) recordDryRunNoMatch(ctx context.Context, instanceID int) []*models.AutomationActivity {
	if s == nil || s.activityStore == nil {
		return nil
	}

	detailsJSON, _ := json.Marshal(map[string]int{"count": 0}) //nolint:errchkjson // string keys and int values cannot fail to encode
	activity := &models.AutomationActivity{
		InstanceID: instanceID,
		Hash:       "",
		Action:     models.ActivityActionDryRunNoMatch,
		Outcome:    models.ActivityOutcomeDryRun,
		Details:    detailsJSON,
		CreatedAt:  time.Now().UTC(),
	}

	activityID, err := s.activityStore.CreateWithID(ctx, activity)
	if err != nil {
		return nil
	}
	activity.ID = activityID
	return []*models.AutomationActivity{activity}
}

func (s *Service) recordDryRunActivities(
	ctx context.Context,
	instanceID int,
	plan runPlan,
	torrentByHash map[string]*qbt.Torrent,
	scope *ruleScope,
	recordNoMatch bool,
) []*models.AutomationActivity {
	if s.activityStore == nil {
		return nil
	}

	r := &stepRun{s: s, instanceID: instanceID, torrentByHash: torrentByHash, scope: scope, dryRun: true}
	r.runSteps(ctx, &plan)
	createActivity := func(action string, details map[string]any, buildItems func() []ActivityRunTorrent) {
		r.record(ctx, stepActivity{action: action, details: details, items: buildItems})
	}

	deletions := plan.deletions.byHash

	if len(plan.programs) > 0 {
		hashesByProgram := make(map[int][]string)
		for _, exec := range plan.programs {
			hashesByProgram[exec.programID] = append(hashesByProgram[exec.programID], exec.hash)
		}
		for programID, hashes := range hashesByProgram {
			uniqueHashes := dedupeHashes(hashes)
			createActivity(externalprograms.ActivityActionExternalProgram, map[string]any{"programId": programID, "count": len(uniqueHashes)}, func() []ActivityRunTorrent {
				return buildRunItemsFromHashes(uniqueHashes, torrentByHash, scope)
			})
		}
	}

	if len(plan.exports) > 0 {
		successByTarget := make(map[int][]string)
		failedByTarget := make(map[int][]string)
		existsByTarget := make(map[int][]string)
		for _, exec := range plan.exports {
			switch exec.mark {
			case exportAlreadyOnTarget:
				existsByTarget[exec.action.TargetInstanceID] = append(existsByTarget[exec.action.TargetInstanceID], exec.hash)
			case exportCheckFailed, exportSavePathUnresolved:
				failedByTarget[exec.action.TargetInstanceID] = append(failedByTarget[exec.action.TargetInstanceID], exec.hash)
			default:
				successByTarget[exec.action.TargetInstanceID] = append(successByTarget[exec.action.TargetInstanceID], exec.hash)
			}
		}
		for targetID, hashes := range successByTarget {
			uniqueHashes := dedupeHashes(hashes)
			createActivity(models.ActivityActionExportedToInstance, map[string]any{"targetInstanceId": targetID, "count": len(uniqueHashes)}, func() []ActivityRunTorrent {
				return buildRunItemsFromHashes(uniqueHashes, torrentByHash, scope)
			})
		}
		for targetID, hashes := range existsByTarget {
			uniqueHashes := dedupeHashes(hashes)
			createActivity(models.ActivityActionExportedToInstance, map[string]any{"targetInstanceId": targetID, "count": len(uniqueHashes), "alreadyOnTarget": true}, func() []ActivityRunTorrent {
				return buildRunItemsFromHashes(uniqueHashes, torrentByHash, scope)
			})
		}
		for targetID, hashes := range failedByTarget {
			uniqueHashes := dedupeHashes(hashes)
			createActivity(models.ActivityActionExportedToInstance, map[string]any{"targetInstanceId": targetID, "count": len(uniqueHashes), "preflightFailed": true}, func() []ActivityRunTorrent {
				return buildRunItemsFromHashes(uniqueHashes, torrentByHash, scope)
			})
		}
	}

	if len(deletions) > 0 {
		hashes := slices.Collect(maps.Keys(deletions))
		createActivity(models.ActivityActionDeletedCondition, map[string]any{"count": len(hashes)}, func() []ActivityRunTorrent {
			return buildRunItemsFromHashes(hashes, torrentByHash, scope)
		})
	}

	if len(plan.transfers) > 0 {
		s.recordDryRunTransfers(ctx, r, plan.transfers)
	}

	if recordNoMatch && len(r.activities) == 0 {
		return s.recordDryRunNoMatch(ctx, instanceID)
	}

	return r.activities
}

func buildRunItemFromHash(hash string, torrentByHash map[string]*qbt.Torrent, scope *ruleScope) ActivityRunTorrent {
	item := ActivityRunTorrent{Hash: hash}
	if t, ok := torrentByHash[hash]; ok {
		item.Name = t.Name
		size := t.Size
		ratio := t.Ratio
		addedOn := t.AddedOn
		item.Size = &size
		item.Ratio = &ratio
		item.AddedOn = &addedOn
		item.TrackerDomain = scope.tracker(t)
	}
	return item
}

func buildRunItemsFromHashes(hashes []string, torrentByHash map[string]*qbt.Torrent, scope *ruleScope) []ActivityRunTorrent {
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
		items = append(items, buildRunItemFromHash(hash, torrentByHash, scope))
	}
	sortActivityRunItems(items)
	return items
}

func buildTagRunItems(tagChanges map[string]*tagChange, torrentByHash map[string]*qbt.Torrent, scope *ruleScope) []ActivityRunTorrent {
	items := make([]ActivityRunTorrent, 0, len(tagChanges))
	for hash, change := range tagChanges {
		if len(change.toAdd) == 0 && len(change.toRemove) == 0 {
			continue
		}
		item := buildRunItemFromHash(hash, torrentByHash, scope)
		item.TagsAdded = slices.Clone(change.toAdd)
		item.TagsRemoved = slices.Clone(change.toRemove)
		slices.Sort(item.TagsAdded)
		slices.Sort(item.TagsRemoved)
		items = append(items, item)
	}
	sortActivityRunItems(items)
	return items
}

func buildCategoryRunItems(byCategory map[string][]string, torrentByHash map[string]*qbt.Torrent, scope *ruleScope) []ActivityRunTorrent {
	items := make([]ActivityRunTorrent, 0, len(byCategory))
	for category, hashes := range byCategory {
		for _, hash := range hashes {
			item := buildRunItemFromHash(hash, torrentByHash, scope)
			item.Category = category
			items = append(items, item)
		}
	}
	sortActivityRunItems(items)
	return items
}

func buildSpeedLimitRunItems(
	uploadSuccess map[int64][]string,
	downloadSuccess map[int64][]string,
	torrentByHash map[string]*qbt.Torrent,
	scope *ruleScope,
) []ActivityRunTorrent {
	itemMap := make(map[string]*ActivityRunTorrent)

	getItem := func(hash string) *ActivityRunTorrent {
		if item, ok := itemMap[hash]; ok {
			return item
		}
		item := buildRunItemFromHash(hash, torrentByHash, scope)
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
	torrentByHash map[string]*qbt.Torrent,
	scope *ruleScope,
) []ActivityRunTorrent {
	itemMap := make(map[string]*ActivityRunTorrent)

	getItem := func(hash string) *ActivityRunTorrent {
		if item, ok := itemMap[hash]; ok {
			return item
		}
		item := buildRunItemFromHash(hash, torrentByHash, scope)
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
	torrentByHash map[string]*qbt.Torrent,
	scope *ruleScope,
) []ActivityRunTorrent {
	itemMap := make(map[string]*ActivityRunTorrent)

	getItem := func(hash string) *ActivityRunTorrent {
		if item, ok := itemMap[hash]; ok {
			return item
		}
		item := buildRunItemFromHash(hash, torrentByHash, scope)
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
		nameA := normalizeLower(items[i].Name)
		nameB := normalizeLower(items[j].Name)
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

func collectManagedTagsForClientReset(rules []*models.Automation) []string {
	if len(rules) == 0 {
		return nil
	}

	unique := make(map[string]struct{})
	for _, rule := range rules {
		if rule == nil || !rule.Enabled || rule.Conditions == nil {
			continue
		}
		for _, action := range rule.Conditions.TagActions() {
			if !shouldResetTagActionInClient(action) {
				continue
			}
			for _, tag := range models.SanitizeCommaSeparatedStringSlice(action.Tags) {
				if tag == "" {
					continue
				}
				unique[tag] = struct{}{}
			}
		}
	}

	if len(unique) == 0 {
		return nil
	}

	tags := make([]string, 0, len(unique))
	for tag := range unique {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
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

type pendingProgramExec struct {
	hash      string
	torrent   qbt.Torrent
	programID int
	ruleID    int
	ruleName  string
}

func (s *Service) executeExternalProgramsFromAutomation(_ context.Context, instanceID int, executions []pendingProgramExec) {
	if len(executions) == 0 {
		return
	}

	if s.externalProgramService == nil {
		log.Error().
			Int("instanceID", instanceID).
			Int("pendingExecutions", len(executions)).
			Msg("external program service not initialized, skipping executions")

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
		torrent := exec.torrent
		ruleID := exec.ruleID
		programID := exec.programID
		ruleName := exec.ruleName

		go func() { //nolint:gosec // G118: external program runs past the automation pass that queued it
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

type pendingExportToInstance struct {
	hash             string
	torrent          qbt.Torrent
	trackerDomain    string
	action           *models.ExportToInstanceAction
	resolvedSavePath string
	ruleID           int
	ruleName         string
	mark             exportMark
	failureReason    string
}

func (s *Service) reserveExports(exports []pendingExportToInstance) []pendingExportToInstance {
	reserved := make([]pendingExportToInstance, 0, len(exports))
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, export := range exports {
		if export.mark == exportAlreadyOnTarget {
			continue
		}
		if export.mark == exportReady {
			flightKey := fmt.Sprintf("%d:%s", export.action.TargetInstanceID, export.hash)
			if _, alreadyInFlight := s.inFlightExports[flightKey]; alreadyInFlight {
				log.Debug().
					Str("hash", export.hash).
					Int("targetInstanceID", export.action.TargetInstanceID).
					Msg("automations: export already in-flight, skipping")
				continue
			}
			s.inFlightExports[flightKey] = struct{}{}
		}
		reserved = append(reserved, export)
	}
	return reserved
}

func (s *Service) executeExportToInstance(ctx context.Context, sourceInstanceID int, executions []pendingExportToInstance) <-chan *models.AutomationActivity {
	ch := make(chan *models.AutomationActivity, len(executions))
	if len(executions) == 0 {
		close(ch)
		return ch
	}

	targetCounts := make(map[int]int)
	for _, exec := range executions {
		targetCounts[exec.action.TargetInstanceID]++
	}

	log.Debug().
		Int("instanceID", sourceInstanceID).
		Int("executions", len(executions)).
		Interface("targetCounts", targetCounts).
		Msg("automations: exporting torrents to instances")

	var wg sync.WaitGroup

	for _, exec := range executions {
		if exec.failureReason != "" {
			ruleID := exec.ruleID
			detailsJSON, _ := json.Marshal(map[string]int{ //nolint:errchkjson // string keys and int values cannot fail to encode
				"targetInstanceId": exec.action.TargetInstanceID,
				"count":            1,
			})
			trackerDomain := exec.trackerDomain
			activity := &models.AutomationActivity{
				InstanceID:    sourceInstanceID,
				Hash:          exec.hash,
				TorrentName:   exec.torrent.Name,
				TrackerDomain: trackerDomain,
				Action:        models.ActivityActionExportedToInstance,
				RuleID:        &ruleID,
				RuleName:      exec.ruleName,
				Outcome:       models.ActivityOutcomeFailed,
				Reason:        exec.failureReason,
				Details:       detailsJSON,
			}
			if s.activityStore != nil {
				if err := s.activityStore.Create(context.Background(), activity); err != nil {
					log.Warn().Err(err).Str("hash", exec.hash).Msg("automations: failed to log export activity")
				}
			}
			ch <- activity
			continue
		}

		wg.Go(func() { s.exportOnTransfers(ctx, sourceInstanceID, exec, ch) })
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch
}

func SortTorrentsWithFallback(torrents []*qbt.Torrent, config *models.SortingConfig, evalCtx *EvalContext, instanceID int, ruleName string) {
	if err := SortTorrents(torrents, config, evalCtx); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Str("rule", ruleName).Msg("invalid sorting config, falling back to default sort")
		_ = SortTorrents(torrents, nil, evalCtx)
	}
}

func rulesWithTorrents(rules []*models.Automation, torrents []*qbt.Torrent, scope *ruleScope) (used map[int]struct{}, considered int) {
	used = make(map[int]struct{})
	for _, torrent := range torrents {
		inSkipWindow := false
		for _, rule := range rules {
			answer := scope.answer(rule, torrent)
			if answer == scopeSkipWindow {
				inSkipWindow = true
			} else if answer != scopeOtherTracker {
				used[rule.ID] = struct{}{}
			}
		}
		if !inSkipWindow {
			considered++
		}
	}
	return used, considered
}

func executeBatch(
	instanceID int,
	currentBatch []*models.Automation,
	torrents []*qbt.Torrent,
	evalCtx *EvalContext,
	crossSeeds *crossSeedGroups,
	scope *ruleScope,
	ruleStats map[int]*ruleRunStats,
	states map[string]*torrentDesiredState,
) {
	if len(currentBatch) == 0 {
		return
	}

	enterRule(evalCtx, currentBatch[0], torrents, scope)
	SortTorrentsWithFallback(torrents, currentBatch[0].SortingConfig, evalCtx, instanceID, currentBatch[0].Name)

	processTorrents(torrents, currentBatch, evalCtx, crossSeeds, scope, ruleStats, states)
}

func buildAndExecuteBatches(
	instanceID int,
	eligibleRules []*models.Automation,
	torrents []*qbt.Torrent,
	evalCtx *EvalContext,
	crossSeeds *crossSeedGroups,
	scope *ruleScope,
	ruleStats map[int]*ruleRunStats,
	states map[string]*torrentDesiredState,
) {
	if len(eligibleRules) == 0 {
		return
	}

	currentBatch := []*models.Automation{eligibleRules[0]}
	for i := 1; i < len(eligibleRules); i++ {
		rule := eligibleRules[i]
		prevRule := eligibleRules[i-1]

		if rulesCanShareSortingBatch(prevRule, rule) {
			currentBatch = append(currentBatch, rule)
		} else {
			executeBatch(instanceID, currentBatch, torrents, evalCtx, crossSeeds, scope, ruleStats, states)
			currentBatch = []*models.Automation{rule}
		}
	}
	if len(currentBatch) > 0 {
		executeBatch(instanceID, currentBatch, torrents, evalCtx, crossSeeds, scope, ruleStats, states)
	}
}

func rulesCanShareSortingBatch(a, b *models.Automation) bool {
	if !sortingConfigEqual(a.SortingConfig, b.SortingConfig) {
		return false
	}

	return !ruleUsesRuleScopedSortingContext(a) && !ruleUsesRuleScopedSortingContext(b)
}

func ruleUsesRuleScopedSortingContext(rule *models.Automation) bool {
	if rule == nil {
		return false
	}

	return sortingConfigUsesField(rule.SortingConfig, FieldFreeSpace) ||
		sortingConfigUsesField(rule.SortingConfig, FieldGroupSize) ||
		sortingConfigUsesField(rule.SortingConfig, FieldIsGrouped)
}

func sortingConfigEqual(a, b *models.SortingConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Type != b.Type || a.SchemaVersion != b.SchemaVersion {
		return false
	}

	switch a.Type {
	case models.SortingTypeSimple:
		return a.Field == b.Field && a.Direction == b.Direction
	case models.SortingTypeScore:
		if a.Direction != b.Direction {
			return false
		}
		return scoreRulesEqual(a.ScoreRules, b.ScoreRules)
	default:
		return false
	}
}

func scoreRulesEqual(rulesA, rulesB []models.ScoreRule) bool {
	if len(rulesA) != len(rulesB) {
		return false
	}
	for i := range rulesA {
		rA := rulesA[i]
		rB := rulesB[i]
		if rA.Type != rB.Type {
			return false
		}
		switch rA.Type {
		case models.ScoreRuleTypeFieldMultiplier:
			if rA.FieldMultiplier == nil || rB.FieldMultiplier == nil {
				if rA.FieldMultiplier != rB.FieldMultiplier {
					return false
				}
				continue
			}
			if rA.FieldMultiplier.Field != rB.FieldMultiplier.Field || rA.FieldMultiplier.Multiplier != rB.FieldMultiplier.Multiplier {
				return false
			}
		case models.ScoreRuleTypeConditional:
			if rA.Conditional == nil || rB.Conditional == nil {
				if rA.Conditional != rB.Conditional {
					return false
				}
				continue
			}
			if rA.Conditional.Score != rB.Conditional.Score {
				return false
			}
			if !conditionEqual(rA.Conditional.Condition, rB.Conditional.Condition) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func conditionEqual(a, b *models.RuleCondition) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Field != b.Field || a.Operator != b.Operator || a.Value != b.Value ||
		a.Regex != b.Regex || a.Negate != b.Negate {
		return false
	}
	if a.GroupID != b.GroupID {
		return false
	}
	if (a.MinValue == nil) != (b.MinValue == nil) {
		return false
	}
	if a.MinValue != nil && *a.MinValue != *b.MinValue {
		return false
	}
	if (a.MaxValue == nil) != (b.MaxValue == nil) {
		return false
	}
	if a.MaxValue != nil && *a.MaxValue != *b.MaxValue {
		return false
	}
	if len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.Conditions {
		if !conditionEqual(a.Conditions[i], b.Conditions[i]) {
			return false
		}
	}
	return true
}
