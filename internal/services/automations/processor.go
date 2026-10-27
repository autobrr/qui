// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/pathcmp"
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
	uploadRule       ruleRef
	downloadRule     ruleRef

	// Share limits (last rule wins)
	ratioLimit       *float64
	seedingMinutes   *int64
	shareLimitAction string
	shareLimitsMode  string
	ratioRule        ruleRef
	seedingRule      ruleRef
	shareActionRule  ruleRef
	shareModeRule    ruleRef

	// Pause (OR - any rule can trigger)
	shouldPause bool
	pauseRule   ruleRef

	// Resume (OR - any rule can trigger)
	shouldResume bool
	resumeRule   ruleRef

	// Recheck (OR - any rule can trigger)
	shouldRecheck bool
	recheckRule   ruleRef

	// Reannounce (OR - any rule can trigger)
	shouldReannounce bool
	reannounceRule   ruleRef

	// Auto management (last rule wins)
	shouldAutoManage bool
	autoManageValue  bool // true = enable ATM, false = disable ATM
	autoManageRule   ruleRef

	// Tags (accumulated, last action per tag wins)
	currentTags  map[string]struct{}
	tagActions   map[string]string // tag -> "add" | "remove"
	tagRuleByTag map[string]ruleRef

	// Category (last rule wins)
	category                  *string
	categoryGroupID           string // Optional group ID to expand category changes
	categoryRuleID            int
	categoryIncludeCrossSeeds bool // Whether winning category rule wants cross-seeds moved
	categoryRule              ruleRef

	// Move (first rule to trigger wins)
	shouldMove           bool
	movePath             string
	moveGroupID          string // Optional group ID to expand moves
	moveAtomic           string
	moveBlockIfCrossSeed bool
	moveCondition        *models.RuleCondition
	moveRuleID           int
	moveRuleName         string
	moveRule             ruleRef

	// External program (last rule wins)
	externalProgramID *int
	programRuleID     int
	programRuleName   string

	// Export to instance (last rule wins)
	exportToInstance         *models.ExportToInstanceAction
	exportToInstanceRuleID   int
	exportToInstanceRuleName string
}

type ruleRef struct {
	id   int
	name string
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
	RecheckApplied                   int
	RecheckConditionNotMet           int
	ReannounceApplied                int
	ReannounceConditionNotMet        int
	AutoManageApplied                int
	AutoManageConditionNotMet        int
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
	movePathWarned                   bool // one relative-path warning per rule per run
	ExternalProgramApplied           int
	ExternalProgramConditionNotMet   int
	ExportToInstanceApplied          int
	ExportToInstanceConditionNotMet  int
}

