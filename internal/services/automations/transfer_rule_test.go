// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/transfer"
)

type transfers struct {
	mu         sync.Mutex
	started    []transfer.Request
	previews   []transfer.Request
	startErr   map[string]error
	previewErr map[string]error
	preview    map[string]*transfer.Preview
	onStart    func()
}

func (f *transfers) Start(ctx context.Context, req transfer.Request) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, req)
	if f.onStart != nil {
		f.onStart()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := f.startErr[req.Hash]; err != nil {
		return 0, err
	}
	return int64(len(f.started)), nil
}

func (f *transfers) Preview(_ context.Context, req transfer.Request) (*transfer.Preview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previews = append(f.previews, req)
	if err := cmp.Or(f.startErr[req.Hash], f.previewErr[req.Hash]); err != nil {
		return nil, err
	}
	if preview, ok := f.preview[req.Hash]; ok {
		return preview, nil
	}
	way := req.Options.ForcedWay
	if way == "" {
		way = transfer.WaySFTP
	}
	return &transfer.Preview{Way: way, Bytes: 10, Members: []transfer.Member{{Hash: req.Hash}}}, nil
}

func (f *transfers) hashes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.started))
	for _, req := range f.started {
		out = append(out, req.Hash)
	}
	slices.Sort(out)
	return out
}

func transferRig(t *testing.T, torrents string) (*Service, *database.DB, *transfers, []int) {
	t.Helper()
	svc, db, instanceIDs := newStubService(t, nil, newStubQbt(t, torrents), newStubQbt(t, `{}`))
	runner := &transfers{}
	svc.SetTransferRunner(runner)
	return svc, db, runner, instanceIDs
}

func transferRule(id, target int, action models.TransferToInstanceAction) *models.Automation {
	action.Enabled, action.TargetInstanceID = true, target
	return &models.Automation{ID: id, Name: "move old seeds", TrackerPattern: "*", Enabled: true, SortOrder: id,
		Conditions: &models.ActionConditions{TransferToInstance: &action}}
}

func transferActivities(t *testing.T, svc *Service, instanceID int, activities []*models.AutomationActivity) []stepActivityView {
	t.Helper()
	return viewActivities(t, svc, instanceID, slices.DeleteFunc(slices.Clone(activities), func(a *models.AutomationActivity) bool {
		return a.Action != models.ActivityActionTransferredToInstance
	}))
}

func storedTransferActivities(t *testing.T, db *database.DB, instanceID int) []models.AutomationActivity {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `SELECT hash, torrent_name, rule_id, rule_name, outcome, reason FROM automation_activity
		WHERE instance_id = ? AND action = ? ORDER BY id`, instanceID, models.ActivityActionTransferredToInstance)
	require.NoError(t, err)
	defer rows.Close()
	var out []models.AutomationActivity
	for rows.Next() {
		var a models.AutomationActivity
		require.NoError(t, rows.Scan(&a.Hash, &a.TorrentName, &a.RuleID, &a.RuleName, &a.Outcome, &a.Reason))
		out = append(out, a)
	}
	require.NoError(t, rows.Err())
	return out
}

func TestTransferToInstanceStartsATransfer(t *testing.T) {
	svc, db, runner, instanceIDs := transferRig(t, snapshotTorrent)
	rule := transferRule(1, instanceIDs[1], models.TransferToInstanceAction{
		SavePath: "/archive/{{ .Category }}", Category: "archive", Tags: []string{"moved"},
		After: "keepFiles", LeaveStopped: true, Way: "bittorrent",
	})

	_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, []*models.Automation{rule}, false)
	require.NoError(t, err)

	require.Equal(t, []transfer.Request{{
		SourceInstanceID: instanceIDs[0],
		Hash:             runningHash,
		TargetInstanceID: instanceIDs[1],
		Placement:        transfer.Placement{SavePath: "/archive/old", Category: "archive", Tags: []string{"moved"}},
		Options:          transfer.Options{After: transfer.AfterKeepFiles, LeaveStopped: true, ForcedWay: transfer.WayBitTorrent},
		StartedBy:        transfer.StartedByRule,
		RuleID:           new(1),
	}}, runner.started)
	require.Equal(t, []models.AutomationActivity{{
		Hash: runningHash, TorrentName: "running", RuleID: new(1), RuleName: "move old seeds", Outcome: models.ActivityOutcomeSuccess,
	}}, storedTransferActivities(t, db, instanceIDs[0]))
}

