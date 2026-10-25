// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"maps"
	"slices"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// runPlan is what one run of the Automation rules does: for each Action, the torrents it
// reaches and the rule that chose each torrent. The live run carries it out, and the dry
// run records it.
type runPlan struct {
	uploadLimits   plannedAction[int64] // limit in KiB
	downloadLimits plannedAction[int64] // limit in KiB
	shareLimits    plannedShareLimits
	pause          plannedTorrents
	resume         plannedTorrents
	recheck        plannedTorrents
	reannounce     plannedTorrents
	autoManage     plannedAction[bool] // true turns automatic torrent management on
	tags           map[string]*tagChange
	category       plannedAction[string] // by category, with the group members and cross-seeds
	move           plannedAction[string] // by save path, with the group members and cross-seeds
	programs       []pendingProgramExec
	exports        []pendingExportToInstance
	deletions      *deletionSelection
}

// targetLookup asks the target instance of an export if it already has the torrent. ADR 0005.
type targetLookup interface {
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
}

// exportMark is what the plan step found out about one export.
type exportMark int

const (
	exportReady exportMark = iota
	exportAlreadyOnTarget
	exportCheckFailed
	exportSavePathUnresolved
)

// plannedAction holds the torrents of one Action by target value, and the rule that chose
// each torrent.
type plannedAction[T comparable] struct {
	byTarget map[T][]string
	rules    map[string]ruleRef
}

func (a *plannedAction[T]) add(target T, hash string, rule ruleRef) {
	if a.byTarget == nil {
		a.byTarget = make(map[T][]string)
		a.rules = make(map[string]ruleRef)
	}
	a.byTarget[target] = append(a.byTarget[target], hash)
	a.rules[hash] = rule
}

// plannedTorrents holds the torrents of an Action without a target value.
type plannedTorrents struct {
	hashes []string
	rules  map[string]ruleRef
}

func (p *plannedTorrents) add(hash string, rule ruleRef) {
	if p.rules == nil {
		p.rules = make(map[string]ruleRef)
	}
	p.hashes = append(p.hashes, hash)
	p.rules[hash] = rule
}

// plannedShareLimits holds the share limits. The ratio, the seeding time, the share action
// and the mode of one torrent can each come from a different rule.
type plannedShareLimits struct {
	byTarget     map[shareKey][]string
	ratioRules   map[string]ruleRef
	seedingRules map[string]ruleRef
	actionRules  map[string]ruleRef
	modeRules    map[string]ruleRef
}

// runPlanInput is what the plan step reads.
type runPlanInput struct {
	instanceID    int
	states        map[string]*torrentDesiredState
	deletions     *deletionSelection
	torrents      []qbt.Torrent
	torrentByHash map[string]qbt.Torrent
	ruleByID      map[int]*models.Automation
	crossSeeds    crossSeedGroups
	evalCtx       *EvalContext
}