func (s *ruleRunStats) totalApplied() int {
	if s == nil {
		return 0
	}
	return s.SpeedApplied + s.ShareApplied + s.PauseApplied + s.ResumeApplied + s.RecheckApplied + s.ReannounceApplied + s.AutoManageApplied + s.TagConditionMet + s.CategoryApplied + s.DeleteApplied + s.MoveApplied + s.ExternalProgramApplied + s.ExportToInstanceApplied
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
	existingStates map[string]*torrentDesiredState,
) map[string]*torrentDesiredState {
	var states map[string]*torrentDesiredState
	if existingStates != nil {
		states = existingStates
	} else {
		states = make(map[string]*torrentDesiredState)
	}

	crossSeeds := buildCrossSeedGroups(torrents, evalCtx)

	for _, torrent := range torrents {
		// Skip if recently processed
		if skipCheck != nil && skipCheck(torrent.Hash) {
			continue
		}

		matchingRules := selectMatchingRules(torrent, rules, sm)
		if len(matchingRules) == 0 {
			continue
		}

		// Initialize or retrieve existing state for this torrent
		var state *torrentDesiredState
		if existing, ok := states[torrent.Hash]; ok {
			state = existing
		} else {
			state = &torrentDesiredState{
				hash:         torrent.Hash,
				name:         torrent.Name,
				currentTags:  parseTorrentTags(torrent.Tags),
				tagActions:   make(map[string]string),
				tagRuleByTag: make(map[string]ruleRef),
			}
		}

		// Get all tracker domains for this torrent
		state.trackerDomains = collectTrackerDomains(torrent, sm)

		// Process each matching rule in order
		for _, rule := range matchingRules {
			ruleStats := getOrCreateRuleStats(stats, rule)
			if ruleStats != nil {
				ruleStats.MatchedTrackers++
			}
			processRuleForTorrent(rule, torrent, state, evalCtx, sm, crossSeeds, ruleStats, torrents)
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
func processRuleForTorrent(rule *models.Automation, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext, sm *qbittorrent.SyncManager, crossSeeds crossSeedGroups, stats *ruleRunStats, allTorrents []qbt.Torrent) {
	conditions := rule.Conditions
	if conditions == nil {
		return
	}

	// Load the rule's free space source state before evaluating any conditions.
	// This ensures FREE_SPACE conditions work correctly across all action types (not just delete).
	if evalCtx != nil && ruleUsesCondition(rule, FieldFreeSpace) {
		evalCtx.LoadFreeSpaceSourceState(GetFreeSpaceRuleKey(rule))
	}

	// Activate rule-scoped grouping (GROUP_SIZE / IS_GROUPED) and allow action expansion to re-use cached indices.
	if evalCtx != nil {
		activateRuleGrouping(evalCtx, rule, allTorrents, sm)
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
				state.uploadRule = ruleRef{id: rule.ID, name: rule.Name}
			}
			if conditions.SpeedLimits.DownloadKiB != nil {
				state.downloadLimitKiB = conditions.SpeedLimits.DownloadKiB
				state.downloadRule = ruleRef{id: rule.ID, name: rule.Name}
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
				state.ratioRule = ruleRef{id: rule.ID, name: rule.Name}
			}
			if conditions.ShareLimits.SeedingTimeMinutes != nil {
				state.seedingMinutes = conditions.ShareLimits.SeedingTimeMinutes
				state.seedingRule = ruleRef{id: rule.ID, name: rule.Name}
			}
			if conditions.ShareLimits.ShareLimitAction != nil {
				state.shareLimitAction = *conditions.ShareLimits.ShareLimitAction
				state.shareActionRule = ruleRef{id: rule.ID, name: rule.Name}
			}
			if conditions.ShareLimits.ShareLimitsMode != nil {
				state.shareLimitsMode = *conditions.ShareLimits.ShareLimitsMode
				state.shareModeRule = ruleRef{id: rule.ID, name: rule.Name}
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
				state.pauseRule = ruleRef{id: rule.ID, name: rule.Name}
				state.resumeRule = ruleRef{}
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
				state.resumeRule = ruleRef{id: rule.ID, name: rule.Name}
				state.pauseRule = ruleRef{}
			}
		} else if stats != nil {
			stats.ResumeConditionNotMet++
		}
	}

	// Recheck (last rule wins)
	if conditions.Recheck != nil && conditions.Recheck.Enabled {
		shouldApply := conditions.Recheck.Condition == nil ||
			EvaluateConditionWithContext(conditions.Recheck.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.RecheckApplied++
			}
			// Avoid re-triggering while already checking.
			if torrent.State != qbt.TorrentStateCheckingUp &&
				torrent.State != qbt.TorrentStateCheckingDl &&
				torrent.State != qbt.TorrentStateCheckingResumeData {
				state.shouldRecheck = true
				state.recheckRule = ruleRef{id: rule.ID, name: rule.Name}
			}
		} else if stats != nil {
			stats.RecheckConditionNotMet++
		}
	}

	// Reannounce (last rule wins)
	if conditions.Reannounce != nil && conditions.Reannounce.Enabled {
		shouldApply := conditions.Reannounce.Condition == nil ||
			EvaluateConditionWithContext(conditions.Reannounce.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.ReannounceApplied++
			}
			state.shouldReannounce = true
			state.reannounceRule = ruleRef{id: rule.ID, name: rule.Name}
		} else if stats != nil {
			stats.ReannounceConditionNotMet++
		}
	}

	// Auto management (last rule wins)
	if conditions.AutoManagement != nil {
		shouldApply := conditions.AutoManagement.Condition == nil ||
			EvaluateConditionWithContext(conditions.AutoManagement.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.AutoManageApplied++
			}
			// Always record the last matching rule's desired state so later
			// rules can override earlier ones (last rule wins). The actual
			// API call is skipped when the torrent already has the desired state.
			state.autoManageValue = conditions.AutoManagement.Enabled
			state.autoManageRule = ruleRef{id: rule.ID, name: rule.Name}
			state.shouldAutoManage = torrent.AutoManaged != state.autoManageValue
		} else if stats != nil {
			stats.AutoManageConditionNotMet++
		}
	}

	// Tags
	for _, tagAction := range conditions.TagActions() {
		if tagAction == nil || !tagAction.Enabled || (len(tagAction.Tags) == 0 && !tagAction.UseTrackerAsTag) {
			continue
		}
		// Skip if condition uses IS_UNREGISTERED but health data isn't available
		if ConditionUsesField(tagAction.Condition, FieldIsUnregistered) &&
			(evalCtx == nil || evalCtx.UnregisteredSet == nil) {
			// Skip tag processing for this rule
			if stats != nil {
				stats.TagSkippedMissingUnregisteredSet++
			}
			continue
		}
		matches := processTagAction(rule, tagAction, torrent, state, evalCtx)
		if stats != nil {
			if matches {
				stats.TagConditionMet++
			} else {
				stats.TagConditionNotMet++
			}
		}
	}

	// Category (last rule wins - just set desired, service will filter no-ops)
	if conditions.Category != nil && conditions.Category.Enabled {
		if shouldApplyCategoryAction(&torrent, getCategoryAction(rule), evalCtx, crossSeeds) {
			if stats != nil {
				stats.CategoryApplied++
			}
			state.category = &conditions.Category.Category
			state.categoryRuleID = rule.ID
			state.categoryIncludeCrossSeeds = conditions.Category.IncludeCrossSeeds
			state.categoryRule = ruleRef{id: rule.ID, name: rule.Name}
			state.categoryGroupID = strings.TrimSpace(conditions.Category.GroupID)
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

	// Export to instance (last rule wins)
	if conditions.ExportToInstance != nil && conditions.ExportToInstance.Enabled && conditions.ExportToInstance.TargetInstanceID > 0 {
		shouldApply := conditions.ExportToInstance.Condition == nil ||
			EvaluateConditionWithContext(conditions.ExportToInstance.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.ExportToInstanceApplied++
			}
			state.exportToInstance = conditions.ExportToInstance
			state.exportToInstanceRuleID = rule.ID
			state.exportToInstanceRuleName = rule.Name
		} else if stats != nil {
			stats.ExportToInstanceConditionNotMet++
		}
	}

	// Move (first rule to trigger wins - skip if already set)
	if conditions.Move != nil && conditions.Move.Enabled && !state.shouldMove {
		evaluateMoveAction(rule, conditions.Move, torrent, evalCtx, crossSeeds, stats, state)
	}
}