const (
	stillHash  = "4444444444444444444444444444444444444444"
	movingHash = "5555555555555555555555555555555555555555"
)

func precedenceTorrents() string {
	torrent := func(name, state string) string {
		return `{"name":"` + name + `","state":"` + state + `","progress":1,"size":10,"tags":"","category":"","save_path":"/data","content_path":"/data/` + name + `"}`
	}
	return `{"` + runningHash + `":` + torrent("running", "stalledUP") +
		`,"` + doomedHash + `":` + torrent("doomed", "stalledUP") +
		`,"` + movingHash + `":` + torrent("moving", "moving") +
		`,"` + stillHash + `":` + torrent("still", "stalledUP") + `}`
}

func TestTransferToInstanceLosesToDeleteAndWaitsForAMove(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		svc, _, runner, instanceIDs := transferRig(t, precedenceTorrents())
		rules := []*models.Automation{
			transferRule(1, instanceIDs[1], models.TransferToInstanceAction{SavePath: "/archive"}),
			{ID: 2, Name: "move", TrackerPattern: "*", Enabled: true, SortOrder: 2, Conditions: &models.ActionConditions{
				Move: &models.MoveAction{Enabled: true, Path: "/moved", Condition: nameIs("running")},
			}},
			{ID: 3, Name: "delete", TrackerPattern: "*", Enabled: true, SortOrder: 3, Conditions: &models.ActionConditions{
				Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeKeepFiles, Condition: nameIs("doomed")},
			}},
		}

		_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, dryRun)
		require.NoError(t, err)

		if dryRun {
			runner.mu.Lock()
			require.Len(t, runner.previews, 1)
			require.Equal(t, stillHash, runner.previews[0].Hash)
			runner.mu.Unlock()
			require.Empty(t, runner.hashes())
			continue
		}
		require.Equal(t, []string{stillHash}, runner.hashes())
	}
}

func TestTransferToInstanceWaitsForAnExportToAnotherInstance(t *testing.T) {
	for _, tc := range []struct {
		name          string
		exported      string
		toTheSameSide bool
		started       []string
		asked         int
	}{
		{name: "the Export is still to do", exported: `{}`, started: []string{}},
		{name: "the other instance has the torrent", exported: snapshotTorrent, started: []string{runningHash}},
		{name: "the Export goes to the target of the Transfer", exported: `{}`, toTheSameSide: true, started: []string{runningHash}, asked: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, instanceIDs := newStubService(t, nil, newStubQbt(t, snapshotTorrent), newStubQbt(t, tc.exported), newStubQbt(t, `{}`))
			runner := &transfers{}
			svc.SetTransferRunner(runner)
			svc.SetExportRunner(exportsEnd{})
			transferTarget := instanceIDs[2]
			if tc.toTheSameSide {
				transferTarget = instanceIDs[1]
			}
			rule := transferRule(1, transferTarget, models.TransferToInstanceAction{SavePath: "/archive"})
			rule.Conditions.ExportToInstance = &models.ExportToInstanceAction{Enabled: true, TargetInstanceID: instanceIDs[1], SavePath: "/exported"}

			_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, []*models.Automation{rule}, false)
			require.NoError(t, err)

			require.Equal(t, tc.started, runner.hashes())
			require.Len(t, runner.previews, tc.asked)
		})
	}
}

