// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/externalprograms"
)

const (
	runningHash             = "1111111111111111111111111111111111111111"
	stoppedHash             = "2222222222222222222222222222222222222222"
	doomedHash              = "3333333333333333333333333333333333333333"
	missingTargetInstanceID = 999
)

// writeActionKeys maps each write request path to its actionRunOrder key. Where go-qbittorrent
// picks the endpoint by Web API version, every spelling is listed.
var writeActionKeys = map[string]string{
	"torrents/setUploadLimit":    "speedLimits",
	"torrents/setDownloadLimit":  "speedLimits",
	"torrents/setShareLimits":    "shareLimits",
	"torrents/pause":             "pause",
	"torrents/stop":              "pause",
	"torrents/resume":            "resume",
	"torrents/start":             "resume",
	"torrents/recheck":           "recheck",
	"torrents/reannounce":        "reannounce",
	"torrents/setAutoManagement": "autoManagement",
	"torrents/setTags":           "tag",
	"torrents/addTags":           "tag",
	"torrents/removeTags":        "tag",
	"torrents/createTags":        "tag",
	"torrents/deleteTags":        "tag",
	"torrents/setCategory":       "category",
	"torrents/createCategory":    "category",
	"torrents/setLocation":       "move",
	"torrents/delete":            "delete",
}

var activityActionKeys = map[string]string{
	models.ActivityActionSpeedLimitsChanged:        "speedLimits",
	models.ActivityActionShareLimitsChanged:        "shareLimits",
	models.ActivityActionPaused:                    "pause",
	models.ActivityActionResumed:                   "resume",
	models.ActivityActionRechecked:                 "recheck",
	models.ActivityActionReannounced:               "reannounce",
	models.ActivityActionAutoManaged:               "autoManagement",
	models.ActivityActionTagsChanged:               "tag",
	models.ActivityActionCategoryChanged:           "category",
	models.ActivityActionMoved:                     "move",
	externalprograms.ActivityActionExternalProgram: "externalProgram",
	models.ActivityActionExportedToInstance:        "exportToInstance",
	models.ActivityActionDeletedCondition:          "delete",
}

// collapse drops consecutive repeats, so several requests or rows from one step count once.
func collapse(keys []string) []string {
	return slices.Compact(slices.Clone(keys))
}

// everyActionTorrents holds a running torrent (it pauses), a stopped one (it resumes) and one to delete.
func everyActionTorrents() string {
	torrent := func(name, state string) string {
		return `{"name":"` + name + `","state":"` + state + `","progress":1,"size":10,"ratio":1,"up_limit":0,"dl_limit":0,"ratio_limit":-2,"seeding_time_limit":-2,"auto_tmm":false,"tags":"","category":"","save_path":"/data","content_path":"/data/` + name + `"}`
	}
	return `{"` + runningHash + `":` + torrent("running", "stalledUP") +
		`,"` + stoppedHash + `":` + torrent("stopped", "stoppedUP") +
		`,"` + doomedHash + `":` + torrent("doomed", "stalledUP") + `}`
}

func nameIs(name string) *models.RuleCondition {
	return &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorEqual, Value: name}
}

// everyActionRules gives one run every action. Delete sits in its own rule because validation keeps it
// standalone. The export targets a missing instance and no external program service is set, so both
// record their rows synchronously when dispatched.
func everyActionRules() []*models.Automation {
	upload := int64(100)
	download := int64(200)
	ratio := 2.0
	return []*models.Automation{
		{
			ID: 1, Name: "every action", TrackerPattern: "*", Enabled: true, SortOrder: 0,
			Conditions: &models.ActionConditions{
				SpeedLimits:      &models.SpeedLimitAction{Enabled: true, UploadKiB: &upload, DownloadKiB: &download},
				ShareLimits:      &models.ShareLimitsAction{Enabled: true, RatioLimit: &ratio},
				Pause:            &models.PauseAction{Enabled: true},
				Recheck:          &models.RecheckAction{Enabled: true},
				Reannounce:       &models.ReannounceAction{Enabled: true},
				AutoManagement:   &models.AutoManagementAction{Enabled: true},
				Tags:             []*models.TagAction{{Enabled: true, Tags: []string{"ordered"}, Mode: models.TagModeAdd}},
				Category:         &models.CategoryAction{Enabled: true, Category: "sorted"},
				Move:             &models.MoveAction{Enabled: true, Path: "/moved"},
				ExternalProgram:  &models.ExternalProgramAction{Enabled: true, ProgramID: 1},
				ExportToInstance: &models.ExportToInstanceAction{Enabled: true, TargetInstanceID: missingTargetInstanceID},
			},
		},
		{
			ID: 2, Name: "resume", TrackerPattern: "*", Enabled: true, SortOrder: 1,
			Conditions: &models.ActionConditions{Resume: &models.ResumeAction{Enabled: true}},
		},
		{
			ID: 3, Name: "delete", TrackerPattern: "*", Enabled: true, SortOrder: 2,
			Conditions: &models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeKeepFiles, Condition: nameIs("doomed")}},
		},
	}
}

