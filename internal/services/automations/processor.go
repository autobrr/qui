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

type torrentDesiredState struct {
	hash           string
	name           string
	trackerDomains []string

	uploadLimitKiB   *int64
	downloadLimitKiB *int64
	uploadRule       ruleRef
	downloadRule     ruleRef

	ratioLimit       *float64
	seedingMinutes   *int64
	shareLimitAction string
	shareLimitsMode  string
	ratioRule        ruleRef
	seedingRule      ruleRef
	shareActionRule  ruleRef
	shareModeRule    ruleRef

	shouldPause bool
	pauseRule   ruleRef

	shouldResume bool
	resumeRule   ruleRef

	shouldRecheck bool
	recheckRule   ruleRef

	shouldReannounce bool
	reannounceRule   ruleRef

	shouldAutoManage bool
	autoManageValue  bool
	autoManageRule   ruleRef

	currentTags    map[string]struct{}
	tagActions     map[string]string
	tagRuleByTag   map[string]ruleRef
	tagExpandByTag map[string]tagExpandIntent
	tagResetTags   map[string]struct{}

	category                  *string
	categoryGroupID           string
	categoryRuleID            int
	categoryIncludeCrossSeeds bool
	categoryRule              ruleRef

	shouldMove           bool
	movePath             string
	moveGroupID          string
	moveAtomic           string
	moveBlockIfCrossSeed bool
	moveCondition        *models.RuleCondition
	moveRuleID           int
	moveRuleName         string
	moveRule             ruleRef

	externalProgramID *int
	programRuleID     int
	programRuleName   string

	exportToInstance         *models.ExportToInstanceAction
	exportToInstanceRuleID   int
	exportToInstanceRuleName string

	transferToInstance     *models.TransferToInstanceAction
	transferToInstanceRule ruleRef
}

type ruleRef struct {
	id   int
	name string
}

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
	movePathWarned                   bool
	ExternalProgramApplied           int
	ExternalProgramConditionNotMet   int
	ExportToInstanceApplied          int
	ExportToInstanceConditionNotMet  int
	TransferApplied                  int
	TransferConditionNotMet          int
}