func TestTransferToInstanceWaitsForTheExportOfACrossSeed(t *testing.T) {
	crossSeed := `{"` + stoppedHash + `":{"name":"stopped","state":"stoppedUP","progress":1,"size":10}}`
	for _, tc := range []struct {
		name     string
		exported string
		alone    bool
		refused  bool
		listed   int
		started  []string
	}{
		{name: "the Export of the cross-seed is still to do", exported: `{}`, started: []string{}},
		{name: "the other instance has the cross-seed", exported: crossSeed, listed: 1, started: []string{runningHash}},
		{name: "the Transfer takes no cross-seed along", exported: `{}`, alone: true, listed: 1, started: []string{runningHash}},
		{name: "the module does not say what the Transfer takes along", exported: `{}`, refused: true, listed: 1, started: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, instanceIDs := newStubService(t, nil, newStubQbt(t, everyActionTorrents()), newStubQbt(t, tc.exported), newStubQbt(t, `{}`))
			runner := &transfers{}
			if !tc.alone {
				runner.preview = map[string]*transfer.Preview{runningHash: {Way: transfer.WaySFTP, Bytes: 10,
					Members: []transfer.Member{{Hash: runningHash}, {Hash: stoppedHash}}}}
			}
			if tc.refused {
				runner.previewErr = map[string]error{runningHash: errors.New("no answer")}
			}
			svc.SetTransferRunner(runner)
			svc.SetExportRunner(exportsEnd{})
			rules := []*models.Automation{
				transferRule(1, instanceIDs[2], models.TransferToInstanceAction{SavePath: "/archive", Condition: nameIs("running")}),
				{ID: 2, Name: "export the cross-seed", TrackerPattern: "*", Enabled: true, SortOrder: 2, Conditions: &models.ActionConditions{
					ExportToInstance: &models.ExportToInstanceAction{Enabled: true, TargetInstanceID: instanceIDs[1], SavePath: "/exported", Condition: nameIs("stopped")}}},
			}

			planned, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, true)
			require.NoError(t, err)
			require.Len(t, transferActivities(t, svc, instanceIDs[0], planned), tc.listed)

			_, err = svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, false)
			require.NoError(t, err)
			require.Equal(t, tc.started, runner.hashes())
		})
	}
}

type exportsEnd struct{}

func (exportsEnd) Export(context.Context, transfer.ExportRequest) (int64, <-chan transfer.ExportResult, error) {
	done := make(chan transfer.ExportResult)
	close(done)
	return 1, done, nil
}

func TestTransferToInstanceDryRunListsWayAndSize(t *testing.T) {
	svc, _, runner, instanceIDs := transferRig(t, everyActionTorrents())
	runner.preview = map[string]*transfer.Preview{
		runningHash: {Way: transfer.WayBitTorrent, Bytes: 4096, Members: []transfer.Member{{Hash: runningHash}, {Hash: stoppedHash}}},
		stoppedHash: {Way: transfer.WayBitTorrent, Bytes: 4096, Members: []transfer.Member{{Hash: stoppedHash}, {Hash: runningHash}}},
		doomedHash:  {Way: transfer.WaySFTP, Bytes: 2048, Members: []transfer.Member{{Hash: doomedHash}}},
	}
	rules := []*models.Automation{transferRule(1, instanceIDs[1], models.TransferToInstanceAction{SavePath: "/archive"})}

	activities, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, true)
	require.NoError(t, err)

	require.Empty(t, runner.hashes(), "a dry run starts no Transfer")
	byWay := map[string]stepActivityView{}
	for _, view := range transferActivities(t, svc, instanceIDs[0], activities) {
		var details struct{ Way string }
		require.NoError(t, json.Unmarshal([]byte(view.Details), &details))
		byWay[details.Way] = view
	}
	require.Len(t, byWay, 2)
	target := strconv.Itoa(instanceIDs[1])
	require.JSONEq(t, `{"targetInstanceId":`+target+`,"way":"sftp","count":1}`, byWay["sftp"].Details)
	require.Len(t, byWay["sftp"].Items, 1)
	require.Equal(t, doomedHash, byWay["sftp"].Items[0].Hash)
	require.Equal(t, new(int64(2048)), byWay["sftp"].Items[0].Size)
	require.JSONEq(t, `{"targetInstanceId":`+target+`,"way":"bittorrent","count":1}`, byWay["bittorrent"].Details)
	require.Len(t, byWay["bittorrent"].Items, 1)
	require.Equal(t, new(int64(4096)), byWay["bittorrent"].Items[0].Size)
	for _, activity := range activities {
		if activity.Action == models.ActivityActionTransferredToInstance {
			require.Equal(t, models.ActivityOutcomeDryRun, activity.Outcome)
		}
	}
}

