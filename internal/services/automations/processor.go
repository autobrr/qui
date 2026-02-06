// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"bytes"
	"sort"
	"strings"
	"text/template"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/pathutil"
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

	// Resume (OR - any rule can trigger)
	shouldResume bool

	// Tags (accumulated, last action per tag wins)
	currentTags map[string]struct{}
	tagActions  map[string]string // tag -> "add" | "remove"

	// Category (last rule wins)
	category                  *string
	categoryIncludeCrossSeeds bool // Whether winning category rule wants cross-seeds moved

	// Delete (first rule to trigger wins)
	shouldDelete           bool
	deleteMode             string
	deleteIncludeHardlinks bool // Whether to expand deletion to hardlink copies
	deleteRuleID           int
	deleteRuleName         string
	deleteReason           string

	// Move (first rule to trigger wins)
	shouldMove bool
	movePath   string

	// External program (last rule wins)
	externalProgramID *int
	programRuleID     int
	programRuleName   string
}

type ruleRunStats struct {
	MatchedTrackers                  int
	SpeedApplied                     int
	SpeedConditionNotMet             int
	ShareApplied                     int
	ShareConditionNotMet             int
	PauseApplied                     int
	PauseConditionNotMet             int
	ResumeApplied                    int
	ResumeConditionNotMet            int
	TagConditionMet                  int
	TagConditionNotMet               int
	TagSkippedMissingUnregisteredSet int
	CategoryApplied                  int
	CategoryConditionNotMetOrBlocked int
	DeleteApplied                    int
	DeleteConditionNotMet            int
	MoveApplied                      int
	MoveConditionNotMet              int
	MoveAlreadyAtDestination         int
	MoveBlockedByCrossSeed           int
	ExternalProgramApplied           int
	ExternalProgramConditionNotMet   int
}

func (s *ruleRunStats) totalApplied() int {
	if s == nil {
		return 0
	}
	return s.SpeedApplied + s.ShareApplied + s.PauseApplied + s.ResumeApplied + s.TagConditionMet + s.CategoryApplied + s.DeleteApplied + s.MoveApplied + s.ExternalProgramApplied
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

	// Stable sort for deterministic pagination: oldest first, then by hash
	sort.Slice(torrents, func(i, j int) bool {
		if torrents[i].AddedOn != torrents[j].AddedOn {
			return torrents[i].AddedOn < torrents[j].AddedOn
		}
		return torrents[i].Hash < torrents[j].Hash
	})

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
			processRuleForTorrent(rule, torrent, state, evalCtx, crossSeedIndex, ruleStats, torrents)
		}

		// Only store if there are actions to take
		if hasActions(state) {
			states[torrent.Hash] = state
		}
	}

	// Persist any active free space source state before returning
	if evalCtx != nil {
		evalCtx.PersistFreeSpaceSourceState()
	}

	return states
}

// processRuleForTorrent applies a single rule to the torrent state.
func processRuleForTorrent(rule *models.Automation, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext, crossSeedIndex map[crossSeedKey][]qbt.Torrent, stats *ruleRunStats, allTorrents []qbt.Torrent) {
	conditions := rule.Conditions
	if conditions == nil {
		return
	}

	// Load the rule's free space source state before evaluating any conditions.
	// This ensures FREE_SPACE conditions work correctly across all action types (not just delete).
	if evalCtx != nil && rulesUseCondition([]*models.Automation{rule}, FieldFreeSpace) {
		evalCtx.LoadFreeSpaceSourceState(GetFreeSpaceRuleKey(rule))
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

	// Pause (last rule wins)
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
				state.shouldResume = false // Clear conflicting resume from earlier rule if any
			}
		} else if stats != nil {
			stats.PauseConditionNotMet++
		}
	}

	// Resume (last rule wins)
	if conditions.Resume != nil && conditions.Resume.Enabled {
		shouldApply := conditions.Resume.Condition == nil ||
			EvaluateConditionWithContext(conditions.Resume.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.ResumeApplied++
			}

			// Only resume if currently paused/stopped
			if torrent.State == qbt.TorrentStatePausedUp || torrent.State == qbt.TorrentStatePausedDl ||
				torrent.State == qbt.TorrentStateStoppedUp || torrent.State == qbt.TorrentStateStoppedDl {
				state.shouldResume = true
				state.shouldPause = false // Clear conflicting pause from earlier rule if any
			}
		} else if stats != nil {
			stats.ResumeConditionNotMet++
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

	// External program (last rule wins)
	if conditions.ExternalProgram != nil && conditions.ExternalProgram.Enabled && conditions.ExternalProgram.ProgramID > 0 {
		shouldApply := conditions.ExternalProgram.Condition == nil ||
			EvaluateConditionWithContext(conditions.ExternalProgram.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.ExternalProgramApplied++
			}
			state.externalProgramID = &conditions.ExternalProgram.ProgramID
			state.programRuleID = rule.ID
			state.programRuleName = rule.Name
		} else if stats != nil {
			stats.ExternalProgramConditionNotMet++
		}
	}

	// Delete
	if conditions.Delete != nil && conditions.Delete.Enabled {
		// Safety: delete must always have an explicit condition.
		if conditions.Delete.Condition == nil {
			if stats != nil {
				stats.DeleteConditionNotMet++
			}
		} else {
			shouldApply := EvaluateConditionWithContext(conditions.Delete.Condition, torrent, evalCtx, 0)
			if shouldApply {
				if stats != nil {
					stats.DeleteApplied++
				}
				state.shouldDelete = true
				state.deleteMode = conditions.Delete.Mode
				if state.deleteMode == "" {
					state.deleteMode = DeleteModeKeepFiles
				}
				state.deleteIncludeHardlinks = conditions.Delete.IncludeHardlinks
				state.deleteRuleID = rule.ID
				state.deleteRuleName = rule.Name
				state.deleteReason = "condition matched"

				// Update the cumulative free space cleared for the "free space" condition.
				// Only call this when the delete condition uses FREE_SPACE, otherwise we might
				// accidentally mutate a previously-loaded rule's projection state.
				if evalCtx != nil && ConditionUsesField(conditions.Delete.Condition, FieldFreeSpace) {
					updateCumulativeFreeSpaceCleared(torrent, evalCtx, state.deleteMode, allTorrents)
				}
			} else if stats != nil {
				stats.DeleteConditionNotMet++
			}
		}
	}

	// Move (first rule to trigger wins - skip if already set)
	if conditions.Move != nil && conditions.Move.Enabled && !state.shouldMove {
		evaluateMoveAction(conditions.Move, torrent, evalCtx, crossSeedIndex, stats, state)
	}
}

