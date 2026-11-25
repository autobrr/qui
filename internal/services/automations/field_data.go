// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"strings"

	"github.com/autobrr/qui/internal/models"
)

type RuleNeeds struct {
	HardlinkScope         bool
	HardlinkCrossScope    bool
	HardlinkSignature     bool
	MissingFiles          bool
	SkippedFiles          bool
	CrossMatch            CrossMatchNeeds
	TagCrossSeeds         bool
	SeasonPack            bool
	SeasonPackAnyInstance bool
	FreeSpace             bool
	TrackerNames          bool
	SavePaths             bool
	TrackerURLs           bool
	TrackerEntries        bool
}

type fieldData struct {
	trustedIdentity bool
	read            bool
	need            func(n *RuleNeeds)
	known           func(ctx *EvalContext, hash string) bool
}

var conditionFieldData = map[ConditionField]fieldData{
	FieldHardlinkScope: {
		trustedIdentity: true,
		need:            func(n *RuleNeeds) { n.HardlinkScope = true },
		known: func(ctx *EvalContext, hash string) bool {
			_, ok := ctx.HardlinkScopeByHash[hash]
			return ok
		},
	},
	FieldHardlinkScopeCross: {
		trustedIdentity: true,
		need:            func(n *RuleNeeds) { n.HardlinkCrossScope = true },
		known: func(ctx *EvalContext, hash string) bool {
			_, ok := ctx.HardlinkCrossScopeByHash[hash]
			return ok
		},
	},
	FieldHasMissingFiles: {
		read: true,
		need: func(n *RuleNeeds) { n.MissingFiles = true },
		known: func(ctx *EvalContext, hash string) bool {
			_, ok := ctx.HasMissingFilesByHash[hash]
			return ok
		},
	},
	FieldHasSkippedFiles: {need: func(n *RuleNeeds) { n.SkippedFiles = true }},
	FieldSeasonPackStatus: {
		need:  func(n *RuleNeeds) { n.SeasonPack = true },
		known: func(ctx *EvalContext, _ string) bool { return ctx.SeasonPackSet != nil },
	},
	FieldSeasonPackStatusAnyInstance: {
		need:  func(n *RuleNeeds) { n.SeasonPackAnyInstance = true },
		known: func(ctx *EvalContext, _ string) bool { return ctx.SeasonPackSetAnyInstance != nil },
	},
	FieldExistsOnSameInstance:   {need: func(n *RuleNeeds) { n.CrossMatch.SameExists = true }, known: sameInstanceCrossSeedKnown},
	FieldSeedingOnSameInstance:  {need: func(n *RuleNeeds) { n.CrossMatch.SameSeeding = true }, known: sameInstanceCrossSeedKnown},
	FieldCrossSeedTags:          {need: func(n *RuleNeeds) { n.CrossMatch.SameTags = true }, known: sameInstanceCrossSeedKnown},
	FieldExistsOnOtherInstance:  {need: func(n *RuleNeeds) { n.CrossMatch.OtherExists = true }, known: otherInstanceCrossSeedKnown},
	FieldSeedingOnOtherInstance: {need: func(n *RuleNeeds) { n.CrossMatch.OtherSeeding = true }, known: otherInstanceCrossSeedKnown},
	FieldFreeSpace:              {need: func(n *RuleNeeds) { n.FreeSpace = true }},
	FieldTracker:                {need: func(n *RuleNeeds) { n.TrackerNames, n.TrackerURLs = true, true }},
	FieldTrackers:               {need: func(n *RuleNeeds) { n.TrackerNames, n.TrackerURLs = true, true }},
	FieldTrackerStatus:          {need: func(n *RuleNeeds) { n.TrackerEntries = true }},
	FieldTrackerMessage:         {need: func(n *RuleNeeds) { n.TrackerEntries = true }},
}

func sameInstanceCrossSeedKnown(ctx *EvalContext, hash string) bool {
	_, unknown := ctx.SameInstanceCrossSeedUnknown[hash]
	return !unknown
}

func otherInstanceCrossSeedKnown(ctx *EvalContext, hash string) bool {
	_, unknown := ctx.CrossInstanceUnknown[hash]
	return !unknown
}

func conditionDataKnown(field ConditionField, hash string, ctx *EvalContext) bool {
	switch field {
	case FieldExistsOnSameInstance, FieldSeedingOnSameInstance, FieldCrossSeedTags,
		FieldExistsOnOtherInstance, FieldSeedingOnOtherInstance:
		if ctx != nil {
			ctx.crossSeeds.askFacts(hash)
		}
	case FieldHardlinkScope, FieldHardlinkScopeCross, FieldHasMissingFiles,
		FieldSeasonPackStatus, FieldSeasonPackStatusAnyInstance:
	default:
		return true
	}
	data, ok := conditionFieldData[field]
	if !ok || data.known == nil {
		return true
	}
	if ctx == nil ||
		data.trustedIdentity && !ctx.InstanceHasTrustedIdentity ||
		data.read && !ctx.InstanceCanRead {
		return false
	}
	if data.known(ctx, hash) {
		return true
	}
	var confirmed map[string]struct{}
	switch field { //nolint:exhaustive // Only these fields have a set of confirmed hashes.
	case FieldExistsOnSameInstance:
		confirmed = ctx.SameInstanceCrossSeedHashSet
	case FieldSeedingOnSameInstance:
		confirmed = ctx.SameInstanceCrossSeedSeedingHashSet
	case FieldExistsOnOtherInstance:
		confirmed = ctx.CrossInstanceHashSet
	case FieldSeedingOnOtherInstance:
		confirmed = ctx.CrossInstanceSeedingHashSet
	}
	_, matched := confirmed[hash]
	return matched
}

