// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	webAPIQbit46 = "2.9.3"
	webAPIQbit52 = "2.15.1"
)

// A torrent list as qBittorrent 5.0 or later sends it: one torrent has metadata, and the
// magnet link has none yet, so it has no private value.
const skipTestTorrents = `{
	"withmeta": {"name": "withmeta", "progress": 1, "state": "stalledUP", "save_path": "/data", "content_path": "/data/withmeta", "has_metadata": true, "private": false},
	"magnet":   {"name": "magnet", "progress": 0, "state": "metaDL", "save_path": "/data", "content_path": "/data/magnet", "has_metadata": false}
}`

func newSkipTestService(t *testing.T, version string) (*Service, int, *stubQbt) {
	t.Helper()
	stub := newStubQbt(t, skipTestTorrents)
	stub.webAPIVersion = version
	svc, _, instanceIDs := newStubService(t, nil, stub)
	return svc, instanceIDs[0], stub
}

func privateIs(value string, negate bool) *models.RuleCondition {
	return &models.RuleCondition{Field: models.FieldPrivate, Operator: models.OperatorEqual, Value: value, Negate: negate}
}

// deletedHashes returns what the stub got in torrents/delete.
func deletedHashes(stub *stubQbt) []string {
	var hashes []string
	for _, write := range stub.recordedWrites() {
		if write.path == "torrents/delete" {
			hashes = append(hashes, write.form.Get("hashes"))
		}
	}
	return hashes
}

// requireSkipEntry checks the one activity entry of a skipped rule.
func requireSkipEntry(t *testing.T, activities []*models.AutomationActivity, outcome, field, requires string) {
	t.Helper()
	require.Len(t, activities, 1)
	require.Equal(t, models.ActivityActionRuleSkipped, activities[0].Action)
	require.Equal(t, outcome, activities[0].Outcome)
	var details RuleSkip
	require.NoError(t, json.Unmarshal(activities[0].Details, &details))
	require.Equal(t, RuleSkip{Field: field, Requires: requires}, details)
}

// regression: #3763
func TestRun_PrivateRuleSkipsAnInstanceBeforeQbit5(t *testing.T) {
	svc, instanceID, stub := newSkipTestService(t, webAPIQbit46)
	rule := parityDeleteRule(DeleteModeKeepFiles, privateIs("false", false))
	rule.ID = 1

	activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)

	require.NoError(t, err)
	require.Empty(t, stub.recordedWrites())
	requireSkipEntry(t, activities, models.ActivityOutcomeSkipped, string(models.FieldPrivate), "qBittorrent 5.0+")
	// The next tick waits for the interval of the rule and fetches no torrent list for it.
	require.NotZero(t, svc.lastRuleRun[ruleKey{instanceID, rule.ID}])
}

// regression: #3763
func TestRun_PlansTheDeletesAtQbit52(t *testing.T) {
	svc, instanceID, stub := newSkipTestService(t, webAPIQbit52)
	rule := parityDeleteRule(DeleteModeKeepFiles, privateIs("false", false))
	rule.ID = 1

	_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)

	require.NoError(t, err)
	// The magnet link has no private value, so it drops out, and the other torrent stays.
	require.Equal(t, []string{"withmeta"}, deletedHashes(stub))
}