// buildRunPlan builds the Run plan from the desired state of each torrent and the Deletion
// selection. It only reads. A claimed Delete trigger and a torrent in the Deletion selection
// get no other Action (ADR 0027).
func (s *Service) buildRunPlan(ctx context.Context, in runPlanInput) runPlan {
	states, deletions, torrentByHash, evalCtx := in.states, in.deletions, in.torrentByHash, in.evalCtx
	var categoryTriggers, moveTriggers plannedAction[string]
	plan := runPlan{
		shareLimits: plannedShareLimits{
			byTarget:     make(map[shareKey][]string),
			ratioRules:   make(map[string]ruleRef),
			seedingRules: make(map[string]ruleRef),
			actionRules:  make(map[string]ruleRef),
			modeRules:    make(map[string]ruleRef),
		},
		tags:      make(map[string]*tagChange),
		deletions: deletions,
	}
	for hash, state := range states {
		// A delete trigger gets no other action, even when its delete was skipped or blocked.
		if deletions.isClaimed(hash) {
			continue
		}
		// Another torrent's delete expansion pulled this one in.
		if _, deleting := deletions.byHash[hash]; deleting {
			continue
		}
		torrent := torrentByHash[hash]

		// Speed limits - only plan the torrents whose current limit differs
		if state.uploadLimitKiB != nil && torrent.UpLimit != *state.uploadLimitKiB*1024 {
			plan.uploadLimits.add(*state.uploadLimitKiB, hash, state.uploadRule)
		}
		if state.downloadLimitKiB != nil && torrent.DlLimit != *state.downloadLimitKiB*1024 {
			plan.downloadLimits.add(*state.downloadLimitKiB, hash, state.downloadRule)
		}

		planShareLimits(&plan.shareLimits, hash, torrent, state)

		if state.shouldPause {
			plan.pause.add(hash, state.pauseRule)
		}
		if state.shouldResume {
			plan.resume.add(hash, state.resumeRule)
		}
		if state.shouldRecheck {
			plan.recheck.add(hash, state.recheckRule)
		}
		if state.shouldReannounce {
			plan.reannounce.add(hash, state.reannounceRule)
		}
		if state.shouldAutoManage {
			plan.autoManage.add(state.autoManageValue, hash, state.autoManageRule)
		}

		if change := plannedTagChange(state); change != nil {
			plan.tags[hash] = change
		}

		if state.category != nil && torrent.Category != *state.category {
			categoryTriggers.add(*state.category, hash, state.categoryRule)
		}
		if state.shouldMove {
			moveTriggers.add(state.movePath, hash, state.moveRule)
		}

		if state.externalProgramID != nil {
			plan.programs = append(plan.programs, pendingProgramExec{
				hash:      hash,
				torrent:   torrent,
				programID: *state.externalProgramID,
				ruleID:    state.programRuleID,
				ruleName:  state.programRuleName,
			})
		}

		if state.exportToInstance != nil {
			plan.exports = append(plan.exports, s.planExport(ctx, hash, torrent, state, evalCtx))
		}
	}
	plan.category = s.planCategories(ctx, in, categoryTriggers)
	plan.move = s.planMoves(ctx, in, moveTriggers)
	return plan
}

// planExport asks the target instance if it already has the torrent and resolves the save path.
func (s *Service) planExport(ctx context.Context, hash string, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) pendingExportToInstance {
	action := state.exportToInstance
	export := pendingExportToInstance{
		hash:     hash,
		torrent:  torrent,
		action:   action,
		ruleID:   state.exportToInstanceRuleID,
		ruleName: state.exportToInstanceRuleName,
	}

	// A bounded timeout keeps a slow or unreachable target from stalling the whole run.
	checkCtx, checkCancel := context.WithTimeout(ctx, 10*time.Second)
	_, exists, err := s.targetLookup.HasTorrentByAnyHash(checkCtx, action.TargetInstanceID, []string{hash})
	checkCancel()
	if err != nil {
		log.Warn().Err(err).
			Str("hash", hash).
			Int("targetInstanceID", action.TargetInstanceID).
			Msg("automations: failed to check target instance for existing torrent")
		export.mark = exportCheckFailed
		export.failureReason = "Failed to check target instance: " + err.Error()
		return export
	}
	if exists {
		log.Debug().
			Str("hash", hash).
			Int("targetInstanceID", action.TargetInstanceID).
			Msg("automations: torrent already exists on target instance, skipping export")
		export.mark = exportAlreadyOnTarget
		return export
	}

	if action.SavePath == "" {
		return export
	}
	resolved, ok := renderPathTemplate(action.SavePath, torrent, state, evalCtx)
	if !ok {
		log.Warn().
			Str("hash", hash).
			Str("rawPath", action.SavePath).
			Str("rule", state.exportToInstanceRuleName).
			Msg("automations: save path template resolution failed")
		export.mark = exportSavePathUnresolved
		export.failureReason = "Save path template resolution failed"
		return export
	}
	export.resolvedSavePath = resolved
	return export
}