func TestTransferToInstanceFollowsTheLastRuleForTorrentsThatShareFiles(t *testing.T) {
	torrent := func(name string) string {
		return `{"name":"` + name + `","state":"stalledUP","progress":1,"size":10,"tags":"","category":"","save_path":"/data","content_path":"/data/shared"}`
	}
	svc, _, runner, instanceIDs := transferRig(t, `{"`+runningHash+`":`+torrent("seed")+`,"`+stillHash+`":`+torrent("cross-seed")+`}`)
	keep := transferRule(1, instanceIDs[1], models.TransferToInstanceAction{SavePath: "/archive", After: "keepSeeding", Condition: nameIs("seed")})
	remove := transferRule(2, instanceIDs[1], models.TransferToInstanceAction{SavePath: "/archive", After: "removeWithFiles", Condition: nameIs("cross-seed")})

	for range 10 {
		for _, rules := range [][]*models.Automation{{keep, remove}, {remove, keep}} {
			rules[0].SortOrder, rules[1].SortOrder = 1, 2
			runner.mu.Lock()
			runner.started = nil
			runner.mu.Unlock()

			_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, false)
			require.NoError(t, err)
			runner.mu.Lock()
			require.Equal(t, rules[1].ID, *runner.started[0].RuleID, "the Transfer of the last rule starts first")
			runner.mu.Unlock()
		}
	}
}

func TestTransferToInstanceAsksAgainAnHourAfterARefusal(t *testing.T) {
	svc, db, runner, instanceIDs := transferRig(t, everyActionTorrents())
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return at }
	runner.startErr = map[string]error{
		runningHash: &transfer.RefusalError{Reason: "Transfer needs qBittorrent 5.1 or newer. Update qBittorrent on stub-1."},
		stoppedHash: transfer.ErrClaimed,
		doomedHash:  transfer.ErrWaitsForUser,
	}
	rules := []*models.Automation{transferRule(1, instanceIDs[1], models.TransferToInstanceAction{SavePath: "/archive"})}
	run := func() {
		t.Helper()
		_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, false)
		require.NoError(t, err)
	}

	run()
	require.ElementsMatch(t, []string{runningHash, stoppedHash, doomedHash}, runner.hashes())
	require.Equal(t, []models.AutomationActivity{{
		Hash: runningHash, TorrentName: "running", RuleID: new(1), RuleName: "move old seeds", Outcome: models.ActivityOutcomeFailed,
		Reason: "Transfer needs qBittorrent 5.1 or newer. Update qBittorrent on stub-1.",
	}}, storedTransferActivities(t, db, instanceIDs[0]))

	at = at.Add(59 * time.Minute)
	run()
	require.Equal(t, 1, count(runner.hashes(), runningHash), "the refusal holds for an hour")
	require.Equal(t, 2, count(runner.hashes(), stoppedHash))

	at = at.Add(2 * time.Minute)
	run()
	require.Equal(t, 2, count(runner.hashes(), runningHash))
	require.Len(t, storedTransferActivities(t, db, instanceIDs[0]), 2)
}

func TestTransferToInstancePassesASetOfHardlinkedCrossSeedsOnce(t *testing.T) {
	svc, db, runner, instanceIDs := transferRig(t, everyActionTorrents())
	rules := []*models.Automation{transferRule(1, instanceIDs[1], models.TransferToInstanceAction{SavePath: "/archive"})}
	runner.startErr = map[string]error{stoppedHash: transfer.ErrStays}

	for range 2 {
		_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, false)
		require.NoError(t, err)
	}
	require.Equal(t, 2, count(runner.hashes(), stoppedHash), "the next run asks again")
	stored := storedTransferActivities(t, db, instanceIDs[0])
	require.Len(t, stored, 4, "each run records the two Transfers that start")
	for _, activity := range stored {
		require.NotEqual(t, stoppedHash, activity.Hash)
		require.Equal(t, models.ActivityOutcomeSuccess, activity.Outcome)
	}

	runner.startErr = nil
	runner.preview = map[string]*transfer.Preview{
		runningHash: {Way: transfer.WayBitTorrent, Bytes: 4096, Members: []transfer.Member{{Hash: runningHash}}, Stay: 1, Versions: []string{stoppedHash}},
		stoppedHash: {Way: transfer.WayBitTorrent, Bytes: 4096, Members: []transfer.Member{{Hash: stoppedHash}}, Stay: 1, Versions: []string{runningHash}},
	}
	activities, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, true)
	require.NoError(t, err)
	listed := 0
	for _, view := range transferActivities(t, svc, instanceIDs[0], activities) {
		var details struct{ Way string }
		require.NoError(t, json.Unmarshal([]byte(view.Details), &details))
		if details.Way == string(transfer.WayBitTorrent) {
			listed += len(view.Items)
		}
	}
	require.Equal(t, 1, listed, "the dry run lists one Transfer torrent for the set")
}

