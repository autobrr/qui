// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"path/filepath"
	"slices"
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
		{Hash: "first", Tags: "delete", Size: 1000, SavePath: root + "/Pack", ContentPath: root + "/Pack/01.mkv"},
		{Hash: "second", Tags: "delete", Size: 1000, SavePath: root + "/Pack", ContentPath: root + "/Pack/02.mkv"},
		{Hash: "other", Tags: "delete", Size: 1000, SavePath: root, ContentPath: root + "/other.mkv"},
		{Hash: "pack", Size: 2000, SavePath: root, ContentPath: root + "/Pack"},
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
			for _, preview := range []bool{false, true} {
				torrents, evalCtx, rule := selectedFilesFixture(t)
				if tc.overlap {
					evalCtx.CrossSeedFilesByHash["first"] = append(evalCtx.CrossSeedFilesByHash["first"], qbt.TorrentFile{Name: "shared.srt", Size: 100})
					evalCtx.CrossSeedFilesByHash["second"] = append(evalCtx.CrossSeedFilesByHash["second"], qbt.TorrentFile{Name: "shared.srt", Size: 100})
					torrents[0].Size, torrents[1].Size = 1100, 1100
				}
				if tc.reverse {
					torrents[0], torrents[1] = torrents[1], torrents[0]
				}
				sm := qbittorrent.NewSyncManager(nil, nil)
				if preview {
					svc := &Service{syncManager: sm}
					result, err := svc.previewDeleteStandard(t.Context(), 1, rule, torrents, evalCtx, DeleteModeWithFiles, false,
						previewConfig{limit: 10}, nil, buildCrossSeedGroups(torrents, evalCtx))
					require.NoError(t, err)
					require.Equal(t, 2, result.TotalMatches)
					require.ElementsMatch(t, []string{"first", "second"}, []string{result.Examples[0].Hash, result.Examples[1].Hash})
				} else {
					states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, nil, nil)
					require.Len(t, states, 2)
					require.True(t, states["first"].shouldDelete)
					require.True(t, states["second"].shouldDelete)
				}
				wantBytes := int64(2000)
				if tc.overlap {
					wantBytes += 100
				}
				require.Equal(t, wantBytes, evalCtx.SpaceToClear)
			}
		})
	}
}

func TestPlainFreeSpaceProjection_UnreadSelectionStopsRule(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		torrents, evalCtx, rule := selectedFilesFixture(t)
		delete(evalCtx.CrossSeedFilesByHash, "second")
		if reverse {
			slices.Reverse(torrents[:2])
		}
		sm := qbittorrent.NewSyncManager(nil, nil)
		stats := make(map[int]*ruleRunStats)
		states := processTorrents(torrents, []*models.Automation{rule}, evalCtx, sm, nil, stats, nil)
		require.Empty(t, states, "a failed estimate cancels earlier selections and stops later deletions")
		require.Zero(t, evalCtx.SpaceToClear)
		require.Zero(t, stats[rule.ID].DeleteApplied)

		torrents, evalCtx, rule = selectedFilesFixture(t)
		delete(evalCtx.CrossSeedFilesByHash, "second")
		svc := &Service{syncManager: sm}
		result, err := svc.previewDeleteStandard(t.Context(), 1, rule, torrents, evalCtx, DeleteModeWithFiles, false,
			previewConfig{limit: 10}, nil, buildCrossSeedGroups(torrents, evalCtx))
		require.Error(t, err)
		require.Nil(t, result)
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
		states := processTorrents(torrents, []*models.Automation{first, second}, evalCtx, qbittorrent.NewSyncManager(nil, nil), nil, nil, nil)
		require.Len(t, states, 2)
		require.Equal(t, first.ID, states["first"].deleteRuleID)
		require.Equal(t, second.ID, states["second"].deleteRuleID)
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
	states := processTorrents(torrents, []*models.Automation{failed, other}, evalCtx, qbittorrent.NewSyncManager(nil, nil), nil, nil, nil)
	require.Len(t, states, 1)
	require.Equal(t, other.ID, states["other"].deleteRuleID)
	require.Zero(t, evalCtx.FreeSpaceStates[GetFreeSpaceRuleKey(failed)].SpaceToClear)
}

func TestPlainFreeSpaceProjection_LoadsFileListsOnlyForFreeSpaceDeletes(t *testing.T) {
	_, _, rule := selectedFilesFixture(t)
	require.True(t, ruleNeedsCrossSeedFiles(rule))
	rule.Conditions.Delete.Condition = rule.Conditions.Delete.Condition.Conditions[0]
	require.False(t, ruleNeedsCrossSeedFiles(rule))
}
