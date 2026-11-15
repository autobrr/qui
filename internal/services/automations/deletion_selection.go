// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

// deletionSelectionInput is what the Deletion selection reads. The run, the dry run and
// the preview all fill it.
type deletionSelectionInput struct {
	// rules in rule order. A rule without an enabled delete action is ignored.
	rules []*models.Automation
	// torrents are sorted in place by the sort order of each rule.
	torrents   []*qbt.Torrent
	evalCtx    *EvalContext
	crossSeeds *crossSeedGroups
	// scope is the Rule scope of the run. The dry run and the preview open it with no Skip window.
	scope *ruleScope
	// stats is optional and counts the delete conditions per rule.
	stats map[int]*ruleRunStats
}

// selectedDeletion is one torrent of the Deletion selection.
type selectedDeletion struct {
	hash       string
	rule       ruleRef
	deleteMode string // the mode of the rule
	batchMode  string // the mode that the delete call uses
	// trigger is true when the torrent's own delete condition chose it, and false when
	// the expansion of another Delete trigger pulled it in.
	trigger      bool
	hardlinkCopy bool
	logMsg       string
}

// deleteFilesForMode refuses a mode that is not a batch mode, such as a
// stored mode that qui no longer knows.
func deleteFilesForMode(mode string) (bool, error) {
	switch mode {
	case DeleteModeKeepFiles:
		return false, nil
	case DeleteModeWithFiles:
		return true, nil
	}
	return false, fmt.Errorf("unknown delete mode: %s", mode)
}

// ruleDeletes holds the torrents that one rule deletes with one mode.
type ruleDeletes struct {
	mode   string
	rule   ruleRef
	hashes []string
}

// groupDeletesByRule splits the queued deletes by rule and mode, in selection
// order, so that each delete names the rule that started it.
func groupDeletesByRule(sel *deletionSelection) []ruleDeletes {
	type key struct {
		mode string
		rule ruleRef
	}
	var groups []ruleDeletes
	index := make(map[key]int)
	for _, hash := range sel.order {
		pending, queued := sel.byHash[hash]
		if !queued {
			continue
		}
		k := key{pending.batchMode, pending.rule}
		i, ok := index[k]
		if !ok {
			i = len(groups)
			index[k] = i
			groups = append(groups, ruleDeletes{mode: pending.batchMode, rule: pending.rule})
		}
		groups[i].hashes = append(groups[i].hashes, hash)
	}
	return groups
}

func (d selectedDeletion) filesKept() bool {
	return d.batchMode == DeleteModeKeepFiles
}

// deletionSelection is the result of selectDeletions.
type deletionSelection struct {
	byHash map[string]selectedDeletion
	order  []string // selection order; a hash that is no longer in byHash is skipped
	// triggers holds every claimed Delete trigger. A trigger gets no other action, also
	// when its group expansion failed.
	triggers map[string]struct{}
	// failedRules holds the rules whose deletes were cancelled because a file list could
	// not be read in delete-with-files mode.
	failedRules map[int]error
}

func (d *deletionSelection) add(entry selectedDeletion) {
	if _, queued := d.byHash[entry.hash]; queued {
		return
	}
	d.byHash[entry.hash] = entry
	d.order = append(d.order, entry.hash)
}

func (d *deletionSelection) isClaimed(hash string) bool {
	_, claimed := d.triggers[hash]
	return claimed
}

type deletionClaim struct {
	hash string
	rule *models.Automation
}

// ruleGroupKey names one group of one rule.
type ruleGroupKey struct {
	ruleID int
	key    string
}

