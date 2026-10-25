// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"maps"
	"slices"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// planTestRun evaluates the rules on in-memory torrents and builds the Run plan.
func planTestRun(t *testing.T, torrents []qbt.Torrent, rules []*models.Automation, deletions *deletionSelection) runPlan {
	t.Helper()
	states := processTorrents(torrents, rules, nil, qbittorrent.NewSyncManager(nil, nil), nil, nil, nil)
	if deletions == nil {
		deletions = &deletionSelection{}
	}
	torrentByHash := make(map[string]qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		torrentByHash[torrent.Hash] = torrent
	}
	return buildRunPlan(states, deletions, torrentByHash)
}

func planTestRule(id int, conditions *models.ActionConditions) *models.Automation {
	return &models.Automation{ID: id, Name: "rule", Enabled: true, TrackerPattern: "*", Conditions: conditions}
}

// A torrent that already has the target limit gets no call.
func TestRunPlan_SpeedLimitsSkipTorrentsOnTarget(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "a", Tags: "x", UpLimit: 0, DlLimit: 2048 * 1024},
		{Hash: "b", Tags: "x", UpLimit: 1024 * 1024, DlLimit: 0},
	}
	rule := planTestRule(7, &models.ActionConditions{SpeedLimits: &models.SpeedLimitAction{
		Enabled: true, UploadKiB: new(int64(1024)), DownloadKiB: new(int64(2048)), Condition: tagsContain("x"),
	}})

	plan := planTestRun(t, torrents, []*models.Automation{rule}, nil)
	require.Equal(t, map[int64][]string{1024: {"a"}}, plan.uploadLimits.byTarget)
	require.Equal(t, map[string]ruleRef{"a": {id: 7, name: "rule"}}, plan.uploadLimits.rules)
	require.Equal(t, map[int64][]string{2048: {"b"}}, plan.downloadLimits.byTarget)
	require.Equal(t, map[string]ruleRef{"b": {id: 7, name: "rule"}}, plan.downloadLimits.rules)
}

// A torrent whose share limits already equal the target gets no call. Each limit keeps the
// rule that set it.
func TestRunPlan_ShareLimitsSkipTorrentsOnTarget(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "on-target", Tags: "x", RatioLimit: 2, SeedingTimeLimit: 60, InactiveSeedingTimeLimit: -2},
		{Hash: "ratio-differs", Tags: "x", RatioLimit: 1, SeedingTimeLimit: 60, InactiveSeedingTimeLimit: -2},
		{Hash: "seeding-differs", Tags: "x", RatioLimit: 2, SeedingTimeLimit: 30, InactiveSeedingTimeLimit: -2},
	}
	ratioRule := planTestRule(1, &models.ActionConditions{ShareLimits: &models.ShareLimitsAction{
		Enabled: true, RatioLimit: new(2.0), Condition: tagsContain("x"),
	}})
	seedingRule := planTestRule(2, &models.ActionConditions{ShareLimits: &models.ShareLimitsAction{
		Enabled: true, SeedingTimeMinutes: new(int64(60)), Condition: tagsContain("x"),
	}})

	plan := planTestRun(t, torrents, []*models.Automation{ratioRule, seedingRule}, nil)
	target := shareKey{ratio: 2, seed: 60, inactive: -2}
	require.Equal(t, []shareKey{target}, slices.Collect(maps.Keys(plan.shareLimits.byTarget)))
	require.ElementsMatch(t, []string{"ratio-differs", "seeding-differs"}, plan.shareLimits.byTarget[target])
	require.Equal(t, map[string]ruleRef{"ratio-differs": {id: 1, name: "rule"}}, plan.shareLimits.ratioRules)
	require.Equal(t, map[string]ruleRef{"seeding-differs": {id: 2, name: "rule"}}, plan.shareLimits.seedingRules)
}

func TestRunPlan_ActionsWithoutTarget(t *testing.T) {
	ref := map[string]ruleRef{"a": {id: 3, name: "rule"}}
	tests := []struct {
		name       string
		conditions *models.ActionConditions
		torrent    qbt.Torrent
		reached    func(runPlan) plannedTorrents
	}{
		{
			name:       "pause",
			conditions: &models.ActionConditions{Pause: &models.PauseAction{Enabled: true}},
			torrent:    qbt.Torrent{State: qbt.TorrentStateUploading},
			reached:    func(p runPlan) plannedTorrents { return p.pause },
		},
		{
			name:       "resume",
			conditions: &models.ActionConditions{Resume: &models.ResumeAction{Enabled: true}},
			torrent:    qbt.Torrent{State: qbt.TorrentStatePausedUp},
			reached:    func(p runPlan) plannedTorrents { return p.resume },
		},
		{
			name:       "recheck",
			conditions: &models.ActionConditions{Recheck: &models.RecheckAction{Enabled: true}},
			torrent:    qbt.Torrent{State: qbt.TorrentStateUploading},
			reached:    func(p runPlan) plannedTorrents { return p.recheck },
		},
		{
			name:       "reannounce",
			conditions: &models.ActionConditions{Reannounce: &models.ReannounceAction{Enabled: true}},
			torrent:    qbt.Torrent{State: qbt.TorrentStateUploading},
			reached:    func(p runPlan) plannedTorrents { return p.reannounce },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.torrent.Hash = "a"
			plan := planTestRun(t, []qbt.Torrent{tt.torrent}, []*models.Automation{planTestRule(3, tt.conditions)}, nil)
			require.Equal(t, plannedTorrents{hashes: []string{"a"}, rules: ref}, tt.reached(plan))
		})
	}
}

