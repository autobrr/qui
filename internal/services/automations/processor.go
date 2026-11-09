// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
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
	currentTags    map[string]struct{}
	tagActions     map[string]string // tag -> "add" | "remove"
	tagRuleByTag   map[string]ruleRef
	tagExpandByTag map[string]tagExpandIntent // tag -> pending cross-seed expansion of a tag decision
	tagResetTags   map[string]struct{}        // tags re-added after a managed reset: a real change even when already present

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

// tagExpandIntent is a pending cross-seed expansion of a tag decision: the rule
// that requested it, the decision to propagate ("add" or "remove"), and whether
// the action runs in managed-reset mode (the tag is wholesale-deleted in the
// client before matches are re-added).
type tagExpandIntent struct {
	rule            ruleRef
	action          string
	resetFromClient bool
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
	TagSkippedMissingTrackerHealth   int
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

// processTorrents processes all torrents against all rules, returning desired states.
func processTorrents(
	torrents []qbt.Torrent,
	rules []*models.Automation,
	evalCtx *EvalContext,
	crossSeeds *crossSeedGroups,
	scope *ruleScope,
	stats map[int]*ruleRunStats,
	existingStates map[string]*torrentDesiredState,
) map[string]*torrentDesiredState {
	var states map[string]*torrentDesiredState
	if existingStates != nil {
		states = existingStates
	} else {
		states = make(map[string]*torrentDesiredState)
	}

	askActionGroups(torrents, rules, evalCtx, crossSeeds, scope, states)
	if evalCtx != nil {
		defer func() { evalCtx.moveChecks = nil }()
	}

	for i := range torrents {
		torrent := &torrents[i]
		state := applyRulesToTorrent(torrent, rules, states[torrent.Hash], evalCtx, crossSeeds, scope, stats, torrents)
		// Only store if there are actions to take
		if state != nil && hasActions(state) {
			states[torrent.Hash] = state
		}
	}

	return states
}

// applyRulesToTorrent applies each enabled rule that holds the torrent in its Rule scope to the
// state of the torrent, in rule order. existing is the state from an earlier batch, or nil. It
// returns nil when no rule handles the torrent.
func applyRulesToTorrent(torrent *qbt.Torrent, rules []*models.Automation, existing *torrentDesiredState, evalCtx *EvalContext, crossSeeds *crossSeedGroups, scope *ruleScope, stats map[int]*ruleRunStats, allTorrents []qbt.Torrent) *torrentDesiredState {
	var state *torrentDesiredState
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		answer := scope.answer(rule, torrent)
		if answer == scopeSkipWindow || answer == scopeOtherTracker {
			continue
		}
		if state == nil {
			state = existing
			if state == nil {
				state = &torrentDesiredState{
					hash:         torrent.Hash,
					name:         torrent.Name,
					currentTags:  parseTorrentTags(torrent.Tags),
					tagActions:   make(map[string]string),
					tagRuleByTag: make(map[string]ruleRef),
				}
			}
			state.trackerDomains = scope.trackerDomains(torrent)
		}
		// The torrent drops out of the rule and keeps its managed tags. ADR 0039.
		if answer == scopeNoValue {
			keepResetTagsOfRule(rule, state)
			continue
		}
		ruleStats := getOrCreateRuleStats(stats, rule)
		if ruleStats != nil {
			ruleStats.MatchedTrackers++
		}
		processRuleForTorrent(rule, *torrent, state, evalCtx, scope, crossSeeds, ruleStats, allTorrents)
	}
	return state
}