// planShareLimits adds the torrent when a share limit (ratio, seeding time, action, mode)
// differs from the target.
func planShareLimits(plan *plannedShareLimits, hash string, torrent qbt.Torrent, state *torrentDesiredState) {
	if state.ratioLimit == nil && state.seedingMinutes == nil &&
		state.shareLimitAction == "" && state.shareLimitsMode == "" {
		return
	}
	ratio := torrent.RatioLimit
	seedMinutes := torrent.SeedingTimeLimit
	inactiveMinutes := torrent.InactiveSeedingTimeLimit // Preserve inactive limit

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

	ratioNeedsUpdate := state.ratioLimit != nil && currentRatio != ratio
	seedingNeedsUpdate := state.seedingMinutes != nil && torrent.SeedingTimeLimit != seedMinutes
	actionNeedsUpdate := state.shareLimitAction != "" &&
		normalizeShareLimitEnum(torrent.ShareLimitAction) != normalizeShareLimitEnum(state.shareLimitAction)
	modeNeedsUpdate := state.shareLimitsMode != "" &&
		normalizeShareLimitEnum(torrent.ShareLimitsMode) != normalizeShareLimitEnum(state.shareLimitsMode)
	if !ratioNeedsUpdate && !seedingNeedsUpdate && !actionNeedsUpdate && !modeNeedsUpdate {
		return
	}
	key := shareKey{ratio: ratio, seed: seedMinutes, inactive: inactiveMinutes, action: state.shareLimitAction, mode: state.shareLimitsMode}
	plan.byTarget[key] = append(plan.byTarget[key], hash)
	if ratioNeedsUpdate {
		plan.ratioRules[hash] = state.ratioRule
	}
	if seedingNeedsUpdate {
		plan.seedingRules[hash] = state.seedingRule
	}
	if actionNeedsUpdate {
		plan.actionRules[hash] = shareLimitRuleRef(state.shareActionRule, state.ratioRule)
	}
	if modeNeedsUpdate {
		plan.modeRules[hash] = shareLimitRuleRef(state.shareModeRule, state.seedingRule)
	}
}

// plannedTagChange returns the tag change of the torrent, or nil when no tag changes.
func plannedTagChange(state *torrentDesiredState) *tagChange {
	if len(state.tagActions) == 0 {
		return nil
	}
	var toAdd, toRemove []string
	desired := make(map[string]struct{})
	for t := range state.currentTags {
		desired[t] = struct{}{}
	}
	for tag, action := range state.tagActions {
		switch action {
		case "add":
			toAdd = append(toAdd, tag)
			desired[tag] = struct{}{}
		case "remove":
			toRemove = append(toRemove, tag)
			delete(desired, tag)
		}
	}
	if len(toAdd) == 0 && len(toRemove) == 0 {
		return nil
	}
	return &tagChange{
		current:   state.currentTags,
		desired:   desired,
		toAdd:     toAdd,
		toRemove:  toRemove,
		ruleByTag: state.tagRuleByTag,
	}
}