// regression: #3763
// Each rule reads a field that the instance does not send, and the torrents would match it on
// the zero value.
func TestRun_SkipsARuleThatReadsAnUnknownField(t *testing.T) {
	deleteAll := func(cond *models.RuleCondition) *models.Automation {
		rule := parityDeleteRule(DeleteModeKeepFiles, cond)
		rule.ID = 1
		return rule
	}
	tests := []struct {
		name     string
		version  string
		rule     *models.Automation
		field    string
		requires string
	}{
		{
			name:     "the not switch on Private",
			version:  webAPIQbit46,
			rule:     deleteAll(privateIs("true", true)),
			field:    string(models.FieldPrivate),
			requires: "qBittorrent 5.0+",
		},
		{
			name:     "Private in a nested group",
			version:  webAPIQbit46,
			rule:     deleteAll(&models.RuleCondition{Operator: models.OperatorOr, Conditions: []*models.RuleCondition{parityDeleteTag, privateIs("false", false)}}),
			field:    string(models.FieldPrivate),
			requires: "qBittorrent 5.0+",
		},
		{
			name:    "tracker status before qBittorrent 5.1",
			version: "2.11.2",
			rule: deleteAll(&models.RuleCondition{
				Field: models.FieldTrackerStatus, Operator: models.OperatorNotEqual, Value: "working",
			}),
			field:    string(models.FieldTrackerStatus),
			requires: "qBittorrent 5.1+",
		},
		{
			name:    "a sort by Comment",
			version: webAPIQbit46,
			rule: func() *models.Automation {
				rule := deleteAll(&models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "with"})
				rule.SortingConfig = &models.SortingConfig{SchemaVersion: "1", Type: models.SortingTypeSimple, Field: models.FieldComment, Direction: models.SortDirectionASC}
				return rule
			}(),
			field:    string(models.FieldComment),
			requires: "qBittorrent 5.0+",
		},
		{
			name:    "a score rule that reads Private",
			version: webAPIQbit46,
			rule: func() *models.Automation {
				rule := deleteAll(&models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "with"})
				rule.SortingConfig = &models.SortingConfig{
					SchemaVersion: "1", Type: models.SortingTypeScore, Direction: models.SortDirectionDESC,
					ScoreRules: []models.ScoreRule{{Type: models.ScoreRuleTypeConditional, Conditional: &models.ConditionalScoreRule{Condition: privateIs("true", false), Score: 1}}},
				}
				return rule
			}(),
			field:    string(models.FieldPrivate),
			requires: "qBittorrent 5.0+",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, instanceID, stub := newSkipTestService(t, tt.version)

			activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{tt.rule}, false)

			require.NoError(t, err)
			require.Empty(t, stub.recordedWrites())
			requireSkipEntry(t, activities, models.ActivityOutcomeSkipped, tt.field, tt.requires)
		})
	}
}

// regression: #3763
// With an unknown version, every requirement is unmet. The run reads the version again, so one
// failed read does not stop the rule until qui restarts.
func TestRun_UnknownVersionSkipsUntilARead(t *testing.T) {
	svc, instanceID, stub := newSkipTestService(t, webAPIQbit52)
	rule := parityDeleteRule(DeleteModeKeepFiles, privateIs("false", false))
	rule.ID = 1
	setVersion := func(version string) {
		stub.mu.Lock()
		stub.webAPIVersion = version
		stub.mu.Unlock()
	}
	client, err := svc.syncManager.GetClient(t.Context(), instanceID)
	require.NoError(t, err)

	setVersion("not-a-version")
	require.Error(t, client.RefreshCapabilities(t.Context()))
	activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)
	require.NoError(t, err)
	require.Empty(t, stub.recordedWrites())
	requireSkipEntry(t, activities, models.ActivityOutcomeSkipped, string(models.FieldPrivate), "qBittorrent 5.0+")

	setVersion(webAPIQbit52)
	_, err = svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)
	require.NoError(t, err)
	require.Equal(t, []string{"withmeta"}, deletedHashes(stub))
}

// regression: #3763
// A field in an action that is off is not read, and the skip of one rule leaves the other rules.
func TestRun_SkipStaysOnTheRuleThatReadsTheField(t *testing.T) {
	svc, instanceID, stub := newSkipTestService(t, webAPIQbit46)
	skipped := parityDeleteRule(DeleteModeKeepFiles, privateIs("false", false))
	skipped.ID = 1
	runs := parityDeleteRule(DeleteModeKeepFiles, &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorEqual, Value: "withmeta"})
	runs.ID = 2
	// An action that is off reads nothing.
	runs.Conditions.Pause = &models.PauseAction{Enabled: false, Condition: privateIs("false", false)}

	activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{skipped, runs}, false)

	require.NoError(t, err)
	require.Equal(t, []string{"withmeta"}, deletedHashes(stub))
	var skips []int
	for _, activity := range activities {
		if activity.Action == models.ActivityActionRuleSkipped {
			skips = append(skips, *activity.RuleID)
		}
	}
	require.Equal(t, []int{1}, skips)
}

