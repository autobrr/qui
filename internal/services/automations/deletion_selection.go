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

type deletionSelectionInput struct {
	rules      []*models.Automation
	torrents   []*qbt.Torrent
	evalCtx    *EvalContext
	crossSeeds *crossSeedGroups
	scope      *ruleScope
	stats      map[int]*ruleRunStats
}

type selectedDeletion struct {
	hash         string
	rule         ruleRef
	deleteMode   string
	batchMode    string
	trigger      bool
	hardlinkCopy bool
	logMsg       string
}

func deleteFilesForMode(mode string) (bool, error) {
	switch mode {
	case DeleteModeKeepFiles:
		return false, nil
	case DeleteModeWithFiles:
		return true, nil
	}
	return false, fmt.Errorf("unknown delete mode: %s", mode)
}

type ruleDeletes struct {
	mode   string
	rule   ruleRef
	hashes []string
}

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

type deletionSelection struct {
	byHash      map[string]selectedDeletion
	order       []string
	triggers    map[string]struct{}
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

type ruleGroupKey struct {
	ruleID int
	key    string
}

func (s *Service) selectDeletions(ctx context.Context, instanceID int, in deletionSelectionInput) *deletionSelection {
	sel := &deletionSelection{
		byHash:      make(map[string]selectedDeletion),
		triggers:    make(map[string]struct{}),
		failedRules: make(map[int]error),
	}
	evalCtx := in.evalCtx
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
	includedCrossSeeds := map[bool]map[string]struct{}{false: {}, true: {}}
	rejectedGroups := make(map[ruleGroupKey]struct{})
	for _, claim := range claims {
		if queued, ok := sel.byHash[claim.hash]; ok && queued.rule.id == claim.rule.ID {
			continue
		}
		s.expandDeleteTrigger(ctx, instanceID, in, sel, *triggerTorrents[claim.hash], claim.rule, includedCrossSeeds, rejectedGroups)
	}
	return sel
}

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

func groupKeepsFilesForOtherInstances(evalCtx *EvalContext, hashes []string) (logMsg string, keep bool) {
	for _, hash := range hashes {
		if logMsg, keep := otherInstanceKeepsFiles(evalCtx, hash); keep {
			return logMsg, true
		}
	}
	return "", false
}

func preserveCrossSeedsDelete(trigger qbt.Torrent, in deletionSelectionInput) (batchMode, logMsg string) {
	if keepMsg, keep := otherInstanceKeepsFiles(in.evalCtx, trigger.Hash); keep {
		return DeleteModeKeepFiles, keepMsg
	}
	if shouldPreserveCrossSeedFiles(trigger, in.crossSeeds, in.evalCtx.CrossSeedFilesByHash) {
		return DeleteModeKeepFiles, "automations: removing torrent (preserve cross-seeds - keeping files)"
	}
	return DeleteModeWithFiles, logMsgRemoveTorrentWithFiles
}
