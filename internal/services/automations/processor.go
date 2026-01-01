// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/metrics/collector"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// torrentDesiredState tracks accumulated actions for a single torrent across all matching rules.
type torrentDesiredState struct {
	hash           string
	name           string
	trackerDomains []string // all tracker domains for this torrent

	// Speed limits (last rule wins)
	uploadLimitKiB   *int64
	downloadLimitKiB *int64

	// Share limits (last rule wins)
	ratioLimit     *float64
	seedingMinutes *int64

	// Pause (OR - any rule can trigger)
	shouldPause bool

	// Tags (accumulated, last action per tag wins)
	currentTags map[string]struct{}
	tagActions  map[string]string // tag -> "add" | "remove"

	// Category (last rule wins)
	category                  *string
	categoryIncludeCrossSeeds bool // Whether winning category rule wants cross-seeds moved

	// Delete (first rule to trigger wins)
	shouldDelete   bool
	deleteMode     string
	deleteRuleID   int
	deleteRuleName string
	deleteReason   string
}

type ruleRunStats struct {
	MatchedTrackers                  int
	SpeedApplied                     int
	SpeedConditionNotMet             int
	ShareApplied                     int
	ShareConditionNotMet             int
	PauseApplied                     int
	PauseConditionNotMet             int
	TagConditionMet                  int
	TagConditionNotMet               int
	TagSkippedMissingUnregisteredSet int
	CategoryApplied                  int
	CategoryConditionNotMetOrBlocked int
	DeleteApplied                    int
	DeleteConditionNotMet            int
}

func (s *ruleRunStats) CollectMetrics(rule *models.Automation, metricsCollector *collector.AutomationCollector, instanceName string) {
	metricsCollector.GetAutomationRuleRunTotal(rule.InstanceID, instanceName, rule.ID, rule.Name).Inc()

	m := metricsCollector.GetAutomationRuleRunActionTotal(rule.InstanceID, instanceName, rule.ID, rule.Name)
	m.WithLabelValues("speed_applied").Add(float64(s.SpeedApplied))
	m.WithLabelValues("speed_condition_not_met").Add(float64(s.SpeedConditionNotMet))
	m.WithLabelValues("share_applied").Add(float64(s.ShareApplied))
	m.WithLabelValues("share_condition_not_met").Add(float64(s.ShareConditionNotMet))
	m.WithLabelValues("pause_applied").Add(float64(s.PauseApplied))
	m.WithLabelValues("pause_condition_not_met").Add(float64(s.PauseConditionNotMet))
	m.WithLabelValues("tag_condition_met").Add(float64(s.TagConditionMet))
	m.WithLabelValues("tag_condition_not_met").Add(float64(s.TagConditionNotMet))
	m.WithLabelValues("tag_skipped_missing_unregistered_set").Add(float64(s.TagSkippedMissingUnregisteredSet))
	m.WithLabelValues("category_applied").Add(float64(s.CategoryApplied))
	m.WithLabelValues("category_condition_not_met_or_blocked").Add(float64(s.CategoryConditionNotMetOrBlocked))
	m.WithLabelValues("delete_applied").Add(float64(s.DeleteApplied))
	m.WithLabelValues("delete_condition_not_met").Add(float64(s.DeleteConditionNotMet))
	m.WithLabelValues("delete_not_completed").Add(float64(s.DeleteNotCompleted))
}

func (s *ruleRunStats) totalApplied() int {
	if s == nil {
		return 0
	}
	return s.SpeedApplied + s.ShareApplied + s.PauseApplied + s.TagConditionMet + s.CategoryApplied + s.DeleteApplied
}

func getOrCreateRuleStats(m map[int]*ruleRunStats, rule *models.Automation) *ruleRunStats {
	if m == nil || rule == nil {
		return nil
	}
	if s, ok := m[rule.ID]; ok {
		return s
	}
	s := &ruleRunStats{}
	m[rule.ID] = s
	return s
}

// selectMatchingRules returns all enabled rules that match the torrent, in sort order.
func selectMatchingRules(torrent qbt.Torrent, rules []*models.Automation, sm *qbittorrent.SyncManager) []*models.Automation {
	trackerDomains := collectTrackerDomains(torrent, sm)
	var matching []*models.Automation

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if !matchesTracker(rule.TrackerPattern, trackerDomains) {
			continue
		}

		matching = append(matching, rule)
	}

	return matching
}