func evaluateMoveAction(rule *models.Automation, action *models.MoveAction, torrent qbt.Torrent, evalCtx *EvalContext, crossSeeds crossSeedGroups, stats *ruleRunStats, state *torrentDesiredState) {
	resolvedPath, ok := renderPathTemplate(action.Path, torrent, state, evalCtx)
	if !ok {
		if stats != nil {
			stats.MoveConditionNotMet++
		}
		return
	}

	conditionMet := action.Condition == nil ||
		EvaluateConditionWithContext(action.Condition, torrent, evalCtx, 0)
	// qBittorrent never reports a relative path back as the save path, so the move would repeat every run.
	if conditionMet && !pathcmp.IsAbsolute(resolvedPath) {
		// One warning per rule per run; stats is per rule per run.
		if stats == nil || !stats.movePathWarned {
			ruleName := ""
			if rule != nil {
				ruleName = rule.Name
			}
			log.Warn().Str("rule", ruleName).Str("path", resolvedPath).
				Msg("automations: skipping move, path is not absolute")
		}
		if stats != nil {
			stats.movePathWarned = true
			stats.MoveConditionNotMet++
		}
		return
	}
	alreadyAtDest := inSavePath(torrent, resolvedPath)

	// Only apply move if condition is met, not already in target path, and not blocked by cross-seed protection
	blocked := conditionMet && !alreadyAtDest && shouldBlockMoveForCrossSeeds(torrent, action, crossSeeds, evalCtx)
	if conditionMet && !alreadyAtDest && !blocked {
		if stats != nil {
			stats.MoveApplied++
		}
		state.shouldMove = true
		state.movePath = resolvedPath
		state.moveGroupID = strings.TrimSpace(action.GroupID)
		state.moveAtomic = strings.TrimSpace(action.Atomic)
		state.moveBlockIfCrossSeed = action.BlockIfCrossSeed
		state.moveCondition = action.Condition
		if rule != nil {
			state.moveRuleID = rule.ID
			state.moveRuleName = rule.Name
			state.moveRule = ruleRef{id: rule.ID, name: rule.Name}
		}
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

func shouldBlockCategoryChangeForCrossSeeds(torrent qbt.Torrent, protectedCategories []string, crossSeeds crossSeedGroups) bool {
	if len(protectedCategories) == 0 {
		return false
	}
	group := crossSeeds[torrent.Hash]
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

func crossSeedGroupKnown(torrent qbt.Torrent, crossSeeds crossSeedGroups, evalCtx *EvalContext) bool {
	if evalCtx == nil {
		return true
	}
	if !sameInstanceCrossSeedKnown(evalCtx, torrent.Hash) {
		return false
	}
	for _, member := range crossSeeds[torrent.Hash] {
		if !sameInstanceCrossSeedKnown(evalCtx, member.Hash) {
			return false
		}
	}
	return true
}

func shouldBlockMoveForCrossSeeds(torrent qbt.Torrent, moveAction *models.MoveAction, crossSeeds crossSeedGroups, evalCtx *EvalContext) bool {
	if moveAction == nil {
		return false
	}
	if !crossSeedGroupKnown(torrent, crossSeeds, evalCtx) {
		return true
	}
	group := crossSeeds[torrent.Hash]
	for _, other := range group {
		if normalizePath(other.SavePath) != normalizePath(torrent.SavePath) {
			return true
		}
	}
	if !moveAction.BlockIfCrossSeed || strings.TrimSpace(moveAction.GroupID) != "" || len(group) == 0 {
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

// renderPathTemplate returns the rendered Move path. The path is executed as a
// Go template with data; paths with no template actions are unchanged. sanitize
// is available in templates for safe path segments (e.g. {{ sanitize .Name }}).
func renderPathTemplate(path string, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) (resolved string, ok bool) {
	return renderPathTemplateData(path, torrentPathData(torrent, state, evalCtx))
}

// renderExportSavePath adds .CurrentSavePath, which Move paths lack: a Move built on it moves again every run.
func renderExportSavePath(path string, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) (resolved string, ok bool) {
	data := torrentPathData(torrent, state, evalCtx)
	data["CurrentSavePath"] = torrent.SavePath
	return renderPathTemplateData(path, data)
}

func torrentPathData(torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) map[string]any {
	tracker := ""
	if state != nil {
		tracker = selectTrackerTag(state.trackerDomains, true, evalCtx)
	}
	return pathTemplateData(torrent, tracker)
}

func renderPathTemplateData(path string, data map[string]any) (resolved string, ok bool) {
	resolvedPath, err := executePathTemplate(path, data)
	if err != nil {
		log.Error().Err(err).Str("path", path).Msg("failed to render path template")
		return "", false
	}
	if resolvedPath == "" {
		return "", false
	}

	return resolvedPath, true
}

// renderMovePathSample renders a move path for a placeholder torrent, to check it on save.
func renderMovePathSample(path string) (string, error) {
	sample := qbt.Torrent{Name: "sample", Hash: strings.Repeat("0", 40), Category: "sample"}
	return executePathTemplate(path, pathTemplateData(sample, "tracker"))
}

func pathTemplateData(torrent qbt.Torrent, tracker string) map[string]any {
	return map[string]any{
		"Name":                torrent.Name,
		"Hash":                torrent.Hash,
		"Category":            torrent.Category,
		"IsolationFolderName": pathutil.HashedIsolationFolder(torrent.Hash, torrent.Name),
		"Tracker":             tracker,
	}
}

func executePathTemplate(path string, data map[string]any) (string, error) {
	tmpl, err := template.New("movePath").
		Option("missingkey=error").
		Funcs(template.FuncMap{
			"sanitize": pathutil.SanitizePathSegment,
		}).
		Parse(path)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
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

// processTagAction handles tag add/remove logic for a single tag action.
func processTagAction(rule *models.Automation, tagAction *models.TagAction, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) bool {
	tagMode := tagAction.Mode
	if tagMode == "" {
		tagMode = models.TagModeFull
	}
	if state.tagRuleByTag == nil {
		state.tagRuleByTag = make(map[string]ruleRef)
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
	resetFromClient := shouldResetTagActionInClient(tagAction)

	for _, managedTag := range tagsToManage {
		// Check current state AND pending changes from earlier rules
		_, hasTag := state.currentTags[managedTag]
		if resetFromClient {
			// Managed reset mode starts from a clean slate: force re-add for current matches.
			hasTag = false
		}
		// Apply pending action if exists
		if pending, ok := state.tagActions[managedTag]; ok {
			hasTag = (pending == "add")
		}

		// Tagging semantics:
		// - FULL: add to matches, remove from non-matches
		// - ADD: add to matches only
		// - REMOVE: remove from matches only
		switch tagMode {
		case models.TagModeAdd:
			if !hasTag && matchesCondition {
				state.tagActions[managedTag] = "add"
				if rule != nil {
					state.tagRuleByTag[managedTag] = ruleRef{id: rule.ID, name: rule.Name}
				}
			}
		case models.TagModeRemove:
			if hasTag && matchesCondition {
				state.tagActions[managedTag] = "remove"
				if rule != nil {
					state.tagRuleByTag[managedTag] = ruleRef{id: rule.ID, name: rule.Name}
				}
			}
		default: // full (incl. unknown/empty)
			if !hasTag && matchesCondition {
				state.tagActions[managedTag] = "add"
				if rule != nil {
					state.tagRuleByTag[managedTag] = ruleRef{id: rule.ID, name: rule.Name}
				}
			} else if hasTag && !matchesCondition {
				state.tagActions[managedTag] = "remove"
				if rule != nil {
					state.tagRuleByTag[managedTag] = ruleRef{id: rule.ID, name: rule.Name}
				}
			}
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
		state.shouldRecheck ||
		state.shouldReannounce ||
		state.shouldAutoManage ||
		len(state.tagActions) > 0 ||
		state.category != nil ||
		state.shouldMove ||
		state.externalProgramID != nil ||
		state.exportToInstance != nil
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
	for t := range strings.SplitSeq(tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			result[t] = struct{}{}
		}
	}
	return result
}

// updateCumulativeFreeSpaceCleared updates the cumulative free space cleared for the "free space" condition.
// Only increments SpaceToClear when deleteFreesSpace returns true for the given mode/torrent.
// This ensures keep-files and preserve-cross-seeds modes don't over-project freed disk space.
// A file shared inside a group, through a path or a hardlink, counts once.
func updateCumulativeFreeSpaceCleared(torrent qbt.Torrent, evalCtx *EvalContext, deleteMode string, crossSeeds crossSeedGroups, includeHardlinks bool) bool {
	if evalCtx == nil || evalCtx.FilesToClear == nil {
		return true
	}

	// Only count toward free space if this delete will actually free disk bytes
	if !deleteFreesSpace(deleteMode, torrent, crossSeeds, evalCtx.CrossSeedFilesByHash) {
		return true
	}

	if deleteMode == DeleteModeWithFilesIncludeCrossSeeds && evalCtx.CrossSeedHashesToClear != nil {
		return updateCrossSeedFreeSpaceCleared(torrent, evalCtx, crossSeeds, includeHardlinks)
	}
	if deleteMode == DeleteModeWithFiles && len(crossSeeds[torrent.Hash]) > 0 {
		files := evalCtx.CrossSeedFilesByHash[torrent.Hash]
		if len(files) == 0 {
			return false
		}
		if evalCtx.FileBytesToClear == nil {
			evalCtx.FileBytesToClear = make(map[string]int64)
		}
		for _, file := range files {
			path := resolvedTorrentFilePath(torrent, file.Name)
			if counted := evalCtx.FileBytesToClear[path]; file.Size > counted {
				evalCtx.SpaceToClear += file.Size - counted
				evalCtx.FileBytesToClear[path] = file.Size
			}
		}
		return true
	}

	// The torrent shares no file: preserve mode verified that above, and a
	// file delete outside a group has nothing to share.
	evalCtx.SpaceToClear += torrent.Size
	return true
}

func updateCrossSeedFreeSpaceCleared(torrent qbt.Torrent, evalCtx *EvalContext, crossSeeds crossSeedGroups, includeHardlinks bool) bool {
	if _, counted := evalCtx.CrossSeedHashesToClear[torrent.Hash]; counted {
		return true
	}

	torrents := evalCtx.torrentsByHash
	if torrents == nil {
		torrents = make(map[string]qbt.Torrent, len(crossSeeds[torrent.Hash])+1)
		torrents[torrent.Hash] = torrent
		for _, member := range crossSeeds[torrent.Hash] {
			torrents[member.Hash] = member
		}
	}
	var hardlinks *HardlinkIndex
	if includeHardlinks {
		hardlinks = evalCtx.hardlinkIndex
	}
	expanded, ok := deleteCrossSeedGroup(torrent, torrents, crossSeeds, hardlinks,
		evalCtx.CrossSeedHashesToClear, cachedTorrentFilesFetcher(evalCtx.CrossSeedFilesByHash))
	if !ok {
		return false
	}
	return projectCrossSeedDeletionGroup(torrents, expanded.expandedSet, evalCtx, hardlinks)
}

func projectCrossSeedDeletionGroup(torrents map[string]qbt.Torrent, hashes map[string]struct{}, evalCtx *EvalContext, hardlinks *HardlinkIndex) bool {
	bytes, ok := verifiedCrossSeedFileBytes(torrents, hashes, evalCtx.CrossSeedFilesByHash, hardlinks)
	if !ok {
		return false
	}
	for hash := range hashes {
		evalCtx.CrossSeedHashesToClear[hash] = struct{}{}
	}
	evalCtx.SpaceToClear += bytes
	return true
}

func verifiedCrossSeedFileBytes(
	torrents map[string]qbt.Torrent,
	hashes map[string]struct{},
	filesByHash map[string]qbt.TorrentFiles,
	hardlinks *HardlinkIndex,
) (int64, bool) {
	// A singleton without a hardlink expansion needs no file list.
	if len(hashes) == 1 {
		for hash := range hashes {
			if len(filesByHash[hash]) == 0 {
				return torrents[hash].Size, true
			}
		}
	}

	idsByPath := make(map[string]fsops.FileKey)
	if hardlinks != nil {
		hardlinks.crossScopeMu.Lock()
		defer hardlinks.crossScopeMu.Unlock()
		if hardlinks.buildState == nil {
			return 0, false
		}
		for hash := range hashes {
			if info := hardlinks.buildState.torrentInfoByHash[hash]; info != nil {
				for _, file := range info.linkedFiles {
					if key, ok := hardlinks.buildState.key(file.fileID); ok {
						idsByPath[normalizePath(file.path)] = key
					}
				}
			}
		}
	}

	maxSizeByPath := make(map[string]int64)
	maxSizeByID := make(map[fsops.FileKey]int64)
	var totalBytes int64
	for hash := range hashes {
		if len(filesByHash[hash]) == 0 {
			return 0, false
		}
		for _, file := range filesByHash[hash] {
			resolvedPath := resolvedTorrentFilePath(torrents[hash], file.Name)
			previousSize := maxSizeByPath[resolvedPath]
			key, linked := idsByPath[resolvedPath]
			if linked {
				previousSize = maxSizeByID[key]
			}
			if file.Size <= previousSize {
				continue
			}
			if linked {
				maxSizeByID[key] = file.Size
			} else {
				maxSizeByPath[resolvedPath] = file.Size
			}
			totalBytes += file.Size - previousSize
		}
	}
	return totalBytes, true
}

// CalculateScore computes the weighted score for a torrent based on configuration.
func CalculateScore(torrent qbt.Torrent, config *models.SortingConfig, evalCtx *EvalContext) float64 {
	var totalScore float64

	if config == nil {
		return 0
	}

	for _, rule := range config.ScoreRules {
		switch rule.Type {
		case models.ScoreRuleTypeFieldMultiplier:
			if rule.FieldMultiplier != nil && rule.FieldMultiplier.Field != "" {
				val := getNumericFieldValue(torrent, rule.FieldMultiplier.Field, evalCtx)
				points := val * rule.FieldMultiplier.Multiplier
				totalScore += points
			}

		case models.ScoreRuleTypeConditional:
			if rule.Conditional != nil && rule.Conditional.Condition != nil {
				if EvaluateConditionWithContext(rule.Conditional.Condition, torrent, evalCtx, 0) {
					totalScore += rule.Conditional.Score
				}
			}
		}
	}

	return totalScore
}

// SortTorrents sorts the slice in-place based on the configuration.
// Always applies Hash (ASC) as a deterministic tiebreaker.
// Returns an error if the sorting configuration is invalid (e.g. unsupported field).
func SortTorrents(torrents []qbt.Torrent, config *models.SortingConfig, evalCtx *EvalContext) error {
	if config != nil {
		if config.SchemaVersion != "1" {
			return fmt.Errorf("invalid schema version: %s", config.SchemaVersion)
		}
		if config.Direction != models.SortDirectionASC && config.Direction != models.SortDirectionDESC {
			return fmt.Errorf("invalid direction: %s", config.Direction)
		}

		switch config.Type {
		case models.SortingTypeSimple:
			if !config.Field.IsNumeric() {
				if !config.Field.IsString() {
					return fmt.Errorf("unsupported sort field: %s", config.Field)
				}
			}
		case models.SortingTypeScore:
			if len(config.ScoreRules) == 0 {
				return errors.New("score sort requires at least one rule")
			}
			for i, r := range config.ScoreRules {
				switch r.Type {
				case models.ScoreRuleTypeFieldMultiplier:
					if r.FieldMultiplier == nil {
						return fmt.Errorf("score rule %d: content missing for field multiplier", i)
					}
					if !r.FieldMultiplier.Field.IsNumeric() {
						return fmt.Errorf("field multiplier requires numeric field, got: %s", r.FieldMultiplier.Field)
					}
				case models.ScoreRuleTypeConditional:
					if r.Conditional == nil {
						return fmt.Errorf("score rule %d: content missing for conditional", i)
					}
					if r.Conditional.Condition == nil {
						return fmt.Errorf("score rule %d: condition missing", i)
					}
				default:
					return fmt.Errorf("score rule %d: unknown type %s", i, r.Type)
				}
			}
		default:
			return fmt.Errorf("unsupported sorting type: %s", config.Type)
		}
	}

	// Optimization: Pre-calculate scores if using score mode to avoid re-evaluating in sort loop
	var scores map[string]float64
	if config != nil && config.Type == models.SortingTypeScore {
		scores = make(map[string]float64, len(torrents))
		for _, t := range torrents {
			scores[t.Hash] = CalculateScore(t, config, evalCtx)
		}
	}

	sort.Slice(torrents, func(i, j int) bool {
		return compareTorrents(torrents[i], torrents[j], config, scores, evalCtx)
	})

	return nil
}

// compareTorrents is a helper function that returns true if t1 should sort before t2.
func compareTorrents(t1, t2 qbt.Torrent, config *models.SortingConfig, scores map[string]float64, evalCtx *EvalContext) bool {
	// 1. Primary Sort
	if config != nil {
		switch config.Type {
		case models.SortingTypeSimple:
			if config.Field.IsNumeric() {
				v1 := getNumericFieldValue(t1, config.Field, evalCtx)
				v2 := getNumericFieldValue(t2, config.Field, evalCtx)
				if v1 != v2 {
					if config.Direction == models.SortDirectionASC {
						return v1 < v2
					}
					return v1 > v2
				}
			} else {
				v1, _ := extractStringValue(t1, config.Field)
				v2, _ := extractStringValue(t2, config.Field)
				if v1 != v2 {
					if config.Direction == models.SortDirectionASC {
						return v1 < v2
					}
					return v1 > v2
				}
			}
		case models.SortingTypeScore:
			s1 := scores[t1.Hash]
			s2 := scores[t2.Hash]
			if s1 != s2 {
				if config.Direction == models.SortDirectionASC {
					return s1 < s2
				}
				return s1 > s2
			}
		default:
			// Fallback for unknown config.Type. Should not happen.
			if t1.AddedOn != t2.AddedOn {
				return t1.AddedOn < t2.AddedOn
			}
		}
	} else if t1.AddedOn != t2.AddedOn {
		// Default sort: Oldest first (AddedOn ASC)
		return t1.AddedOn < t2.AddedOn
	}

	// 2. Tiebreaker: Hash ASC
	return t1.Hash < t2.Hash
}

// getNowUnix returns the current time from context or system time.
func getNowUnix(evalCtx *EvalContext) int64 {
	if evalCtx != nil && evalCtx.NowUnix != 0 {
		return evalCtx.NowUnix
	}
	return time.Now().Unix()
}

// getNumericFieldValue returns the float64 representation of a field for scoring.
// Returns 0 if field is not numeric or not found.
//
//nolint:exhaustive // Only numeric sort fields are supported.
func getNumericFieldValue(t qbt.Torrent, field models.ConditionField, evalCtx *EvalContext) float64 {
	switch field {
	case models.FieldSize:
		return float64(t.Size)
	case models.FieldTotalSize:
		return float64(t.TotalSize)
	case models.FieldDownloaded:
		return float64(t.Downloaded)
	case models.FieldUploaded:
		return float64(t.Uploaded)
	case models.FieldAmountLeft:
		return float64(t.AmountLeft)
	case models.FieldFreeSpace:
		if evalCtx != nil && evalCtx.FreeSpace > 0 {
			return float64(evalCtx.FreeSpace)
		}
		return 0
	case models.FieldAddedOn:
		if t.AddedOn <= 0 {
			return 0
		}
		return float64(t.AddedOn)
	case models.FieldCompletionOn:
		return float64(qbittorrent.NormalizeCompletionTimestamp(t.CompletionOn))
	case models.FieldLastActivity:
		if t.LastActivity <= 0 {
			return 0
		}
		return float64(t.LastActivity)
	case models.FieldSeedingTime:
		if t.SeedingTime <= 0 {
			return 0
		}
		return float64(t.SeedingTime)
	case models.FieldTimeActive:
		if t.TimeActive <= 0 {
			return 0
		}
		return float64(t.TimeActive)
	case models.FieldAddedOnAge, models.FieldCompletionOnAge, models.FieldLastActivityAge:
		return getAgeFieldValue(evalCtx, field, t)
	case models.FieldRatio:
		return t.Ratio
	case models.FieldProgress:
		return t.Progress * 100
	case models.FieldAvailability:
		return t.Availability
	case models.FieldDlSpeed:
		return float64(t.DlSpeed)
	case models.FieldUpSpeed:
		return float64(t.UpSpeed)
	case models.FieldNumSeeds:
		return float64(t.NumSeeds)
	case models.FieldNumLeechs:
		return float64(t.NumLeechs)
	case models.FieldNumComplete:
		return float64(t.NumComplete)
	case models.FieldNumIncomplete:
		return float64(t.NumIncomplete)
	case models.FieldTrackersCount:
		return float64(t.TrackersCount)
	case models.FieldSystemHour:
		return float64(evaluateTime(evalCtx).Hour())
	case models.FieldSystemMinute:
		return float64(evaluateTime(evalCtx).Minute())
	case models.FieldSystemDayOfWeek:
		return float64(evaluateTime(evalCtx).Weekday())
	case models.FieldSystemDay:
		return float64(evaluateTime(evalCtx).Day())
	case models.FieldSystemMonth:
		return float64(evaluateTime(evalCtx).Month())
	case models.FieldSystemYear:
		return float64(evaluateTime(evalCtx).Year())
	default:
		return 0
	}
}

//nolint:exhaustive // Only age-backed fields are supported.
func getAgeFieldValue(evalCtx *EvalContext, field models.ConditionField, t qbt.Torrent) float64 {
	var ts int64
	switch field {
	case models.FieldAddedOnAge:
		ts = t.AddedOn
	case models.FieldCompletionOnAge:
		ts = qbittorrent.NormalizeCompletionTimestamp(t.CompletionOn)
	case models.FieldLastActivityAge:
		ts = t.LastActivity
	default:
		return 0
	}
	if ts <= 0 {
		return 0
	}
	return float64(getNowUnix(evalCtx) - ts)
}

//nolint:exhaustive // Only string sort fields are supported.
func extractStringValue(t qbt.Torrent, field models.ConditionField) (string, bool) {
	switch field {
	case models.FieldName:
		return strings.ToLower(t.Name), true
	case models.FieldCategory:
		return strings.ToLower(t.Category), true
	case models.FieldTags:
		return strings.ToLower(t.Tags), true
	case models.FieldTracker:
		return strings.ToLower(t.Tracker), true
	case models.FieldState:
		return strings.ToLower(string(t.State)), true
	case models.FieldSavePath:
		return strings.ToLower(t.SavePath), true
	case models.FieldContentPath:
		return strings.ToLower(t.ContentPath), true
	case models.FieldComment:
		return strings.ToLower(t.Comment), true
	default:
		return "", false
	}
}