// regression: #3763
// The dry run and each preview show the reason of the live run.
func TestDryRunAndPreviews_ShowTheSkip(t *testing.T) {
	svc, instanceID, stub := newSkipTestService(t, webAPIQbit46)
	want := &RuleSkip{Field: string(models.FieldPrivate), Requires: "qBittorrent 5.0+"}
	action := func() *models.RuleCondition { return privateIs("false", true) }

	deleteRule := parityDeleteRule(DeleteModeKeepFiles, action())
	activities, err := svc.ApplyRuleDryRun(t.Context(), instanceID, deleteRule)
	require.NoError(t, err)
	requireSkipEntry(t, activities, models.ActivityOutcomeDryRun, want.Field, want.Requires)

	deletePreview, err := svc.PreviewDeleteRule(t.Context(), instanceID, deleteRule, 10, 0, "")
	require.NoError(t, err)
	require.Equal(t, want, deletePreview.Skipped)
	require.Empty(t, deletePreview.Examples)

	categoryRule := parityCategoryRule(models.CategoryAction{})
	categoryRule.Conditions.Category.Condition = action()
	categoryPreview, err := svc.PreviewCategoryRule(t.Context(), instanceID, categoryRule, 10, 0)
	require.NoError(t, err)
	require.Equal(t, want, categoryPreview.Skipped)
	require.Zero(t, categoryPreview.TotalMatches)

	tagRule := parityTagRule(models.TagAction{})
	tagRule.Conditions.Tags[0].Condition = action()
	tagPreview, err := svc.PreviewTagRule(t.Context(), instanceID, tagRule, 10, 0)
	require.NoError(t, err)
	require.Equal(t, want, tagPreview.Skipped)
	require.Zero(t, tagPreview.TotalMatches)

	require.Empty(t, stub.recordedWrites())
}

// regression: #3763
// A preview of one action skips for a field in another action, because the live run skips the whole rule.
func TestPreview_SkipsForAFieldInAnotherAction(t *testing.T) {
	svc, instanceID, _ := newSkipTestService(t, webAPIQbit46)
	rule := parityCategoryRule(models.CategoryAction{})
	rule.Conditions.Pause = &models.PauseAction{Enabled: true, Condition: privateIs("false", false)}

	preview, err := svc.PreviewCategoryRule(t.Context(), instanceID, rule, 10, 0)

	require.NoError(t, err)
	require.Equal(t, &RuleSkip{Field: string(models.FieldPrivate), Requires: "qBittorrent 5.0+"}, preview.Skipped)
}

// regression: #3763
func TestRun_WritesOneEntryForEachRuleOnEachInstance(t *testing.T) {
	svc, instanceID, stub := newSkipTestService(t, webAPIQbit46)
	rule := parityDeleteRule(DeleteModeKeepFiles, privateIs("false", false))
	rule.ID = 1
	rule.IntervalSeconds = new(0) // each scheduled run below reaches the rule
	run := func(svc *Service) {
		t.Helper()
		_, err := svc.applyRulesForInstance(t.Context(), instanceID, false, []*models.Automation{rule}, false)
		require.NoError(t, err)
	}
	entries := func() int {
		t.Helper()
		activities, err := svc.activityStore.ListByInstance(t.Context(), instanceID, 100)
		require.NoError(t, err)
		return len(activities)
	}

	run(svc)
	run(svc)
	run(svc)
	require.Equal(t, 1, entries())
	require.Empty(t, stub.recordedWrites())

	// qui keeps the state in memory, so a restart writes the entry again.
	restarted := NewService(Config{ApplyTimeout: DefaultConfig().ApplyTimeout}, svc.instanceStore, nil, svc.activityStore, nil, svc.syncManager, nil, nil, nil, svc.backendPool)
	run(restarted)
	run(restarted)
	require.Equal(t, 2, entries())

	// A run that a person starts always says why the rule did nothing.
	_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)
	require.NoError(t, err)
	require.Equal(t, 3, entries())
}

// regression: #3763
// When the rule can run again, a later skip is a new fact, so it gets a new entry.
func TestRun_WritesTheEntryAgainAfterTheRuleRan(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "scheduled dry run"}[dryRun], func(t *testing.T) {
			svc, instanceID, stub := newSkipTestService(t, webAPIQbit46)
			rule := parityDeleteRule(DeleteModeKeepFiles, &models.RuleCondition{Field: models.FieldPrivate, Operator: models.OperatorEqual, Value: "true"})
			rule.ID = 1
			rule.IntervalSeconds = new(0)
			skipEntries := func() int {
				t.Helper()
				activities, err := svc.activityStore.ListByInstance(t.Context(), instanceID, 100)
				require.NoError(t, err)
				count := 0
				for _, activity := range activities {
					if activity.Action == models.ActivityActionRuleSkipped {
						count++
					}
				}
				return count
			}
			runAt := func(version string) {
				t.Helper()
				stub.mu.Lock()
				stub.webAPIVersion = version
				stub.mu.Unlock()
				// qui reads the version on a full sync update. Read it here, whatever the sync cache holds.
				client, err := svc.syncManager.GetClient(t.Context(), instanceID)
				require.NoError(t, err)
				require.NoError(t, client.RefreshCapabilities(t.Context()))
				_, err = svc.applyRulesForInstance(t.Context(), instanceID, false, []*models.Automation{rule}, dryRun)
				require.NoError(t, err)
			}

			runAt(webAPIQbit46)
			require.Equal(t, 1, skipEntries())

			runAt(webAPIQbit52)
			require.Equal(t, 1, skipEntries())

			runAt(webAPIQbit46)
			require.Equal(t, 2, skipEntries())
		})
	}
}