// processTorrents processes all torrents against all rules, returning desired states.
func processTorrents(
	torrents []qbt.Torrent,
	rules []*models.Automation,
	evalCtx *EvalContext,
	sm *qbittorrent.SyncManager,
	skipCheck func(hash string) bool,
	stats map[int]*ruleRunStats,
) map[string]*torrentDesiredState {
	states := make(map[string]*torrentDesiredState)
	crossSeedIndex := buildCrossSeedIndex(torrents)

	for _, torrent := range torrents {
		// Skip if recently processed
		if skipCheck != nil && skipCheck(torrent.Hash) {
			continue
		}

		matchingRules := selectMatchingRules(torrent, rules, sm)
		if len(matchingRules) == 0 {
			continue
		}

		// Initialize state for this torrent
		state := &torrentDesiredState{
			hash:        torrent.Hash,
			name:        torrent.Name,
			currentTags: parseTorrentTags(torrent.Tags),
			tagActions:  make(map[string]string),
		}

		// Get all tracker domains for this torrent
		state.trackerDomains = collectTrackerDomains(torrent, sm)

		// Process each matching rule in order
		for _, rule := range matchingRules {
			if state.shouldDelete {
				// Once delete is triggered, stop processing further rules
				break
			}
			ruleStats := getOrCreateRuleStats(stats, rule)
			if ruleStats != nil {
				ruleStats.MatchedTrackers++
			}
			processRuleForTorrent(rule, torrent, state, evalCtx, crossSeedIndex, ruleStats)
		}

		// Only store if there are actions to take
		if hasActions(state) {
			states[torrent.Hash] = state
		}
	}

	return states
}

// processRuleForTorrent applies a single rule to the torrent state.
func processRuleForTorrent(rule *models.Automation, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext, crossSeedIndex map[crossSeedKey][]qbt.Torrent, stats *ruleRunStats) {
	conditions := rule.Conditions
	if conditions == nil {
		return
	}

	// Speed limits
	if conditions.SpeedLimits != nil && conditions.SpeedLimits.Enabled {
		shouldApply := conditions.SpeedLimits.Condition == nil ||
			EvaluateConditionWithContext(conditions.SpeedLimits.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.SpeedApplied++
			}
			if conditions.SpeedLimits.UploadKiB != nil {
				state.uploadLimitKiB = conditions.SpeedLimits.UploadKiB
			}
			if conditions.SpeedLimits.DownloadKiB != nil {
				state.downloadLimitKiB = conditions.SpeedLimits.DownloadKiB
			}
		} else if stats != nil {
			stats.SpeedConditionNotMet++
		}
	}

	// Share limits (ratio/seeding time)
	if conditions.ShareLimits != nil && conditions.ShareLimits.Enabled {
		shouldApply := conditions.ShareLimits.Condition == nil ||
			EvaluateConditionWithContext(conditions.ShareLimits.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.ShareApplied++
			}
			if conditions.ShareLimits.RatioLimit != nil {
				state.ratioLimit = conditions.ShareLimits.RatioLimit
			}
			if conditions.ShareLimits.SeedingTimeMinutes != nil {
				state.seedingMinutes = conditions.ShareLimits.SeedingTimeMinutes
			}
		} else if stats != nil {
			stats.ShareConditionNotMet++
		}
	}

	// Pause
	if conditions.Pause != nil && conditions.Pause.Enabled {
		shouldApply := conditions.Pause.Condition == nil ||
			EvaluateConditionWithContext(conditions.Pause.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.PauseApplied++
			}
			// Only pause if not already paused/stopped
			if torrent.State != qbt.TorrentStatePausedUp && torrent.State != qbt.TorrentStatePausedDl &&
				torrent.State != qbt.TorrentStateStoppedUp && torrent.State != qbt.TorrentStateStoppedDl {
				state.shouldPause = true
			}
		} else if stats != nil {
			stats.PauseConditionNotMet++
		}
	}

	// Tags
	if conditions.Tag != nil && conditions.Tag.Enabled && (len(conditions.Tag.Tags) > 0 || conditions.Tag.UseTrackerAsTag) {
		// Skip if condition uses IS_UNREGISTERED but health data isn't available
		if ConditionUsesField(conditions.Tag.Condition, FieldIsUnregistered) &&
			(evalCtx == nil || evalCtx.UnregisteredSet == nil) {
			// Skip tag processing for this rule
			if stats != nil {
				stats.TagSkippedMissingUnregisteredSet++
			}
		} else {
			matches := processTagAction(conditions.Tag, torrent, state, evalCtx)
			if stats != nil {
				if matches {
					stats.TagConditionMet++
				} else {
					stats.TagConditionNotMet++
				}
			}
		}
	}

	// Category (last rule wins - just set desired, service will filter no-ops)
	if conditions.Category != nil && conditions.Category.Enabled && conditions.Category.Category != "" {
		shouldApply := conditions.Category.Condition == nil ||
			EvaluateConditionWithContext(conditions.Category.Condition, torrent, evalCtx, 0)

		// Apply category change only if condition matches AND not blocked by cross-seed protection
		if shouldApply && !shouldBlockCategoryChangeForCrossSeeds(torrent, conditions.Category.BlockIfCrossSeedInCategories, crossSeedIndex) {
			if stats != nil {
				stats.CategoryApplied++
			}
			state.category = &conditions.Category.Category
			state.categoryIncludeCrossSeeds = conditions.Category.IncludeCrossSeeds
		} else if stats != nil {
			stats.CategoryConditionNotMetOrBlocked++
		}
	}

	// Delete
	if conditions.Delete != nil && conditions.Delete.Enabled {
		shouldApply := conditions.Delete.Condition == nil ||
			EvaluateConditionWithContext(conditions.Delete.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.DeleteApplied++
			}
			state.shouldDelete = true
			state.deleteMode = conditions.Delete.Mode
			if state.deleteMode == "" {
				state.deleteMode = DeleteModeKeepFiles
			}
			state.deleteRuleID = rule.ID
			state.deleteRuleName = rule.Name
			state.deleteReason = "condition matched"
		} else if stats != nil {
			stats.DeleteConditionNotMet++
		}
	}
}