func TestTransferToInstanceAsksAgainInTheNextRunWhileATorrentMoves(t *testing.T) {
	svc, db, runner, instanceIDs := transferRig(t, snapshotTorrent)
	runner.startErr = map[string]error{runningHash: fmt.Errorf("Show.S01E01.mkv shares the files of this torrent: %w", transfer.ErrMoving)}
	rules := []*models.Automation{transferRule(1, instanceIDs[1], models.TransferToInstanceAction{SavePath: "/archive"})}
	run := func(dryRun bool) []*models.AutomationActivity {
		t.Helper()
		activities, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, dryRun)
		require.NoError(t, err)
		return activities
	}

	require.Empty(t, transferActivities(t, svc, instanceIDs[0], run(true)), "a dry run lists nothing")
	run(false)
	run(false)
	require.Equal(t, []string{runningHash, runningHash}, runner.hashes(), "each run asks again")
	require.Empty(t, storedTransferActivities(t, db, instanceIDs[0]))
}

func TestTransferToInstanceStartsTheRestInTheNextRunAfterTheRunEnds(t *testing.T) {
	svc, _, runner, instanceIDs := transferRig(t, everyActionTorrents())
	rules := []*models.Automation{transferRule(1, instanceIDs[1], models.TransferToInstanceAction{SavePath: "/archive"})}
	ctx, cancel := context.WithCancel(t.Context())
	runner.onStart = cancel

	_, _ = svc.applyRulesForInstance(ctx, instanceIDs[0], true, rules, false)
	require.Len(t, runner.hashes(), 1, "the run ends during its first Start")

	runner.onStart = nil
	_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, false)
	require.NoError(t, err)
	hashes := runner.hashes()
	require.Len(t, hashes, 4, "the next run starts each torrent")
	require.Subset(t, hashes, []string{runningHash, stoppedHash, doomedHash})
}

func TestTransferToInstanceWaitsForASlowTargetOncePerRun(t *testing.T) {
	torrents := []qbt.Torrent{{Hash: "a", Tags: "x"}, {Hash: "b", Tags: "x"}, {Hash: "c", Tags: "x"}}
	rule := planTestRule(1, &models.ActionConditions{TransferToInstance: &models.TransferToInstanceAction{
		Enabled: true, TargetInstanceID: 2, Condition: tagsContain("x"),
	}})
	for _, tc := range []struct {
		name    string
		target  fakeTargetLookup
		wait    time.Duration
		planned []string
	}{
		{name: "slow target", target: fakeTargetLookup{slow: true}, wait: 10 * time.Second, planned: []string{"a", "b", "c"}},
		{name: "target answers", target: fakeTargetLookup{onTarget: map[string]bool{"b": true, "c": true}}, planned: []string{"a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				svc := &Service{targetLookup: tc.target, transferRunner: &transfers{}}
				start := time.Now()
				plan := planTestRunWith(t, svc, torrents, []*models.Automation{rule}, nil, nil)
				require.Equal(t, tc.wait, time.Since(start))
				planned := make([]string, 0, len(plan.transfers))
				for _, pending := range plan.transfers {
					planned = append(planned, pending.hash)
				}
				slices.Sort(planned)
				require.Equal(t, tc.planned, planned)
			})
		})
	}
}

func count(hashes []string, hash string) int {
	n := 0
	for _, h := range hashes {
		if h == hash {
			n++
		}
	}
	return n
}