// selectDeletions builds the Deletion selection. The first delete rule that matches a
// torrent wins it. A FREE_SPACE rule stops when its condition is satisfied. The claimed
// triggers then expand to their Shared-file groups and cross-seeds in claim order.
func (s *Service) selectDeletions(ctx context.Context, instanceID int, in deletionSelectionInput) *deletionSelection {
	sel := &deletionSelection{
		byHash:      make(map[string]selectedDeletion),
		triggers:    make(map[string]struct{}),
		failedRules: make(map[int]error),
	}
	evalCtx := in.evalCtx
	// Build the map only for the modes that expand groups, so the preview of another mode
	// does not pay for it. The run builds the map later in any case.
	if evalCtx.torrentsByHash == nil && slices.ContainsFunc(in.rules, deleteExpandsGroups) {
		evalCtx.torrentsByHash = make(map[string]*qbt.Torrent, len(in.torrents))
		for _, torrent := range in.torrents {
			evalCtx.torrentsByHash[torrent.Hash] = torrent
		}
	}

	var claims []deletionClaim
	var sortedFor *models.Automation
	for _, rule := range in.rules {
		if rule.Conditions == nil || rule.Conditions.Delete == nil || !rule.Conditions.Delete.Enabled {
			continue
		}
		enterRule(evalCtx, rule, in.torrents, in.scope)
		if sortedFor == nil || !rulesCanShareSortingBatch(sortedFor, rule) {
			SortTorrentsWithFallback(in.torrents, rule.SortingConfig, evalCtx, instanceID, rule.Name)
			sortedFor = rule
		}
		ruleClaims, err := claimDeleteTriggers(rule, in, sel)
		if err != nil {
			sel.failedRules[rule.ID] = err
			evalCtx.activeFreeSpace.reset()
			continue
		}
		for _, claim := range ruleClaims {
			sel.triggers[claim.hash] = struct{}{}
		}
		claims = append(claims, ruleClaims...)
	}
	askDeleteGroups(claims, in.crossSeeds, evalCtx.hardlinkIndex)

	triggerTorrents := make(map[string]*qbt.Torrent, len(sel.triggers))
	for _, torrent := range in.torrents {
		if sel.isClaimed(torrent.Hash) {
			triggerTorrents[torrent.Hash] = torrent
		}
	}
	// Torrents that an include-cross-seeds group took, so a later group does not check them
	// again. A rule with hardlinks keeps its own set, so it still follows the hardlinks of a
	// torrent that a rule without hardlinks took.
	includedCrossSeeds := map[bool]map[string]struct{}{false: {}, true: {}}
	// Groups whose members do not all match, so the next member does not check them again.
	rejectedGroups := make(map[ruleGroupKey]struct{})
	for _, claim := range claims {
		// An earlier claim of the same rule already expanded this torrent. The expansion of
		// another rule can be narrower, so the claim still expands.
		if queued, ok := sel.byHash[claim.hash]; ok && queued.rule.id == claim.rule.ID {
			continue
		}
		s.expandDeleteTrigger(ctx, instanceID, in, sel, *triggerTorrents[claim.hash], claim.rule, includedCrossSeeds, rejectedGroups)
	}
	return sel
}

// askDeleteGroups asks the run session in one round for the groups of the
// claimed triggers that expand or preserve, with the hardlink copies that an
// include rule follows, and for the other-instance Shared-file check.
func askDeleteGroups(claims []deletionClaim, crossSeeds *crossSeedGroups, hardlinks *HardlinkIndex) {
	var hashes, linkHashes []string
	for _, claim := range claims {
		switch getDeleteMode(claim.rule) {
		case DeleteModeWithFilesIncludeCrossSeeds:
			if claim.rule.Conditions.Delete.IncludeHardlinks && hardlinks != nil {
				linkHashes = append(linkHashes, claim.hash)
			} else {
				hashes = append(hashes, claim.hash)
			}
		case DeleteModeWithFilesPreserveCrossSeeds:
			hashes = append(hashes, claim.hash)
		}
	}
	crossSeeds.resolveDeletes(hashes, linkHashes, hardlinks, true)
}

