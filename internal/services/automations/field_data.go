// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"strings"

	"github.com/autobrr/qui/internal/models"
)

// RuleNeeds is the prepared data that a rule set reads. buildEvalContext loads
// only what it asks for.
type RuleNeeds struct {
	HardlinkScope         bool
	HardlinkCrossScope    bool
	HardlinkSignature     bool
	MissingFiles          bool
	SkippedFiles          bool
	CrossMatch            CrossMatchNeeds
	SeasonPack            bool
	SeasonPackAnyInstance bool
	FreeSpace             bool
	TrackerNames          bool
	TrackerURLs           bool // the URL of each tracker, from MainData
	TrackerEntries        bool // the per-torrent tracker list with status and message
}

// fieldData is the prepared data one condition field reads. A field without a
// row reads only the torrent and is always known.
type fieldData struct {
	trustedIdentity bool // needs models.FilesystemCapabilitiesOf(instance).TrustedIdentity
	read            bool // needs models.FilesystemCapabilitiesOf(instance).Read
	// need marks the data in RuleNeeds.
	need func(n *RuleNeeds)
	// known reports whether ctx holds the field's data for the torrent. A row
	// without it is always known.
	known func(ctx *EvalContext, hash string) bool
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
	// The switch skips the map lookup for the common field that reads only the
	// torrent; this runs for every leaf on every torrent.
	switch field {
	case FieldHardlinkScope, FieldHardlinkScopeCross, FieldHasMissingFiles,
		FieldSeasonPackStatus, FieldSeasonPackStatusAnyInstance,
		FieldExistsOnSameInstance, FieldSeedingOnSameInstance, FieldCrossSeedTags,
		FieldExistsOnOtherInstance, FieldSeedingOnOtherInstance:
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
	// An unread extra candidate cannot undo a confirmed positive match.
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

// NeedsFor reports the data that the enabled rules read: through the conditions
// of their enabled actions and their sorting, and through includeHardlinks,
// hardlink signature grouping, and tracker display names in tags and path
// templates.
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
			// includeHardlinks expands a delete only in the include-cross-seeds mode.
			if del := ac.Delete; del != nil && del.Enabled && del.IncludeHardlinks && del.Mode == DeleteModeWithFilesIncludeCrossSeeds {
				needs.HardlinkScope = true
			}
			if ruleTemplatesUseTrackerName(ac) {
				needs.TrackerNames = true
			}
			if actionsGroupCrossSeeds(ac) {
				needs.CrossMatch.SameContent = true
			}
			if deleteKeepsFilesForOtherInstances(ac.Delete) {
				needs.CrossMatch.OtherShared = true
			}
		}
	}
	return needs
}

// actionsGroupCrossSeeds reports whether an enabled action reads the cross-seed
// groups: a delete that keeps, expands to, or counts once the torrents that share
// files, any move (it takes the torrents that share files along), a category
// change that blocks on or includes cross-seeds, and a tag action that includes
// cross-seeds.
func actionsGroupCrossSeeds(ac *models.ActionConditions) bool {
	if del := ac.Delete; del != nil && del.Enabled {
		switch del.Mode {
		case DeleteModeWithFilesIncludeCrossSeeds, DeleteModeWithFilesPreserveCrossSeeds:
			return true
		case DeleteModeWithFiles:
			if ConditionUsesField(del.Condition, FieldFreeSpace) {
				return true
			}
		}
	}
	if move := ac.Move; move != nil && move.Enabled {
		return true
	}
	for _, tag := range ac.TagActions() {
		if tag != nil && tag.Enabled && tag.IncludeCrossSeeds && !tag.UseTrackerAsTag {
			return true
		}
	}
	cat := ac.Category
	return cat != nil && cat.Enabled && (cat.IncludeCrossSeeds || len(cat.BlockIfCrossSeedInCategories) > 0)
}

// deleteKeepsFilesForOtherInstances reports whether the delete mode keeps the
// files that a torrent on another instance needs.
func deleteKeepsFilesForOtherInstances(del *models.DeleteAction) bool {
	return del != nil && del.Enabled &&
		(del.Mode == DeleteModeWithFilesPreserveCrossSeeds || del.Mode == DeleteModeWithFilesIncludeCrossSeeds)
}

// previewNeeds is NeedsFor of one rule as if it were enabled: the preview shows
// what the rule would do once it runs.
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

// ruleTemplatesUseTrackerName reports whether an enabled action needs the tracker display-name map
// outside its condition. The "Tracker" match pairs with the key pathTemplateData passes to path
// templates, so it also catches {{ index . "Tracker" }}.
func ruleTemplatesUseTrackerName(ac *models.ActionConditions) bool {
	if move := ac.Move; move != nil && move.Enabled && strings.Contains(move.Path, "Tracker") {
		return true
	}
	if export := ac.ExportToInstance; export != nil && export.Enabled && strings.Contains(export.SavePath, "Tracker") {
		return true
	}
	for _, tag := range ac.TagActions() {
		if tag != nil && tag.Enabled && tag.UseTrackerAsTag && tag.UseDisplayName {
			return true
		}
	}
	return false
}