func evaluateMoveAction(action *models.MoveAction, torrent qbt.Torrent, evalCtx *EvalContext, crossSeedIndex map[crossSeedKey][]qbt.Torrent, stats *ruleRunStats, state *torrentDesiredState) {
	resolvedPath, pathValid := resolveMovePath(action.Path, torrent, state, evalCtx)
	if !pathValid {
		if stats != nil {
			stats.MoveConditionNotMet++
		}
		return
	}

	conditionMet := action.Condition == nil ||
		EvaluateConditionWithContext(action.Condition, torrent, evalCtx, 0)
	alreadyAtDest := inSavePath(torrent, resolvedPath)

	// Only apply move if condition is met, not already in target path, and not blocked by cross-seed protection
	if conditionMet && !alreadyAtDest && !shouldBlockMoveForCrossSeeds(torrent, action, crossSeedIndex, evalCtx) {
		if stats != nil {
			stats.MoveApplied++
		}
		state.shouldMove = true
		state.movePath = resolvedPath
		return
	}
	if stats == nil {
		return
	}

	switch {
	case !conditionMet:
		stats.MoveConditionNotMet++
	case alreadyAtDest:
		stats.MoveAlreadyAtDestination++
	default:
		stats.MoveBlockedByCrossSeed++
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

func shouldBlockMoveForCrossSeeds(torrent qbt.Torrent, moveAction *models.MoveAction, crossSeedIndex map[crossSeedKey][]qbt.Torrent, evalCtx *EvalContext) bool {
	if moveAction == nil || !moveAction.BlockIfCrossSeed {
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

	// If condition is nil, it means "always apply" - all cross-seeds are considered matching,
	// so don't block. This aligns with processRuleForTorrent where nil condition means unconditional apply.
	if moveAction.Condition == nil {
		return false
	}

	// If we have any other torrent in the same cross-seed group, evaluate the condition for each torrent.
	// Block if any cross-seed does NOT match the condition.
	for _, other := range group {
		if other.Hash == torrent.Hash {
			continue
		}
		if !EvaluateConditionWithContext(moveAction.Condition, other, evalCtx, 0) {
			return true
		}
	}

	return false
}

func inSavePath(torrent qbt.Torrent, savePath string) bool {
	return normalizePath(torrent.SavePath) == normalizePath(savePath)
}

// resolveMovePath returns the path to use for a move. The path is executed as a
// Go template with data; paths with no template actions are unchanged. sanitize
// is available in templates for safe path segments (e.g. {{ sanitize .Name }}).
func resolveMovePath(path string, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) (resolved string, ok bool) {
	tracker := ""
	if state != nil {
		tracker = selectTrackerTag(state.trackerDomains, true, evalCtx)
	}

	data := map[string]any{
		"Name":                torrent.Name,
		"Hash":                torrent.Hash,
		"Category":            torrent.Category,
		"IsolationFolderName": pathutil.IsolationFolderName(torrent.Hash, torrent.Name),
		"Tracker":             tracker,
	}

	tmpl, err := template.New("movePath").
		Option("missingkey=error").
		Funcs(template.FuncMap{
			"sanitize": pathutil.SanitizePathSegment,
		}).
		Parse(path)
	if err != nil {
		// Log template parse error for debugging
		log.Error().Err(err).Str("path", path).Msg("failed to parse move path template")
		return "", false
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		// Log template execution error for debugging
		log.Error().Err(err).Str("path", path).Msg("failed to execute move path template")
		return "", false
	}

	resolvedPath := strings.TrimSpace(buf.String())

	if resolvedPath == "" {
		return "", false
	}

	return resolvedPath, true
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
	tagsToManage := tagAction.Tags
	if tagAction.UseTrackerAsTag && len(state.trackerDomains) > 0 {
		// Derive tag from tracker domain, preferring domains with customizations
		if tag := selectTrackerTag(state.trackerDomains, tagAction.UseDisplayName, evalCtx); tag != "" {
			tagsToManage = []string{tag}
		} else {
			tagsToManage = nil
		}
	}

	for _, managedTag := range tagsToManage {
		// Check current state AND pending changes from earlier rules
		_, hasTag := state.currentTags[managedTag]
		// Apply pending action if exists
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
		state.shouldResume ||
		len(state.tagActions) > 0 ||
		state.category != nil ||
		state.shouldDelete ||
		state.shouldMove ||
		state.externalProgramID != nil
}

// selectTrackerTag picks the best tracker domain to use as a tag.
// If useDisplayName is true, it prefers domains that have a customization (display name).
// Falls back to the first domain if no customizations match.
func selectTrackerTag(domains []string, useDisplayName bool, evalCtx *EvalContext) string {
	if len(domains) == 0 {
		return ""
	}

	// If using display names, prefer domains that have a customization
	if useDisplayName {
		if displayName, ok := getTrackerDisplayName(domains, evalCtx); ok {
			return displayName
		}
	}

	// Fall back to the first domain
	return domains[0]
}

// getTrackerDisplayName picks the best tracker display name available.
func getTrackerDisplayName(domains []string, evalCtx *EvalContext) (displayName string, ok bool) {
	if evalCtx == nil {
		return "", false
	}

	for _, domain := range domains {
		if displayName, found := evalCtx.TrackerDisplayNameByDomain[strings.ToLower(domain)]; found {
			return displayName, true
		}
	}

	return "", false
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

// updateCumulativeFreeSpaceCleared updates the cumulative free space cleared for the "free space" condition.
// Only increments SpaceToClear when deleteFreesSpace returns true for the given mode/torrent.
// This ensures keep-files and preserve-cross-seeds modes don't over-project freed disk space.
// When HardlinkSignatureByHash is populated, also dedupes by hardlink signature to avoid
// double-counting torrents that share the same physical files via hardlinks.
func updateCumulativeFreeSpaceCleared(torrent qbt.Torrent, evalCtx *EvalContext, deleteMode string, allTorrents []qbt.Torrent) {
	if evalCtx == nil || evalCtx.FilesToClear == nil {
		return
	}

	// Only count toward free space if this delete will actually free disk bytes
	if !deleteFreesSpace(deleteMode, torrent, allTorrents) {
		return
	}

	// First, check hardlink signature dedupe (if enabled and using include-cross-seeds mode).
	// Hardlink signature dedupe only makes sense when the delete mode can actually delete the
	// whole hardlink group via expansion; this avoids affecting other delete modes.
	if deleteMode == DeleteModeWithFilesIncludeCrossSeeds &&
		evalCtx.HardlinkSignatureByHash != nil && evalCtx.HardlinkSignaturesToClear != nil {
		if sig, ok := evalCtx.HardlinkSignatureByHash[torrent.Hash]; ok && sig != "" {
			if _, counted := evalCtx.HardlinkSignaturesToClear[sig]; counted {
				// Already counted this hardlink group
				return
			}
			// Mark signature as counted and add size
			evalCtx.HardlinkSignaturesToClear[sig] = struct{}{}
			evalCtx.SpaceToClear += torrent.Size
			return
		}
	}

	// Fall back to cross-seed key dedupe
	crossSeedKey, ok := makeCrossSeedKey(torrent)
	if !ok {
		// If the torrent cannot be a cross-seed, we add the file size to the cumulative space to clear
		evalCtx.SpaceToClear += torrent.Size
		return
	}

	// If the torrent is a cross-seed of a torrent that has already been counted, we don't count it again
	if _, ok := evalCtx.FilesToClear[crossSeedKey]; ok {
		return
	}

	// This is a new torrent, so we add the file size to the cumulative space to clear
	evalCtx.SpaceToClear += torrent.Size
	evalCtx.FilesToClear[crossSeedKey] = struct{}{}
}
