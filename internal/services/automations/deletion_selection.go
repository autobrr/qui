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
	torrents   []qbt.Torrent
	evalCtx    *EvalContext
	crossSeeds crossSeedGroups
	// skip leaves out the torrents that a live run handled recently. The dry run and the
	// preview pass nil.
	skip func(hash string) bool
	// eligibleView ignores the FREE_SPACE stop, for the "eligible" preview view.
	eligibleView bool
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

// trackerMatchCache matches each tracker pattern once per distinct set of tracker domains.
// Most torrents share their trackers, so the cache holds few sets.
type trackerMatchCache struct {
	keyByHash map[string]string          // torrent hash -> domain set key
	domains   map[string][]string        // domain set key -> domains
	matches   map[string]map[string]bool // pattern -> domain set key -> match
}

func newTrackerMatchCache() *trackerMatchCache {
	return &trackerMatchCache{
		keyByHash: make(map[string]string),
		domains:   make(map[string][]string),
		matches:   make(map[string]map[string]bool),
	}
}

func (c *trackerMatchCache) match(pattern string, torrent qbt.Torrent, s *Service) bool {
	if pattern == "*" {
		return true
	}
	key, ok := c.keyByHash[torrent.Hash]
	if !ok {
		domains := collectTrackerDomains(torrent, s.syncManager) // sorted
		key = strings.Join(domains, ",")
		c.keyByHash[torrent.Hash] = key
		c.domains[key] = domains
	}
	byKey := c.matches[pattern]
	if byKey == nil {
		byKey = make(map[string]bool)
		c.matches[pattern] = byKey
	}
	matched, ok := byKey[key]
	if !ok {
		matched = matchesTracker(pattern, c.domains[key])
		byKey[key] = matched
	}
	return matched
}

type deletionClaim struct {
	hash string
	rule *models.Automation
}

// ruleGroupKey names one keep-files group of one rule.
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
	// A torrent is about 2 KB. Build the map only for the modes that expand groups, so the
	// preview of another mode does not pay for it. The run builds the map later in any case.
	if evalCtx.torrentsByHash == nil && slices.ContainsFunc(in.rules, deleteExpandsGroups) {
		evalCtx.torrentsByHash = make(map[string]qbt.Torrent, len(in.torrents))
		for _, torrent := range in.torrents {
			evalCtx.torrentsByHash[torrent.Hash] = torrent
		}
	}

	var claims []deletionClaim
	var sortedFor *models.Automation
	trackers := newTrackerMatchCache()
	for _, rule := range in.rules {
		if rule.Conditions == nil || rule.Conditions.Delete == nil || !rule.Conditions.Delete.Enabled {
			continue
		}
		loadRuleScopedEvalContext(rule, in.torrents, evalCtx, s.syncManager)
		if sortedFor == nil || !rulesCanShareSortingBatch(sortedFor, rule) {
			SortTorrentsWithFallback(in.torrents, rule.SortingConfig, evalCtx, instanceID, rule.Name)
			sortedFor = rule
		}
		ruleClaims, err := s.claimDeleteTriggers(rule, in, sel, trackers)
		if err != nil {
			sel.failedRules[rule.ID] = err
			evalCtx.SpaceToClear = 0
			clear(evalCtx.FilesToClear)
			clear(evalCtx.FileBytesToClear)
			continue
		}
		for _, claim := range ruleClaims {
			sel.triggers[claim.hash] = struct{}{}
		}
		claims = append(claims, ruleClaims...)
	}
	evalCtx.PersistFreeSpaceSourceState()

	// The sort order is final now, so pointers into in.torrents stay valid.
	triggerTorrents := make(map[string]*qbt.Torrent, len(sel.triggers))
	for i := range in.torrents {
		if sel.isClaimed(in.torrents[i].Hash) {
			triggerTorrents[in.torrents[i].Hash] = &in.torrents[i]
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
func (s *Service) claimDeleteTriggers(rule *models.Automation, in deletionSelectionInput, sel *deletionSelection, trackers *trackerMatchCache) ([]deletionClaim, error) {
	deleteAction := rule.Conditions.Delete
	deleteMode := getDeleteMode(rule)
	projects := !in.eligibleView && ConditionUsesField(deleteAction.Condition, FieldFreeSpace)
	stats := getOrCreateRuleStats(in.stats, rule)
	countNotMet := func() {
		if stats != nil {
			stats.DeleteConditionNotMet++
		}
	}

	var claims []deletionClaim
	for _, torrent := range in.torrents {
		if sel.isClaimed(torrent.Hash) || (in.skip != nil && in.skip(torrent.Hash)) {
			continue
		}
		if !trackers.match(rule.TrackerPattern, torrent, s) {
			continue
		}
		// Safety: delete must always have an explicit condition.
		if deleteAction.Condition == nil || !shouldDeleteTorrent(rule, &torrent, in.evalCtx) {
			countNotMet()
			continue
		}
		if projects && !updateCumulativeFreeSpaceCleared(torrent, in.evalCtx, deleteMode, in.crossSeeds, deleteAction.IncludeHardlinks) {
			countNotMet()
			if deleteMode == DeleteModeWithFiles {
				return nil, fmt.Errorf("cannot estimate freed space: file list unavailable for torrent %s", torrent.Hash)
			}
			continue
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
		idx := getOrBuildGroupIndexForRule(in.evalCtx, rule, groupID, in.torrents, s.syncManager)
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
		loadRuleScopedEvalContext(rule, in.torrents, in.evalCtx, s.syncManager)
		if !allGroupMembersMatchCondition(members, in.evalCtx.torrentsByHash, deleteAction.Condition, in.evalCtx) {
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
