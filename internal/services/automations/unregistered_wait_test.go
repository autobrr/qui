// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/automations/verdict"
)

var waitTestNow = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

func waitTestSnapshot() *qbittorrent.TrackerHealthSnapshot {
	return qbittorrent.NewTrackerHealthSnapshotWithSince(
		map[string]qbittorrent.TrackerHealth{
			"fresh": qbittorrent.TrackerHealthUnregistered,
			"old":   qbittorrent.TrackerHealthUnregistered,
			"down":  qbittorrent.TrackerHealthDown,
		},
		map[string]time.Time{
			"fresh": waitTestNow.Add(-24 * time.Hour),
			"old":   waitTestNow.Add(-8 * 24 * time.Hour),
			"down":  waitTestNow.Add(-8 * 24 * time.Hour),
		},
	)
}

func waitTestContext(snapshot *qbittorrent.TrackerHealthSnapshot) *EvalContext {
	evalCtx := &EvalContext{waited: verdict.ForDelete(snapshot, waitTestNow)}
	evalCtx.health = evalCtx.waited.Raw()
	return evalCtx
}

func unregisteredIs(value string) *models.RuleCondition {
	return &models.RuleCondition{Field: FieldIsUnregistered, Operator: models.OperatorEqual, Value: value}
}

func ratioBelow(value string) *models.RuleCondition {
	return &models.RuleCondition{Field: FieldRatio, Operator: models.OperatorLessThan, Value: value}
}

func waitDeleteRule(id int, wait *int, cond *models.RuleCondition) *models.Automation {
	return &models.Automation{ID: id, Name: "rule", Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
		Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeKeepFiles, UnregisteredWaitSeconds: wait, Condition: cond},
	}}
}

func waitingOf(sel *deletionSelection) []string {
	hashes := make([]string, 0, len(sel.waiting))
	for _, waiting := range sel.waiting {
		hashes = append(hashes, waiting.hash)
	}
	return hashes
}

func TestDeleteWaitDecidesWhichUnregisteredTorrentsADeleteRuleTakes(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "fresh", Ratio: 0.5, AddedOn: 1, State: qbt.TorrentStateStalledUp},
		{Hash: "old", Ratio: 0.5, AddedOn: 2, State: qbt.TorrentStateStalledUp},
		{Hash: "down", Ratio: 0.5, AddedOn: 3, State: qbt.TorrentStateStalledUp},
		{Hash: "fine", Ratio: 3, AddedOn: 4, State: qbt.TorrentStateStalledUp},
	}
	tests := []struct {
		name        string
		rules       []*models.Automation
		notLoaded   bool
		wantDeleted []string
		wantWaiting []string
	}{
		{name: "a stored rule on State unregistered with no wait waits 7 days", rules: []*models.Automation{waitDeleteRule(1, nil,
			&models.RuleCondition{Field: FieldState, Operator: models.OperatorEqual, Value: "unregistered"})},
			wantDeleted: []string{"old"}, wantWaiting: []string{"fresh", "down"}},
		{name: "a negated read never deletes a waiting torrent", rules: []*models.Automation{waitDeleteRule(1, nil,
			&models.RuleCondition{Field: FieldIsUnregistered, Operator: models.OperatorEqual, Value: "true", Negate: true})},
			wantDeleted: []string{"fine"}, wantWaiting: []string{}},
		{name: "a second rule on ratio still deletes the waiting torrents", rules: []*models.Automation{
			waitDeleteRule(1, nil, unregisteredIs("true")), waitDeleteRule(2, nil, ratioBelow("1")),
		}, wantDeleted: []string{"fresh", "old", "down"}, wantWaiting: []string{}},
		{name: "a rule skips its delete until the times load", notLoaded: true, rules: []*models.Automation{
			waitDeleteRule(1, nil, unregisteredIs("true")), waitDeleteRule(2, nil, &models.RuleCondition{Field: FieldRatio, Operator: models.OperatorGreaterThan, Value: "2"}),
		}, wantDeleted: []string{"fine"}, wantWaiting: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := waitTestSnapshot()
			if tt.notLoaded {
				snapshot = nil
			}
			sel := selectTestDeletions(t, tt.rules, slices.Clone(torrents), waitTestContext(snapshot))
			require.ElementsMatch(t, tt.wantDeleted, slices.Collect(maps.Keys(sel.byHash)))
			require.ElementsMatch(t, tt.wantWaiting, waitingOf(sel))
		})
	}
}

func TestDeleteWaitTimeIsTheUnregisteredSincePlusTheWait(t *testing.T) {
	torrents := []qbt.Torrent{{Hash: "fresh", State: qbt.TorrentStateStalledUp}}
	sel := selectTestDeletions(t, []*models.Automation{waitDeleteRule(1, new(3*24*3600), unregisteredIs("true"))}, torrents, waitTestContext(waitTestSnapshot()))
	require.Len(t, sel.waiting, 1)
	require.Equal(t, waitTestNow.Add(2*24*time.Hour), sel.waiting[0].until)
}

func TestDeleteWaitHoldsAnUnregisteredTorrentThatHasNoTimeYet(t *testing.T) {
	snapshot := qbittorrent.NewTrackerHealthSnapshotWithSince(map[string]qbittorrent.TrackerHealth{"untimed": qbittorrent.TrackerHealthUnregistered}, nil)
	torrents := []qbt.Torrent{{Hash: "untimed", State: qbt.TorrentStateStalledUp}}
	sel := selectTestDeletions(t, []*models.Automation{waitDeleteRule(1, nil, unregisteredIs("true"))}, torrents, waitTestContext(snapshot))
	require.Empty(t, sel.byHash, "the raw verdict keeps it, and the wait has not started")
	require.Equal(t, []waitingDelete{{hash: "untimed", rule: ruleRef{id: 1, name: "rule"}, until: waitTestNow.Add(models.DefaultUnregisteredWait)}}, sel.waiting)
}