// activityKeysByID returns the run's activity rows in insertion order, as actionRunOrder keys.
func activityKeysByID(t *testing.T, db *database.DB, instanceID int) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `SELECT action FROM automation_activity WHERE instance_id = ? ORDER BY id`, instanceID)
	require.NoError(t, err)
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var action string
		require.NoError(t, rows.Scan(&action))
		key, ok := activityActionKeys[action]
		require.True(t, ok, "activity action %q has no run order key", action)
		keys = append(keys, key)
	}
	require.NoError(t, rows.Err())
	return keys
}

// A live run sends its actions in actionRunOrder: the synchronous steps by request arrival,
// and the steps it dispatches to the background by the rows they record at dispatch.
func TestApplyRules_SendsActionsInRunOrder(t *testing.T) {
	stub := newStubQbt(t, everyActionTorrents())
	svc, db, instanceIDs := newStubService(t, stub)

	_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, everyActionRules(), false)
	require.NoError(t, err)

	writes := stub.recordedWrites()
	requestKeys := make([]string, 0, len(writes))
	for _, write := range writes {
		key, ok := writeActionKeys[write.path]
		require.True(t, ok, "write %q has no run order key", write.path)
		requestKeys = append(requestKeys, key)
	}
	synchronous := slices.DeleteFunc(slices.Clone(actionRunOrder), func(key string) bool {
		return key == "externalProgram" || key == "exportToInstance"
	})
	require.Equal(t, synchronous, collapse(requestKeys), "write requests out of run order")

	require.Equal(t, actionRunOrder, collapse(activityKeysByID(t, db, instanceIDs[0])), "activity rows out of run order")
}

// A dry run reports its actions in actionRunOrder.
func TestApplyRulesDryRun_ReportsActionsInRunOrder(t *testing.T) {
	stub := newStubQbt(t, everyActionTorrents())
	svc, _, instanceIDs := newStubService(t, stub)

	activities, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, everyActionRules(), true)
	require.NoError(t, err)
	require.Empty(t, stub.recordedWrites(), "a dry run sends no writes")

	keys := make([]string, 0, len(activities))
	for _, activity := range activities {
		key, ok := activityActionKeys[activity.Action]
		require.True(t, ok, "activity action %q has no run order key", activity.Action)
		keys = append(keys, key)
	}
	require.Equal(t, actionRunOrder, collapse(keys))
}

// A torrent a rule chooses for delete loses the actions earlier rules chose for it.
func TestApplyRules_DeleteDropsTheTorrentsOwnActions(t *testing.T) {
	stub := newStubQbt(t, everyActionTorrents())
	svc, _, instanceIDs := newStubService(t, stub)
	rules := []*models.Automation{
		{
			ID: 1, Name: "tag", TrackerPattern: "*", Enabled: true, SortOrder: 0,
			Conditions: &models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, Tags: []string{"seen"}, Mode: models.TagModeAdd}}},
		},
		{
			ID: 2, Name: "delete", TrackerPattern: "*", Enabled: true, SortOrder: 1,
			Conditions: &models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeKeepFiles, Condition: nameIs("doomed")}},
		},
	}

	_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, false)
	require.NoError(t, err)

	tagged := map[string]bool{}
	var deleted []string
	for _, write := range stub.recordedWrites() {
		switch writeActionKeys[write.path] {
		case "tag":
			for hash := range strings.SplitSeq(write.form.Get("hashes"), "|") {
				tagged[hash] = true
			}
		case "delete":
			deleted = append(deleted, strings.Split(write.form.Get("hashes"), "|")...)
		}
	}
	require.Equal(t, []string{doomedHash}, deleted)
	require.True(t, tagged[runningHash], "the torrent no rule deletes is tagged")
	require.False(t, tagged[doomedHash], "the deleted torrent must not be tagged")
}

