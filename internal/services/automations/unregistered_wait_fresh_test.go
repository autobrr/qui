// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/automations/verdict"
	"github.com/autobrr/qui/internal/testutil/logbudget"
)

func TestDeleteWaitFreshReadHoldsOnlyTheDeletesThatNeedTheVerdict(t *testing.T) {
	torrents := []qbt.Torrent{
		{Hash: waitOldHash, Ratio: 0.5, AddedOn: 1, State: qbt.TorrentStateStalledUp, SavePath: "/data", ContentPath: "/data/movie"},
		{Hash: waitFreshHash, Ratio: 0.5, AddedOn: 2, State: qbt.TorrentStateStalledUp, SavePath: "/data", ContentPath: "/data/movie"},
	}
	files := qbt.TorrentFiles{{Name: "movie/movie.mkv", Size: 10}}
	group := waitDeleteRule(1, new(0), unregisteredIs("true"))
	group.Conditions.Delete.GroupID = GroupCrossSeedContentPath
	group.Conditions.Delete.Atomic = "all"
	orRatio := waitDeleteRule(1, new(0), &models.RuleCondition{Operator: models.OperatorOr, Conditions: []*models.RuleCondition{unregisteredIs("true"), ratioBelow("1")}})
	ranked := waitDeleteRule(1, new(0), ratioBelow("1"))
	ranked.SortingConfig = &models.SortingConfig{SchemaVersion: "1", Type: models.SortingTypeScore, Direction: models.SortDirectionDESC, ScoreRules: []models.ScoreRule{
		{Type: models.ScoreRuleTypeConditional, Conditional: &models.ConditionalScoreRule{Score: 100, Condition: unregisteredIs("true")}},
	}}

	tests := []struct {
		name        string
		rule        *models.Automation
		still       map[string]bool
		err         error
		wantDeleted []string
		wantAsked   [][]string
		wantApplied int
		wantSkipped int
	}{
		{name: "a group that needs every member holds when the read clears one", rule: group, still: map[string]bool{waitOldHash: true},
			wantDeleted: []string{}, wantAsked: [][]string{{waitOldHash, waitFreshHash}}, wantApplied: 1, wantSkipped: 1},
		{name: "the same group deletes when the read confirms both", rule: group,
			wantDeleted: []string{waitOldHash, waitFreshHash}, wantAsked: [][]string{{waitOldHash, waitFreshHash}}, wantApplied: 2},
		{name: "an OR arm on ratio deletes what the read cleared", rule: orRatio, still: map[string]bool{},
			wantDeleted: []string{waitOldHash, waitFreshHash}, wantAsked: [][]string{{waitOldHash, waitFreshHash}}, wantApplied: 2},
		{name: "a score that reads the verdict asks for no read", rule: ranked, still: map[string]bool{},
			wantDeleted: []string{waitOldHash, waitFreshHash}, wantApplied: 2},
		{name: "a failed read deletes none and writes no skip line", rule: waitDeleteRule(1, new(0), unregisteredIs("true")), err: errors.New("qBittorrent did not answer"),
			wantDeleted: []string{}, wantAsked: [][]string{{waitOldHash, waitFreshHash}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := qbittorrent.NewTrackerHealthSnapshotWithSince(
				map[string]qbittorrent.TrackerHealth{waitOldHash: qbittorrent.TrackerHealthUnregistered, waitFreshHash: qbittorrent.TrackerHealthUnregistered},
				map[string]time.Time{waitOldHash: waitTestNow.Add(-8 * 24 * time.Hour), waitFreshHash: waitTestNow.Add(-24 * time.Hour)},
			)
			health := &fakeTrackerHealth{snapshot: snapshot, still: tt.still, err: tt.err}
			evalCtx := &EvalContext{waited: verdict.ForDelete(snapshot, waitTestNow)}
			evalCtx.health = evalCtx.waited.Raw()
			evalCtx.SameInstanceContentByHash = sharingByContentPath(torrents)
			evalCtx.CrossSeedFilesByHash = map[string]qbt.TorrentFiles{waitOldHash: files, waitFreshHash: files}
			rules := []*models.Automation{tt.rule}
			svc := &Service{syncManager: qbittorrent.NewSyncManager(nil, nil), trackerHealth: health}

			stats := map[int]*ruleRunStats{}
			var sel *deletionSelection
			lines := logbudget.Count(t, func() {
				sel = svc.selectDeletions(t.Context(), 1, deletionSelectionInput{
					rules:      rules,
					torrents:   torrentRefs(torrents),
					evalCtx:    evalCtx,
					crossSeeds: buildCrossSeedGroups(torrentRefs(torrents), evalCtx),
					scope:      noSkipWindow(rules...),
					stats:      stats,
					live:       true,
				})
			})

			require.ElementsMatch(t, tt.wantDeleted, slices.Collect(maps.Keys(sel.byHash)))
			require.Equal(t, tt.wantAsked, health.asked)
			require.Equal(t, tt.wantApplied, stats[1].DeleteApplied, "the run counts only the deletes that the read kept")
			require.Equal(t, tt.wantSkipped, lines.Message("automations: skipped the deletes of torrents that are no longer unregistered"))
		})
	}
}
