// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"maps"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type runPlan struct {
	uploadLimits   plannedAction[int64]
	downloadLimits plannedAction[int64]
	shareLimits    plannedShareLimits
	pause          plannedTorrents
	resume         plannedTorrents
	recheck        plannedTorrents
	reannounce     plannedTorrents
	autoManage     plannedAction[bool]
	tags           map[string]*tagChange
	category       plannedAction[string]
	move           plannedAction[string]
	programs       []pendingProgramExec
	exports        []pendingExportToInstance
	transfers      []pendingTransfer
	deletions      *deletionSelection
}

type RuleSkip struct {
	Field    string `json:"field"`
	Requires string `json:"requires"`
}

type skippedRule struct {
	RuleSkip

	rule ruleRef
}

type reportedSkipKey struct {
	ruleKey

	dryRun bool
}

var versionedFields = func() []ConditionField {
	var fields []ConditionField
	for _, field := range slices.Sorted(maps.Keys(ConditionFields)) {
		if ConditionFields[field].Requires.Capability != nil {
			fields = append(fields, field)
		}
	}
	return fields
}()

func (s *Service) instanceSupports(ctx context.Context, instanceID int) func(qbittorrent.Capability) bool {
	client, err := s.syncManager.GetClient(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("automations: no client to read the instance capabilities")
		return func(qbittorrent.Capability) bool { return false }
	}
	if err := client.EnsureCapabilities(ctx); err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("automations: could not read the qBittorrent version")
	}
	return client.Supports
}

func ruleSkipOf(rule *models.Automation, supports func(qbittorrent.Capability) bool) (RuleSkip, bool) {
	for _, field := range versionedFields {
		requires := ConditionFields[field].Requires
		if !supports(*requires.Capability) && ruleUsesCondition(rule, field) {
			return RuleSkip{Field: string(field), Requires: requires.Label}, true
		}
	}
	return RuleSkip{}, false
}

func (s *Service) skipRulesWithUnknownFields(ctx context.Context, instanceID int, rules []*models.Automation, now time.Time) ([]*models.Automation, []skippedRule) {
	supports := s.instanceSupports(ctx, instanceID)
	runnable := make([]*models.Automation, 0, len(rules))
	var skipped []skippedRule
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rule := range rules {
		key := ruleKey{instanceID, rule.ID}
		skip, ok := ruleSkipOf(rule, supports)
		if !ok {
			delete(s.reportedSkips, reportedSkipKey{ruleKey: key})
			delete(s.reportedSkips, reportedSkipKey{ruleKey: key, dryRun: true})
			runnable = append(runnable, rule)
			continue
		}
		s.lastRuleRun[key] = now
		skipped = append(skipped, skippedRule{RuleSkip: skip, rule: ruleRef{id: rule.ID, name: rule.Name}})
	}
	return runnable, skipped
}

func (s *Service) recordRuleSkips(ctx context.Context, instanceID int, rules []skippedRule, force, dryRun bool) []*models.AutomationActivity {
	if s.activityStore == nil {
		return nil
	}
	outcome := models.ActivityOutcomeSkipped
	if dryRun {
		outcome = models.ActivityOutcomeDryRun
	}
	var activities []*models.AutomationActivity
	for _, skipped := range rules {
		key := reportedSkipKey{instanceID: instanceID, ruleID: skipped.rule.id, dryRun: dryRun}
		s.mu.RLock()
		reported := s.reportedSkips[key] == skipped.RuleSkip
		s.mu.RUnlock()
		if reported && !force {
			continue
		}
		details, _ := json.Marshal(skipped.RuleSkip) //nolint:errchkjson // string fields cannot fail to encode
		activity := &models.AutomationActivity{
			InstanceID: instanceID,
			Action:     models.ActivityActionRuleSkipped,
			RuleID:     &skipped.rule.id,
			RuleName:   skipped.rule.name,
			Outcome:    outcome,
			Details:    details,
			CreatedAt:  time.Now().UTC(),
		}
		id, err := s.activityStore.CreateWithID(ctx, activity)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Int("ruleID", skipped.rule.id).Msg("automations: failed to record a skipped rule")
			continue
		}
		activity.ID = id
		activities = append(activities, activity)
		s.mu.Lock()
		s.reportedSkips[key] = skipped.RuleSkip
		s.mu.Unlock()
	}
	s.publishActivity(instanceID, activities)
	return activities
}

type targetLookup interface {
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
}

type exportMark int

