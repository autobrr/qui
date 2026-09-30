// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestPlanQueueMoves(t *testing.T) {
	top := func(hash string, pos int64) queueMoveInput {
		return queueMoveInput{Hash: hash, TargetPosition: models.QueuePositionTop, CurrentPriority: pos}
	}
	bottom := func(hash string, pos int64) queueMoveInput {
		return queueMoveInput{Hash: hash, TargetPosition: models.QueuePositionBottom, CurrentPriority: pos}
	}

	tests := []struct {
		name             string
		inputs           []queueMoveInput
		queueingEnabled  bool
		totalDownloading int
		batchSize        int
		wantTop          [][]string
		wantBottom       [][]string
	}{
		{
			name:             "queueing off sends nothing",
			inputs:           []queueMoveInput{top("a", 5), bottom("b", 1)},
			queueingEnabled:  false,
			totalDownloading: 5,
			batchSize:        50,
		},
		{
			name:             "no inputs sends nothing",
			queueingEnabled:  true,
			totalDownloading: 5,
			batchSize:        50,
		},
		{
			name:             "top already at 1..k is skipped",
			inputs:           []queueMoveInput{top("b", 2), top("a", 1)},
			queueingEnabled:  true,
			totalDownloading: 5,
			batchSize:        50,
		},
		{
			name:             "top with a gap is moved in current order",
			inputs:           []queueMoveInput{top("c", 3), top("a", 1)},
			queueingEnabled:  true,
			totalDownloading: 5,
			batchSize:        50,
			wantTop:          [][]string{{"a", "c"}},
		},
		{
			name:             "bottom already at N-k+1..N is skipped",
			inputs:           []queueMoveInput{bottom("e", 5), bottom("d", 4)},
			queueingEnabled:  true,
			totalDownloading: 5,
			batchSize:        50,
		},
		{
			name:             "bottom not at the end is moved in current order",
			inputs:           []queueMoveInput{bottom("d", 4), bottom("b", 2)},
			queueingEnabled:  true,
			totalDownloading: 5,
			batchSize:        50,
			wantBottom:       [][]string{{"b", "d"}},
		},
		{
			// Each topPrio call lands above the previous one, so the batch
			// holding the lowest positions goes last to end up at 1..k.
			name:             "top split across batches sends lowest positions last",
			inputs:           []queueMoveInput{top("e", 9), top("a", 3), top("d", 8), top("b", 5), top("c", 6)},
			queueingEnabled:  true,
			totalDownloading: 10,
			batchSize:        2,
			wantTop:          [][]string{{"e"}, {"c", "d"}, {"a", "b"}},
		},
		{
			// Each bottomPrio call lands below the previous one, so the batch
			// holding the highest positions goes last to end up at N.
			name:             "bottom split across batches sends highest positions last",
			inputs:           []queueMoveInput{bottom("e", 9), bottom("a", 1), bottom("d", 8), bottom("b", 2), bottom("c", 6)},
			queueingEnabled:  true,
			totalDownloading: 10,
			batchSize:        2,
			wantBottom:       [][]string{{"a", "b"}, {"c", "d"}, {"e"}},
		},
		{
			name:             "mixed top and bottom in one pass",
			inputs:           []queueMoveInput{top("c", 3), bottom("a", 1), top("d", 4), bottom("b", 2)},
			queueingEnabled:  true,
			totalDownloading: 6,
			batchSize:        50,
			wantTop:          [][]string{{"c", "d"}},
			wantBottom:       [][]string{{"a", "b"}},
		},
		{
			name:             "mixed with top in place moves only bottom",
			inputs:           []queueMoveInput{top("a", 1), bottom("b", 2)},
			queueingEnabled:  true,
			totalDownloading: 6,
			batchSize:        50,
			wantBottom:       [][]string{{"b"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTop, gotBottom := planQueueMoves(tt.inputs, tt.queueingEnabled, tt.totalDownloading, tt.batchSize)
			require.Equal(t, tt.wantTop, gotTop)
			require.Equal(t, tt.wantBottom, gotBottom)
		})
	}
}