// snapshotTorrent sits in category "old" before the run.
const snapshotTorrent = `{"` + runningHash + `":{"name":"running","state":"stalledUP","progress":1,"size":10,"category":"old","tags":"","save_path":"/data","content_path":"/data/running"}}`

// Every rule in a run sees the torrent as it was when the run started, even after an earlier
// rule changed it.
func TestApplyRulesDryRun_RulesSeeStartOfRunTorrent(t *testing.T) {
	stub := newStubQbt(t, snapshotTorrent)
	svc, _, instanceIDs := newStubService(t, stub)
	categoryIs := func(category string) *models.RuleCondition {
		return &models.RuleCondition{Field: models.FieldCategory, Operator: models.OperatorEqual, Value: category}
	}
	rules := []*models.Automation{
		{
			ID: 1, Name: "recategorise", TrackerPattern: "*", Enabled: true, SortOrder: 0,
			Conditions: &models.ActionConditions{
				Category: &models.CategoryAction{Enabled: true, Category: "new"},
				Move:     &models.MoveAction{Enabled: true, Path: "/data/{{ .Category }}"},
			},
		},
		{
			ID: 2, Name: "old category", TrackerPattern: "*", Enabled: true, SortOrder: 1,
			Conditions: &models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, Tags: []string{"saw-old"}, Mode: models.TagModeAdd, Condition: categoryIs("old")}}},
		},
		{
			ID: 3, Name: "new category", TrackerPattern: "*", Enabled: true, SortOrder: 2,
			Conditions: &models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, Tags: []string{"saw-new"}, Mode: models.TagModeAdd, Condition: categoryIs("new")}}},
		},
	}

	activities, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, true)
	require.NoError(t, err)

	details := map[string]map[string]json.RawMessage{}
	for _, activity := range activities {
		var d map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(activity.Details, &d))
		details[activity.Action] = d
	}
	require.Contains(t, details, models.ActivityActionCategoryChanged, "rule 1 changes the category")
	require.JSONEq(t, `{"saw-old":1}`, string(details[models.ActivityActionTagsChanged]["added"]), "only the old category matches")
	require.JSONEq(t, `{"/data/old":1}`, string(details[models.ActivityActionMoved]["paths"]), "the move path uses the old category")
}

// An export in the same run as a category change resolves its save path with the old category.
func TestApplyRules_ExportSeesStartOfRunTorrent(t *testing.T) {
	source := newStubQbt(t, snapshotTorrent)
	target := newStubQbt(t, `{}`)
	target.torrentsAfterAdd = `{"` + runningHash + `":{"name":"running","state":"stalledUP","progress":1,"size":10,"save_path":"/target/old"}}`
	svc, _, instanceIDs := newStubService(t, source, target)
	rules := []*models.Automation{{
		ID: 1, Name: "recategorise and export", TrackerPattern: "*", Enabled: true,
		Conditions: &models.ActionConditions{
			Category:         &models.CategoryAction{Enabled: true, Category: "new"},
			ExportToInstance: &models.ExportToInstanceAction{Enabled: true, TargetInstanceID: instanceIDs[1], SavePath: "/target/{{ .Category }}"},
		},
	}}

	_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, false)
	require.NoError(t, err)

	require.True(t, slices.ContainsFunc(source.recordedWrites(), func(w stubWrite) bool {
		return w.path == "torrents/setCategory" && w.form.Get("category") == "new"
	}), "the category changes before the export")
	var savePaths []string
	for _, write := range target.recordedWrites() {
		if write.path == "torrents/add" {
			savePaths = append(savePaths, write.form.Get("savepath"))
		}
	}
	require.Equal(t, []string{"/target/old"}, savePaths)
}
