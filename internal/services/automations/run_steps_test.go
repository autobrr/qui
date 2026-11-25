// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json"
	"maps"
	"net/url"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/notifications"
)

func newStepRig(t *testing.T, torrents map[string]map[string]any, configure func(*stubQbt)) (*Service, int, *stubQbt) {
	t.Helper()
	full := make(map[string]map[string]any, len(torrents))
	for hash, fields := range torrents {
		torrent := map[string]any{
			"name": "Quiet.Coast.S01E01.1080p.WEB-GRP." + hash, "progress": 1, "size": 10, "state": "stalledUP",
			"save_path": "/data", "content_path": "/data/" + hash,
			"ratio_limit": -2, "seeding_time_limit": -2, "inactive_seeding_time_limit": -2,
		}
		maps.Copy(torrent, fields)
		full[hash] = torrent
	}
	encoded, err := json.Marshal(full)
	require.NoError(t, err)
	stub := newStubQbt(t, string(encoded))
	if configure != nil {
		configure(stub)
	}
	svc, _, instanceIDs := newStubService(t, parityCrossMatcher{}, stub)
	return svc, instanceIDs[0], stub
}

func stepRule(id int, conditions *models.ActionConditions) *models.Automation {
	return &models.Automation{ID: id, Name: "steps", TrackerPattern: "*", Enabled: true, Conditions: conditions}
}

func tagCondition(tag string) *models.RuleCondition {
	return &models.RuleCondition{Field: models.FieldTags, Operator: models.OperatorContains, Value: tag}
}

type stepActivityView struct {
	Action  string
	Details string
	Items   []ActivityRunTorrent
}

func viewActivities(t *testing.T, svc *Service, instanceID int, activities []*models.AutomationActivity) []stepActivityView {
	t.Helper()
	views := make([]stepActivityView, 0, len(activities))
	for _, activity := range activities {
		view := stepActivityView{Action: activity.Action, Details: string(activity.Details)}
		if run, err := svc.GetActivityRun(instanceID, activity.ID, 100, 0); err == nil {
			view.Items = run.Items
		}
		views = append(views, view)
	}
	return views
}

func TestPlanSteps_DryRunMatchesLiveRun(t *testing.T) {
	tests := []struct {
		name       string
		torrent    map[string]any
		conditions models.ActionConditions
	}{
		{
			name:       "speed limits",
			conditions: models.ActionConditions{SpeedLimits: &models.SpeedLimitAction{Enabled: true, UploadKiB: new(int64(100)), DownloadKiB: new(int64(200))}},
		},
		{
			name:       "share limits",
			conditions: models.ActionConditions{ShareLimits: &models.ShareLimitsAction{Enabled: true, RatioLimit: new(2.0), SeedingTimeMinutes: new(int64(60))}},
		},
		{name: "pause", conditions: models.ActionConditions{Pause: &models.PauseAction{Enabled: true}}},
		{name: "resume", torrent: map[string]any{"state": "stoppedUP"}, conditions: models.ActionConditions{Resume: &models.ResumeAction{Enabled: true}}},
		{name: "recheck", conditions: models.ActionConditions{Recheck: &models.RecheckAction{Enabled: true}}},
		{name: "reannounce", conditions: models.ActionConditions{Reannounce: &models.ReannounceAction{Enabled: true}}},
		{name: "auto management", torrent: map[string]any{"auto_tmm": false}, conditions: models.ActionConditions{AutoManagement: &models.AutoManagementAction{Enabled: true}}},
		{
			name:       "tags",
			conditions: models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, Tags: []string{"managed"}, Mode: models.TagModeAdd}}},
		},
		{
			name:       "category",
			torrent:    map[string]any{"category": "tv"},
			conditions: models.ActionConditions{Category: &models.CategoryAction{Enabled: true, Category: "archive"}},
		},
		{name: "move", conditions: models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "/moved"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, instanceID, stub := newStepRig(t, map[string]map[string]any{"a": tt.torrent, "b": tt.torrent}, nil)
			conditions := tt.conditions
			rule := stepRule(7, &conditions)

			dryRun, err := svc.ApplyRuleDryRun(t.Context(), instanceID, rule)
			require.NoError(t, err)
			require.Empty(t, stub.recordedWrites(), "the dry run sends nothing")

			live, err := svc.ApplyRuleNow(t.Context(), instanceID, rule)
			require.NoError(t, err)
			require.NotEmpty(t, stub.recordedWrites())

			require.Len(t, live, 1)
			require.Equal(t, models.ActivityOutcomeSuccess, live[0].Outcome)
			require.Equal(t, viewActivities(t, svc, instanceID, live), viewActivities(t, svc, instanceID, dryRun))
		})
	}
}