// askActionGroups asks the run session once for the groups that the moves, tags and categories
// read, before the rule loop reads them one torrent at a time.
func askActionGroups(torrents []qbt.Torrent, rules []*models.Automation, evalCtx *EvalContext, crossSeeds *crossSeedGroups, scope *ruleScope, states map[string]*torrentDesiredState) {
	if evalCtx == nil || !crossSeeds.readsGroups() {
		return
	}
	var moveRules, groupRules []*models.Automation
	asksTagScope := make(map[*models.Automation]bool)
	for _, rule := range rules {
		c := rule.Conditions
		if c == nil {
			continue
		}
		if c.Move != nil && c.Move.Enabled {
			moveRules = append(moveRules, rule)
		}
		// The tag ownership check reads the group of each torrent in the scope of every tag rule.
		asksTagScope[rule] = crossSeeds.session.tagGroups && slices.ContainsFunc(c.TagActions(), func(a *models.TagAction) bool {
			return tagActionHasTags(a) && !tagActionMissesTrackerHealth(a, evalCtx)
		})
		if asksTagScope[rule] || getCategoryAction(rule).readsCrossSeedGroups() {
			groupRules = append(groupRules, rule)
		}
	}
	if len(moveRules)+len(groupRules) == 0 {
		return
	}
	if len(moveRules) > 0 {
		evalCtx.moveChecks = make(map[moveCheckKey]checkedMove)
	}
	movedGroups := make(map[askedGroup]struct{})
	categoryGroups := make(map[askedGroup]struct{})
	var moveHashes, groupHashes []string
	for i := range torrents {
		torrent := &torrents[i]
		if state := states[torrent.Hash]; len(moveRules) > 0 && (state == nil || !state.shouldMove) {
			moveHashes = append(moveHashes, moveAsks(torrent, moveRules, torrents, evalCtx, scope, movedGroups)...)
		}
		if len(groupRules) > 0 {
			groupHashes = append(groupHashes, groupReadAsks(torrent, groupRules, asksTagScope, torrents, evalCtx, scope, categoryGroups)...)
		}
	}
	slices.Sort(moveHashes)
	moveHashes = slices.Compact(moveHashes)
	asks := slices.Concat(groupHashes, moveHashes)
	slices.Sort(asks)
	crossSeeds.session.resolveGroups(slices.Compact(asks), false)
	// A move that blocks on a cross-seed checks its condition on each member.
	if slices.ContainsFunc(moveRules, moveBlockReadsCrossSeedFacts) {
		var members []string
		for _, hash := range moveHashes {
			members = append(members, memberHashes(crossSeeds.members(hash))...)
		}
		slices.Sort(members)
		crossSeeds.session.resolveFacts(slices.Compact(members))
	}
}

// groupReadAsks returns the torrent when a tag or category action of a rule that holds it in its
// Rule scope reads its group, with the members of the category GroupID that asked does not hold yet.
// A tag rule in asksTagScope reads the group of each torrent in its scope. A category reads the
// group only when its condition matches.
func groupReadAsks(torrent *qbt.Torrent, rules []*models.Automation, asksTagScope map[*models.Automation]bool, torrents []qbt.Torrent, evalCtx *EvalContext, scope *ruleScope, asked map[askedGroup]struct{}) []string {
	var hashes []string
	for _, rule := range rules {
		if !rule.Enabled || scope.answer(rule, torrent) != scopeIn {
			continue
		}
		if asksTagScope[rule] {
			hashes = append(hashes, torrent.Hash)
		}
		cat := getCategoryAction(rule)
		if !cat.readsCrossSeedGroups() {
			continue
		}
		enterRule(evalCtx, rule, torrents, scope)
		if cat.condition != nil && !EvaluateConditionWithContext(cat.condition, *torrent, evalCtx, 0) {
			continue
		}
		hashes = append(hashes, torrent.Hash)
		if groupID := strings.TrimSpace(rule.Conditions.Category.GroupID); groupID != "" {
			hashes = append(hashes, groupIDMembers(rule, groupID, torrent.Hash, torrents, evalCtx, scope, asked)...)
		}
	}
	return hashes
}

func moveBlockReadsCrossSeedFacts(rule *models.Automation) bool {
	move := rule.Conditions.Move
	return move.BlockIfCrossSeed && strings.TrimSpace(move.GroupID) == "" && conditionReadsCrossSeedFacts(move.Condition)
}

// askedGroup is a GroupID group of one rule that the read-ahead already asked for.
type askedGroup struct {
	rule *models.Automation
	key  string
}

// groupIDMembers returns the members of the GroupID group of the torrent, or nil when asked
// already holds the group.
func groupIDMembers(rule *models.Automation, groupID, hash string, torrents []qbt.Torrent, evalCtx *EvalContext, scope *ruleScope, asked map[askedGroup]struct{}) []string {
	idx := getOrBuildGroupIndexForRule(evalCtx, rule, groupID, torrents, scope)
	group := askedGroup{rule, idx.KeyForHash(hash)}
	if _, seen := asked[group]; seen {
		return nil
	}
	asked[group] = struct{}{}
	return idx.MembersForHash(hash)
}