// planCategories expands the category of each trigger to the group members and the cross-seeds
// that the winning rule asks for. A pulled-in torrent keeps the rule of the torrent that pulled it in.
func (s *Service) planCategories(ctx context.Context, in runPlanInput, triggers plannedAction[string]) plannedAction[string] {
	var planned plannedAction[string]
	ruleByHash := triggers.rules
	for _, category := range slices.Sorted(maps.Keys(triggers.byTarget)) {
		hashes := triggers.byTarget[category]
		expandedHashes := make([]string, 0, len(hashes))

		type groupExpandKey struct {
			ruleID  int
			groupID string
		}

		// Find torrents whose winning category rule requested grouping expansion (via groupId
		// or legacy includeCrossSeeds), then expand other torrents that share the same group key.
		keysToExpand := make(map[groupExpandKey]map[string]struct{})
		groupIndexByKey := make(map[groupExpandKey]*groupIndex)
		groupEligibilityByKey := make(map[groupExpandKey]map[string]bool)
		ruleByGroupKey := make(map[groupExpandKey]ruleRef)
		for _, hash := range hashes {
			state, exists := in.states[hash]
			if !exists || state == nil {
				continue
			}
			if state.categoryGroupID == "" || state.categoryRuleID <= 0 {
				expandedHashes = append(expandedHashes, hash)
				continue
			}
			rule := in.ruleByID[state.categoryRuleID]
			if rule == nil {
				// GroupId semantics are strict all-or-none: unresolved grouping rules are skipped.
				// Legacy includeCrossSeeds should still apply to the trigger even if expansion can't be resolved.
				if state.categoryIncludeCrossSeeds {
					expandedHashes = append(expandedHashes, hash)
				}
				continue
			}

			catAction := getCategoryAction(rule)

			k := groupExpandKey{ruleID: rule.ID, groupID: state.categoryGroupID}
			idx := groupIndexByKey[k]
			if idx == nil {
				idx = getOrBuildGroupIndexForRule(in.evalCtx, rule, state.categoryGroupID, in.torrents, s.syncManager)
				groupIndexByKey[k] = idx
			}
			if idx == nil {
				continue
			}

			groupKey := idx.KeyForHash(hash)
			if groupKey == "" {
				continue
			}
			if groupEligibilityByKey[k] == nil {
				groupEligibilityByKey[k] = make(map[string]bool)
			}
			eligible, computed := groupEligibilityByKey[k][groupKey]
			if !computed {
				eligible = s.shouldExpandGroupWithAmbiguityPolicy(ctx, in.instanceID, rule, state.categoryGroupID, idx, hash, in.torrentByHash)
				// GroupId is strict all-or-none: every member must satisfy the action condition.
				if eligible {
					if in.evalCtx != nil && catAction.condition != nil && ConditionUsesField(catAction.condition, FieldFreeSpace) {
						in.evalCtx.LoadFreeSpaceSourceState(GetFreeSpaceRuleKey(rule))
					}
					activateRuleGrouping(in.evalCtx, rule, in.torrents, s.syncManager)
					members := idx.MembersForHash(hash)
					eligible = allGroupMembersMatchCategoryAction(members, in.torrentByHash, catAction, in.evalCtx, in.crossSeeds)
				}
				groupEligibilityByKey[k][groupKey] = eligible
			}
			if !eligible {
				continue
			}

			expandedHashes = append(expandedHashes, hash)
			if keysToExpand[k] == nil {
				keysToExpand[k] = make(map[string]struct{})
			}
			keysToExpand[k][groupKey] = struct{}{}
			if _, exists := ruleByGroupKey[k]; !exists {
				ruleByGroupKey[k] = ruleRef{id: rule.ID, name: rule.Name}
			}
		}

		if len(keysToExpand) > 0 {
			expandedSet := make(map[string]struct{})
			for _, h := range expandedHashes {
				expandedSet[h] = struct{}{}
			}

			for _, t := range in.torrents {
				if t.Category == category {
					continue // Already in target category
				}
				if _, deleting := in.deletions.byHash[t.Hash]; deleting {
					continue
				}
				if _, exists := expandedSet[t.Hash]; exists {
					continue // Already in batch
				}
				// CRITICAL: Don't override torrent's own computed desired category
				// If this torrent has its own category set by rules, respect "last rule wins"
				if state, hasState := in.states[t.Hash]; hasState && state.category != nil {
					if *state.category != category {
						continue // Torrent's winning rule chose a different category
					}
				}
				for k, keySet := range keysToExpand {
					gk := groupIndexByKey[k].KeyForHash(t.Hash)
					if gk == "" {
						continue
					}
					if _, ok := keySet[gk]; ok {
						expandedHashes = append(expandedHashes, t.Hash)
						expandedSet[t.Hash] = struct{}{}
						if _, exists := ruleByHash[t.Hash]; !exists {
							ruleByHash[t.Hash] = ruleByGroupKey[k]
						}
						break
					}
				}
			}
		}

		for _, added := range categoryCrossSeedExpansion(category, expandedHashes, in.states, in.crossSeeds, in.deletions.byHash) {
			expandedHashes = append(expandedHashes, added.hash)
			if _, exists := ruleByHash[added.hash]; !exists {
				if ref, ok := ruleByHash[added.trigger]; ok {
					ruleByHash[added.hash] = ref
				}
			}
		}

		for _, hash := range expandedHashes {
			planned.add(category, hash, ruleByHash[hash])
		}
	}
	return planned
}

