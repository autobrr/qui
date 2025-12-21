// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// torrentDesiredState tracks accumulated actions for a single torrent across all matching rules.
type torrentDesiredState struct {
	hash          string
	name          string
	trackerDomain string

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

		// Get primary tracker domain
		if domains := collectTrackerDomains(torrent, sm); len(domains) > 0 {
			state.trackerDomain = domains[0]
		}

		// Process each matching rule in order
		for _, rule := range matchingRules {
			if state.shouldDelete {
				// Once delete is triggered, stop processing further rules
				break
			}
			processRuleForTorrent(rule, torrent, state, evalCtx, crossSeedIndex)
		}

		// Only store if there are actions to take
		if hasActions(state) {
			states[torrent.Hash] = state
		}
	}

	return states
}

// processRuleForTorrent applies a single rule to the torrent state.
func processRuleForTorrent(rule *models.Automation, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext, crossSeedIndex map[crossSeedKey][]qbt.Torrent) {
	conditions := rule.Conditions
	if conditions == nil {
		return
	}

	// Speed limits
	if conditions.SpeedLimits != nil && conditions.SpeedLimits.Enabled {
		shouldApply := conditions.SpeedLimits.Condition == nil ||
			EvaluateConditionWithContext(conditions.SpeedLimits.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if conditions.SpeedLimits.UploadKiB != nil {
				state.uploadLimitKiB = conditions.SpeedLimits.UploadKiB
			}
			if conditions.SpeedLimits.DownloadKiB != nil {
				state.downloadLimitKiB = conditions.SpeedLimits.DownloadKiB
			}
		}
	}

	// Share limits (ratio/seeding time)
	if conditions.ShareLimits != nil && conditions.ShareLimits.Enabled {
		shouldApply := conditions.ShareLimits.Condition == nil ||
			EvaluateConditionWithContext(conditions.ShareLimits.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if conditions.ShareLimits.RatioLimit != nil {
				state.ratioLimit = conditions.ShareLimits.RatioLimit
			}
			if conditions.ShareLimits.SeedingTimeMinutes != nil {
				state.seedingMinutes = conditions.ShareLimits.SeedingTimeMinutes
			}
		}
	}

	// Pause
	if conditions.Pause != nil && conditions.Pause.Enabled {
		shouldApply := conditions.Pause.Condition == nil ||
			EvaluateConditionWithContext(conditions.Pause.Condition, torrent, evalCtx, 0)

		if shouldApply {
			// Only pause if not already paused
			if torrent.State != qbt.TorrentStatePausedUp && torrent.State != qbt.TorrentStatePausedDl {
				state.shouldPause = true
			}
		}
	}

	// Tags
	if conditions.Tag != nil && conditions.Tag.Enabled && len(conditions.Tag.Tags) > 0 {
		// Skip if condition uses IS_UNREGISTERED but health data isn't available
		if ConditionUsesField(conditions.Tag.Condition, FieldIsUnregistered) &&
			(evalCtx == nil || evalCtx.UnregisteredSet == nil) {
			// Skip tag processing for this rule
		} else {
			processTagAction(conditions.Tag, torrent, state, evalCtx)
		}
	}

	// Category (last rule wins - just set desired, service will filter no-ops)
	if conditions.Category != nil && conditions.Category.Enabled && conditions.Category.Category != "" {
		shouldApply := conditions.Category.Condition == nil ||
			EvaluateConditionWithContext(conditions.Category.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if shouldBlockCategoryChangeForCrossSeeds(torrent, conditions.Category.BlockIfCrossSeedInCategories, crossSeedIndex) {
				return
			}
			state.category = &conditions.Category.Category
			state.categoryIncludeCrossSeeds = conditions.Category.IncludeCrossSeeds
		}
	}

	// Delete
	if conditions.Delete != nil && conditions.Delete.Enabled {
		// Only delete completed torrents
		if torrent.Progress >= 1.0 {
			shouldApply := conditions.Delete.Condition == nil ||
				EvaluateConditionWithContext(conditions.Delete.Condition, torrent, evalCtx, 0)

			if shouldApply {
				state.shouldDelete = true
				state.deleteMode = conditions.Delete.Mode
				if state.deleteMode == "" {
					state.deleteMode = DeleteModeKeepFiles
				}
				state.deleteRuleID = rule.ID
				state.deleteRuleName = rule.Name
				state.deleteReason = "condition matched"
			}
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
func processTagAction(tagAction *models.TagAction, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) {
	tagMode := tagAction.Mode
	if tagMode == "" {
		tagMode = models.TagModeFull
	}

	// Evaluate condition
	matchesCondition := tagAction.Condition == nil ||
		EvaluateConditionWithContext(tagAction.Condition, torrent, evalCtx, 0)

	for _, managedTag := range tagAction.Tags {
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

// parseTorrentTags parses the comma-separated tag string into a set.
func parseTorrentTags(tags string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, t := range strings.Split(tags, ", ") {
		if t = strings.TrimSpace(t); t != "" {
			result[t] = struct{}{}
		}
	}
	return result
}
