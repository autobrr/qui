// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"errors"
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
	return planTestRunWith(t, &Service{}, torrents, rules, deletions, nil)
}

// planTestRunWith takes the cross-seeds from sharing: the torrents that share files with
// each torrent.
func planTestRunWith(t *testing.T, svc *Service, torrents []qbt.Torrent, rules []*models.Automation, deletions *deletionSelection, sharing map[string][]string) runPlan {
	t.Helper()
	evalCtx := &EvalContext{SameInstanceContentByHash: sharing}
	states := processTorrents(torrents, rules, evalCtx, buildCrossSeedGroups(torrents, evalCtx), qbittorrent.NewSyncManager(nil, nil), nil, nil, nil)
	if deletions == nil {
		deletions = &deletionSelection{}
	}
	torrentByHash := make(map[string]qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		torrentByHash[torrent.Hash] = torrent
	}
	ruleByID := make(map[int]*models.Automation, len(rules))
	for _, rule := range rules {
		ruleByID[rule.ID] = rule
	}
	return svc.buildRunPlan(t.Context(), runPlanInput{
		states:        states,
		deletions:     deletions,
		torrents:      torrents,
		torrentByHash: torrentByHash,
		ruleByID:      ruleByID,
		crossSeeds:    buildCrossSeedGroups(torrents, evalCtx),
		evalCtx:       evalCtx,
	})
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

// A rule that sets the share action or the mode does not replace the rule that set the ratio
// or the seeding time of the same torrent.
func TestRunPlan_ShareLimitsKeepEachRule(t *testing.T) {
	torrents := []qbt.Torrent{{
		Hash: "a", Tags: "x", RatioLimit: 1, SeedingTimeLimit: 30, InactiveSeedingTimeLimit: -2,
		ShareLimitAction: "Stop", ShareLimitsMode: "MatchAny",
	}}
	limitRule := planTestRule(1, &models.ActionConditions{ShareLimits: &models.ShareLimitsAction{
		Enabled: true, RatioLimit: new(2.0), SeedingTimeMinutes: new(int64(60)), Condition: tagsContain("x"),
	}})
	actionRule := planTestRule(2, &models.ActionConditions{ShareLimits: &models.ShareLimitsAction{
		Enabled: true, ShareLimitAction: new("Remove"), ShareLimitsMode: new("MatchAll"), Condition: tagsContain("x"),
	}})

	plan := planTestRun(t, torrents, []*models.Automation{limitRule, actionRule}, nil)
	limitRef := map[string]ruleRef{"a": {id: 1, name: "rule"}}
	actionRef := map[string]ruleRef{"a": {id: 2, name: "rule"}}
	require.Equal(t, limitRef, plan.shareLimits.ratioRules)
	require.Equal(t, limitRef, plan.shareLimits.seedingRules)
	require.Equal(t, actionRef, plan.shareLimits.actionRules)
	require.Equal(t, actionRef, plan.shareLimits.modeRules)
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

// fakeTargetLookup answers with the torrents in onTarget, and fails for the hashes in fail.
type fakeTargetLookup struct {
	onTarget map[string]bool
	fail     map[string]bool
}

func (f fakeTargetLookup) HasTorrentByAnyHash(_ context.Context, _ int, hashes []string) (*qbt.Torrent, bool, error) {
	if f.fail[hashes[0]] {
		return nil, false, errors.New("target offline")
	}
	return nil, f.onTarget[hashes[0]], nil
}

func TestRunPlan_ExportMarks(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "ready", Tags: "x", Category: "tv"},
		{Hash: "on-target", Tags: "x", Category: "tv"},
		{Hash: "check-failed", Tags: "x", Category: "tv"},
		{Hash: "no-category", Tags: "x"},
	}
	action := &models.ExportToInstanceAction{Enabled: true, TargetInstanceID: 2, SavePath: "{{.Category}}", Condition: tagsContain("x")}
	rule := planTestRule(8, &models.ActionConditions{ExportToInstance: action})
	svc := &Service{targetLookup: fakeTargetLookup{onTarget: map[string]bool{"on-target": true}, fail: map[string]bool{"check-failed": true}}}

	plan := planTestRunWith(t, svc, torrents, []*models.Automation{rule}, nil, nil)
	byHash := make(map[string]pendingExportToInstance, len(plan.exports))
	for _, export := range plan.exports {
		require.Same(t, action, export.action)
		require.Equal(t, 8, export.ruleID)
		byHash[export.hash] = export
	}
	require.Len(t, byHash, 4)
	require.Equal(t, exportReady, byHash["ready"].mark)
	require.Equal(t, "tv", byHash["ready"].resolvedSavePath)
	require.Empty(t, byHash["ready"].failureReason)
	require.Equal(t, exportAlreadyOnTarget, byHash["on-target"].mark)
	require.Equal(t, exportCheckFailed, byHash["check-failed"].mark)
	require.Equal(t, "Failed to check target instance: target offline", byHash["check-failed"].failureReason)
	require.Equal(t, exportSavePathUnresolved, byHash["no-category"].mark)
	require.Equal(t, "Save path template resolution failed", byHash["no-category"].failureReason)
}