const (
	exportReady exportMark = iota
	exportAlreadyOnTarget
	exportCheckFailed
	exportSavePathUnresolved
)

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

type plannedShareLimits struct {
	byTarget     map[shareKey][]string
	ratioRules   map[string]ruleRef
	seedingRules map[string]ruleRef
	actionRules  map[string]ruleRef
	modeRules    map[string]ruleRef
}

type runPlanInput struct {
	instanceID    int
	states        map[string]*torrentDesiredState
	deletions     *deletionSelection
	torrents      []*qbt.Torrent
	torrentByHash map[string]*qbt.Torrent
	ruleByID      map[int]*models.Automation
	crossSeeds    *crossSeedGroups
	evalCtx       *EvalContext
	scope         *ruleScope
}

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
		if _, deleting := deletions.byHash[hash]; deleting || deletions.isClaimed(hash) {
			state.tagExpandByTag = nil
		}
	}
	expandTagStatesForCrossSeeds(states, in.ruleByID, in.crossSeeds)
	failedTransferTargets := map[int]bool{}
	for hash, state := range states {
		if deletions.isClaimed(hash) {
			continue
		}
		if _, deleting := deletions.byHash[hash]; deleting {
			continue
		}
		torrent := *torrentByHash[hash]

		planSpeedAndState(&plan, hash, torrent, state)
		planShareLimits(&plan.shareLimits, hash, torrent, state)

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
			export := s.planExport(ctx, hash, torrent, state, evalCtx)
			export.trackerDomain = in.scope.tracker(&torrent)
			plan.exports = append(plan.exports, export)
		}

		if state.transferToInstance != nil && s.transferRunner != nil {
			if pending, ok := s.planTransfer(ctx, in.instanceID, hash, torrent, state, evalCtx, failedTransferTargets); ok {
				pending.trackerDomain = in.scope.tracker(&torrent)
				plan.transfers = append(plan.transfers, pending)
			}
		}
	}
	plan.category = s.planCategories(ctx, in, categoryTriggers)
	plan.move = s.planMoves(ctx, in, moveTriggers)
	plan.transfers = transfersAfterExports(transfersAfterMoves(plan.transfers, plan.move.rules), plan.exports)
	var waitingExports []pendingExportToInstance
	plan.exports, waitingExports = exportsAfterMoves(plan.exports, plan.move.rules)
	if len(plan.transfers) > 1 {
		pos := rulePositions(in.ruleByID)
		slices.SortFunc(plan.transfers, func(a, b pendingTransfer) int {
			return cmp.Or(cmp.Compare(pos[b.rule.id], pos[a.rule.id]), strings.Compare(a.hash, b.hash))
		})
	}
	if len(waitingExports) > 0 {
		log.Debug().Int("instanceID", in.instanceID).Int("count", len(waitingExports)).
			Msg("automations: exports wait for a later run, their torrents are moving")
	}
	return plan
}

func exportsAfterMoves(exports []pendingExportToInstance, moved map[string]ruleRef) (ready, waiting []pendingExportToInstance) {
	for _, exec := range exports {
		_, movesNow := moved[exec.hash]
		if (movesNow || exec.torrent.State == qbt.TorrentStateMoving) && exec.failureReason == "" && strings.Contains(exec.action.SavePath, "CurrentSavePath") {
			waiting = append(waiting, exec)
			continue
		}
		ready = append(ready, exec)
	}
	return ready, waiting
}

func planSpeedAndState(plan *runPlan, hash string, torrent qbt.Torrent, state *torrentDesiredState) {
	if state.uploadLimitKiB != nil && torrent.UpLimit != *state.uploadLimitKiB*1024 {
		plan.uploadLimits.add(*state.uploadLimitKiB, hash, state.uploadRule)
	}
	if state.downloadLimitKiB != nil && torrent.DlLimit != *state.downloadLimitKiB*1024 {
		plan.downloadLimits.add(*state.downloadLimitKiB, hash, state.downloadRule)
	}
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
}