func deleteExpandsGroups(rule *models.Automation) bool {
	if rule.Conditions == nil || rule.Conditions.Delete == nil || !rule.Conditions.Delete.Enabled {
		return false
	}
	switch getDeleteMode(rule) {
	case DeleteModeWithFilesIncludeCrossSeeds:
		return true
	case DeleteModeKeepFiles:
		return strings.TrimSpace(rule.Conditions.Delete.GroupID) != ""
	}
	return false
}

// claimDeleteTriggers returns the torrents whose delete condition matches the rule, in sort
// order. A torrent that an earlier rule claimed stays with that rule. The error cancels all
// deletes of the rule.
func claimDeleteTriggers(rule *models.Automation, in deletionSelectionInput, sel *deletionSelection) ([]deletionClaim, error) {
	deleteAction := rule.Conditions.Delete
	deleteMode := getDeleteMode(rule)
	ledger := in.evalCtx.activeFreeSpace
	stats := getOrCreateRuleStats(in.stats, rule)
	countNotMet := func() {
		if stats != nil {
			stats.DeleteConditionNotMet++
		}
	}

	inScope := func(torrent *qbt.Torrent) bool {
		return !sel.isClaimed(torrent.Hash) && in.scope.answer(rule, torrent) == scopeIn
	}
	var readAhead *freeSpaceReadAhead
	if ledger.projects() {
		readAhead = newFreeSpaceReadAhead(in.crossSeeds, ledger.hardlinks(in.evalCtx), deleteKeepsFilesForOtherInstances(deleteAction))
	}

	var claims []deletionClaim
	for i := range in.torrents {
		torrent := in.torrents[i]
		if !inScope(torrent) {
			continue
		}
		// Safety: delete must always have an explicit condition.
		if deleteAction.Condition == nil || !shouldDeleteTorrent(rule, torrent, in.evalCtx) {
			countNotMet()
			continue
		}
		if ledger.projects() {
			readAhead.ask(in.torrents, i, inScope)
			if !ledger.count(*torrent, in.crossSeeds, in.evalCtx) {
				countNotMet()
				if deleteMode == DeleteModeWithFiles {
					return nil, fmt.Errorf("cannot estimate freed space: file list unavailable for torrent %s", torrent.Hash)
				}
				continue
			}
		}
		claims = append(claims, deletionClaim{hash: torrent.Hash, rule: rule})
	}
	if stats != nil {
		stats.DeleteApplied += len(claims)
	}
	return claims, nil
}