func TestPlanSteps_CoverEveryRunPlanField(t *testing.T) {
	ownCode := map[string]bool{"programs": true, "exports": true, "deletions": true, "transfers": true}
	covered := make(map[string]bool)
	for _, step := range planSteps {
		for _, field := range step.fields {
			covered[field] = true
		}
	}
	planType := reflect.TypeFor[runPlan]()
	fields := make(map[string]bool)
	for field := range planType.Fields() {
		name := field.Name
		fields[name] = true
		if !ownCode[name] {
			require.True(t, covered[name], "the Run plan field %q has no step in planSteps", name)
		}
	}
	for field := range covered {
		require.True(t, fields[field], "planSteps names %q, which the Run plan does not have", field)
	}
}

func TestPlanSteps_ShareLimitsAddUpGroupsWithTheSameLimits(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "dry run"}[dryRun], func(t *testing.T) {
			svc, instanceID, _ := newStepRig(t, map[string]map[string]any{
				"a": {"tags": "stop"},
				"b": {"tags": "remove"},
				"c": {"tags": "remove"},
			}, nil)
			shareRule := func(id int, action string) *models.Automation {
				return stepRule(id, &models.ActionConditions{ShareLimits: &models.ShareLimitsAction{
					Enabled: true, RatioLimit: new(2.0), SeedingTimeMinutes: new(int64(2880)), ShareLimitAction: new(action), Condition: tagCondition(action),
				}})
			}

			activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{shareRule(1, "stop"), shareRule(2, "remove")}, dryRun)
			require.NoError(t, err)
			require.Len(t, activities, 1)
			require.Equal(t, models.ActivityActionShareLimitsChanged, activities[0].Action)
			require.JSONEq(t, `{"limits":{"2.00:2880:-2":3}}`, string(activities[0].Details))
		})
	}
}

func TestPlanSteps_TagsCountOnlyAcceptedTorrents(t *testing.T) {
	for _, webAPI := range []string{"2.10.0", "2.11.4"} {
		t.Run(webAPI, func(t *testing.T) {
			svc, instanceID, _ := newStepRig(t, map[string]map[string]any{"a": nil, "b": nil}, func(stub *stubQbt) {
				stub.webAPIVersion = webAPI
				stub.refuse = func(path string, form url.Values) bool {
					return (path == "torrents/addTags" || path == "torrents/setTags") && form.Get("hashes") == "b"
				}
			})
			svc.cfg.MaxBatchHashes = 1
			rule := stepRule(7, &models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, Tags: []string{"managed"}, Mode: models.TagModeAdd}}})

			activities, err := svc.ApplyRuleNow(t.Context(), instanceID, rule)
			require.NoError(t, err)
			views := viewActivities(t, svc, instanceID, activities)
			require.Len(t, views, 1)
			require.Equal(t, models.ActivityActionTagsChanged, views[0].Action)
			require.JSONEq(t, `{"added":{"managed":1},"removed":{}}`, views[0].Details)
			require.Len(t, views[0].Items, 1)
			require.Equal(t, "a", views[0].Items[0].Hash)
		})
	}
}