func (s *ruleRunStats) totalApplied() int {
	if s == nil {
		return 0
	}
	return s.SpeedApplied + s.ShareApplied + s.PauseApplied + s.ResumeApplied + s.RecheckApplied + s.ReannounceApplied + s.AutoManageApplied + s.TagConditionMet + s.CategoryApplied + s.DeleteApplied + s.MoveApplied + s.ExternalProgramApplied + s.ExportToInstanceApplied + s.TransferApplied
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

func processTorrents(
	torrents []*qbt.Torrent,
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

	for _, torrent := range torrents {
		state := applyRulesToTorrent(torrent, rules, states[torrent.Hash], evalCtx, crossSeeds, scope, stats, torrents)
		if state != nil && hasActions(state) {
			states[torrent.Hash] = state
		}
	}

	return states
}

func applyRulesToTorrent(torrent *qbt.Torrent, rules []*models.Automation, existing *torrentDesiredState, evalCtx *EvalContext, crossSeeds *crossSeedGroups, scope *ruleScope, stats map[int]*ruleRunStats, allTorrents []*qbt.Torrent) *torrentDesiredState {
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

func askActionGroups(torrents []*qbt.Torrent, rules []*models.Automation, evalCtx *EvalContext, crossSeeds *crossSeedGroups, scope *ruleScope, states map[string]*torrentDesiredState) {
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
	for _, torrent := range torrents {
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
	if slices.ContainsFunc(moveRules, moveBlockReadsCrossSeedFacts) {
		var members []string
		for _, hash := range moveHashes {
			members = append(members, memberHashes(crossSeeds.members(hash))...)
		}
		slices.Sort(members)
		crossSeeds.session.resolveFacts(slices.Compact(members))
	}
}

func groupReadAsks(torrent *qbt.Torrent, rules []*models.Automation, asksTagScope map[*models.Automation]bool, torrents []*qbt.Torrent, evalCtx *EvalContext, scope *ruleScope, asked map[askedGroup]struct{}) []string {
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

type askedGroup struct {
	rule *models.Automation
	key  string
}

func groupIDMembers(rule *models.Automation, groupID, hash string, torrents []*qbt.Torrent, evalCtx *EvalContext, scope *ruleScope, asked map[askedGroup]struct{}) []string {
	idx := getOrBuildGroupIndexForRule(evalCtx, rule, groupID, torrents, scope)
	group := askedGroup{rule, idx.KeyForHash(hash)}
	if _, seen := asked[group]; seen {
		return nil
	}
	asked[group] = struct{}{}
	return idx.MembersForHash(hash)
}

func moveAsks(torrent *qbt.Torrent, rules []*models.Automation, torrents []*qbt.Torrent, evalCtx *EvalContext, scope *ruleScope, asked map[askedGroup]struct{}) []string {
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

func enterRule(evalCtx *EvalContext, rule *models.Automation, allTorrents []*qbt.Torrent, scope *ruleScope) {
	if evalCtx == nil {
		return
	}
	evalCtx.activeFreeSpace = nil
	if rule != nil {
		evalCtx.activeFreeSpace = evalCtx.freeSpaceLedgers[rule.ID]
	}
	activateRuleGrouping(evalCtx, rule, allTorrents, scope)
}

func processRuleForTorrent(rule *models.Automation, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext, scope *ruleScope, crossSeeds *crossSeedGroups, stats *ruleRunStats, allTorrents []*qbt.Torrent) {
	conditions := rule.Conditions
	if conditions == nil {
		return
	}

	enterRule(evalCtx, rule, allTorrents, scope)

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

	if conditions.Pause != nil && conditions.Pause.Enabled {
		shouldApply := conditions.Pause.Condition == nil ||
			EvaluateConditionWithContext(conditions.Pause.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.PauseApplied++
			}
			if torrent.State != qbt.TorrentStatePausedUp && torrent.State != qbt.TorrentStatePausedDl &&
				torrent.State != qbt.TorrentStateStoppedUp && torrent.State != qbt.TorrentStateStoppedDl {
				state.shouldPause = true
				state.shouldResume = false
				state.pauseRule = ruleRef{id: rule.ID, name: rule.Name}
				state.resumeRule = ruleRef{}
			}
		} else if stats != nil {
			stats.PauseConditionNotMet++
		}
	}

	if conditions.Resume != nil && conditions.Resume.Enabled {
		shouldApply := conditions.Resume.Condition == nil ||
			EvaluateConditionWithContext(conditions.Resume.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.ResumeApplied++
			}

			if torrent.State == qbt.TorrentStatePausedUp || torrent.State == qbt.TorrentStatePausedDl ||
				torrent.State == qbt.TorrentStateStoppedUp || torrent.State == qbt.TorrentStateStoppedDl {
				state.shouldResume = true
				state.shouldPause = false
				state.resumeRule = ruleRef{id: rule.ID, name: rule.Name}
				state.pauseRule = ruleRef{}
			}
		} else if stats != nil {
			stats.ResumeConditionNotMet++
		}
	}

	if conditions.Recheck != nil && conditions.Recheck.Enabled {
		shouldApply := conditions.Recheck.Condition == nil ||
			EvaluateConditionWithContext(conditions.Recheck.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.RecheckApplied++
			}
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

	if conditions.AutoManagement != nil {
		shouldApply := conditions.AutoManagement.Condition == nil ||
			EvaluateConditionWithContext(conditions.AutoManagement.Condition, torrent, evalCtx, 0)

		if shouldApply {
			if stats != nil {
				stats.AutoManageApplied++
			}
			state.autoManageValue = conditions.AutoManagement.Enabled
			state.autoManageRule = ruleRef{id: rule.ID, name: rule.Name}
			state.shouldAutoManage = torrent.AutoManaged != state.autoManageValue
		} else if stats != nil {
			stats.AutoManageConditionNotMet++
		}
	}

	for _, tagAction := range conditions.TagActions() {
		if !tagActionHasTags(tagAction) {
			continue
		}
		if tagActionMissesTrackerHealth(tagAction, evalCtx) {
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

	if transfer := conditions.TransferToInstance; transfer != nil && transfer.Enabled && transfer.TargetInstanceID > 0 {
		if transfer.Condition == nil || EvaluateConditionWithContext(transfer.Condition, torrent, evalCtx, 0) {
			if stats != nil {
				stats.TransferApplied++
			}
			state.transferToInstance = transfer
			state.transferToInstanceRule = ruleRef{id: rule.ID, name: rule.Name}
		} else if stats != nil {
			stats.TransferConditionNotMet++
		}
	}

	if conditions.Move != nil && conditions.Move.Enabled && !state.shouldMove {
		evaluateMoveAction(rule, conditions.Move, torrent, evalCtx, crossSeeds, stats, state)
	}
}

type moveCheck int

const (
	moveConditionNotMet moveCheck = iota
	moveRelativePath
	moveAtDestination
	moveReady
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

	if moveAction.Condition == nil {
		return false
	}

	for _, other := range group {
		if other.Hash == torrent.Hash {
			continue
		}
		if !EvaluateConditionWithContext(moveAction.Condition, *other, evalCtx, 0) {
			return true
		}
	}

	return false
}

func inSavePath(torrent qbt.Torrent, savePath string) bool {
	return normalizePath(torrent.SavePath) == normalizePath(savePath)
}

func renderPathTemplate(path string, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) (resolved string, ok bool) {
	return renderPathTemplateData(path, torrent.Hash, torrentPathData(torrent, state, evalCtx))
}

func renderExportSavePath(path string, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) (resolved string, ok bool) {
	data := torrentPathData(torrent, state, evalCtx)
	data["CurrentSavePath"] = torrent.SavePath
	return renderPathTemplateData(path, torrent.Hash, data)
}

func torrentPathData(torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) map[string]any {
	tracker := ""
	if state != nil {
		tracker = selectTrackerTag(state.trackerDomains, true, evalCtx)
	}
	return pathTemplateData(torrent, tracker, evalCtx)
}

func renderPathTemplateData(path, hash string, data map[string]any) (resolved string, ok bool) {
	resolvedPath, err := executePathTemplate(path, data)
	if err != nil {
		if _, ok := errors.AsType[template.ExecError](err); ok {
			log.Trace().Err(err).Str("path", path).Str("hash", hash).Msg("failed to render path template")
		} else {
			log.Error().Err(err).Str("path", path).Msg("failed to render path template")
		}
		return "", false
	}
	if resolvedPath == "" {
		return "", false
	}

	return resolvedPath, true
}

func renderMovePathSample(path string) (string, error) {
	sample := qbt.Torrent{Name: "sample", Hash: strings.Repeat("0", 40), Category: "sample"}
	sampleCtx := &EvalContext{CategorySavePaths: map[string]string{"sample": "/data/sample", "": "/data"}}
	return executePathTemplate(path, pathTemplateData(sample, "tracker", sampleCtx))
}

func pathTemplateData(torrent qbt.Torrent, tracker string, evalCtx *EvalContext) map[string]any {
	data := map[string]any{
		"Name":                torrent.Name,
		"Hash":                torrent.Hash,
		"Category":            torrent.Category,
		"IsolationFolderName": pathutil.HashedIsolationFolder(torrent.Hash, torrent.Name),
		"Tracker":             tracker,
	}
	if evalCtx != nil {
		if categorySavePath, found := evalCtx.CategorySavePaths[torrent.Category]; found {
			data[categorySavePathVariable] = categorySavePath
		}
		if defaultSavePath, found := evalCtx.CategorySavePaths[""]; found {
			data[defaultSavePathVariable] = defaultSavePath
		}
	}
	return data
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

func tagActionMissesTrackerHealth(tagAction *models.TagAction, evalCtx *EvalContext) bool {
	return ConditionUsesField(tagAction.Condition, FieldIsUnregistered) && (evalCtx == nil || evalCtx.TrackerHealth == nil)
}

func processTagAction(rule *models.Automation, tagAction *models.TagAction, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext, crossSeeds *crossSeedGroups) bool {
	tagMode := tagAction.Mode
	if tagMode == "" {
		tagMode = models.TagModeFull
	}
	if state.tagRuleByTag == nil {
		state.tagRuleByTag = make(map[string]ruleRef)
	}

	matchesCondition := tagAction.Condition == nil ||
		EvaluateConditionWithContext(tagAction.Condition, torrent, evalCtx, 0)

	tagsToManage := tagAction.Tags
	if tagAction.UseTrackerAsTag && len(state.trackerDomains) > 0 {
		if tag := selectTrackerTag(state.trackerDomains, tagAction.UseDisplayName, evalCtx); tag != "" {
			tagsToManage = []string{tag}
		} else {
			tagsToManage = nil
		}
	}
	resetFromClient := shouldResetTagActionInClient(tagAction)
	expandCrossSeeds := tagAction.IncludeCrossSeeds && !tagAction.UseTrackerAsTag
	if expandCrossSeeds && !crossSeedGroupKnown(torrent, crossSeeds, evalCtx) {
		if resetFromClient && rule != nil {
			keepCurrentTags(rule, tagsToManage, state)
		}
		return false
	}

	for _, managedTag := range tagsToManage {
		_, hasTag := state.currentTags[managedTag]
		if resetFromClient {
			hasTag = false
		}
		if pending, ok := state.tagActions[managedTag]; ok {
			hasTag = (pending == "add")
		}

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
		default:
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
			if _, decided := state.tagRuleByTag[managedTag]; !decided {
				state.tagRuleByTag[managedTag] = ruleRef{id: rule.ID, name: rule.Name}
			}
		}
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

func keepResetTagsOfRule(rule *models.Automation, state *torrentDesiredState) {
	for _, action := range rule.Conditions.TagActions() {
		if shouldResetTagActionInClient(action) {
			keepCurrentTags(rule, action.Tags, state)
		}
	}
}

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
		state.exportToInstance != nil ||
		state.transferToInstance != nil
}

func selectTrackerTag(domains []string, useDisplayName bool, evalCtx *EvalContext) string {
	if len(domains) == 0 {
		return ""
	}

	if useDisplayName {
		if displayName, ok := getTrackerDisplayName(domains, evalCtx); ok {
			return displayName
		}
	}

	return domains[0]
}

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
	torrents map[string]*qbt.Torrent,
	hashes map[string]struct{},
	filesByHash map[string]qbt.TorrentFiles,
	hardlinks *HardlinkIndex,
) (int64, bool) {
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
		torrent := *torrents[hash]
		for _, file := range filesByHash[hash] {
			resolvedPath := resolvedTorrentFilePath(torrent, file.Name)
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

func SortTorrents(torrents []*qbt.Torrent, config *models.SortingConfig, evalCtx *EvalContext) error {
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

	var scores map[string]float64
	if config != nil && config.Type == models.SortingTypeScore {
		scores = make(map[string]float64, len(torrents))
		for _, t := range torrents {
			scores[t.Hash] = CalculateScore(*t, config, evalCtx)
		}
	}

	var sortValue SortValue
	if config != nil && config.Type == models.SortingTypeSimple {
		sortValue = ConditionFields[config.Field].Sort
	}

	sort.Slice(torrents, func(i, j int) bool {
		return compareTorrents(*torrents[i], *torrents[j], config, sortValue, scores, evalCtx)
	})

	return nil
}

func compareTorrents(t1, t2 qbt.Torrent, config *models.SortingConfig, sortValue SortValue, scores map[string]float64, evalCtx *EvalContext) bool {
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
			if t1.AddedOn != t2.AddedOn {
				return t1.AddedOn < t2.AddedOn
			}
		}
	} else if t1.AddedOn != t2.AddedOn {
		return t1.AddedOn < t2.AddedOn
	}

	return t1.Hash < t2.Hash
}

func getNowUnix(evalCtx *EvalContext) int64 {
	if evalCtx != nil && evalCtx.NowUnix != 0 {
		return evalCtx.NowUnix
	}
	return time.Now().Unix()
}

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