var (
	queueHashA = strings.Repeat("a", 40)
	queueHashB = strings.Repeat("b", 40)
	queueHashC = strings.Repeat("c", 40)
	queueHashD = strings.Repeat("d", 40)
	queueHashS = strings.Repeat("e", 40)
)

// queueStubTorrents holds a queue of four downloads (keep-1, keep-2, move-3, move-4)
// plus a finished "move" torrent that has no queue position.
func queueStubTorrents() string {
	torrent := func(hash, name string, priority int, state string) string {
		return fmt.Sprintf(`%q:{"name":%q,"priority":%d,"state":%q,"progress":0.5,"size":10,"save_path":"/data","content_path":"/data/%s"}`, hash, name, priority, state, name)
	}
	return "{" + strings.Join([]string{
		torrent(queueHashA, "keep-1", 1, "downloading"),
		torrent(queueHashB, "keep-2", 2, "downloading"),
		torrent(queueHashC, "move-3", 3, "downloading"),
		torrent(queueHashD, "move-4", 4, "downloading"),
		torrent(queueHashS, "move-seed", 0, "stalledUP"),
	}, ",") + "}"
}

func queuePositionRule(position, nameContains string) *models.Automation {
	return &models.Automation{
		ID:             1,
		Name:           "queue",
		TrackerPattern: "*",
		Enabled:        true,
		Conditions: &models.ActionConditions{
			QueuePosition: &models.QueuePositionAction{
				Enabled:   true,
				Position:  position,
				Condition: &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: nameContains},
			},
		},
	}
}

func TestApplyRules_QueuePosition(t *testing.T) {
	tests := []struct {
		name         string
		rule         *models.Automation
		wantPath     string
		wantHashes   string
		wantActivity string
	}{
		{
			name:         "top moves torrents not at 1..k and skips the finished one",
			rule:         queuePositionRule(models.QueuePositionTop, "move"),
			wantPath:     "/api/v2/torrents/topPrio",
			wantHashes:   queueHashC + "|" + queueHashD,
			wantActivity: models.ActivityActionQueueTopped,
		},
		{
			name: "top already at 1..k sends nothing",
			rule: queuePositionRule(models.QueuePositionTop, "keep"),
		},
		{
			name:         "bottom moves torrents not at N-k+1..N",
			rule:         queuePositionRule(models.QueuePositionBottom, "keep"),
			wantPath:     "/api/v2/torrents/bottomPrio",
			wantHashes:   queueHashA + "|" + queueHashB,
			wantActivity: models.ActivityActionQueueBottomed,
		},
		{
			name: "bottom already at N-k+1..N sends nothing",
			rule: queuePositionRule(models.QueuePositionBottom, "move"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &qbitStub{torrents: queueStubTorrents(), queueingEnabled: true}
			svc, instanceID := newStubQbitService(t, stub)

			_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{tt.rule}, false)
			require.NoError(t, err)

			top := stub.postsTo("/api/v2/torrents/topPrio")
			bottom := stub.postsTo("/api/v2/torrents/bottomPrio")
			activities, err := svc.activityStore.ListByInstance(t.Context(), instanceID, 10)
			require.NoError(t, err)

			if tt.wantPath == "" {
				require.Empty(t, top)
				require.Empty(t, bottom)
				require.Empty(t, activities)
				return
			}
			require.Equal(t, []string{tt.wantHashes}, stub.postsTo(tt.wantPath))
			require.Len(t, append(top, bottom...), 1)

			require.Len(t, activities, 1)
			require.Equal(t, tt.wantActivity, activities[0].Action)
			require.Equal(t, models.ActivityOutcomeSuccess, activities[0].Outcome)
			require.JSONEq(t, `{"count":2}`, string(activities[0].Details))

			run, err := svc.GetActivityRun(instanceID, activities[0].ID, 10, 0)
			require.NoError(t, err)
			gotHashes := make([]string, 0, len(run.Items))
			for _, item := range run.Items {
				gotHashes = append(gotHashes, item.Hash)
			}
			require.ElementsMatch(t, strings.Split(tt.wantHashes, "|"), gotHashes)
		})
	}
}