func shouldBlockCategoryChangeForCrossSeeds(torrent qbt.Torrent, protectedCategories []string, crossSeedIndex map[crossSeedKey][]qbt.Torrent) bool {
	if len(protectedCategories) == 0 || crossSeedIndex == nil {
		return false
	}
	key, ok := makeCrossSeedKey(torrent)
	if !ok {
		return false
	}
	group, ok := crossSeedIndex[key]
	if !ok || len(group) == 0 {
		return false
	}
	for _, other := range group {
		if other.Hash == torrent.Hash {
			continue
		}
		if containsStringFold(protectedCategories, other.Category) {
			return true
		}
	}
	return false
}

func containsStringFold(list []string, candidate string) bool {
	if candidate == "" {
		return false
	}
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), candidate) {
			return true
		}
	}
	return false
}

func buildCrossSeedIndex(torrents []qbt.Torrent) map[crossSeedKey][]qbt.Torrent {
	if len(torrents) == 0 {
		return nil
	}
	index := make(map[crossSeedKey][]qbt.Torrent)
	for _, t := range torrents {
		key, ok := makeCrossSeedKey(t)
		if !ok {
			continue
		}
		index[key] = append(index[key], t)
	}
	return index
}

// processTagAction handles tag add/remove logic for a single tag action.
func processTagAction(tagAction *models.TagAction, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) bool {
	tagMode := tagAction.Mode
	if tagMode == "" {
		tagMode = models.TagModeFull
	}

	// Evaluate condition
	matchesCondition := tagAction.Condition == nil ||
		EvaluateConditionWithContext(tagAction.Condition, torrent, evalCtx, 0)

	// Determine tags to manage - either from static list or derived from tracker
	var tagsToManage []string
	if tagAction.UseTrackerAsTag && len(state.trackerDomains) > 0 {
		// Derive tag from tracker domain, preferring domains with customizations
		tag := selectTrackerTag(state.trackerDomains, tagAction.UseDisplayName, evalCtx)
		if tag != "" {
			tagsToManage = []string{tag}
		}
	} else {
		tagsToManage = tagAction.Tags
	}

	for _, managedTag := range tagsToManage {
		// Check current state AND pending changes from earlier rules
		hasTagNow := false
		if _, ok := state.currentTags[managedTag]; ok {
			hasTagNow = true
		}

		// Apply pending action if exists
		hasTag := hasTagNow
		if pending, ok := state.tagActions[managedTag]; ok {
			hasTag = (pending == "add")
		}

		// Smart tagging logic:
		// - ADD: doesn't have tag + matches + mode allows add
		// - REMOVE: has tag + doesn't match + mode allows remove
		if !hasTag && matchesCondition && (tagMode == models.TagModeFull || tagMode == models.TagModeAdd) {
			state.tagActions[managedTag] = "add"
		} else if hasTag && !matchesCondition && (tagMode == models.TagModeFull || tagMode == models.TagModeRemove) {
			state.tagActions[managedTag] = "remove"
		}
	}

	return matchesCondition
}

// hasActions returns true if the state has any actions to execute.
func hasActions(state *torrentDesiredState) bool {
	return state.uploadLimitKiB != nil ||
		state.downloadLimitKiB != nil ||
		state.ratioLimit != nil ||
		state.seedingMinutes != nil ||
		state.shouldPause ||
		len(state.tagActions) > 0 ||
		state.category != nil ||
		state.shouldDelete
}

// selectTrackerTag picks the best tracker domain to use as a tag.
// If useDisplayName is true, it prefers domains that have a customization (display name).
// Falls back to the first domain if no customizations match.
func selectTrackerTag(domains []string, useDisplayName bool, evalCtx *EvalContext) string {
	if len(domains) == 0 {
		return ""
	}

	// If using display names, prefer domains that have a customization
	if useDisplayName && evalCtx != nil && evalCtx.TrackerDisplayNameByDomain != nil {
		for _, domain := range domains {
			if displayName, ok := evalCtx.TrackerDisplayNameByDomain[strings.ToLower(domain)]; ok {
				return displayName
			}
		}
	}

	// Fall back to the first domain
	return domains[0]
}

// parseTorrentTags parses the comma-separated tag string into a set.
func parseTorrentTags(tags string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, t := range strings.Split(tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			result[t] = struct{}{}
		}
	}
	return result
}