// sharedFiles marks each pair of hashes as cross-seeds that share files.
func sharedFiles(pairs ...[2]string) map[string][]string {
	sharing := make(map[string][]string)
	for _, pair := range pairs {
		sharing[pair[0]] = append(sharing[pair[0]], pair[1])
		sharing[pair[1]] = append(sharing[pair[1]], pair[0])
	}
	return sharing
}

// A move takes the cross-seeds of each torrent along, with the rule of the torrent. A
// torrent the run deletes stays out.
func TestRunPlan_MoveTakesCrossSeeds(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "trigger", Tags: "x", SavePath: "/old"},
		{Hash: "cross-seed", SavePath: "/old"},
		{Hash: "deleting", SavePath: "/old"},
	}
	rule := planTestRule(3, &models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "/new", Condition: tagsContain("x")}})
	deletions := &deletionSelection{byHash: map[string]selectedDeletion{"deleting": {hash: "deleting"}}}
	sharing := sharedFiles([2]string{"trigger", "cross-seed"}, [2]string{"trigger", "deleting"})

	plan := planTestRunWith(t, &Service{}, torrents, []*models.Automation{rule}, deletions, sharing)
	require.Equal(t, map[string][]string{"/new": {"trigger", "cross-seed"}}, plan.move.byTarget)
	ref := ruleRef{id: 3, name: "rule"}
	require.Equal(t, map[string]ruleRef{"trigger": ref, "cross-seed": ref}, plan.move.rules)
}

// groupTestRule matches only the torrents on the tracker groupTestTracker, so a group member
// on another tracker comes in only through the group.
func groupTestRule(id int, conditions *models.ActionConditions) *models.Automation {
	rule := planTestRule(id, conditions)
	rule.TrackerPattern = "a.test"
	return rule
}

const (
	groupTestTracker  = "https://a.test/announce"
	memberTestTracker = "https://b.test/announce"
)

// A move with a group takes the other members along, with the rule of the torrent. A member
// already at the path stays out.
func TestRunPlan_MoveTakesGroupMembers(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "trigger", Tracker: groupTestTracker, SavePath: "/old", ContentPath: "/old/show"},
		{Hash: "member", Tracker: memberTestTracker, SavePath: "/old", ContentPath: "/old/show"},
		{Hash: "at-target", Tracker: memberTestTracker, SavePath: "/new", ContentPath: "/old/show"},
		{Hash: "other", Tracker: memberTestTracker, SavePath: "/old", ContentPath: "/old/film"},
	}
	rule := groupTestRule(4, &models.ActionConditions{Move: &models.MoveAction{
		Enabled: true, Path: "/new", GroupID: GroupCrossSeedContentPath,
	}})

	plan := planTestRunWith(t, &Service{}, torrents, []*models.Automation{rule}, nil, sharedFiles([2]string{"trigger", "member"}))
	require.Equal(t, map[string][]string{"/new": {"member", "trigger"}}, plan.move.byTarget)
	ref := ruleRef{id: 4, name: "rule"}
	require.Equal(t, map[string]ruleRef{"trigger": ref, "member": ref}, plan.move.rules)
}

// planFilesReader answers with the file list of each hash.
type planFilesReader map[string]qbt.TorrentFiles