// regression: #3763
// A magnet link without metadata has no private value, whatever the rule asks about it.
func TestRun_TorrentWithoutMetadataDropsOutOfAPrivateRule(t *testing.T) {
	name := func(value string) *models.RuleCondition {
		return &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: value}
	}
	tests := []struct {
		name string
		cond *models.RuleCondition
	}{
		{"the not switch", privateIs("true", true)},
		{"an OR whose other branch matches", &models.RuleCondition{Operator: models.OperatorOr, Conditions: []*models.RuleCondition{privateIs("true", false), name("magnet"), name("withmeta")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, instanceID, stub := newSkipTestService(t, webAPIQbit52)
			rule := parityDeleteRule(DeleteModeKeepFiles, tt.cond)
			rule.ID = 1

			_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)

			require.NoError(t, err)
			require.Equal(t, []string{"withmeta"}, deletedHashes(stub))
		})
	}
}

// regression: #3763
// The torrent drops out of the rule, also of an action in it that does not read Private.
func TestPlan_TorrentWithoutMetadataDropsOutOfEachActionOfTheRule(t *testing.T) {
	torrents := []qbt.Torrent{{Hash: "withmeta", HasMetadata: new(true)}, {Hash: "magnet", HasMetadata: new(false)}}
	rule := planTestRule(7, &models.ActionConditions{
		Pause:  &models.PauseAction{Enabled: true, Condition: &models.RuleCondition{Field: models.FieldHash, Operator: models.OperatorContains, Value: "m"}},
		Resume: &models.ResumeAction{Enabled: true, Condition: privateIs("false", false)},
	})

	plan := planTestRun(t, torrents, []*models.Automation{rule}, nil)

	require.Equal(t, []string{"withmeta"}, plan.pause.hashes)
}

// regression: #3763
// A group member is another torrent than the one that the rule chose, so its condition says unknown itself.
func TestEvaluate_UnknownPrivateNeverMatches(t *testing.T) {
	torrent := qbt.Torrent{Hash: "magnet", HasMetadata: new(false)}
	for _, cond := range []*models.RuleCondition{privateIs("false", false), privateIs("true", true)} {
		require.False(t, EvaluateConditionWithContext(cond, torrent, nil, 0))
	}
}

// regression: #3763
// A dry run of a saved rule keeps the ID of the rule, and the live run still writes its entry.
func TestRun_DryRunDoesNotHideTheLiveEntry(t *testing.T) {
	svc, instanceID, _ := newSkipTestService(t, webAPIQbit46)
	rule := parityDeleteRule(DeleteModeKeepFiles, privateIs("false", false))
	rule.ID = 1
	rule.IntervalSeconds = new(0)

	dryRun, err := svc.ApplyRuleDryRun(t.Context(), instanceID, rule)
	require.NoError(t, err)
	requireSkipEntry(t, dryRun, models.ActivityOutcomeDryRun, string(models.FieldPrivate), "qBittorrent 5.0+")

	live, err := svc.applyRulesForInstance(t.Context(), instanceID, false, []*models.Automation{rule}, false)
	require.NoError(t, err)
	requireSkipEntry(t, live, models.ActivityOutcomeSkipped, string(models.FieldPrivate), "qBittorrent 5.0+")
}

// regression: #3763
// A preview shows one action of the rule, and the torrent drops out as in the run of the whole rule.
func TestPreview_TorrentWithoutMetadataDropsOutForAFieldInAnotherAction(t *testing.T) {
	svc, instanceID, _ := newSkipTestService(t, webAPIQbit52)
	rule := parityCategoryRule(models.CategoryAction{})
	rule.Conditions.Category.Condition = &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "e"}
	rule.Conditions.Pause = &models.PauseAction{Enabled: true, Condition: privateIs("false", false)}

	preview, err := svc.PreviewCategoryRule(t.Context(), instanceID, rule, 10, 0)

	require.NoError(t, err)
	require.Nil(t, preview.Skipped)
	require.Equal(t, 1, preview.TotalMatches)
	require.Equal(t, "withmeta", preview.Examples[0].Hash)
}

