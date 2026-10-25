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