// moveAsks returns the torrent when the move of a rule that holds it in its Rule scope can apply
// to it, with the members of the move GroupID that asked does not hold yet.
func moveAsks(torrent *qbt.Torrent, rules []*models.Automation, torrents []qbt.Torrent, evalCtx *EvalContext, scope *ruleScope, asked map[askedGroup]struct{}) []string {
	state := &torrentDesiredState{trackerDomains: scope.trackerDomains(torrent)}
	var hashes []string
	for _, rule := range rules {
		if !rule.Enabled || scope.answer(rule, torrent) != scopeIn {
			continue
		}
		enterRule(evalCtx, rule, torrents, scope)
		move := rule.Conditions.Move
		path, check := checkMove(move, *torrent, state, evalCtx)
		evalCtx.moveChecks[moveCheckKey{rule, torrent.Hash}] = checkedMove{path, check}
		if check != moveReady {
			continue
		}
		hashes = append(hashes, torrent.Hash)
		if groupID := strings.TrimSpace(move.GroupID); groupID != "" {
			hashes = append(hashes, groupIDMembers(rule, groupID, torrent.Hash, torrents, evalCtx, scope, asked)...)
		}
	}
	return hashes
}

// enterRule makes the free space ledger and the grouping (GROUP_SIZE / IS_GROUPED) of the rule
// active. Each pass calls it before it evaluates the rule.
func enterRule(evalCtx *EvalContext, rule *models.Automation, allTorrents []qbt.Torrent, scope *ruleScope) {
	if evalCtx == nil {
		return
	}
	evalCtx.activeFreeSpace = nil
	if rule != nil {
		evalCtx.activeFreeSpace = evalCtx.freeSpaceLedgers[rule.ID]
	}
	activateRuleGrouping(evalCtx, rule, allTorrents, scope)
}

