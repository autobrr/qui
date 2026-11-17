// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json"
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
	"github.com/autobrr/qui/internal/rollout"
	"github.com/autobrr/qui/internal/services/transfer"
)

// transfers stands in for the Transfer module. It records each request.
type transfers struct {
	mu       sync.Mutex
	started  []transfer.Request
	previews []transfer.Request
	// startErr is the answer of Start and Preview, by infohash.
	startErr map[string]error
	// preview is the answer of Preview, by infohash. Without one, Preview
	// answers a Transfer of the torrent alone of 10 bytes, the way of the
	// request or "sftp".
	preview map[string]*transfer.Preview
	// onStart runs at each Start, before Start reads its context.
	onStart func()
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
	if err := f.startErr[req.Hash]; err != nil {
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

func (f *transfers) untouched(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Empty(t, f.started, "no Transfer starts")
	require.Empty(t, f.previews, "no Transfer is previewed")
}

var transfersOn = rollout.Switches{TransfersAndShares: true}

// transferRig is a source instance with torrents, a target instance, and the
// Transfer module behind the switch.
func transferRig(t *testing.T, torrents string, switches rollout.Switches) (*Service, *database.DB, *transfers, []int) {
	t.Helper()
	svc, db, instanceIDs := newStubService(t, nil, newStubQbt(t, torrents), newStubQbt(t, `{}`))
	runner := &transfers{}
	svc.SetTransferRunner(runner, switches)
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

// A rule with Transfer to Instance starts a Transfer of each torrent that it
// matches, with the fields of the action and the save path rendered for the
// torrent, and records each start.
func TestTransferToInstanceStartsATransfer(t *testing.T) {
	svc, db, runner, instanceIDs := transferRig(t, snapshotTorrent, transfersOn)
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

// While the switch for Transfers and Shares is off, a stored rule with
// Transfer to Instance reaches no Transfer: no start, no preview, and no
// activity, in a live run and in a dry run. With the switch on, a run without
// a rule with the action never asks the Transfer module.
func TestTransferToInstanceStaysOffWithoutTheSwitch(t *testing.T) {
	svc, db, runner, instanceIDs := transferRig(t, everyActionTorrents(), rollout.Production())
	run := func(rules []*models.Automation) []*models.AutomationActivity {
		t.Helper()
		_, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, false)
		require.NoError(t, err)
		activities, err := svc.applyRulesForInstance(t.Context(), instanceIDs[0], true, rules, true)
		require.NoError(t, err)
		return activities
	}

	activities := run([]*models.Automation{transferRule(1, instanceIDs[1], models.TransferToInstanceAction{SavePath: "/archive"})})
	runner.untouched(t)
	require.Empty(t, storedTransferActivities(t, db, instanceIDs[0]))
	require.Empty(t, transferActivities(t, svc, instanceIDs[0], activities))

	svc.SetTransferRunner(runner, transfersOn)
	run(everyActionRules())
	runner.untouched(t)
}

const (
	stillHash  = "4444444444444444444444444444444444444444"
	movingHash = "5555555555555555555555555555555555555555"
)

// precedenceTorrents holds a torrent that a rule moves, one that a rule
// deletes, one that qBittorrent moves now, and one that only the Transfer
// rule matches.
func precedenceTorrents() string {
	torrent := func(name, state string) string {
		return `{"name":"` + name + `","state":"` + state + `","progress":1,"size":10,"tags":"","category":"","save_path":"/data","content_path":"/data/` + name + `"}`
	}
	return `{"` + runningHash + `":` + torrent("running", "stalledUP") +
		`,"` + doomedHash + `":` + torrent("doomed", "stalledUP") +
		`,"` + movingHash + `":` + torrent("moving", "moving") +
		`,"` + stillHash + `":` + torrent("still", "stalledUP") + `}`
}

// Delete wins over Transfer in one run, and a Move in the same run, or one
// that qBittorrent runs now, makes the Transfer wait for a later run. A dry
// run lists the same Transfers.
func TestTransferToInstanceLosesToDeleteAndWaitsForAMove(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		svc, _, runner, instanceIDs := transferRig(t, precedenceTorrents(), transfersOn)
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

// A dry run lists the Transfers that a live run starts, by target and way,
// each with its size. A torrent that the Transfer of another torrent takes
// along, as a cross-seed, shows once.
func TestTransferToInstanceDryRunListsWayAndSize(t *testing.T) {
	svc, _, runner, instanceIDs := transferRig(t, everyActionTorrents(), transfersOn)
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

// When the Transfer module refuses a Transfer, the rule records the reason
// and asks again no sooner than an hour later. A torrent that a Transfer
// holds, or whose last Transfer failed for good and waits for the user, gets
// no new Transfer and no activity.
func TestTransferToInstanceAsksAgainAnHourAfterARefusal(t *testing.T) {
	svc, db, runner, instanceIDs := transferRig(t, everyActionTorrents(), transfersOn)
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

// regression: #4242
// When qBittorrent moves a torrent of the group, the module refuses the
// Transfer with ErrMoving. The rule records nothing and sets no hold, so the
// next run asks again. A dry run lists nothing for it.
func TestTransferToInstanceAsksAgainInTheNextRunWhileATorrentMoves(t *testing.T) {
	svc, db, runner, instanceIDs := transferRig(t, snapshotTorrent, transfersOn)
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

// regression: #4242
// When the run ends during a Start, the Transfers that did not start get no
// hold, and the next run starts them.
func TestTransferToInstanceStartsTheRestInTheNextRunAfterTheRunEnds(t *testing.T) {
	svc, _, runner, instanceIDs := transferRig(t, everyActionTorrents(), transfersOn)
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

// regression: #4242
// A target that answers no check before the timeout costs the plan of a run
// one wait, and not one for each torrent, so the other actions of the run keep
// their time. Each torrent still goes to the Transfer module, which decides.
// A target that answers is asked for each torrent, and the torrents that it
// has stay out.
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