func (f planFilesReader) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	out := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		if files, ok := f[hash]; ok {
			out[hash] = files
		}
	}
	return out, nil
}

func (planFilesReader) GetCachedInstanceTorrents(context.Context, int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	return nil, nil
}

// Two torrents directly in one save path have the same content path, so the group needs the
// file lists to show that they share files.
func TestRunPlan_MoveGroupInSharedSavePathChecksFiles(t *testing.T) {
	triggerFiles := qbt.TorrentFiles{{Name: "show.mkv", Size: 100}}
	for _, tc := range []struct {
		name        string
		memberFiles qbt.TorrentFiles
		want        map[string][]string
	}{
		{name: "same files", memberFiles: triggerFiles, want: map[string][]string{"/new": {"member", "trigger"}}},
		{name: "other files", memberFiles: qbt.TorrentFiles{{Name: "film.mkv", Size: 100}}, want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			torrents := []qbt.Torrent{
				{Hash: "trigger", Tracker: groupTestTracker, SavePath: "/old", ContentPath: "/old"},
				{Hash: "member", Tracker: memberTestTracker, SavePath: "/old", ContentPath: "/old"},
			}
			rule := groupTestRule(5, &models.ActionConditions{Move: &models.MoveAction{
				Enabled: true, Path: "/new", GroupID: GroupCrossSeedContentPath,
			}})
			svc := &Service{filesReader: planFilesReader{"trigger": triggerFiles, "member": tc.memberFiles}}

			plan := planTestRunWith(t, svc, torrents, []*models.Automation{rule}, nil, sharedFiles([2]string{"trigger", "member"}))
			require.Equal(t, tc.want, plan.move.byTarget)
		})
	}
}

// A category change with includeCrossSeeds takes the cross-seeds along, with the rule of the
// torrent. A cross-seed already in the category stays out.
func TestRunPlan_CategoryTakesCrossSeeds(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "trigger", Tags: "x", Category: "tv"},
		{Hash: "cross-seed", Category: "tv"},
		{Hash: "in-category", Category: "archive"},
		{Hash: "already", Tags: "x", Category: "archive"},
	}
	rule := planTestRule(6, &models.ActionConditions{Category: &models.CategoryAction{
		Enabled: true, Category: "archive", IncludeCrossSeeds: true, Condition: tagsContain("x"),
	}})
	sharing := sharedFiles([2]string{"trigger", "cross-seed"}, [2]string{"trigger", "in-category"})

	plan := planTestRunWith(t, &Service{}, torrents, []*models.Automation{rule}, nil, sharing)
	require.Equal(t, map[string][]string{"archive": {"trigger", "cross-seed"}}, plan.category.byTarget)
	ref := ruleRef{id: 6, name: "rule"}
	require.Equal(t, map[string]ruleRef{"trigger": ref, "cross-seed": ref}, plan.category.rules)
}

// A category change with a group takes the other members along, with the rule of the
// torrent. A rule without an ID changes only its own torrent.
func TestRunPlan_CategoryTakesGroupMembers(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "trigger", Tracker: groupTestTracker, Category: "tv", SavePath: "/data", ContentPath: "/data/show"},
		{Hash: "member", Tracker: memberTestTracker, Category: "tv", SavePath: "/data", ContentPath: "/data/show"},
	}
	for _, tc := range []struct {
		name   string
		ruleID int
		want   []string
	}{
		{name: "rule with an ID", ruleID: 7, want: []string{"trigger", "member"}},
		{name: "rule without an ID", ruleID: 0, want: []string{"trigger"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule := groupTestRule(tc.ruleID, &models.ActionConditions{Category: &models.CategoryAction{
				Enabled: true, Category: "archive", GroupID: GroupCrossSeedContentPath,
			}})

			plan := planTestRunWith(t, &Service{}, torrents, []*models.Automation{rule}, nil, sharedFiles([2]string{"trigger", "member"}))
			require.Equal(t, map[string][]string{"archive": tc.want}, plan.category.byTarget)
			for _, hash := range tc.want {
				require.Equal(t, ruleRef{id: tc.ruleID, name: "rule"}, plan.category.rules[hash])
			}
		})
	}
}