func TestPlanSteps_MoveRowsListOnlyPathsWithTorrents(t *testing.T) {
	svc, instanceID, _ := newStepRig(t, map[string]map[string]any{
		"a": {"tags": "one"},
		"b": {"tags": "two"},
	}, func(stub *stubQbt) {
		stub.refuse = func(path string, form url.Values) bool {
			return path == "torrents/setLocation" && form.Get("location") == "/two"
		}
	})
	moveRule := func(id int, tag string) *models.Automation {
		return stepRule(id, &models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "/" + tag, Condition: tagCondition(tag)}})
	}

	activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{moveRule(1, "one"), moveRule(2, "two")}, false)
	require.NoError(t, err)
	require.Len(t, activities, 2)
	require.Equal(t, models.ActivityActionMoved, activities[0].Action)
	require.Equal(t, models.ActivityOutcomeSuccess, activities[0].Outcome)
	require.JSONEq(t, `{"paths":{"/one":1}}`, string(activities[0].Details))
	require.Equal(t, models.ActivityActionMoved, activities[1].Action)
	require.Equal(t, models.ActivityOutcomeFailed, activities[1].Outcome)
	require.JSONEq(t, `{"paths":{"/two":1}}`, string(activities[1].Details))
}

func TestPlanSteps_RefusedPauseBatchWritesNoFailedRow(t *testing.T) {
	svc, instanceID, _ := newStepRig(t, map[string]map[string]any{"a": nil, "b": nil}, func(stub *stubQbt) {
		stub.refuse = func(path string, form url.Values) bool {
			return (path == "torrents/pause" || path == "torrents/stop") && form.Get("hashes") == "b"
		}
	})
	svc.cfg.MaxBatchHashes = 1

	activities, err := svc.ApplyRuleNow(t.Context(), instanceID, stepRule(7, &models.ActionConditions{Pause: &models.PauseAction{Enabled: true}}))
	require.NoError(t, err)
	views := viewActivities(t, svc, instanceID, activities)
	require.Len(t, views, 1)
	require.Equal(t, models.ActivityActionPaused, views[0].Action)
	require.JSONEq(t, `{"count":1}`, views[0].Details)
	require.Len(t, views[0].Items, 1)
	require.Equal(t, "a", views[0].Items[0].Hash)
}

type recordingNotifier struct{ events []notifications.Event }

func (n *recordingNotifier) Notify(_ context.Context, event notifications.Event) {
	n.events = append(n.events, event)
}

func TestPlanSteps_CategoryNotificationCountsEachTorrentOnce(t *testing.T) {
	svc, instanceID, _ := newStepRig(t, map[string]map[string]any{
		"a": {"tags": "x", "content_path": "/data/shared"},
		"b": {"tags": "y", "content_path": "/data/shared"},
		"c": {"content_path": "/data/shared"},
	}, nil)
	svc.crossMatcher = parityCrossMatcher{sameContent: map[string][]string{"a": {"b", "c"}, "b": {"a", "c"}, "c": {"a", "b"}}}
	notifier := &recordingNotifier{}
	svc.notifier = notifier
	categoryRule := func(id int, tag string) *models.Automation {
		rule := stepRule(id, &models.ActionConditions{Category: &models.CategoryAction{
			Enabled: true, Category: tag + "-cat", IncludeCrossSeeds: true, Condition: tagCondition(tag),
		}})
		rule.Notify = true
		return rule
	}

	_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{categoryRule(1, "x"), categoryRule(2, "y")}, false)
	require.NoError(t, err)
	require.Len(t, notifier.events, 1)
	applied := make(map[int]int)
	for _, rule := range notifier.events[0].Automations.Rules {
		applied[rule.RuleID] = rule.Applied
	}
	require.Equal(t, map[int]int{1: 2, 2: 1}, applied)
}