func TestRunPlan_AutoManagementByTarget(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "off", Tags: "on", AutoManaged: false},
		{Hash: "on", Tags: "off", AutoManaged: true},
	}
	rules := []*models.Automation{
		planTestRule(1, &models.ActionConditions{AutoManagement: &models.AutoManagementAction{Enabled: true, Condition: tagsContain("on")}}),
		planTestRule(2, &models.ActionConditions{AutoManagement: &models.AutoManagementAction{Enabled: false, Condition: tagsContain("off")}}),
	}

	plan := planTestRun(t, torrents, rules, nil)
	require.Equal(t, map[bool][]string{true: {"off"}, false: {"on"}}, plan.autoManage.byTarget)
	require.Equal(t, map[string]ruleRef{"off": {id: 1, name: "rule"}, "on": {id: 2, name: "rule"}}, plan.autoManage.rules)
}

// A full tag action adds its tag to the matches and removes it from the other torrents.
func TestRunPlan_Tags(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "match", Tags: "x"},
		{Hash: "stale", Tags: "keep, managed"},
		{Hash: "done", Tags: "x, managed"},
	}
	rule := planTestRule(4, &models.ActionConditions{Tags: []*models.TagAction{{
		Enabled: true, Tags: []string{"managed"}, Mode: "full", Condition: tagsContain("x"),
	}}})

	plan := planTestRun(t, torrents, []*models.Automation{rule}, nil)
	ref := map[string]ruleRef{"managed": {id: 4, name: "rule"}}
	require.Equal(t, map[string]*tagChange{
		"match": {
			current: map[string]struct{}{"x": {}}, desired: map[string]struct{}{"x": {}, "managed": {}},
			toAdd: []string{"managed"}, ruleByTag: ref,
		},
		"stale": {
			current: map[string]struct{}{"keep": {}, "managed": {}}, desired: map[string]struct{}{"keep": {}},
			toRemove: []string{"managed"}, ruleByTag: ref,
		},
	}, plan.tags)
}

func TestRunPlan_ExternalProgram(t *testing.T) {
	torrent := qbt.Torrent{Hash: "a", Name: "Example.Movie.2021.1080p.BluRay.x264-GRP", Tags: "x"}
	rule := planTestRule(5, &models.ActionConditions{ExternalProgram: &models.ExternalProgramAction{
		Enabled: true, ProgramID: 9, Condition: tagsContain("x"),
	}})

	plan := planTestRun(t, []qbt.Torrent{torrent, {Hash: "b"}}, []*models.Automation{rule}, nil)
	require.Equal(t, []pendingProgramExec{{hash: "a", torrent: torrent, programID: 9, ruleID: 5, ruleName: "rule"}}, plan.programs)
}

// A torrent in the Deletion selection and a claimed Delete trigger get no other Action, also
// when the delete of the trigger was blocked.
func TestRunPlan_DeletionSelectionGetsNoOtherAction(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "deleted", State: qbt.TorrentStateUploading},
		{Hash: "blocked-trigger", State: qbt.TorrentStateUploading},
		{Hash: "kept", State: qbt.TorrentStateUploading},
	}
	deletions := &deletionSelection{
		byHash:   map[string]selectedDeletion{"deleted": {hash: "deleted"}},
		triggers: map[string]struct{}{"blocked-trigger": {}},
	}
	rule := planTestRule(6, &models.ActionConditions{
		Pause:           &models.PauseAction{Enabled: true},
		Tags:            []*models.TagAction{{Enabled: true, Tags: []string{"seen"}, Mode: "add"}},
		ExternalProgram: &models.ExternalProgramAction{Enabled: true, ProgramID: 1},
	})

	plan := planTestRun(t, torrents, []*models.Automation{rule}, deletions)
	require.Equal(t, []string{"kept"}, plan.pause.hashes)
	require.Equal(t, []string{"kept"}, slices.Collect(maps.Keys(plan.tags)))
	require.Len(t, plan.programs, 1)
	require.Equal(t, "kept", plan.programs[0].hash)
	require.Same(t, deletions, plan.deletions)
}