// processRuleForTorrent applies a single rule to the torrent state.
func processRuleForTorrent(rule *models.Automation, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext, scope *ruleScope, crossSeeds *crossSeedGroups, stats *ruleRunStats, allTorrents []qbt.Torrent) {
	conditions := rule.Conditions
	if conditions == nil {
		return
	}

	enterRule(evalCtx, rule, allTorrents, scope)

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
		if !tagActionHasTags(tagAction) {
			continue
		}
		if tagActionMissesTrackerHealth(tagAction, evalCtx) {
			// Skip tag processing for this rule
			if stats != nil {
				stats.TagSkippedMissingTrackerHealth++
			}
			continue
		}
		matches := processTagAction(rule, tagAction, torrent, state, evalCtx, crossSeeds)
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

// moveCheck is what a move finds before the cross-seed check.
type moveCheck int

const (
	moveConditionNotMet moveCheck = iota // also when the path does not render
	moveRelativePath
	moveAtDestination
	moveReady // the cross-seed check decides
)

type moveCheckKey struct {
	rule *models.Automation
	hash string
}

type checkedMove struct {
	path  string
	check moveCheck
}

func checkMove(action *models.MoveAction, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) (string, moveCheck) {
	resolvedPath, ok := renderPathTemplate(action.Path, torrent, state, evalCtx)
	switch {
	case !ok:
		return "", moveConditionNotMet
	case action.Condition != nil && !EvaluateConditionWithContext(action.Condition, torrent, evalCtx, 0):
		return resolvedPath, moveConditionNotMet
	// qBittorrent never reports a relative path back as the save path, so the move would repeat every run.
	case !pathcmp.IsAbsolute(resolvedPath):
		return resolvedPath, moveRelativePath
	case inSavePath(torrent, resolvedPath):
		return resolvedPath, moveAtDestination
	}
	return resolvedPath, moveReady
}

func (ctx *EvalContext) checkedMove(rule *models.Automation, hash string) (checkedMove, bool) {
	if ctx == nil {
		return checkedMove{}, false
	}
	checked, ok := ctx.moveChecks[moveCheckKey{rule, hash}]
	return checked, ok
}

func evaluateMoveAction(rule *models.Automation, action *models.MoveAction, torrent qbt.Torrent, evalCtx *EvalContext, crossSeeds *crossSeedGroups, stats *ruleRunStats, state *torrentDesiredState) {
	checked, ok := evalCtx.checkedMove(rule, torrent.Hash)
	if !ok {
		checked.path, checked.check = checkMove(action, torrent, state, evalCtx)
	}
	resolvedPath, check := checked.path, checked.check
	if check == moveRelativePath {
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
		}
	}

	// Only apply move if condition is met, not already in target path, and not blocked by cross-seed protection
	if check == moveReady && !shouldBlockMoveForCrossSeeds(torrent, action, crossSeeds, evalCtx) {
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

	switch check {
	case moveConditionNotMet, moveRelativePath:
		stats.MoveConditionNotMet++
	case moveAtDestination:
		stats.MoveAlreadyAtDestination++
	case moveReady:
		stats.MoveBlockedByCrossSeed++
	}
}

func shouldBlockCategoryChangeForCrossSeeds(torrent qbt.Torrent, protectedCategories []string, crossSeeds *crossSeedGroups) bool {
	if len(protectedCategories) == 0 {
		return false
	}
	group := crossSeeds.members(torrent.Hash)
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

func crossSeedGroupKnown(torrent qbt.Torrent, crossSeeds *crossSeedGroups, evalCtx *EvalContext) bool {
	if evalCtx == nil {
		return true
	}
	// members reads the state of a torrent that no ask covered.
	members := crossSeeds.members(torrent.Hash)
	if !sameInstanceCrossSeedKnown(evalCtx, torrent.Hash) {
		return false
	}
	for _, member := range members {
		if !sameInstanceCrossSeedKnown(evalCtx, member.Hash) {
			return false
		}
	}
	return true
}

func shouldBlockMoveForCrossSeeds(torrent qbt.Torrent, moveAction *models.MoveAction, crossSeeds *crossSeedGroups, evalCtx *EvalContext) bool {
	if moveAction == nil {
		return false
	}
	if !crossSeedGroupKnown(torrent, crossSeeds, evalCtx) {
		return true
	}
	group := crossSeeds.members(torrent.Hash)
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

// renderPathTemplate returns the rendered Move or Export path. The path is executed as a
// Go template with data; paths with no template actions are unchanged. sanitize
// is available in templates for safe path segments (e.g. {{ sanitize .Name }}).
func renderPathTemplate(path string, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) (resolved string, ok bool) {
	tracker := ""
	if state != nil {
		tracker = selectTrackerTag(state.trackerDomains, true, evalCtx)
	}

	resolvedPath, err := executePathTemplate(path, pathTemplateData(torrent, tracker))
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

func tagActionHasTags(tagAction *models.TagAction) bool {
	return tagAction != nil && tagAction.Enabled && (len(tagAction.Tags) > 0 || tagAction.UseTrackerAsTag)
}

// tagActionMissesTrackerHealth reports whether the tag action reads IS_UNREGISTERED without the
// tracker health, so the rule loop skips it.
func tagActionMissesTrackerHealth(tagAction *models.TagAction, evalCtx *EvalContext) bool {
	return ConditionUsesField(tagAction.Condition, FieldIsUnregistered) && (evalCtx == nil || evalCtx.TrackerHealth == nil)
}

// processTagAction handles tag add/remove logic for a single tag action.
func processTagAction(rule *models.Automation, tagAction *models.TagAction, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext, crossSeeds *crossSeedGroups) bool {
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
	// Tracker-derived tags never expand: cross-seed siblings sit on other trackers.
	expandCrossSeeds := tagAction.IncludeCrossSeeds && !tagAction.UseTrackerAsTag
	// As category: a group whose file check did not complete gets no decision,
	// so the copies never diverge.
	if expandCrossSeeds && !crossSeedGroupKnown(torrent, crossSeeds, evalCtx) {
		if resetFromClient && rule != nil {
			keepCurrentTags(rule, tagsToManage, state)
		}
		return false
	}

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
		// action is the local change; assert is the steady-state decision a
		// matching copy that already looks right still pushes across its
		// cross-seed siblings (any-match-wins, and siblings added after the
		// first run). Full-mode removals happen on non-matches and stay local.
		action, assert := "", ""
		switch tagMode {
		case models.TagModeAdd:
			switch {
			case !hasTag && matchesCondition:
				action = "add"
			case hasTag && matchesCondition:
				assert = "add"
			}
		case models.TagModeRemove:
			switch {
			case hasTag && matchesCondition:
				action = "remove"
			case !hasTag && matchesCondition:
				assert = "remove"
			}
		default: // full (incl. unknown/empty)
			switch {
			case !hasTag && matchesCondition:
				action = "add"
			case hasTag && !matchesCondition:
				action = "remove"
			case hasTag && matchesCondition:
				assert = "add"
			}
		}
		if action != "" {
			state.tagActions[managedTag] = action
			if rule != nil {
				state.tagRuleByTag[managedTag] = ruleRef{id: rule.ID, name: rule.Name}
			}
			markTagReset(state, managedTag, resetFromClient && action == "add")
			if matchesCondition {
				assert = action
			}
		} else if rule != nil && len(crossSeeds.tagOwnerMembers(torrent.Hash)) > 1 &&
			(assert != "" || (tagMode != models.TagModeAdd && tagMode != models.TagModeRemove)) {
			// A no-op decision (full mode always decides) still owns the tag:
			// another rule's expansion must not override it, or the copy
			// flips on every other run.
			if _, decided := state.tagRuleByTag[managedTag]; !decided {
				state.tagRuleByTag[managedTag] = ruleRef{id: rule.ID, name: rule.Name}
			}
		}
		// A later rule's decision without expansion clears earlier intent (last rule wins).
		if assert == "" || !expandCrossSeeds || rule == nil {
			if action != "" {
				delete(state.tagExpandByTag, managedTag)
			}
			continue
		}
		if state.tagExpandByTag == nil {
			state.tagExpandByTag = make(map[string]tagExpandIntent)
		}
		state.tagExpandByTag[managedTag] = tagExpandIntent{
			rule:            ruleRef{id: rule.ID, name: rule.Name},
			action:          assert,
			resetFromClient: resetFromClient,
		}
	}

	return matchesCondition
}

// keepResetTagsOfRule keeps the tags of a torrent that dropped out of the rule. ADR 0039.
func keepResetTagsOfRule(rule *models.Automation, state *torrentDesiredState) {
	for _, action := range rule.Conditions.TagActions() {
		if shouldResetTagActionInClient(action) {
			keepCurrentTags(rule, action.Tags, state)
		}
	}
}

// keepCurrentTags adds back each of the tags that the torrent has. A managed reset deletes the
// tag from every torrent first, so a torrent that gets no decision keeps the tags it has.
func keepCurrentTags(rule *models.Automation, tags []string, state *torrentDesiredState) {
	for tag := range parseTorrentTags(strings.Join(tags, ",")) {
		_, hasTag := state.currentTags[tag]
		if _, pending := state.tagActions[tag]; hasTag && !pending {
			state.tagActions[tag] = "add"
			state.tagRuleByTag[tag] = ruleRef{id: rule.ID, name: rule.Name}
			markTagReset(state, tag, true)
		}
	}
}

// markTagReset records or clears a managed-reset re-add for tag, so the preview
// counts it as the live path does (the tag is wholesale-deleted first).
func markTagReset(state *torrentDesiredState, tag string, reset bool) {
	if !reset {
		delete(state.tagResetTags, tag)
		return
	}
	if state.tagResetTags == nil {
		state.tagResetTags = make(map[string]struct{})
	}
	state.tagResetTags[tag] = struct{}{}
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
		len(state.tagExpandByTag) > 0 ||
		len(state.tagRuleByTag) > 0 ||
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

// sortFieldsError reports the first field of config that cannot sort or score.
// unknown starts the error text for a simple sort field; rule validation and the sort word it differently.
func sortFieldsError(config *models.SortingConfig, unknown string) error {
	if config.Type == models.SortingTypeSimple && !ConditionFields[config.Field].Sortable() {
		return fmt.Errorf("%s: %s", unknown, config.Field)
	}
	if config.Type != models.SortingTypeScore {
		return nil
	}
	for _, r := range config.ScoreRules {
		if r.Type == models.ScoreRuleTypeFieldMultiplier && r.FieldMultiplier != nil && !ConditionFields[r.FieldMultiplier.Field].Scorable() {
			return fmt.Errorf("field multiplier requires numeric field, got: %s", r.FieldMultiplier.Field)
		}
	}
	return nil
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
				if read := ConditionFields[rule.FieldMultiplier.Field].Sort.Number; read != nil {
					totalScore += read(torrent, evalCtx) * rule.FieldMultiplier.Multiplier
				}
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
		if err := sortFieldsError(config, "unsupported sort field"); err != nil {
			return err
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

	var sortValue SortValue
	if config != nil && config.Type == models.SortingTypeSimple {
		sortValue = ConditionFields[config.Field].Sort
	}

	sort.Slice(torrents, func(i, j int) bool {
		return compareTorrents(torrents[i], torrents[j], config, sortValue, scores, evalCtx)
	})

	return nil
}

// compareTorrents is a helper function that returns true if t1 should sort before t2.
func compareTorrents(t1, t2 qbt.Torrent, config *models.SortingConfig, sortValue SortValue, scores map[string]float64, evalCtx *EvalContext) bool {
	// 1. Primary Sort
	if config != nil {
		switch config.Type {
		case models.SortingTypeSimple:
			if sortValue.Number != nil {
				v1 := sortValue.Number(t1, evalCtx)
				v2 := sortValue.Number(t2, evalCtx)
				if v1 != v2 {
					if config.Direction == models.SortDirectionASC {
						return v1 < v2
					}
					return v1 > v2
				}
			} else if sortValue.Text != nil {
				v1 := sortValue.Text(t1)
				v2 := sortValue.Text(t2)
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