// planMoves expands the move of each trigger to the group members and the cross-seeds that
// share files with it. A torrent already at the target path stays out, and a torrent moves to
// one path only. A pulled-in torrent keeps the rule of the torrent that pulled it in.
func (s *Service) planMoves(ctx context.Context, in runPlanInput, triggers plannedAction[string]) plannedAction[string] {
	var planned plannedAction[string]
	ruleByHash := triggers.rules
	movedHashes := make(map[string]struct{})
	for _, path := range slices.Sorted(maps.Keys(triggers.byTarget)) {
		hashes := triggers.byTarget[path]
		ruleByCrossSeedKey := crossSeedRuleRefsByKey(hashes, in.torrentByHash, ruleByHash, in.crossSeeds)

		normalizedDest := normalizePath(path)

		// Before moving, we need to expand move targets to avoid breaking related torrents.
		var expandedHashes []string
		type ruleGroupKey struct {
			ruleID  int
			groupID string
		}
		groupIndexByKey := make(map[ruleGroupKey]*groupIndex)

		legacyKeysToExpand := make(map[string]struct{})

		for _, hash := range hashes {
			if _, exists := movedHashes[hash]; exists {
				continue // Already planned for a path
			}

			state := in.states[hash]
			if state != nil && state.moveGroupID != "" {
				var rule *models.Automation
				if state.moveRuleID > 0 {
					rule = in.ruleByID[state.moveRuleID]
				}

				// GroupId set but rule can't be resolved: skip (strict all-or-none semantics).
				if rule == nil {
					continue
				}

				rgk := ruleGroupKey{ruleID: rule.ID, groupID: state.moveGroupID}
				idx := groupIndexByKey[rgk]
				if idx == nil {
					idx = getOrBuildGroupIndexForRule(in.evalCtx, rule, state.moveGroupID, in.torrents, s.syncManager)
					groupIndexByKey[rgk] = idx
				}

				members := []string{hash}
				if idx != nil {
					if m := idx.MembersForHash(hash); len(m) > 0 {
						members = m
					}
				}
				if !groupHoldsSharedFiles(members, in.crossSeeds) {
					continue
				}

				// Ambiguous ContentPath safety (ContentPath == SavePath): verify overlap or skip.
				if !s.shouldExpandGroupWithAmbiguityPolicy(ctx, in.instanceID, rule, state.moveGroupID, idx, hash, in.torrentByHash) {
					continue
				}

				// GroupId semantics are strict: every member must satisfy the move condition.
				var cond *models.RuleCondition
				if rule.Conditions != nil && rule.Conditions.Move != nil {
					cond = rule.Conditions.Move.Condition
				}
				if in.evalCtx != nil && cond != nil && ConditionUsesField(cond, FieldFreeSpace) {
					in.evalCtx.LoadFreeSpaceSourceState(GetFreeSpaceRuleKey(rule))
				}
				activateRuleGrouping(in.evalCtx, rule, in.torrents, s.syncManager)
				if !allGroupMembersMatchCondition(members, in.torrentByHash, cond, in.evalCtx) {
					continue
				}

				for _, memberHash := range members {
					if _, exists := movedHashes[memberHash]; exists {
						continue
					}
					if _, deleting := in.deletions.byHash[memberHash]; deleting {
						continue
					}
					memberTorrent, ok := in.torrentByHash[memberHash]
					if !ok {
						continue
					}
					if normalizePath(memberTorrent.SavePath) == normalizedDest {
						continue // Already in target path
					}
					expandedHashes = append(expandedHashes, memberHash)
					movedHashes[memberHash] = struct{}{}
					inheritRuleRefForMoveGroup(memberHash, hash, ruleByHash)
				}
				continue
			}

			// Legacy cross-seed expansion behavior
			expandedHashes = append(expandedHashes, hash)
			movedHashes[hash] = struct{}{}
			if t, exists := in.torrentByHash[hash]; exists {
				legacyKeysToExpand[in.crossSeeds.key(t)] = struct{}{}
			}
		}

		if len(legacyKeysToExpand) > 0 {
			for _, t := range in.torrents {
				if normalizePath(t.SavePath) == normalizedDest {
					continue // Already in target path
				}
				if _, exists := movedHashes[t.Hash]; exists {
					continue // Already planned for a path
				}
				if _, deleting := in.deletions.byHash[t.Hash]; deleting {
					continue
				}
				key := in.crossSeeds.key(t)
				if _, matched := legacyKeysToExpand[key]; matched {
					expandedHashes = append(expandedHashes, t.Hash)
					movedHashes[t.Hash] = struct{}{}
					inheritRuleRefForCrossSeed(t.Hash, key, ruleByHash, ruleByCrossSeedKey)
				}
			}
		}

		for _, hash := range expandedHashes {
			planned.add(path, hash, ruleByHash[hash])
		}
	}
	return planned
}
