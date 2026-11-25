// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"fmt"
	"slices"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func planMoveTriggers(t *testing.T, svc *Service, torrents []qbt.Torrent, rule *models.Automation, evalCtx *EvalContext, order []string) plannedAction[string] {
	t.Helper()
	crossSeeds := buildCrossSeedGroups(torrentRefs(torrents), evalCtx)
	states := processTorrents(torrentRefs(torrents), []*models.Automation{rule}, evalCtx, crossSeeds, noSkipWindow(rule), nil, nil)
	torrentByHash := make(map[string]qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		torrentByHash[torrent.Hash] = torrent
	}
	var triggers plannedAction[string]
	for _, hash := range order {
		state := states[hash]
		require.NotNil(t, state, "%s is no move trigger", hash)
		require.NotEmpty(t, state.movePath, "%s is no move trigger", hash)
		triggers.add(state.movePath, hash, state.moveRule)
	}
	m := newMoveExpansion(runPlanInput{
		states:        states,
		deletions:     &deletionSelection{},
		torrents:      torrentRefs(torrents),
		torrentByHash: torrentRefMap(torrentByHash),
		ruleByID:      map[int]*models.Automation{rule.ID: rule},
		crossSeeds:    crossSeeds,
		evalCtx:       evalCtx,
		scope:         noSkipWindow(rule),
	}, triggers.rules)
	return svc.expandMoves(t.Context(), m, triggers)
}

func triggerOrders(hashes ...string) [][]string {
	if len(hashes) <= 1 {
		return [][]string{hashes}
	}
	var orders [][]string
	for i, first := range hashes {
		for _, tail := range triggerOrders(slices.Delete(slices.Clone(hashes), i, i+1)...) {
			orders = append(orders, append([]string{first}, tail...))
		}
	}
	return orders
}

func groupTriggers(savePath, contentPath string) []qbt.Torrent {
	triggers := make([]qbt.Torrent, 0, 3)
	for _, hash := range []string{"t1", "t2", "t3"} {
		triggers = append(triggers, qbt.Torrent{
			Hash: hash, Tracker: groupTestTracker, Tags: "x", SavePath: savePath, ContentPath: contentPath, HasMetadata: new(true),
		})
	}
	return triggers
}

func TestPlanMoves_GroupCheckIsOrderIndependent(t *testing.T) {
	moveRule := func(cond *models.RuleCondition, grouping *models.GroupingConfig, groupID string) *models.Automation {
		return groupTestRule(11, &models.ActionConditions{
			Grouping: grouping,
			Move:     &models.MoveAction{Enabled: true, Path: "/new", GroupID: groupID, Condition: cond},
		})
	}
	readsPrivate := moveRule(tagsContain("x"), nil, GroupCrossSeedContentPath)
	readsPrivate.Conditions.Pause = &models.PauseAction{Enabled: true, Condition: &models.RuleCondition{
		Field: models.FieldPrivate, Operator: models.OperatorEqual, Value: "true",
	}}
	skipGrouping := &models.GroupingConfig{Groups: []models.GroupDefinition{{
		ID: "content", Keys: []string{groupKeyContentPath}, AmbiguousPolicy: groupAmbiguousSkip,
	}}}

	for _, tc := range []struct {
		name     string
		torrents []qbt.Torrent
		rule     *models.Automation
		sharing  map[string][]string
		want     map[string][]string
	}{
		{
			name: "every member matches",
			torrents: append(groupTriggers("/old", "/old/show"),
				qbt.Torrent{Hash: "member", Tracker: memberTestTracker, Tags: "x", SavePath: "/old", ContentPath: "/old/show", HasMetadata: new(true)}),
			rule: moveRule(tagsContain("x"), nil, GroupCrossSeedContentPath),
			want: map[string][]string{"/new": {"member", "t1", "t2", "t3"}},
		},
		{
			name: "a member fails the move condition",
			torrents: append(groupTriggers("/old", "/old/show"),
				qbt.Torrent{Hash: "member", Tracker: memberTestTracker, SavePath: "/old", ContentPath: "/old/show", HasMetadata: new(true)}),
			rule: moveRule(tagsContain("x"), nil, GroupCrossSeedContentPath),
		},
		{
			name: "a member is dropped",
			torrents: append(groupTriggers("/old", "/old/show"),
				qbt.Torrent{Hash: "member", Tracker: memberTestTracker, Tags: "x", SavePath: "/old", ContentPath: "/old/show"}),
			rule: readsPrivate,
		},
		{
			name: "a member shares files with a torrent outside the group",
			torrents: append(groupTriggers("/old", "/old/show"),
				qbt.Torrent{Hash: "outside", Tracker: memberTestTracker, SavePath: "/old", ContentPath: "/old/other", HasMetadata: new(true)}),
			rule:    moveRule(tagsContain("x"), nil, GroupCrossSeedContentPath),
			sharing: sharedFiles([2]string{"t2", "outside"}),
		},
		{
			name:     "an ambiguous content path group with the policy skip",
			torrents: groupTriggers("/old", "/old"),
			rule:     moveRule(tagsContain("x"), skipGrouping, "content"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, order := range triggerOrders("t1", "t2", "t3") {
				t.Run(fmt.Sprint(order), func(t *testing.T) {
					evalCtx := &EvalContext{SameInstanceContentByHash: tc.sharing}
					planned := planMoveTriggers(t, &Service{}, tc.torrents, tc.rule, evalCtx, order)
					require.Equal(t, tc.want, planned.byTarget)
				})
			}
		})
	}
}

