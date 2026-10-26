// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

func selectedFilesFixture(t *testing.T) ([]qbt.Torrent, *EvalContext, *models.Automation) {
	t.Helper()
	root := filepath.ToSlash(t.TempDir())
	torrents := []qbt.Torrent{
		{Hash: "first", Tags: "delete", Size: 1000, AddedOn: 1, SavePath: root + "/Pack", ContentPath: root + "/Pack/01.mkv"},
		{Hash: "second", Tags: "delete", Size: 1000, AddedOn: 2, SavePath: root + "/Pack", ContentPath: root + "/Pack/02.mkv"},
		{Hash: "other", Tags: "delete", Size: 1000, AddedOn: 3, SavePath: root, ContentPath: root + "/other.mkv"},
		{Hash: "pack", Size: 2000, AddedOn: 4, SavePath: root, ContentPath: root + "/Pack"},
	}
	evalCtx := &EvalContext{
		FilesToClear: make(map[string]int64),
		SameInstanceContentByHash: map[string][]string{
			"first": {"pack"}, "second": {"pack"}, "pack": {"first", "second"},
		},
		CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"first":  {{Name: "01.mkv", Size: 1000}},
			"second": {{Name: "02.mkv", Size: 1000}},
			"pack":   {{Name: "Pack/01.mkv", Size: 1000}, {Name: "Pack/02.mkv", Size: 1000}},
		},
	}
	rule := &models.Automation{ID: 1, Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
		Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeWithFiles, Condition: &models.RuleCondition{
			Operator: models.OperatorAnd, Conditions: []*models.RuleCondition{
				{Field: FieldTags, Operator: models.OperatorContains, Value: "delete"},
				{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1500"},
			},
		}},
	}}
	return torrents, evalCtx, rule
}

func TestPlainFreeSpaceProjection_SelectedFileUnion(t *testing.T) {
	for _, tc := range []struct {
		name             string
		reverse, overlap bool
	}{
		{name: "episodes through excluded pack"},
		{name: "reverse order", reverse: true},
		{name: "partial overlap", overlap: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			torrents, evalCtx, rule := selectedFilesFixture(t)
			if tc.overlap {
				evalCtx.CrossSeedFilesByHash["first"] = append(evalCtx.CrossSeedFilesByHash["first"], qbt.TorrentFile{Name: "shared.srt", Size: 100})
				evalCtx.CrossSeedFilesByHash["second"] = append(evalCtx.CrossSeedFilesByHash["second"], qbt.TorrentFile{Name: "shared.srt", Size: 100})
				torrents[0].Size, torrents[1].Size = 1100, 1100
			}
			if tc.reverse {
				torrents[0].AddedOn, torrents[1].AddedOn = torrents[1].AddedOn, torrents[0].AddedOn
			}
			selection := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false)
			require.ElementsMatch(t, []string{"first", "second"}, selection.order)
			wantBytes := int64(2000)
			if tc.overlap {
				wantBytes += 100
			}
			require.Equal(t, wantBytes, evalCtx.SpaceToClear)
		})
	}
}

// A checking torrent frees no space, so the rule picks the next torrent to reach the target.
func TestPlainFreeSpaceProjection_SkipsCheckingTorrent(t *testing.T) {
	torrents, evalCtx, rule := selectedFilesFixture(t)
	torrents[0].State = qbt.TorrentStateCheckingUp
	selection := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx, false)
	require.ElementsMatch(t, []string{"second", "other"}, selection.order)
	require.Equal(t, int64(2000), evalCtx.SpaceToClear)
}

