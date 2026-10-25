// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	qbt "github.com/autobrr/go-qbittorrent"
)

// runPlan is what one run of the Automation rules does: for each Action, the torrents it
// reaches and the rule that chose each torrent. The live run carries it out, and the dry
// run records it. Category, move and export to instance are not in the plan yet.
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
	programs       []pendingProgramExec
	deletions      *deletionSelection
}

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

// buildRunPlan builds the Run plan from the desired state of each torrent and the Deletion
// selection. It only reads. A claimed Delete trigger and a torrent in the Deletion selection
// get no other Action (ADR 0027).
func buildRunPlan(states map[string]*torrentDesiredState, deletions *deletionSelection, torrentByHash map[string]qbt.Torrent) runPlan {
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

		if state.externalProgramID != nil {
			plan.programs = append(plan.programs, pendingProgramExec{
				hash:      hash,
				torrent:   torrent,
				programID: *state.externalProgramID,
				ruleID:    state.programRuleID,
				ruleName:  state.programRuleName,
			})
		}
	}
	return plan
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