func TestPlanMoves_GroupThatFailsTheConditionReadsNoFiles(t *testing.T) {
	files := qbt.TorrentFiles{{Name: "show.mkv", Size: 100}}
	torrents := append(groupTriggers("/old", "/old"),
		qbt.Torrent{Hash: "member", Tracker: memberTestTracker, SavePath: "/old", ContentPath: "/old", HasMetadata: new(true)})
	rule := groupTestRule(12, &models.ActionConditions{Move: &models.MoveAction{
		Enabled: true, Path: "/new", GroupID: GroupCrossSeedContentPath, Condition: tagsContain("x"),
	}})
	reader := &countingFilesReader{staticFilesReader: staticFilesReader{"t1": files, "t2": files, "t3": files, "member": files}}

	planned := planMoveTriggers(t, &Service{filesReader: reader}, torrents, rule, &EvalContext{}, []string{"t1", "t2", "t3"})
	require.Empty(t, planned.byTarget)
	require.Empty(t, reader.asked)
}

func TestPlanMoves_OverlapFailureDoesNotBlockTheNextTrigger(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "a", Tracker: groupTestTracker, Tags: "x", SavePath: "/old", ContentPath: "/old"},
		{Hash: "b", Tracker: groupTestTracker, Tags: "x", SavePath: "/old", ContentPath: "/old"},
		{Hash: "c", Tracker: memberTestTracker, Tags: "x", SavePath: "/old", ContentPath: "/old"},
	}
	rule := groupTestRule(13, &models.ActionConditions{Move: &models.MoveAction{
		Enabled: true, Path: "/new", GroupID: GroupCrossSeedContentPath, Condition: tagsContain("x"),
	}})
	reader := planFilesReader{
		"a": {{Name: "show.mkv", Size: 100}},
		"b": {{Name: "show.mkv", Size: 100}, {Name: "film.mkv", Size: 100}},
		"c": {{Name: "film.mkv", Size: 100}},
	}

	planned := planMoveTriggers(t, &Service{filesReader: reader}, torrents, rule, &EvalContext{}, []string{"a", "b"})
	require.Equal(t, map[string][]string{"/new": {"a", "b", "c"}}, planned.byTarget)
	ref := ruleRef{id: 13, name: "rule"}
	require.Equal(t, map[string]ruleRef{"a": ref, "b": ref, "c": ref}, planned.rules)
}

func TestPlanMoves_TriggersWithoutGroupKeyArePlannedApart(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "blocked", Tracker: groupTestTracker, Tags: "x", SavePath: "/old", ContentPath: "/old/show"},
		{Hash: "free", Tracker: groupTestTracker, Tags: "x", SavePath: "/old", ContentPath: "/old/film"},
		{Hash: "outside", Tracker: memberTestTracker, SavePath: "/old", ContentPath: "/old/show"},
	}
	rule := groupTestRule(14, &models.ActionConditions{Move: &models.MoveAction{
		Enabled: true, Path: "/new", GroupID: GroupHardlinkSignature, Condition: tagsContain("x"),
	}})

	for _, order := range triggerOrders("blocked", "free") {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			evalCtx := &EvalContext{SameInstanceContentByHash: sharedFiles([2]string{"blocked", "outside"})}
			planned := planMoveTriggers(t, &Service{}, torrents, rule, evalCtx, order)
			require.Equal(t, map[string][]string{"/new": {"free"}}, planned.byTarget)
		})
	}
}