func TestPlainFreeSpaceProjection_UnreadSelectionStopsRule(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		torrents, evalCtx, rule := selectedFilesFixture(t)
		delete(evalCtx.CrossSeedFilesByHash, "second")
		if reverse {
			torrents[0].AddedOn, torrents[1].AddedOn = torrents[1].AddedOn, torrents[0].AddedOn
		}
		stats := make(map[int]*ruleRunStats)
		svc := &Service{syncManager: qbittorrent.NewSyncManager(nil, nil)}
		selection := svc.selectDeletions(t.Context(), 1, deletionSelectionInput{
			rules: []*models.Automation{rule}, torrents: torrents, evalCtx: evalCtx,
			crossSeeds: buildCrossSeedGroups(torrents, evalCtx), stats: stats,
		})
		require.Empty(t, selection.byHash, "a failed estimate cancels earlier selections and stops later deletions")
		require.Empty(t, selection.triggers)
		require.Error(t, selection.failedRules[rule.ID])
		require.Zero(t, evalCtx.SpaceToClear)
		require.Zero(t, stats[rule.ID].DeleteApplied)
	}
}

func TestPlainFreeSpaceProjection_RuleAndSourceIsolation(t *testing.T) {
	for _, otherSource := range []*models.FreeSpaceSource{nil, {Type: models.FreeSpaceSourcePath, Path: filepath.ToSlash(t.TempDir())}} {
		torrents, evalCtx, first := selectedFilesFixture(t)
		_, _, second := selectedFilesFixture(t)
		second.ID = 2
		second.FreeSpaceSource = otherSource
		first.Conditions.Delete.Condition.Conditions[1].Value = "500"
		second.Conditions.Delete.Condition.Conditions[1].Value = "500"
		evalCtx.CrossSeedFilesByHash["second"] = evalCtx.CrossSeedFilesByHash["first"]
		evalCtx.FreeSpaceStates = make(map[string]*FreeSpaceSourceState)
		for _, rule := range []*models.Automation{first, second} {
			evalCtx.FreeSpaceStates[GetFreeSpaceRuleKey(rule)] = &FreeSpaceSourceState{FilesToClear: make(map[string]int64)}
		}
		selection := selectTestDeletions(t, []*models.Automation{first, second}, torrents, evalCtx, false)
		require.Len(t, selection.byHash, 2)
		require.Equal(t, first.ID, selection.byHash["first"].rule.id)
		require.Equal(t, second.ID, selection.byHash["second"].rule.id)
		for _, state := range evalCtx.FreeSpaceStates {
			require.Equal(t, int64(1000), state.SpaceToClear)
			require.Len(t, state.FileBytesToClear, 1)
		}
	}
}

func TestPlainFreeSpaceProjection_FailureDoesNotStopOtherRules(t *testing.T) {
	torrents, evalCtx, failed := selectedFilesFixture(t)
	delete(evalCtx.CrossSeedFilesByHash, "second")
	torrents[2].Tags = "other-rule"
	_, _, other := selectedFilesFixture(t)
	other.ID = 2
	other.Conditions.Delete.Condition.Conditions[0].Value = "other-rule"
	evalCtx.FreeSpaceStates = map[string]*FreeSpaceSourceState{
		GetFreeSpaceRuleKey(failed): {FilesToClear: make(map[string]int64)},
		GetFreeSpaceRuleKey(other):  {FilesToClear: make(map[string]int64)},
	}
	selection := selectTestDeletions(t, []*models.Automation{failed, other}, torrents, evalCtx, false)
	require.Equal(t, []string{"other"}, selection.order)
	require.Equal(t, other.ID, selection.byHash["other"].rule.id)
	require.Zero(t, evalCtx.FreeSpaceStates[GetFreeSpaceRuleKey(failed)].SpaceToClear)
}

func TestPlainFreeSpaceProjection_LoadsFileListsOnlyForFreeSpaceDeletes(t *testing.T) {
	_, _, rule := selectedFilesFixture(t)
	require.True(t, ruleNeedsCrossSeedFiles(rule))
	rule.Conditions.Delete.Condition = rule.Conditions.Delete.Condition.Conditions[0]
	require.False(t, ruleNeedsCrossSeedFiles(rule))
}