// A rule saved while queueing was on keeps running its other actions after queueing is
// turned off; only the queue move is skipped, and it leaves no failed activity behind.
func TestApplyRules_QueuePositionSkippedWhenQueueingDisabled(t *testing.T) {
	stub := &qbitStub{torrents: queueStubTorrents(), queueingEnabled: false}
	svc, instanceID := newStubQbitService(t, stub)

	rule := queuePositionRule(models.QueuePositionTop, "move")
	rule.Conditions.Pause = &models.PauseAction{Enabled: true}

	_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)
	require.NoError(t, err)

	require.Empty(t, stub.postsTo("/api/v2/torrents/topPrio"))
	require.NotEmpty(t, append(stub.postsTo("/api/v2/torrents/stop"), stub.postsTo("/api/v2/torrents/pause")...), "pause must still run")

	activities, err := svc.activityStore.ListByInstance(t.Context(), instanceID, 10)
	require.NoError(t, err)
	require.Len(t, activities, 1)
	require.Equal(t, models.ActivityActionPaused, activities[0].Action)
}

func TestApplyRuleDryRun_QueuePositionMatchesLiveRun(t *testing.T) {
	for _, position := range []string{models.QueuePositionTop, models.QueuePositionBottom} {
		t.Run(position, func(t *testing.T) {
			nameContains := map[string]string{models.QueuePositionTop: "move", models.QueuePositionBottom: "keep"}[position]

			dryStub := &qbitStub{torrents: queueStubTorrents(), queueingEnabled: true}
			drySvc, dryInstanceID := newStubQbitService(t, dryStub)
			dryActivities, err := drySvc.ApplyRuleDryRun(t.Context(), dryInstanceID, queuePositionRule(position, nameContains))
			require.NoError(t, err)
			require.Empty(t, dryStub.postsTo("/api/v2/torrents/topPrio"), "dry run must not move torrents")
			require.Empty(t, dryStub.postsTo("/api/v2/torrents/bottomPrio"), "dry run must not move torrents")

			liveStub := &qbitStub{torrents: queueStubTorrents(), queueingEnabled: true}
			liveSvc, liveInstanceID := newStubQbitService(t, liveStub)
			_, err = liveSvc.applyRulesForInstance(t.Context(), liveInstanceID, true, []*models.Automation{queuePositionRule(position, nameContains)}, false)
			require.NoError(t, err)
			liveActivities, err := liveSvc.activityStore.ListByInstance(t.Context(), liveInstanceID, 10)
			require.NoError(t, err)

			require.Len(t, dryActivities, 1)
			require.Len(t, liveActivities, 1)
			require.Equal(t, models.ActivityOutcomeDryRun, dryActivities[0].Outcome)
			require.Equal(t, liveActivities[0].Action, dryActivities[0].Action)
			require.JSONEq(t, string(liveActivities[0].Details), string(dryActivities[0].Details))

			runHashes := func(svc *Service, instanceID, activityID int) []string {
				run, err := svc.GetActivityRun(instanceID, activityID, 10, 0)
				require.NoError(t, err)
				hashes := make([]string, 0, len(run.Items))
				for _, item := range run.Items {
					hashes = append(hashes, item.Hash)
				}
				return hashes
			}
			require.ElementsMatch(t,
				runHashes(liveSvc, liveInstanceID, liveActivities[0].ID),
				runHashes(drySvc, dryInstanceID, dryActivities[0].ID))
		})
	}
}