func (s *Service) planExport(ctx context.Context, hash string, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext) pendingExportToInstance {
	action := state.exportToInstance
	export := pendingExportToInstance{
		hash:     hash,
		torrent:  torrent,
		action:   action,
		ruleID:   state.exportToInstanceRuleID,
		ruleName: state.exportToInstanceRuleName,
	}

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
	resolved, ok := renderExportSavePath(action.SavePath, torrent, state, evalCtx)
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

func planShareLimits(plan *plannedShareLimits, hash string, torrent qbt.Torrent, state *torrentDesiredState) {
	if state.ratioLimit == nil && state.seedingMinutes == nil &&
		state.shareLimitAction == "" && state.shareLimitsMode == "" {
		return
	}
	ratio := torrent.RatioLimit
	seedMinutes := torrent.SeedingTimeLimit
	inactiveMinutes := torrent.InactiveSeedingTimeLimit

	if state.ratioLimit != nil {
		ratio = *state.ratioLimit
	}
	if state.seedingMinutes != nil {
		seedMinutes = *state.seedingMinutes
	}

	normalizeRatio := func(r float64) float64 {
		if r >= 0 {
			return float64(int(r*100+0.5)) / 100
		}
		return r
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

type groupExpandKey struct {
	ruleID  int
	groupID string
}

type categoryGroups struct {
	indexes  map[groupExpandKey]*groupIndex
	eligible map[groupExpandKey]map[string]bool
	expand   map[groupExpandKey]map[string]struct{}
	rules    map[groupExpandKey]ruleRef
}

func (s *Service) planCategories(ctx context.Context, in runPlanInput, triggers plannedAction[string]) plannedAction[string] {
	var planned plannedAction[string]
	ruleByHash := triggers.rules
	for _, category := range slices.Sorted(maps.Keys(triggers.byTarget)) {
		hashes := triggers.byTarget[category]
		expandedHashes := make([]string, 0, len(hashes))

		groups := &categoryGroups{
			indexes:  make(map[groupExpandKey]*groupIndex),
			eligible: make(map[groupExpandKey]map[string]bool),
			expand:   make(map[groupExpandKey]map[string]struct{}),
			rules:    make(map[groupExpandKey]ruleRef),
		}
		for _, hash := range hashes {
			if s.planCategoryTrigger(ctx, in, groups, hash) {
				expandedHashes = append(expandedHashes, hash)
			}
		}
		expandedHashes = groups.addMembers(in, category, expandedHashes, ruleByHash)

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

func (s *Service) planCategoryTrigger(ctx context.Context, in runPlanInput, groups *categoryGroups, hash string) bool {
	state, exists := in.states[hash]
	if !exists || state == nil {
		return false
	}
	if state.categoryGroupID == "" || state.categoryRuleID <= 0 {
		return true
	}
	rule := in.ruleByID[state.categoryRuleID]
	if rule == nil {
		return state.categoryIncludeCrossSeeds
	}

	k := groupExpandKey{ruleID: rule.ID, groupID: state.categoryGroupID}
	idx := groups.indexes[k]
	if idx == nil {
		idx = getOrBuildGroupIndexForRule(in.evalCtx, rule, state.categoryGroupID, in.torrents, in.scope)
		groups.indexes[k] = idx
	}
	if idx == nil {
		return false
	}

	groupKey := idx.KeyForHash(hash)
	if groupKey == "" {
		return false
	}
	if groups.eligible[k] == nil {
		groups.eligible[k] = make(map[string]bool)
	}
	eligible, computed := groups.eligible[k][groupKey]
	if !computed {
		eligible = s.categoryGroupEligible(ctx, in, rule, state.categoryGroupID, idx, hash)
		groups.eligible[k][groupKey] = eligible
	}
	if !eligible {
		return false
	}

	if groups.expand[k] == nil {
		groups.expand[k] = make(map[string]struct{})
	}
	groups.expand[k][groupKey] = struct{}{}
	if _, exists := groups.rules[k]; !exists {
		groups.rules[k] = ruleRef{id: rule.ID, name: rule.Name}
	}
	return true
}

func (s *Service) categoryGroupEligible(ctx context.Context, in runPlanInput, rule *models.Automation, groupID string, idx *groupIndex, hash string) bool {
	if !s.shouldExpandGroupWithAmbiguityPolicy(ctx, in.instanceID, rule, groupID, idx, hash, in.torrentByHash) {
		return false
	}
	catAction := getCategoryAction(rule)
	enterRule(in.evalCtx, rule, in.torrents, in.scope)
	members := idx.MembersForHash(hash)
	return allGroupMembersMatchCategoryAction(members, in.torrentByHash, catAction, in.evalCtx, in.crossSeeds) &&
		!in.scope.groupDropsMember(rule, members, in.torrentByHash)
}

func (g *categoryGroups) addMembers(in runPlanInput, category string, expandedHashes []string, ruleByHash map[string]ruleRef) []string {
	if len(g.expand) == 0 {
		return expandedHashes
	}
	expandedSet := make(map[string]struct{})
	for _, h := range expandedHashes {
		expandedSet[h] = struct{}{}
	}

	for _, t := range in.torrents {
		if t.Category == category {
			continue
		}
		if _, deleting := in.deletions.byHash[t.Hash]; deleting {
			continue
		}
		if _, exists := expandedSet[t.Hash]; exists {
			continue
		}
		if state, hasState := in.states[t.Hash]; hasState && state.category != nil && *state.category != category {
			continue
		}
		k, ok := g.groupOf(t.Hash)
		if !ok {
			continue
		}
		expandedHashes = append(expandedHashes, t.Hash)
		expandedSet[t.Hash] = struct{}{}
		if _, exists := ruleByHash[t.Hash]; !exists {
			ruleByHash[t.Hash] = g.rules[k]
		}
	}
	return expandedHashes
}

func (g *categoryGroups) groupOf(hash string) (groupExpandKey, bool) {
	for k, keySet := range g.expand {
		gk := g.indexes[k].KeyForHash(hash)
		if gk == "" {
			continue
		}
		if _, ok := keySet[gk]; ok {
			return k, true
		}
	}
	return groupExpandKey{}, false
}

type moveExpansion struct {
	in           runPlanInput
	ruleByHash   map[string]ruleRef
	moved        map[string]struct{}
	indexes      map[groupExpandKey]*groupIndex
	failedGroups map[ruleGroupKey]struct{}
}

func newMoveExpansion(in runPlanInput, ruleByHash map[string]ruleRef) *moveExpansion {
	return &moveExpansion{in: in, ruleByHash: ruleByHash, moved: make(map[string]struct{}), failedGroups: make(map[ruleGroupKey]struct{})}
}

func (s *Service) planMoves(ctx context.Context, in runPlanInput, triggers plannedAction[string]) plannedAction[string] {
	return s.expandMoves(ctx, newMoveExpansion(in, triggers.rules), triggers)
}

func (s *Service) expandMoves(ctx context.Context, m *moveExpansion, triggers plannedAction[string]) plannedAction[string] {
	in := m.in
	var planned plannedAction[string]
	for _, path := range slices.Sorted(maps.Keys(triggers.byTarget)) {
		hashes := triggers.byTarget[path]
		ruleByCrossSeedKey := crossSeedRuleRefsByKey(hashes, in.torrentByHash, m.ruleByHash, in.crossSeeds)
		normalizedDest := normalizePath(path)

		var expandedHashes, legacyTriggers []string
		m.indexes = make(map[groupExpandKey]*groupIndex)

		for _, hash := range hashes {
			if _, exists := m.moved[hash]; exists {
				continue
			}

			state := in.states[hash]
			if state != nil && state.moveGroupID != "" {
				expandedHashes = append(expandedHashes, s.planMoveGroup(ctx, m, state, hash, normalizedDest)...)
				continue
			}

			expandedHashes = append(expandedHashes, hash)
			m.moved[hash] = struct{}{}
			if _, exists := in.torrentByHash[hash]; exists {
				legacyTriggers = append(legacyTriggers, hash)
			}
		}

		expandedHashes = append(expandedHashes, m.legacyCrossSeeds(legacyTriggers, normalizedDest, ruleByCrossSeedKey)...)

		for _, hash := range expandedHashes {
			planned.add(path, hash, m.ruleByHash[hash])
		}
	}
	return planned
}

func (s *Service) planMoveGroup(ctx context.Context, m *moveExpansion, state *torrentDesiredState, hash, normalizedDest string) []string {
	in := m.in
	var rule *models.Automation
	if state.moveRuleID > 0 {
		rule = in.ruleByID[state.moveRuleID]
	}

	if rule == nil {
		return nil
	}

	rgk := groupExpandKey{ruleID: rule.ID, groupID: state.moveGroupID}
	idx := m.indexes[rgk]
	if idx == nil {
		idx = getOrBuildGroupIndexForRule(in.evalCtx, rule, state.moveGroupID, in.torrents, in.scope)
		m.indexes[rgk] = idx
	}

	groupKey := ruleGroupKey{ruleID: rule.ID, key: idx.KeyForHash(hash)}
	if _, failed := m.failedGroups[groupKey]; failed {
		return nil
	}

	members := []string{hash}
	if idx != nil {
		if found := idx.MembersForHash(hash); len(found) > 0 {
			members = found
		}
	}
	expand, verifyOverlap, minPercent := s.moveGroupChecks(in, rule, state.moveGroupID, idx, hash, members)
	if !expand {
		if groupKey.key != "" {
			m.failedGroups[groupKey] = struct{}{}
		}
		return nil
	}
	if verifyOverlap && !s.triggerOverlapsGroup(ctx, in.instanceID, idx, hash, in.torrentByHash, minPercent) {
		return nil
	}

	var expandedHashes []string
	for _, memberHash := range members {
		if !m.canMove(memberHash, normalizedDest) {
			continue
		}
		expandedHashes = append(expandedHashes, memberHash)
		m.moved[memberHash] = struct{}{}
		inheritRuleRefForMoveGroup(memberHash, hash, m.ruleByHash)
	}
	return expandedHashes
}

func (s *Service) moveGroupChecks(in runPlanInput, rule *models.Automation, groupID string, idx *groupIndex, hash string, members []string) (expand, verifyOverlap bool, minPercent int) {
	if !groupHoldsSharedFiles(members, in.crossSeeds) {
		return false, false, 0
	}
	expand, verifyOverlap, minPercent = s.groupAmbiguityPolicy(rule, groupID, idx, hash)
	if !expand {
		return false, false, 0
	}

	var cond *models.RuleCondition
	if rule.Conditions != nil && rule.Conditions.Move != nil {
		cond = rule.Conditions.Move.Condition
	}
	enterRule(in.evalCtx, rule, in.torrents, in.scope)
	if !allGroupMembersMatchCondition(members, in.torrentByHash, cond, in.evalCtx) ||
		in.scope.groupDropsMember(rule, members, in.torrentByHash) {
		return false, false, 0
	}
	return true, verifyOverlap, minPercent
}

func (m *moveExpansion) canMove(hash, normalizedDest string) bool {
	if _, exists := m.moved[hash]; exists {
		return false
	}
	if _, deleting := m.in.deletions.byHash[hash]; deleting {
		return false
	}
	t, ok := m.in.torrentByHash[hash]
	return ok && normalizePath(t.SavePath) != normalizedDest
}

func (m *moveExpansion) legacyCrossSeeds(triggers []string, normalizedDest string, ruleByCrossSeedKey map[string]ruleRef) []string {
	var expandedHashes []string
	walked := make(map[string]struct{})
	for _, trigger := range triggers {
		key := m.in.crossSeeds.key(*m.in.torrentByHash[trigger])
		if _, seen := walked[key]; seen {
			continue
		}
		walked[key] = struct{}{}
		for _, t := range m.in.crossSeeds.members(trigger) {
			if !m.canMove(t.Hash, normalizedDest) {
				continue
			}
			expandedHashes = append(expandedHashes, t.Hash)
			m.moved[t.Hash] = struct{}{}
			inheritRuleRefForCrossSeed(t.Hash, key, m.ruleByHash, ruleByCrossSeedKey)
		}
	}
	return expandedHashes
}

func (s *Service) planPreview(ctx context.Context, instanceID int, rule *models.Automation, keep models.ActionConditions, torrents []*qbt.Torrent, evalCtx *EvalContext, crossSeeds *crossSeedGroups, scope *ruleScope) (runPlan, map[string]struct{}) {
	keep.SchemaVersion = rule.Conditions.SchemaVersion
	keep.Grouping = rule.Conditions.Grouping
	only := *rule
	only.Conditions = &keep
	only.Enabled = true

	states := make(map[string]*torrentDesiredState)
	executeBatch(instanceID, []*models.Automation{&only}, torrents, evalCtx, crossSeeds, scope, nil, states)
	for i := range torrents {
		if scope.answer(rule, torrents[i]) == scopeNoValue {
			delete(states, torrents[i].Hash)
		}
	}

	torrentByHash := make(map[string]*qbt.Torrent, len(torrents))
	for _, t := range torrents {
		torrentByHash[t.Hash] = t
	}
	own := make(map[string]struct{})
	for hash, state := range states {
		if (state.category != nil && torrentByHash[hash].Category != *state.category) || plannedTagChange(state) != nil {
			own[hash] = struct{}{}
		}
	}

	plan := s.buildRunPlan(ctx, runPlanInput{
		instanceID:    instanceID,
		states:        states,
		deletions:     &deletionSelection{},
		torrents:      torrents,
		torrentByHash: torrentByHash,
		ruleByID:      map[int]*models.Automation{rule.ID: rule},
		crossSeeds:    crossSeeds,
		evalCtx:       evalCtx,
		scope:         scope,
	})
	return plan, own
}