// regression: #3763
// A managed reset deletes the tag from every torrent, so the run adds it back to a torrent that
// dropped out of the rule. The torrent stays as it was.
func TestRun_TorrentWithoutMetadataKeepsItsManagedTags(t *testing.T) {
	const torrents = `{
		"withmeta": {"name": "withmeta", "progress": 1, "state": "stalledUP", "save_path": "/data", "content_path": "/data/withmeta", "has_metadata": true, "private": false},
		"magnet":   {"name": "magnet", "progress": 0, "state": "metaDL", "save_path": "/data", "content_path": "/data/magnet", "has_metadata": false, "tags": "managed"}
	}`
	tagAction := func(cond *models.RuleCondition) *models.TagAction {
		return &models.TagAction{Enabled: true, Tags: []string{"managed"}, Mode: models.TagModeFull, DeleteFromClient: true, Condition: cond}
	}
	tests := []struct {
		name       string
		conditions *models.ActionConditions
	}{
		{"the tag action reads Private", &models.ActionConditions{Tags: []*models.TagAction{tagAction(privateIs("false", false))}}},
		{"another action reads Private", &models.ActionConditions{
			Tags:  []*models.TagAction{tagAction(nil)},
			Pause: &models.PauseAction{Enabled: true, Condition: privateIs("false", false)},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStubQbt(t, torrents)
			stub.webAPIVersion = webAPIQbit52
			svc, _, instanceIDs := newStubService(t, nil, stub)
			rule := &models.Automation{ID: 1, Name: "tags", TrackerPattern: "*", Enabled: true, Conditions: tt.conditions}

			_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, []*models.Automation{rule}, false)

			require.NoError(t, err)
			var reset, kept bool
			for _, write := range stub.recordedWrites() {
				reset = reset || write.path == "torrents/deleteTags"
				if write.path == "torrents/addTags" || write.path == "torrents/setTags" {
					kept = kept || (strings.Contains(write.form.Get("hashes"), "magnet") && strings.Contains(write.form.Get("tags"), "managed"))
				}
			}
			require.True(t, reset)
			require.True(t, kept, "the magnet link lost its tag: %+v", stub.recordedWrites())
		})
	}
}

// regression: #3763
// The reset splits and trims the configured tags, so an entry written that way must not lose
// them. The torrent keeps the spelling of its own tag.
func TestRun_TorrentWithoutMetadataKeepsATagThatIsWrittenDifferently(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		onTorrent  string
	}{
		{"a space around the configured tag", " managed ", "managed"},
		{"an upper-case tag", "Managed", "Managed"},
		{"two tags in one entry", "one,two", "one,two"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStubQbt(t, `{"magnet": {"name": "magnet", "progress": 0, "state": "metaDL", "save_path": "/data", "content_path": "/data/magnet", "has_metadata": false, "tags": "`+tt.onTorrent+`"}}`)
			stub.webAPIVersion = webAPIQbit52
			svc, _, instanceIDs := newStubService(t, nil, stub)
			rule := &models.Automation{ID: 1, Name: "tags", TrackerPattern: "*", Enabled: true, Conditions: &models.ActionConditions{
				Tags: []*models.TagAction{{Enabled: true, Tags: []string{tt.configured}, Mode: models.TagModeFull, DeleteFromClient: true, Condition: privateIs("false", false)}},
			}}

			_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, []*models.Automation{rule}, false)

			require.NoError(t, err)
			var kept bool
			for _, write := range stub.recordedWrites() {
				if write.path == "torrents/setTags" {
					kept = kept || (write.form.Get("hashes") == "magnet" &&
						slices.Equal(slices.Sorted(strings.SplitSeq(write.form.Get("tags"), ",")), slices.Sorted(strings.SplitSeq(tt.onTorrent, ","))))
				}
			}
			require.True(t, kept, "the magnet link lost its tag: %+v", stub.recordedWrites())
		})
	}
}