func TestDeleteWaitHoldsAGroupWithAWaitingMemberAndTakesAWaitingCrossSeed(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "old", AddedOn: 1, State: qbt.TorrentStateStalledUp, SavePath: "/data", ContentPath: "/data/movie"},
		{Hash: "fresh", AddedOn: 2, State: qbt.TorrentStateStalledUp, SavePath: "/data", ContentPath: "/data/movie"},
	}
	files := qbt.TorrentFiles{{Name: "movie/movie.mkv", Size: 10}}
	context := func() *EvalContext {
		evalCtx := waitTestContext(waitTestSnapshot())
		evalCtx.SameInstanceContentByHash = sharingByContentPath(torrents)
		evalCtx.CrossSeedFilesByHash = map[string]qbt.TorrentFiles{"old": files, "fresh": files}
		return evalCtx
	}
	group := waitDeleteRule(1, nil, unregisteredIs("true"))
	group.Conditions.Delete.GroupID = GroupCrossSeedContentPath
	group.Conditions.Delete.Atomic = "all"
	crossSeeds := waitDeleteRule(1, nil, unregisteredIs("true"))
	crossSeeds.Conditions.Delete.Mode = DeleteModeWithFilesIncludeCrossSeeds

	tests := []struct {
		name        string
		rule        *models.Automation
		wantDeleted []string
	}{
		{name: "a group that needs every member holds while one waits", rule: group, wantDeleted: []string{}},
		{name: "a waiting cross-seed of a due torrent goes with it", rule: crossSeeds, wantDeleted: []string{"old", "fresh"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := selectTestDeletions(t, []*models.Automation{tt.rule}, slices.Clone(torrents), context())
			require.ElementsMatch(t, tt.wantDeleted, slices.Collect(maps.Keys(sel.byHash)))
			if len(tt.wantDeleted) > 0 {
				require.Empty(t, sel.waiting, "a deleted torrent is not reported as waiting")
			}
		})
	}
}

func TestDeleteWaitRanksAWaitingTorrentAsNotUnregistered(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "fresh", Ratio: 0.5, AddedOn: 1, State: qbt.TorrentStateStalledUp},
		{Hash: "old", Ratio: 0.5, AddedOn: 2, State: qbt.TorrentStateStalledUp},
	}
	ranked := func(wait *int) *models.Automation {
		rule := waitDeleteRule(1, wait, ratioBelow("1"))
		rule.SortingConfig = &models.SortingConfig{SchemaVersion: "1", Type: models.SortingTypeScore, Direction: models.SortDirectionDESC, ScoreRules: []models.ScoreRule{
			{Type: models.ScoreRuleTypeConditional, Conditional: &models.ConditionalScoreRule{Score: 100, Condition: unregisteredIs("true")}},
		}}
		return rule
	}

	sel := selectTestDeletions(t, []*models.Automation{ranked(new(0))}, slices.Clone(torrents), waitTestContext(waitTestSnapshot()))
	require.Equal(t, []string{"fresh", "old"}, sel.order, "with no wait both rank as unregistered, and the older add comes first")

	sel = selectTestDeletions(t, []*models.Automation{ranked(nil)}, slices.Clone(torrents), waitTestContext(waitTestSnapshot()))
	require.Equal(t, []string{"old", "fresh"}, sel.order, "the waiting torrent ranks below the due one")
}

func TestDeleteWaitSortsAgainForARuleWithAnotherWait(t *testing.T) {
	root := filepath.ToSlash(t.TempDir())
	torrents := []qbt.Torrent{
		{Hash: "fine", Size: 1000, AddedOn: 1, State: qbt.TorrentStateStalledUp, SavePath: root, ContentPath: root + "/fine.mkv"},
		{Hash: "fresh", Size: 1000, AddedOn: 2, State: qbt.TorrentStateStalledUp, SavePath: root, ContentPath: root + "/fresh.mkv"},
		{Hash: "old", Size: 1000, AddedOn: 3, State: qbt.TorrentStateStalledUp, SavePath: root, ContentPath: root + "/old.mkv"},
	}
	unregisteredFirst := &models.SortingConfig{SchemaVersion: "1", Type: models.SortingTypeScore, Direction: models.SortDirectionDESC,
		ScoreRules: []models.ScoreRule{{Type: models.ScoreRuleTypeConditional, Conditional: &models.ConditionalScoreRule{Condition: unregisteredIs("true"), Score: 100}}}}
	waiting := waitDeleteRule(1, nil, &models.RuleCondition{Field: FieldTags, Operator: models.OperatorContains, Value: "never"})
	waiting.SortingConfig = unregisteredFirst
	freeing := waitDeleteRule(2, new(0), &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1500"})
	freeing.Conditions.Delete.Mode = DeleteModeWithFiles
	freeing.SortingConfig = unregisteredFirst
	evalCtx := waitTestContext(waitTestSnapshot())
	evalCtx.CrossSeedFilesByHash = map[string]qbt.TorrentFiles{}
	for _, torrent := range torrents {
		evalCtx.CrossSeedFilesByHash[torrent.Hash] = qbt.TorrentFiles{{Name: torrent.Hash + ".mkv", Size: 1000}}
	}
	evalCtx.freeSpaceLedgers = testFreeSpaceLedgers(0, freeing)

	sel := selectTestDeletions(t, []*models.Automation{waiting, freeing}, torrents, evalCtx)
	require.ElementsMatch(t, []string{"fresh", "old"}, slices.Collect(maps.Keys(sel.byHash)), "a rule with no wait ranks the torrent unregistered for a day first")
}
