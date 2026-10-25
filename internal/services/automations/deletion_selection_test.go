// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"slices"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// selectTestDeletions runs the Deletion selection without a qBittorrent client.
func selectTestDeletions(t *testing.T, rules []*models.Automation, torrents []qbt.Torrent, evalCtx *EvalContext, eligibleView bool) *deletionSelection {
	t.Helper()
	svc := &Service{syncManager: qbittorrent.NewSyncManager(nil, nil)}
	return svc.selectDeletions(t.Context(), 1, deletionSelectionInput{
		rules:        rules,
		torrents:     torrents,
		evalCtx:      evalCtx,
		crossSeeds:   buildCrossSeedGroups(torrents, evalCtx),
		eligibleView: eligibleView,
	})
}

// A cross-seed that two rules reach goes to the rule that claims first, in rule order.
func TestDeletionSelection_SharedCrossSeedGoesToTheFirstRule(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "b", Tags: "second", AddedOn: 1, SavePath: "/data", ContentPath: "/data/movie"},
		{Hash: "a", Tags: "first", AddedOn: 2, SavePath: "/data", ContentPath: "/data/movie"},
	}
	files := qbt.TorrentFiles{{Name: "movie/movie.mkv", Size: 10}}
	evalCtx := &EvalContext{
		SameInstanceContentByHash: sharingByContentPath(torrents),
		CrossSeedFilesByHash:      map[string]qbt.TorrentFiles{"a": files, "b": files},
	}
	rule := func(id int, tag string) *models.Automation {
		return &models.Automation{ID: id, Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeWithFilesIncludeCrossSeeds,
				Condition: &models.RuleCondition{Field: FieldTags, Operator: models.OperatorContains, Value: tag}},
		}}
	}

	for range 20 {
		selection := selectTestDeletions(t, []*models.Automation{rule(1, "first"), rule(2, "second")}, slices.Clone(torrents), evalCtx, false)
		require.Equal(t, map[string]struct{}{"a": {}, "b": {}}, selection.triggers)
		require.Equal(t, 1, selection.byHash["a"].rule.id)
		require.Equal(t, 1, selection.byHash["b"].rule.id, "rule 1 claims first, so its expansion takes the cross-seed")
		require.False(t, selection.byHash["b"].trigger)
	}
}

// Each delete rule claims only the torrents on the trackers of its pattern.
func TestDeletionSelection_TrackerPatternPerRule(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "a", Tags: "old", AddedOn: 1, Tracker: "https://alpha.example/announce"},
		{Hash: "b", Tags: "old", AddedOn: 2, Tracker: "https://beta.example/announce"},
		{Hash: "c", Tags: "old", AddedOn: 3, Tracker: "https://alpha.example/announce"},
	}
	rule := func(id int, pattern string) *models.Automation {
		return &models.Automation{ID: id, Enabled: true, TrackerPattern: pattern, Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeKeepFiles,
				Condition: &models.RuleCondition{Field: FieldTags, Operator: models.OperatorContains, Value: "old"}},
		}}
	}

	selection := selectTestDeletions(t, []*models.Automation{rule(1, "alpha.example"), rule(2, "!alpha.example")}, torrents, &EvalContext{}, false)
	require.Len(t, selection.byHash, 3)
	require.Equal(t, 1, selection.byHash["a"].rule.id)
	require.Equal(t, 2, selection.byHash["b"].rule.id)
	require.Equal(t, 1, selection.byHash["c"].rule.id)
}

// A later rule that claims a torrent still expands it, when an earlier rule's expansion
// already took the torrent but with a narrower scope.
func TestDeletionSelection_LaterClaimStillExpandsWiderScope(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "a", Tags: "first", AddedOn: 1, SavePath: "/data", ContentPath: "/data/movie"},
		{Hash: "b", Tags: "second", AddedOn: 2, SavePath: "/data", ContentPath: "/data/movie"},
		{Hash: "c", AddedOn: 3, SavePath: "/library", ContentPath: "/library/movie"},
	}
	files := qbt.TorrentFiles{{Name: "movie/movie.mkv", Size: 10}}
	evalCtx := &EvalContext{
		SameInstanceContentByHash: sharingByContentPath(torrents),
		CrossSeedFilesByHash:      map[string]qbt.TorrentFiles{"a": files, "b": files, "c": files},
		hardlinkIndex: &HardlinkIndex{
			DeleteSafeSignatureByHash:  map[string]string{"b": "x", "c": "x"},
			DeleteSafeGroupBySignature: map[string][]string{"x": {"b", "c"}},
		},
	}
	rule := func(id int, tag string, hardlinks bool) *models.Automation {
		return &models.Automation{ID: id, Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeWithFilesIncludeCrossSeeds, IncludeHardlinks: hardlinks,
				Condition: &models.RuleCondition{Field: FieldTags, Operator: models.OperatorContains, Value: tag}},
		}}
	}

	selection := selectTestDeletions(t, []*models.Automation{rule(1, "first", false), rule(2, "second", true)}, torrents, evalCtx, false)
	require.Equal(t, 1, selection.byHash["b"].rule.id, "rule 1 claims first, so its expansion takes the cross-seed")
	require.Contains(t, selection.byHash, "c", "rule 2 claims b with hardlinks, so the hardlink copy goes too")
	require.Equal(t, 2, selection.byHash["c"].rule.id)
	require.True(t, selection.byHash["c"].hardlinkCopy)
}