func NeedsFor(rules []*models.Automation) RuleNeeds {
	var needs RuleNeeds
	for _, rule := range rules {
		if rule == nil || !rule.Enabled {
			continue
		}
		for c := range rule.Conditions.Conditions() {
			if c.Enabled {
				needs.addTree(c.Condition)
			}
		}
		needs.addSorting(rule.SortingConfig)
		if ruleUsesHardlinkSignatureGrouping(rule) {
			needs.HardlinkSignature = true
		}
		if ac := rule.Conditions; ac != nil {
			if del := ac.Delete; del != nil && del.Enabled && del.IncludeHardlinks && del.Mode == DeleteModeWithFilesIncludeCrossSeeds {
				needs.HardlinkScope = true
			}
			if ruleTemplatesUseTrackerName(ac) {
				needs.TrackerNames = true
			}
			if ruleTemplatesUseSavePaths(ac) {
				needs.SavePaths = true
			}
			needs.addCrossSeedActions(ac)
		}
	}
	return needs
}

func (n *RuleNeeds) addCrossSeedActions(ac *models.ActionConditions) {
	if deleteKeepsFilesForOtherInstances(ac.Delete) {
		n.CrossMatch.SameContent = true
		n.CrossMatch.OtherShared = true
	}
	if del := ac.Delete; del != nil && del.Enabled && del.Mode == DeleteModeWithFiles && ConditionUsesField(del.Condition, FieldFreeSpace) {
		n.CrossMatch.SameContent = true
	}
	if move := ac.Move; move != nil && move.Enabled {
		n.CrossMatch.SameContent = true
	}
	if categoryActionOf(ac.Category).readsCrossSeedGroups() {
		n.CrossMatch.SameContent = true
	}
	if tagsIncludeCrossSeeds(ac) {
		n.CrossMatch.SameContent = true
		n.TagCrossSeeds = true
	}
}

func tagsIncludeCrossSeeds(ac *models.ActionConditions) bool {
	for _, tag := range ac.TagActions() {
		if tag != nil && tag.Enabled && tag.IncludeCrossSeeds && !tag.UseTrackerAsTag {
			return true
		}
	}
	return false
}

func deleteKeepsFilesForOtherInstances(del *models.DeleteAction) bool {
	return del != nil && del.Enabled &&
		(del.Mode == DeleteModeWithFilesPreserveCrossSeeds || del.Mode == DeleteModeWithFilesIncludeCrossSeeds)
}

func previewNeeds(rule *models.Automation) RuleNeeds {
	enabled := *rule
	enabled.Enabled = true
	return NeedsFor([]*models.Automation{&enabled})
}

func (n *RuleNeeds) addField(field ConditionField) {
	if data, ok := conditionFieldData[field]; ok && data.need != nil {
		data.need(n)
	}
}

func (n *RuleNeeds) addTree(cond *RuleCondition) {
	if cond == nil {
		return
	}
	n.addField(cond.Field)
	for _, child := range cond.Conditions {
		n.addTree(child)
	}
}

func (n *RuleNeeds) addSorting(config *models.SortingConfig) {
	if config == nil {
		return
	}
	switch config.Type {
	case models.SortingTypeSimple:
		n.addField(config.Field)
	case models.SortingTypeScore:
		for _, rule := range config.ScoreRules {
			if rule.FieldMultiplier != nil {
				n.addField(rule.FieldMultiplier.Field)
			}
			if rule.Conditional != nil {
				n.addTree(rule.Conditional.Condition)
			}
		}
	}
}

func ruleTemplatesUseTrackerName(ac *models.ActionConditions) bool {
	if move := ac.Move; move != nil && move.Enabled && strings.Contains(move.Path, "Tracker") {
		return true
	}
	if export := ac.ExportToInstance; export != nil && export.Enabled && strings.Contains(export.SavePath, "Tracker") {
		return true
	}
	if transfer := ac.TransferToInstance; transfer != nil && transfer.Enabled && strings.Contains(transfer.SavePath, "Tracker") {
		return true
	}
	for _, tag := range ac.TagActions() {
		if tag != nil && tag.Enabled && tag.UseTrackerAsTag && tag.UseDisplayName {
			return true
		}
	}
	return false
}
