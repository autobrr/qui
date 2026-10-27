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

func TestDeletionSelection_HardlinkRuleFollowsTakenTorrent(t *testing.T) {
	// The pack shares files with both episodes, and the library copy is a hardlink of the pack.
	torrents := []qbt.Torrent{
		{Hash: "episode1", Tags: "first", AddedOn: 1, SavePath: "/data", ContentPath: "/data/show/e1.mkv"},
		{Hash: "episode2", Tags: "second", AddedOn: 2, SavePath: "/data", ContentPath: "/data/show/e2.mkv"},
		{Hash: "pack", AddedOn: 3, SavePath: "/data", ContentPath: "/data/show"},
		{Hash: "copy", AddedOn: 4, SavePath: "/library", ContentPath: "/library/show"},
	}
	packFiles := qbt.TorrentFiles{{Name: "show/e1.mkv", Size: 10}, {Name: "show/e2.mkv", Size: 10}}
	evalCtx := &EvalContext{
		SameInstanceContentByHash: map[string][]string{"episode1": {"pack"}, "episode2": {"pack"}, "pack": {"episode1", "episode2"}},
		CrossSeedFilesByHash: map[string]qbt.TorrentFiles{
			"episode1": {{Name: "show/e1.mkv", Size: 10}},
			"episode2": {{Name: "show/e2.mkv", Size: 10}},
			"pack":     packFiles,
			"copy":     packFiles,
		},
		hardlinkIndex: &HardlinkIndex{
			DeleteSafeSignatureByHash:  map[string]string{"pack": "x", "copy": "x"},
			DeleteSafeGroupBySignature: map[string][]string{"x": {"pack", "copy"}},
		},
	}
	rule := func(id int, tag string, hardlinks bool) *models.Automation {
		return &models.Automation{ID: id, Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeWithFilesIncludeCrossSeeds, IncludeHardlinks: hardlinks,
				Condition: &models.RuleCondition{Field: FieldTags, Operator: models.OperatorContains, Value: tag}},
		}}
	}

	selection := selectTestDeletions(t, []*models.Automation{rule(1, "first", false), rule(2, "second", true)}, torrents, evalCtx, false)
	require.Equal(t, 1, selection.byHash["pack"].rule.id, "rule 1 claims first, so its expansion takes the pack")
	require.Contains(t, selection.byHash, "copy", "rule 2 includes hardlinks, so the hardlink copy of the pack goes too")
	require.True(t, selection.byHash["copy"].hardlinkCopy)
}

func TestDeletePreview_KeepFilesGroupMemberGetsBadge(t *testing.T) {
	torrents := []qbt.Torrent{{Hash: "trigger"}, {Hash: "member"}}
	selection := &deletionSelection{byHash: map[string]selectedDeletion{
		"trigger": {hash: "trigger", deleteMode: DeleteModeKeepFiles, batchMode: DeleteModeKeepFiles, trigger: true},
		"member":  {hash: "member", deleteMode: DeleteModeKeepFiles, batchMode: DeleteModeKeepFiles},
	}}
	svc := &Service{syncManager: qbittorrent.NewSyncManager(nil, nil)}
	result := svc.buildDeletePreviewResult(torrents, selection, &EvalContext{}, previewConfig{limit: 10}, &models.Automation{}, nil)
	require.False(t, result.Examples[0].IsCrossSeed)
	require.True(t, result.Examples[1].IsCrossSeed, "the group expansion took the member")
	require.Zero(t, result.CrossSeedCount, "a keep-files group member is not a cross-seed")
}

// filesByHashReader gives each torrent its own file list, or fails the read with err.
type filesByHashReader struct {
	files map[string]qbt.TorrentFiles
	err   error
}

func (r filesByHashReader) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		if files, ok := r.files[hash]; ok {
			out[hash] = files
		}
	}
	return out, nil
}

func (filesByHashReader) GetCachedInstanceTorrents(context.Context, int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	return nil, nil
}

// Two torrents with the same folder as content path and save path form an ambiguous
// group. The group delete goes ahead only when their files overlap.
func TestDeletionSelection_SharedSavePathChecksFileOverlap(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: "a", Tags: "old", AddedOn: 1, SavePath: "/data", ContentPath: "/data"},
		{Hash: "b", Tags: "old", AddedOn: 2, SavePath: "/data", ContentPath: "/data"},
	}
	rule := &models.Automation{ID: 1, Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
		Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeKeepFiles, GroupID: GroupCrossSeedContentPath,
			Condition: &models.RuleCondition{Field: FieldTags, Operator: models.OperatorContains, Value: "old"}},
	}}

	tests := []struct {
		name  string
		files filesByHashReader
		want  []string
	}{
		{
			name: "files overlap",
			files: filesByHashReader{files: map[string]qbt.TorrentFiles{
				"a": {{Name: "movie.mkv", Size: 10}},
				"b": {{Name: "movie.mkv", Size: 10}},
			}},
			want: []string{"a", "b"},
		},
		{
			name: "files do not overlap",
			files: filesByHashReader{files: map[string]qbt.TorrentFiles{
				"a": {{Name: "first.mkv", Size: 10}},
				"b": {{Name: "second.mkv", Size: 10}},
			}},
			want: []string{},
		},
		{
			name:  "file read fails",
			files: filesByHashReader{err: errors.New("qBittorrent did not answer")},
			want:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{filesReader: tt.files}
			evalCtx := &EvalContext{}
			selection := svc.selectDeletions(t.Context(), 1, deletionSelectionInput{
				rules:      []*models.Automation{rule},
				torrents:   slices.Clone(torrents),
				evalCtx:    evalCtx,
				crossSeeds: buildCrossSeedGroups(torrents, evalCtx),
			})
			require.ElementsMatch(t, tt.want, slices.Collect(maps.Keys(selection.byHash)))
		})
	}
}
