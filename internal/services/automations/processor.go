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

		// Check category filter (for legacy rules only - expression rules handle this in conditions)
		if !rule.UsesExpressions() && len(rule.Categories) > 0 {
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

		// Check tag filter (for legacy rules only)
		if !rule.UsesExpressions() && len(rule.Tags) > 0 {
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
			processRuleForTorrent(rule, torrent, state, evalCtx)
		}

		// Only store if there are actions to take
		if hasActions(state) {
			states[torrent.Hash] = state
		}
	}

	return states
}

// processRuleForTorrent applies a single rule to the torrent state.
func processRuleForTorrent(rule *models.Automation, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) {
	if rule.UsesExpressions() {
		processExpressionRule(rule, torrent, state, evalCtx)
	} else {
		processLegacyRule(rule, torrent, state, evalCtx)
	}
}

// processExpressionRule handles expression-based (advanced) rules.
func processExpressionRule(rule *models.Automation, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) {
	conditions := rule.Conditions

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
				state.deleteReason = "expression condition matched"
			}
		}
	}
}

// processLegacyRule handles legacy field-based rules.
func processLegacyRule(rule *models.Automation, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) {
	// Speed limits
	if rule.UploadLimitKiB != nil {
		state.uploadLimitKiB = rule.UploadLimitKiB
	}
	if rule.DownloadLimitKiB != nil {
		state.downloadLimitKiB = rule.DownloadLimitKiB
	}

	// Share limits
	if rule.RatioLimit != nil {
		state.ratioLimit = rule.RatioLimit
	}
	if rule.SeedingTimeLimitMinutes != nil {
		state.seedingMinutes = rule.SeedingTimeLimitMinutes
	}

	// Delete based on ratio/seeding time
	if shouldDeleteTorrentLegacy(torrent, rule) {
		state.shouldDelete = true
		state.deleteMode = *rule.DeleteMode
		state.deleteRuleID = rule.ID
		state.deleteRuleName = rule.Name
		state.deleteReason = buildDeleteReason(torrent, rule)
	}

	// Unregistered deletion (legacy mode)
	if !state.shouldDelete && rule.DeleteUnregistered && rule.DeleteMode != nil && *rule.DeleteMode != "" && *rule.DeleteMode != DeleteModeNone {
		if evalCtx != nil && evalCtx.UnregisteredSet != nil {
			if _, isUnregistered := evalCtx.UnregisteredSet[torrent.Hash]; isUnregistered {
				state.shouldDelete = true
				state.deleteMode = *rule.DeleteMode
				state.deleteRuleID = rule.ID
				state.deleteRuleName = rule.Name
				state.deleteReason = "unregistered"
			}
		}
	}
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

// shouldDeleteTorrentLegacy checks if a torrent should be deleted based on legacy rule fields.
func shouldDeleteTorrentLegacy(torrent qbt.Torrent, rule *models.Automation) bool {
	// Only delete completed torrents
	if torrent.Progress < 1.0 {
		return false
	}

	// Must have delete mode enabled
	if rule.DeleteMode == nil || *rule.DeleteMode == "" || *rule.DeleteMode == DeleteModeNone {
		return false
	}

	// Must have at least one limit configured
	hasRatioLimit := rule.RatioLimit != nil && *rule.RatioLimit > 0
	hasSeedingTimeLimit := rule.SeedingTimeLimitMinutes != nil && *rule.SeedingTimeLimitMinutes > 0

	if !hasRatioLimit && !hasSeedingTimeLimit {
		return false
	}

	// Check ratio limit
	if hasRatioLimit && torrent.Ratio >= *rule.RatioLimit {
		return true
	}

	// Check seeding time limit (torrent.SeedingTime is in seconds)
	if hasSeedingTimeLimit {
		limitSeconds := *rule.SeedingTimeLimitMinutes * 60
		if torrent.SeedingTime >= limitSeconds {
			return true
		}
	}

	return false
}

// buildDeleteReason creates a human-readable reason for deletion.
func buildDeleteReason(torrent qbt.Torrent, rule *models.Automation) string {
	hasRatioLimit := rule.RatioLimit != nil && *rule.RatioLimit > 0
	hasSeedingTimeLimit := rule.SeedingTimeLimitMinutes != nil && *rule.SeedingTimeLimitMinutes > 0
	ratioMet := hasRatioLimit && torrent.Ratio >= *rule.RatioLimit
	seedingTimeMet := hasSeedingTimeLimit && torrent.SeedingTime >= *rule.SeedingTimeLimitMinutes*60

	if ratioMet && seedingTimeMet {
		return "ratio and seeding time limits reached"
	} else if ratioMet {
		return "ratio limit reached"
	}
	return "seeding time limit reached"
}

// hasActions returns true if the state has any actions to execute.
func hasActions(state *torrentDesiredState) bool {
	return state.uploadLimitKiB != nil ||
		state.downloadLimitKiB != nil ||
		state.ratioLimit != nil ||
		state.seedingMinutes != nil ||
		state.shouldPause ||
		len(state.tagActions) > 0 ||
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
