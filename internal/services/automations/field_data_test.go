// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestNeedsFor(t *testing.T) {
	t.Parallel()

	enabled := func(ac *models.ActionConditions) []*models.Automation {
		return []*models.Automation{{Enabled: true, Conditions: ac}}
	}
	onTag := func(field ConditionField) []*models.Automation {
		return enabled(&models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, Condition: &RuleCondition{Field: field}}}})
	}
	scope := &RuleCondition{Field: FieldHardlinkScope}
	sorted := func(sorting *models.SortingConfig) []*models.Automation {
		return []*models.Automation{{Enabled: true, SortingConfig: sorting}}
	}

	tests := []struct {
		name  string
		rules []*models.Automation
		want  RuleNeeds
	}{
		// One row per field in conditionFieldData.
		{name: "HARDLINK_SCOPE", rules: onTag(FieldHardlinkScope), want: RuleNeeds{HardlinkScope: true}},
		{name: "HARDLINK_SCOPE_CROSS", rules: onTag(FieldHardlinkScopeCross), want: RuleNeeds{HardlinkCrossScope: true}},
		{name: "HAS_MISSING_FILES", rules: onTag(FieldHasMissingFiles), want: RuleNeeds{MissingFiles: true}},
		{name: "HAS_SKIPPED_FILES", rules: onTag(FieldHasSkippedFiles), want: RuleNeeds{SkippedFiles: true}},
		{name: "SEASON_PACK_STATUS", rules: onTag(FieldSeasonPackStatus), want: RuleNeeds{SeasonPack: true}},
		{name: "SEASON_PACK_STATUS_ANY_INSTANCE", rules: onTag(FieldSeasonPackStatusAnyInstance), want: RuleNeeds{SeasonPackAnyInstance: true}},
		{name: "EXISTS_ON_SAME_INSTANCE", rules: onTag(FieldExistsOnSameInstance), want: RuleNeeds{CrossMatch: CrossMatchNeeds{SameExists: true}}},
		{name: "SEEDING_ON_SAME_INSTANCE", rules: onTag(FieldSeedingOnSameInstance), want: RuleNeeds{CrossMatch: CrossMatchNeeds{SameSeeding: true}}},
		{name: "CROSS_SEED_TAGS", rules: onTag(FieldCrossSeedTags), want: RuleNeeds{CrossMatch: CrossMatchNeeds{SameTags: true}}},
		{name: "EXISTS_ON_OTHER_INSTANCE", rules: onTag(FieldExistsOnOtherInstance), want: RuleNeeds{CrossMatch: CrossMatchNeeds{OtherExists: true}}},
		{name: "SEEDING_ON_OTHER_INSTANCE", rules: onTag(FieldSeedingOnOtherInstance), want: RuleNeeds{CrossMatch: CrossMatchNeeds{OtherSeeding: true}}},
		{name: "FREE_SPACE", rules: onTag(FieldFreeSpace), want: RuleNeeds{FreeSpace: true}},
		{name: "TRACKER", rules: onTag(FieldTracker), want: RuleNeeds{TrackerNames: true, TrackerEntries: true}},
		{name: "TRACKERS", rules: onTag(FieldTrackers), want: RuleNeeds{TrackerNames: true, TrackerEntries: true}},
		{name: "TRACKER_STATUS", rules: onTag(FieldTrackerStatus), want: RuleNeeds{TrackerEntries: true}},
		{name: "TRACKER_MESSAGE", rules: onTag(FieldTrackerMessage), want: RuleNeeds{TrackerEntries: true}},
		{name: "a torrent-only field", rules: onTag(FieldName)},
		{
			name: "a field nested in a group",
			rules: enabled(&models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: &RuleCondition{
				Operator:   models.OperatorAnd,
				Conditions: []*RuleCondition{{Field: FieldName}, {Field: FieldFreeSpace}},
			}}}),
			want: RuleNeeds{FreeSpace: true},
		},

		// One row per action.
		{name: "speed limits", rules: enabled(&models.ActionConditions{SpeedLimits: &models.SpeedLimitAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "share limits", rules: enabled(&models.ActionConditions{ShareLimits: &models.ShareLimitsAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "pause", rules: enabled(&models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "resume", rules: enabled(&models.ActionConditions{Resume: &models.ResumeAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "recheck", rules: enabled(&models.ActionConditions{Recheck: &models.RecheckAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "reannounce", rules: enabled(&models.ActionConditions{Reannounce: &models.ReannounceAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "delete", rules: enabled(&models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "legacy tag", rules: enabled(&models.ActionConditions{Tag: &models.TagAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "tags", rules: enabled(&models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, Condition: scope}}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "category", rules: enabled(&models.ActionConditions{Category: &models.CategoryAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "move", rules: enabled(&models.ActionConditions{Move: &models.MoveAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "external program", rules: enabled(&models.ActionConditions{ExternalProgram: &models.ExternalProgramAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "auto management that turns ATM off", rules: enabled(&models.ActionConditions{AutoManagement: &models.AutoManagementAction{Enabled: false, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},
		{name: "export to instance", rules: enabled(&models.ActionConditions{ExportToInstance: &models.ExportToInstanceAction{Enabled: true, Condition: scope}}), want: RuleNeeds{HardlinkScope: true}},

		{name: "disabled action", rules: enabled(&models.ActionConditions{Pause: &models.PauseAction{Condition: scope}})},
		{name: "disabled rule", rules: []*models.Automation{{Conditions: &models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: scope}}}}},
		{name: "nil rule", rules: []*models.Automation{nil}},
		{
			name:  "simple sorting",
			rules: sorted(&models.SortingConfig{Type: models.SortingTypeSimple, Field: FieldHardlinkScopeCross}),
			want:  RuleNeeds{HardlinkCrossScope: true},
		},
		{
			name: "score field multiplier",
			rules: sorted(&models.SortingConfig{Type: models.SortingTypeScore, ScoreRules: []models.ScoreRule{{
				FieldMultiplier: &models.FieldMultiplierScoreRule{Field: FieldFreeSpace},
			}}}),
			want: RuleNeeds{FreeSpace: true},
		},
		{
			name: "score conditional",
			rules: sorted(&models.SortingConfig{Type: models.SortingTypeScore, ScoreRules: []models.ScoreRule{{
				Conditional: &models.ConditionalScoreRule{Condition: &RuleCondition{Field: FieldHasSkippedFiles}},
			}}}),
			want: RuleNeeds{SkippedFiles: true},
		},
		{
			name:  "include hardlinks",
			rules: enabled(&models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, IncludeHardlinks: true, Mode: DeleteModeWithFilesIncludeCrossSeeds}}),
			want:  RuleNeeds{HardlinkScope: true},
		},
		{
			name:  "include hardlinks outside the include-cross-seeds mode",
			rules: enabled(&models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, IncludeHardlinks: true, Mode: DeleteModeWithFiles}}),
		},
		{
			name:  "include hardlinks on a disabled delete",
			rules: enabled(&models.ActionConditions{Delete: &models.DeleteAction{IncludeHardlinks: true}}),
		},
		{
			name:  "hardlink signature grouping",
			rules: enabled(&models.ActionConditions{Category: &models.CategoryAction{Enabled: true, GroupID: GroupHardlinkSignature}}),
			want:  RuleNeeds{HardlinkSignature: true},
		},
		{
			name:  "tracker name in a move path",
			rules: enabled(&models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "/data/{{.Tracker}}"}}),
			want:  RuleNeeds{TrackerNames: true},
		},
		{
			name:  "needs of several rules add up",
			rules: append(onTag(FieldFreeSpace), onTag(FieldHasMissingFiles)...),
			want:  RuleNeeds{FreeSpace: true, MissingFiles: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, NeedsFor(tt.rules))
		})
	}
}
