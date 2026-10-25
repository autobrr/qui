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

// trackerDomainCache computes the tracker domains of each torrent once for all delete rules.
type trackerDomainCache map[string][]string

func (c trackerDomainCache) get(torrent qbt.Torrent, s *Service) []string {
	domains, ok := c[torrent.Hash]
	if !ok {
		domains = collectTrackerDomains(torrent, s.syncManager)
		c[torrent.Hash] = domains
	}
	return domains
}

type deletionClaim struct {
	torrent qbt.Torrent
	rule    *models.Automation
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
	if evalCtx.torrentsByHash == nil {
		evalCtx.torrentsByHash = make(map[string]qbt.Torrent, len(in.torrents))
		for _, torrent := range in.torrents {
			evalCtx.torrentsByHash[torrent.Hash] = torrent
		}
	}

	var claims []deletionClaim
	var sortedFor *models.Automation
	domains := make(trackerDomainCache)
	for _, rule := range in.rules {
		if rule.Conditions == nil || rule.Conditions.Delete == nil || !rule.Conditions.Delete.Enabled {
			continue
		}
		loadRuleScopedEvalContext(rule, in.torrents, evalCtx, s.syncManager)
		if sortedFor == nil || !rulesCanShareSortingBatch(sortedFor, rule) {
			SortTorrentsWithFallback(in.torrents, rule.SortingConfig, evalCtx, instanceID, rule.Name)
			sortedFor = rule
		}
		ruleClaims, err := s.claimDeleteTriggers(rule, in, sel, domains)
		if err != nil {
			sel.failedRules[rule.ID] = err
			evalCtx.SpaceToClear = 0
			clear(evalCtx.FilesToClear)
			clear(evalCtx.FileBytesToClear)
			continue
		}
		for _, claim := range ruleClaims {
			sel.triggers[claim.torrent.Hash] = struct{}{}
		}
		claims = append(claims, ruleClaims...)
	}
	evalCtx.PersistFreeSpaceSourceState()

	// Torrents that an include-cross-seeds group took, so a later group does not check them again.
	includedCrossSeeds := make(map[string]struct{})
	for _, claim := range claims {
		if _, queued := sel.byHash[claim.torrent.Hash]; queued {
			continue
		}
		s.expandDeleteTrigger(ctx, instanceID, in, sel, claim, includedCrossSeeds)
	}
	return sel
}

// claimDeleteTriggers returns the torrents whose delete condition matches the rule, in sort
// order. A torrent that an earlier rule claimed stays with that rule. The error cancels all
// deletes of the rule.
func (s *Service) claimDeleteTriggers(rule *models.Automation, in deletionSelectionInput, sel *deletionSelection, domains trackerDomainCache) ([]deletionClaim, error) {
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
		if !matchesTracker(rule.TrackerPattern, domains.get(torrent, s)) {
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
		claims = append(claims, deletionClaim{torrent: torrent, rule: rule})
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
	claim deletionClaim,
	includedCrossSeeds map[string]struct{},
) {
	trigger, rule := claim.torrent, claim.rule
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
		expanded, ok := deleteCrossSeedGroup(trigger, in.evalCtx.torrentsByHash, in.crossSeeds, hardlinks,
			includedCrossSeeds, cachedTorrentFilesFetcher(in.evalCtx.CrossSeedFilesByHash))
		if !ok {
			return
		}
		for hash := range expanded.expandedSet {
			if hash != trigger.Hash {
				includedCrossSeeds[hash] = struct{}{}
			}
		}
		logMsg := logMsgRemoveTorrentWithFiles
		if len(expanded.expandedSet) > 1 {
			logMsg = "automations: removing torrent with files (include cross-seeds - verified)"
		}
		if len(expanded.hardlinkCopySet) > 0 {
			logMsg = "automations: removing torrent with files (include cross-seeds + hardlinks)"
		}
		addAll(slices.Sorted(maps.Keys(expanded.expandedSet)), DeleteModeWithFiles, logMsg, expanded.hardlinkCopySet)
	case DeleteModeWithFilesPreserveCrossSeeds:
		if shouldPreserveCrossSeedFiles(trigger, in.crossSeeds, in.evalCtx.CrossSeedFilesByHash) {
			addAll([]string{trigger.Hash}, DeleteModeKeepFiles, "automations: removing torrent (preserve cross-seeds - keeping files)", nil)
		} else {
			addAll([]string{trigger.Hash}, DeleteModeWithFiles, logMsgRemoveTorrentWithFiles, nil)
		}
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
		members := idx.MembersForHash(trigger.Hash)
		if len(members) == 0 ||
			!s.shouldExpandGroupWithAmbiguityPolicy(ctx, instanceID, rule, groupID, idx, trigger.Hash, in.evalCtx.torrentsByHash) {
			return
		}
		loadRuleScopedEvalContext(rule, in.torrents, in.evalCtx, s.syncManager)
		if !allGroupMembersMatchCondition(members, in.evalCtx.torrentsByHash, deleteAction.Condition, in.evalCtx) {
			return
		}
		addAll(members, DeleteModeKeepFiles, "automations: removing torrent group (keeping files)", nil)
	default:
		addAll([]string{trigger.Hash}, deleteMode, logMsgRemoveTorrentWithFiles, nil)
	}
}
