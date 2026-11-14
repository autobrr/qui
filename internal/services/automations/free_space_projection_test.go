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
	"github.com/autobrr/qui/internal/testutil/testdb"
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
	evalCtx.freeSpaceLedgers = testFreeSpaceLedgers(0, rule)
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
			selection := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx)
			require.ElementsMatch(t, []string{"first", "second"}, selection.order)
			wantBytes := int64(2000)
			if tc.overlap {
				wantBytes += 100
			}
			require.Equal(t, wantBytes, evalCtx.freeSpaceLedgers[rule.ID].projectedFreedBytes())
		})
	}
}

// A checking torrent frees no space, so the rule picks the next torrent to reach the target.
func TestPlainFreeSpaceProjection_SkipsCheckingTorrent(t *testing.T) {
	torrents, evalCtx, rule := selectedFilesFixture(t)
	torrents[0].State = qbt.TorrentStateCheckingUp
	selection := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx)
	require.ElementsMatch(t, []string{"second", "other"}, selection.order)
	require.Equal(t, int64(2000), evalCtx.freeSpaceLedgers[rule.ID].projectedFreedBytes())
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
			rules: []*models.Automation{rule}, torrents: torrentRefs(torrents), evalCtx: evalCtx,
			crossSeeds: buildCrossSeedGroups(torrentRefs(torrents), evalCtx), scope: noSkipWindow(rule), stats: stats,
		})
		require.Empty(t, selection.byHash, "a failed estimate cancels earlier selections and stops later deletions")
		require.Empty(t, selection.triggers)
		require.Error(t, selection.failedRules[rule.ID])
		require.Zero(t, evalCtx.freeSpaceLedgers[rule.ID].projectedFreedBytes())
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
		evalCtx.freeSpaceLedgers = testFreeSpaceLedgers(0, first, second)
		selection := selectTestDeletions(t, []*models.Automation{first, second}, torrents, evalCtx)
		require.Len(t, selection.byHash, 2)
		require.Equal(t, first.ID, selection.byHash["first"].rule.id)
		require.Equal(t, second.ID, selection.byHash["second"].rule.id)
		for _, rule := range []*models.Automation{first, second} {
			require.Equal(t, int64(1000), evalCtx.freeSpaceLedgers[rule.ID].projectedFreedBytes())
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
	evalCtx.freeSpaceLedgers = testFreeSpaceLedgers(0, failed, other)
	selection := selectTestDeletions(t, []*models.Automation{failed, other}, torrents, evalCtx)
	require.Equal(t, []string{"other"}, selection.order)
	require.Equal(t, other.ID, selection.byHash["other"].rule.id)
	require.Zero(t, evalCtx.freeSpaceLedgers[failed.ID].projectedFreedBytes())
}

func TestPlainFreeSpaceProjection_LoadsFileListsOnlyForFreeSpaceDeletes(t *testing.T) {
	_, _, rule := selectedFilesFixture(t)
	require.True(t, ruleNeedsCrossSeedFiles(rule))
	rule.Conditions.Delete.Condition = rule.Conditions.Delete.Condition.Conditions[0]
	require.False(t, ruleNeedsCrossSeedFiles(rule))
}

// regression: #2953
func TestPreviewFreeSpaceRule_FailsWhenTheInstanceDoesNotLoad(t *testing.T) {
	svc, instanceID := newDryRunParityService(t, defaultParityFixture())
	// qBittorrent still answers, but the instance row does not load.
	emptyStore, err := models.NewInstanceStore(testdb.NewMigratedSQLite(t, t.Name()+"-empty"), make([]byte, 32))
	require.NoError(t, err)
	svc.instanceStore = emptyStore
	freeSpaceBelow := &models.RuleCondition{Field: FieldFreeSpace, Operator: models.OperatorLessThan, Value: "50000000000"}

	rule := parityDeleteRule(DeleteModeWithFiles, freeSpaceBelow)
	for _, view := range []string{"needed", "eligible"} {
		result, err := svc.PreviewDeleteRule(t.Context(), instanceID, rule, 10, 0, view)
		require.Error(t, err, view)
		require.Nil(t, result, view)
	}

	tagRule := parityTagRule(models.TagAction{})
	tagRule.Conditions.Tags[0].Condition = freeSpaceBelow
	result, err := svc.PreviewTagRule(t.Context(), instanceID, tagRule, 10, 0)
	require.Error(t, err)
	require.Nil(t, result)

	// A rule that does not read FREE_SPACE previews without the instance.
	result, err = svc.PreviewDeleteRule(t.Context(), instanceID, parityDeleteRule(DeleteModeWithFiles, parityDeleteTag), 10, 0, "needed")
	require.NoError(t, err)
	require.NotNil(t, result)
}