// expandDeleteTrigger adds the trigger and the torrents that its delete takes along.
func (s *Service) expandDeleteTrigger(
	ctx context.Context,
	instanceID int,
	in deletionSelectionInput,
	sel *deletionSelection,
	trigger qbt.Torrent,
	rule *models.Automation,
	includedCrossSeeds map[bool]map[string]struct{},
	rejectedGroups map[ruleGroupKey]struct{},
) {
	deleteAction := rule.Conditions.Delete
	deleteMode := getDeleteMode(rule)
	addAll := func(hashes []string, batchMode, logMsg string, hardlinkCopies map[string]struct{}) {
		for _, hash := range hashes {
			_, linked := hardlinkCopies[hash]
			sel.add(selectedDeletion{
				hash:         hash,
				rule:         ruleRef{id: rule.ID, name: rule.Name},
				deleteMode:   deleteMode,
				batchMode:    batchMode,
				trigger:      hash == trigger.Hash,
				hardlinkCopy: linked,
				logMsg:       logMsg,
			})
		}
	}

	switch deleteMode {
	case DeleteModeWithFilesIncludeCrossSeeds:
		var hardlinks *HardlinkIndex
		if deleteAction.IncludeHardlinks {
			hardlinks = in.evalCtx.hardlinkIndex
		}
		included := includedCrossSeeds[deleteAction.IncludeHardlinks]
		expanded, ok := deleteCrossSeedGroup(trigger, in.evalCtx.torrentsByHash, in.crossSeeds, hardlinks,
			included, cachedTorrentFilesFetcher(in.evalCtx.CrossSeedFilesByHash))
		if !ok {
			return
		}
		for hash := range expanded.expandedSet {
			if hash != trigger.Hash {
				included[hash] = struct{}{}
			}
		}
		logMsg := logMsgRemoveTorrentWithFiles
		if len(expanded.expandedSet) > 1 {
			logMsg = "automations: removing torrent with files (include cross-seeds - verified)"
		}
		if len(expanded.hardlinkCopySet) > 0 {
			logMsg = "automations: removing torrent with files (include cross-seeds + hardlinks)"
		}
		hashes := slices.Sorted(maps.Keys(expanded.expandedSet))
		if keepMsg, keep := groupKeepsFilesForOtherInstances(in.evalCtx, hashes); keep {
			addAll(hashes, DeleteModeKeepFiles, keepMsg, expanded.hardlinkCopySet)
			return
		}
		addAll(hashes, DeleteModeWithFiles, logMsg, expanded.hardlinkCopySet)
	case DeleteModeWithFilesPreserveCrossSeeds:
		batchMode, logMsg := preserveCrossSeedsDelete(trigger, in)
		addAll([]string{trigger.Hash}, batchMode, logMsg, nil)
	case DeleteModeKeepFiles:
		groupID := strings.TrimSpace(deleteAction.GroupID)
		if groupID == "" {
			addAll([]string{trigger.Hash}, DeleteModeKeepFiles, "automations: removing torrent (keeping files)", nil)
			return
		}
		// A group delete is all or none: every member must match the delete condition.
		idx := getOrBuildGroupIndexForRule(in.evalCtx, rule, groupID, in.torrents, in.scope)
		if idx == nil {
			return
		}
		groupKey := ruleGroupKey{ruleID: rule.ID, key: idx.KeyForHash(trigger.Hash)}
		if _, rejected := rejectedGroups[groupKey]; rejected {
			return
		}
		members := idx.MembersForHash(trigger.Hash)
		if len(members) == 0 ||
			!s.shouldExpandGroupWithAmbiguityPolicy(ctx, instanceID, rule, groupID, idx, trigger.Hash, in.evalCtx.torrentsByHash) {
			return
		}
		enterRule(in.evalCtx, rule, in.torrents, in.scope)
		if !allGroupMembersMatchCondition(members, in.evalCtx.torrentsByHash, deleteAction.Condition, in.evalCtx) ||
			in.scope.groupDropsMember(rule, members, in.evalCtx.torrentsByHash) {
			rejectedGroups[groupKey] = struct{}{}
			return
		}
		addAll(members, DeleteModeKeepFiles, "automations: removing torrent group (keeping files)", nil)
	default:
		addAll([]string{trigger.Hash}, deleteMode, logMsgRemoveTorrentWithFiles, nil)
	}
}

// groupKeepsFilesForOtherInstances runs otherInstanceKeepsFiles for each member
// of a group. The members share files, so one member that another instance
// needs keeps the files of all.
func groupKeepsFilesForOtherInstances(evalCtx *EvalContext, hashes []string) (logMsg string, keep bool) {
	for _, hash := range hashes {
		if logMsg, keep := otherInstanceKeepsFiles(evalCtx, hash); keep {
			return logMsg, true
		}
	}
	return "", false
}

// preserveCrossSeedsDelete keeps the files of the trigger when a torrent on
// this instance or on another instance can need them.
func preserveCrossSeedsDelete(trigger qbt.Torrent, in deletionSelectionInput) (batchMode, logMsg string) {
	if keepMsg, keep := otherInstanceKeepsFiles(in.evalCtx, trigger.Hash); keep {
		return DeleteModeKeepFiles, keepMsg
	}
	if shouldPreserveCrossSeedFiles(trigger, in.crossSeeds, in.evalCtx.CrossSeedFilesByHash) {
		return DeleteModeKeepFiles, "automations: removing torrent (preserve cross-seeds - keeping files)"
	}
	return DeleteModeWithFiles, logMsgRemoveTorrentWithFiles
}
