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
	TrackerEntries        bool // the per-torrent tracker list
}

// fieldData is the prepared data one condition field reads. A field without a
// row reads only the torrent and is always known.
type fieldData struct {
	fileIdentity bool // needs models.FilesystemCapabilitiesOf(instance).Identity
	localAccess  bool // needs instance.HasLocalFilesystemAccess
	// need marks the data in RuleNeeds.
	need func(n *RuleNeeds)
	// known reports whether ctx holds the field's data for the torrent. A row
	// without it is always known.
	known func(ctx *EvalContext, hash string) bool
}

var conditionFieldData = map[ConditionField]fieldData{
	FieldHardlinkScope: {
		fileIdentity: true,
		need:         func(n *RuleNeeds) { n.HardlinkScope = true },
		known: func(ctx *EvalContext, hash string) bool {
			_, ok := ctx.HardlinkScopeByHash[hash]
			return ok
		},
	},
	FieldHardlinkScopeCross: {
		fileIdentity: true,
		need:         func(n *RuleNeeds) { n.HardlinkCrossScope = true },
		known: func(ctx *EvalContext, hash string) bool {
			_, ok := ctx.HardlinkCrossScopeByHash[hash]
			return ok
		},
	},
	FieldHasMissingFiles: {
		localAccess: true,
		need:        func(n *RuleNeeds) { n.MissingFiles = true },
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
	FieldExistsOnSameInstance:   {need: func(n *RuleNeeds) { n.CrossMatch.SameExists = true }},
	FieldSeedingOnSameInstance:  {need: func(n *RuleNeeds) { n.CrossMatch.SameSeeding = true }},
	FieldCrossSeedTags:          {need: func(n *RuleNeeds) { n.CrossMatch.SameTags = true }},
	FieldExistsOnOtherInstance:  {need: func(n *RuleNeeds) { n.CrossMatch.OtherExists = true }},
	FieldSeedingOnOtherInstance: {need: func(n *RuleNeeds) { n.CrossMatch.OtherSeeding = true }},
	FieldFreeSpace:              {need: func(n *RuleNeeds) { n.FreeSpace = true }},
	FieldTracker:                {need: func(n *RuleNeeds) { n.TrackerNames, n.TrackerEntries = true, true }},
	FieldTrackers:               {need: func(n *RuleNeeds) { n.TrackerNames, n.TrackerEntries = true, true }},
	FieldTrackerStatus:          {need: func(n *RuleNeeds) { n.TrackerEntries = true }},
	FieldTrackerMessage:         {need: func(n *RuleNeeds) { n.TrackerEntries = true }},
}

func conditionDataKnown(field ConditionField, hash string, ctx *EvalContext) bool {
	data, ok := conditionFieldData[field]
	if !ok || data.known == nil {
		return true
	}
	if ctx == nil ||
		data.fileIdentity && !ctx.InstanceHasFileIdentity ||
		data.localAccess && !ctx.InstanceHasLocalAccess {
		return false
	}
	return data.known(ctx, hash)
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
		}
	}
	return needs
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
// outside its condition. The "Tracker" match pairs with the key resolveMovePath passes to path
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