// regression: #3763
// A grouped action is all or none, so a member that dropped out of the rule stops it. The
// condition of the grouped action matches both torrents. A delete cannot share a rule with
// another action, so it reads Private in an OR. The other rules read it in their pause action.
func TestRun_GroupWithATorrentWithoutMetadataSkipsTheGroupedAction(t *testing.T) {
	name := &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "m"}
	grouping := &models.GroupingConfig{Groups: []models.GroupDefinition{{ID: "bysave", Keys: []string{"savePath"}}}}
	pause := &models.PauseAction{Enabled: true, Condition: privateIs("false", false)}
	tests := []struct {
		name  string
		cond  *models.ActionConditions
		write string
	}{
		{"delete", &models.ActionConditions{Grouping: grouping, Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeKeepFiles, GroupID: "bysave", Condition: &models.RuleCondition{
			Operator: models.OperatorOr, Conditions: []*models.RuleCondition{privateIs("false", false), name},
		}}}, "torrents/delete"},
		{"category", &models.ActionConditions{Grouping: grouping, Pause: pause, Category: &models.CategoryAction{Enabled: true, Category: "new", GroupID: "bysave", Condition: name}}, "torrents/setCategory"},
		{"move", &models.ActionConditions{Grouping: grouping, Pause: pause, Move: &models.MoveAction{Enabled: true, Path: "/new", GroupID: "bysave", Condition: name}}, "torrents/setLocation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, instanceID, stub := newSkipTestService(t, webAPIQbit52)
			rule := &models.Automation{ID: 1, Name: "group", TrackerPattern: "*", Enabled: true, Conditions: tt.cond}

			_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)

			require.NoError(t, err)
			require.False(t, slices.ContainsFunc(stub.recordedWrites(), func(write stubWrite) bool { return write.path == tt.write }))
		})
	}

	t.Run("a rule that does not read Private keeps the member", func(t *testing.T) {
		svc, instanceID, stub := newSkipTestService(t, webAPIQbit52)
		rule := &models.Automation{ID: 1, Name: "group", TrackerPattern: "*", Enabled: true, Conditions: &models.ActionConditions{
			Grouping: grouping, Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeKeepFiles, GroupID: "bysave", Condition: name},
		}}

		_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)

		require.NoError(t, err)
		require.ElementsMatch(t, []string{"magnet", "withmeta"}, strings.Split(strings.Join(deletedHashes(stub), "|"), "|"))
	})
}

// regression: #3763
// The preview keeps only the category action, but the live run skips the group for the whole rule.
func TestPreview_GroupWithATorrentWithoutMetadataShowsNoCategoryChange(t *testing.T) {
	svc, instanceID, _ := newSkipTestService(t, webAPIQbit52)
	rule := parityCategoryRule(models.CategoryAction{})
	rule.Conditions.Grouping = &models.GroupingConfig{Groups: []models.GroupDefinition{{ID: "bysave", Keys: []string{"savePath"}}}}
	rule.Conditions.Category.GroupID = "bysave"
	rule.Conditions.Category.Condition = &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "m"}
	rule.Conditions.Pause = &models.PauseAction{Enabled: true, Condition: privateIs("false", false)}

	preview, err := svc.PreviewCategoryRule(t.Context(), instanceID, rule, 10, 0)

	require.NoError(t, err)
	require.Zero(t, preview.TotalMatches)
}

// regression: #3763
// The skip is known before the run reads other data, so a failure for another rule does not hide it.
func TestRun_WritesTheSkipEntryWhenSetupFailsForAnotherRule(t *testing.T) {
	svc, instanceID, _ := newSkipTestService(t, webAPIQbit46)
	emptyStore, err := models.NewInstanceStore(testdb.NewMigratedSQLite(t, t.Name()+"-empty"), make([]byte, 32))
	require.NoError(t, err)
	svc.instanceStore = emptyStore
	publisher := &recordingPublisher{}
	svc.SetActivityPublisher(publisher)
	skippedRule := parityDeleteRule(DeleteModeKeepFiles, privateIs("false", false))
	skippedRule.ID = 1
	runnable := parityDeleteRule(DeleteModeKeepFiles, parityDeleteTag)
	runnable.ID = 2

	_, err = svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{skippedRule, runnable}, false)

	require.Error(t, err)
	activities, err := svc.activityStore.ListByInstance(t.Context(), instanceID, 100)
	require.NoError(t, err)
	require.Len(t, activities, 1)
	require.Equal(t, models.ActivityActionRuleSkipped, activities[0].Action)
	// The run stops with an error, so only this signal tells an open activity log about the entry.
	require.Len(t, publisher.events, 1)
}
