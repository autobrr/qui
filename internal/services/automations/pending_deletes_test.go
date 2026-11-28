// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json/v2"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const pendingYear = 365 * 24 * 3600

type pendingRig struct {
	t          *testing.T
	svc        *Service
	db         *database.DB
	instanceID int
	stub       *stubQbt
	health     *fakeTrackerHealth
	rules      *models.AutomationStore
	store      *models.PendingDeleteStore
}

func newPendingRig(t *testing.T, torrents string) *pendingRig {
	t.Helper()
	stub := newStubQbt(t, torrents)
	stub.webAPIVersion = webAPIQbit52
	svc, db, instanceIDs := newStubService(t, nil, stub)
	rig := &pendingRig{t: t, svc: svc, db: db, instanceID: instanceIDs[0], stub: stub, health: &fakeTrackerHealth{},
		rules: models.NewAutomationStore(db), store: models.NewPendingDeleteStore(db)}
	rig.setHealth(map[string]qbittorrent.TrackerHealth{
		waitFreshHash: qbittorrent.TrackerHealthUnregistered,
		waitOldHash:   qbittorrent.TrackerHealthUnregistered,
		waitDownHash:  qbittorrent.TrackerHealthDown,
	}, map[string]time.Time{
		waitFreshHash: waitTestNow.Add(-24 * time.Hour),
		waitOldHash:   waitTestNow.Add(-8 * 24 * time.Hour),
		waitDownHash:  waitTestNow.Add(-8 * 24 * time.Hour),
	})
	rig.restart()
	return rig
}

func (r *pendingRig) restart() {
	r.svc = NewService(Config{ApplyTimeout: DefaultConfig().ApplyTimeout}, r.svc.instanceStore, r.rules, models.NewAutomationActivityStore(r.db), nil, r.svc.syncManager, nil, nil, nil, r.svc.backendPool)
	r.svc.trackerHealth = r.health
	r.svc.clock = func() time.Time { return waitTestNow }
	r.svc.SetPendingDeleteStore(r.store)
}

func (r *pendingRig) setHealth(health map[string]qbittorrent.TrackerHealth, since map[string]time.Time) {
	r.health.mu.Lock()
	r.health.snapshot = qbittorrent.NewTrackerHealthSnapshotWithSince(health, since)
	r.health.mu.Unlock()
}

func (r *pendingRig) createRule(rule *models.Automation) *models.Automation {
	r.t.Helper()
	rule.ID = 0
	rule.InstanceID = r.instanceID
	stored, err := r.rules.Create(r.t.Context(), rule)
	require.NoError(r.t, err)
	return stored
}

func (r *pendingRig) run(rules ...*models.Automation) []*models.AutomationActivity {
	r.t.Helper()
	activities, err := r.svc.applyRulesForInstance(r.t.Context(), r.instanceID, true, rules, false)
	require.NoError(r.t, err)
	return waitLines(activities)
}

func (r *pendingRig) rows() map[string]time.Time {
	r.t.Helper()
	rows, err := r.store.ListPendingDeletes(r.t.Context(), r.instanceID)
	require.NoError(r.t, err)
	byHash := make(map[string]time.Time, len(rows))
	for _, row := range rows {
		byHash[row.Hash] = row.UnregisteredSince
	}
	return byHash
}

func (r *pendingRig) seed(ruleID int, since map[string]time.Time) {
	r.t.Helper()
	var change models.PendingDeleteChange
	for hash, at := range since {
		change.Put = append(change.Put, models.PendingDelete{RuleID: ruleID, Hash: hash, UnregisteredSince: at})
	}
	require.NoError(r.t, r.store.ApplyPendingDeletes(r.t.Context(), r.instanceID, change))
}

func waitLines(activities []*models.AutomationActivity) []*models.AutomationActivity {
	var lines []*models.AutomationActivity
	for _, activity := range activities {
		if activity.Action == models.ActivityActionUnregisteredWaitStarted || activity.Action == models.ActivityActionUnregisteredWaitEnded {
			lines = append(lines, activity)
		}
	}
	return lines
}

func requireStartedLine(t *testing.T, line *models.AutomationActivity, rule *models.Automation, count int) {
	t.Helper()
	require.Equal(t, models.ActivityActionUnregisteredWaitStarted, line.Action)
	require.Equal(t, models.ActivityOutcomeSuccess, line.Outcome)
	require.Positive(t, line.ID)
	var details waitStartedDetails
	require.NoError(t, json.Unmarshal(line.Details, &details))
	require.Equal(t, waitStartedDetails{Count: count, Rules: []waitRuleCount{{ID: rule.ID, Name: rule.Name, Count: count}}}, details)
}

func requireEndedLine(t *testing.T, line *models.AutomationActivity, rule *models.Automation, causes waitEndedCauses) {
	t.Helper()
	require.Equal(t, models.ActivityActionUnregisteredWaitEnded, line.Action)
	var details waitEndedDetails
	require.NoError(t, json.Unmarshal(line.Details, &details))
	count := causes.Stopped + causes.TrackerWorks
	require.Equal(t, waitEndedDetails{Count: count, Causes: causes, Rules: []waitRuleCount{{ID: rule.ID, Name: rule.Name, Count: count}}}, details)
}

func TestPendingDeletesReportEachTorrentOnceWhenItStartsAndLeavesTheWait(t *testing.T) {
	rig := newPendingRig(t, waitRunTorrents)
	rule := rig.createRule(waitDeleteRule(0, new(pendingYear), unregisteredIs("true")))

	lines := rig.run(rule)
	require.Len(t, lines, 1)
	requireStartedLine(t, lines[0], rule, 3)
	require.Equal(t, map[string]time.Time{
		waitFreshHash: waitTestNow.Add(-24 * time.Hour),
		waitOldHash:   waitTestNow.Add(-8 * 24 * time.Hour),
		waitDownHash:  waitTestNow.Add(-8 * 24 * time.Hour),
	}, rig.rows())
	items, err := rig.svc.GetActivityRun(rig.instanceID, lines[0].ID, 10, 0)
	require.NoError(t, err)
	require.Equal(t, 3, items.Total, "the line lists the torrents behind it")

	require.Empty(t, rig.run(rule), "the second run reports nothing new")

	rig.setHealth(map[string]qbittorrent.TrackerHealth{
		waitFreshHash: qbittorrent.TrackerHealthDown,
		waitOldHash:   qbittorrent.TrackerHealthDown,
		waitDownHash:  qbittorrent.TrackerHealthDown,
	}, map[string]time.Time{
		waitFreshHash: waitTestNow.Add(-24 * time.Hour),
		waitOldHash:   waitTestNow.Add(-2 * 24 * time.Hour),
		waitDownHash:  waitTestNow.Add(-8 * 24 * time.Hour),
	})
	require.Empty(t, rig.run(rule), "a tracker that is down and a moved time write nothing")
	require.Equal(t, waitTestNow.Add(-2*24*time.Hour), rig.rows()[waitOldHash], "a moved time updates the row")
	rig.setHealth(map[string]qbittorrent.TrackerHealth{
		waitFreshHash: qbittorrent.TrackerHealthDown,
		waitOldHash:   qbittorrent.TrackerHealthDown,
		waitDownHash:  qbittorrent.TrackerHealthDown,
	}, map[string]time.Time{
		waitFreshHash: waitTestNow.Add(-24 * time.Hour),
		waitOldHash:   waitTestNow.Add(-2*24*time.Hour + time.Second),
		waitDownHash:  waitTestNow.Add(-8 * 24 * time.Hour),
	})
	require.Empty(t, rig.run(rule))
	require.Equal(t, waitTestNow.Add(-2*24*time.Hour+time.Second), rig.rows()[waitOldHash], "a time moved by a second updates the row")

	rig.restart()
	require.Empty(t, rig.run(rule), "a restart reports the same torrents again")

	rig.setHealth(map[string]qbittorrent.TrackerHealth{
		waitOldHash:  qbittorrent.TrackerHealthDown,
		waitDownHash: qbittorrent.TrackerHealthDown,
	}, map[string]time.Time{
		waitOldHash:  waitTestNow.Add(-2 * 24 * time.Hour),
		waitDownHash: waitTestNow.Add(-8 * 24 * time.Hour),
	})
	lines = rig.run(rule)
	require.Len(t, lines, 1)
	requireEndedLine(t, lines[0], rule, waitEndedCauses{TrackerWorks: 1})
	require.NotContains(t, rig.rows(), waitFreshHash)
	require.Len(t, rig.rows(), 2)
	require.Empty(t, rig.run(rule))
}

func TestPendingDeletesGiveStoppedAsTheCauseForAStoppedTorrent(t *testing.T) {
	rig := newPendingRig(t, `{
		"`+waitFreshHash+`": {"name": "Some.Show.S01E01.1080p.WEB-GRP", "ratio": 0.5, "progress": 1, "added_on": 1, "state": "stoppedUP", "save_path": "/data", "content_path": "/data/fresh"},
		"`+waitOldHash+`": {"name": "Other.Film.2024.2160p.BluRay-GRP", "ratio": 0.5, "progress": 1, "added_on": 2, "state": "stalledUP", "save_path": "/data", "content_path": "/data/old"}
	}`)
	rule := rig.createRule(waitDeleteRule(0, new(pendingYear), unregisteredIs("true")))
	rig.seed(rule.ID, map[string]time.Time{waitFreshHash: waitTestNow.Add(-time.Hour), waitOldHash: waitTestNow.Add(-time.Hour)})
	rig.setHealth(nil, nil)

	lines := rig.run(rule)
	require.Len(t, lines, 1)
	requireEndedLine(t, lines[0], rule, waitEndedCauses{Stopped: 1, TrackerWorks: 1})
	require.Empty(t, rig.rows())
}

func TestPendingDeletesRemoveRowsWithoutALineWhenTheTorrentIsGoneDeletedOrStillTimed(t *testing.T) {
	rig := newPendingRig(t, waitRunTorrents)
	rule := rig.createRule(waitDeleteRule(0, new(pendingYear), &models.RuleCondition{Operator: models.OperatorAnd, Conditions: []*models.RuleCondition{
		unregisteredIs("true"), {Field: FieldName, Operator: models.OperatorNotContains, Value: "Third.Show"},
	}}))
	ratio := rig.createRule(&models.Automation{Name: "ratio", Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
		Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeKeepFiles, Condition: &models.RuleCondition{Field: FieldName, Operator: models.OperatorContains, Value: "Some.Show"}},
	}})
	gone := "1234567890123456789012345678901234567890"
	rig.seed(rule.ID, map[string]time.Time{gone: waitTestNow.Add(-time.Hour), waitFreshHash: waitTestNow.Add(-time.Hour), waitDownHash: waitTestNow.Add(-time.Hour)})

	lines := rig.run(rule, ratio)
	require.Equal(t, []string{waitFreshHash}, runDeletedHashes(rig.stub), "the second rule deletes the torrent that waits under the first")
	require.Len(t, lines, 1)
	requireStartedLine(t, lines[0], rule, 1)
	require.Equal(t, map[string]time.Time{waitOldHash: waitTestNow.Add(-8 * 24 * time.Hour)}, rig.rows(),
		"no row for the deleted, the gone, and the torrent that no longer matches")
}

func TestPendingDeletesKeepTheRowOfADueTorrentThatIsNotDeleted(t *testing.T) {
	const twoDays = 2 * 24 * 3600
	seeded := map[string]time.Time{
		waitFreshHash: waitTestNow.Add(-24 * time.Hour),
		waitOldHash:   waitTestNow.Add(-8 * 24 * time.Hour),
		waitDownHash:  waitTestNow.Add(-8 * 24 * time.Hour),
	}
	for _, tt := range []struct {
		name  string
		setup func(rig *pendingRig)
	}{
		{name: "the delete fails", setup: func(rig *pendingRig) {
			rig.stub.refuse = func(path string, _ url.Values) bool { return path == "torrents/delete" }
		}},
		{name: "the fresh tracker read finds the tracker down", setup: func(rig *pendingRig) {
			rig.health.still = map[string]bool{}
			rig.health.afterRead = qbittorrent.NewTrackerHealthSnapshotWithSince(map[string]qbittorrent.TrackerHealth{
				waitFreshHash: qbittorrent.TrackerHealthUnregistered,
				waitOldHash:   qbittorrent.TrackerHealthDown,
				waitDownHash:  qbittorrent.TrackerHealthDown,
			}, seeded)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rig := newPendingRig(t, waitRunTorrents)
			rule := rig.createRule(waitDeleteRule(0, new(twoDays), unregisteredIs("true")))
			rig.seed(rule.ID, seeded)
			tt.setup(rig)

			require.Empty(t, rig.run(rule), "a held delete writes no line")
			require.Equal(t, seeded, rig.rows())
			require.Empty(t, rig.run(rule), "the next run starts no second wait")
			require.Equal(t, seeded, rig.rows())
		})
	}
}

func TestPendingDeletesEndTheWaitOfATorrentThatAFreshReadOfAnotherRuleFindsRecovered(t *testing.T) {
	rig := newPendingRig(t, waitRunTorrents)
	rig.setHealth(map[string]qbittorrent.TrackerHealth{waitFreshHash: qbittorrent.TrackerHealthUnregistered},
		map[string]time.Time{waitFreshHash: waitTestNow.Add(-3 * 24 * time.Hour)})
	long := rig.createRule(waitDeleteRule(0, new(pendingYear), unregisteredIs("true")))
	short := rig.createRule(waitDeleteRule(0, new(2*24*3600), unregisteredIs("true")))
	rig.seed(long.ID, map[string]time.Time{waitFreshHash: waitTestNow.Add(-3 * 24 * time.Hour)})
	rig.health.still = map[string]bool{}
	rig.health.afterRead = qbittorrent.NewTrackerHealthSnapshotWithSince(map[string]qbittorrent.TrackerHealth{}, map[string]time.Time{})

	lines := rig.run(long, short)
	require.Len(t, lines, 1)
	requireEndedLine(t, lines[0], long, waitEndedCauses{TrackerWorks: 1})
	require.Empty(t, rig.rows())
	require.Empty(t, runDeletedHashes(rig.stub))
}

type changingPendingStore struct {
	*models.PendingDeleteStore
	afterList func()
}

func (s changingPendingStore) ListPendingDeletes(ctx context.Context, instanceID int) ([]models.PendingDelete, error) {
	rows, err := s.PendingDeleteStore.ListPendingDeletes(ctx, instanceID)
	s.afterList()
	return rows, err
}

func TestPendingDeletesWriteNothingWhenTheRuleStopsKeepingThemDuringTheRun(t *testing.T) {
	rig := newPendingRig(t, waitRunTorrents)
	rule := rig.createRule(waitDeleteRule(0, new(pendingYear), unregisteredIs("true")))
	rig.svc.SetPendingDeleteStore(changingPendingStore{PendingDeleteStore: rig.store, afterList: func() {
		saved := *rule
		saved.Enabled = false
		_, err := rig.rules.Update(t.Context(), &saved)
		require.NoError(t, err)
	}})

	require.Empty(t, rig.run(rule))
	require.Empty(t, rig.rows())
}

func TestPendingDeletesClearOldRowsWhenTheInstanceHasNoTorrents(t *testing.T) {
	rig := newPendingRig(t, `{}`)
	rule := rig.createRule(waitDeleteRule(0, new(pendingYear), unregisteredIs("true")))
	rig.seed(rule.ID, map[string]time.Time{waitFreshHash: waitTestNow.Add(-time.Hour)})

	require.Empty(t, rig.run(rule))
	require.Empty(t, rig.rows())
}

func TestPendingDeletesStayWhileTheTimesAreNotLoaded(t *testing.T) {
	rig := newPendingRig(t, waitRunTorrents)
	rule := rig.createRule(waitDeleteRule(0, new(pendingYear), unregisteredIs("true")))
	rig.seed(rule.ID, map[string]time.Time{waitFreshHash: waitTestNow.Add(-time.Hour), waitFineHash: waitTestNow.Add(-time.Hour)})
	rig.health.mu.Lock()
	rig.health.snapshot = nil
	rig.health.mu.Unlock()

	require.Empty(t, rig.run(rule))
	require.Len(t, rig.rows(), 2)
}

func TestPendingDeletesAreNotStoredOutsideALiveRunOfAnEnabledRule(t *testing.T) {
	t.Run("dry run", func(t *testing.T) {
		rig := newPendingRig(t, waitRunTorrents)
		rule := rig.createRule(waitDeleteRule(0, new(pendingYear), unregisteredIs("true")))
		_, err := rig.svc.applyRulesForInstance(t.Context(), rig.instanceID, true, []*models.Automation{rule}, true)
		require.NoError(t, err)
		require.Empty(t, rig.rows())
	})
	t.Run("run now on a disabled rule", func(t *testing.T) {
		rig := newPendingRig(t, waitRunTorrents)
		stored := waitDeleteRule(0, new(pendingYear), unregisteredIs("true"))
		stored.Enabled = false
		rule := rig.createRule(stored)
		activities, err := rig.svc.ApplyRuleNow(t.Context(), rig.instanceID, rule)
		require.NoError(t, err)
		require.Empty(t, waitLines(activities))
		require.Empty(t, rig.rows())
	})
	t.Run("run now on an enabled rule", func(t *testing.T) {
		rig := newPendingRig(t, waitRunTorrents)
		rule := rig.createRule(waitDeleteRule(0, new(pendingYear), unregisteredIs("true")))
		activities, err := rig.svc.ApplyRuleNow(t.Context(), rig.instanceID, rule)
		require.NoError(t, err)
		require.Len(t, waitLines(activities), 1)
		require.Len(t, rig.rows(), 3)
	})
}

func TestPendingDeletesOfARuleGoWhenTheRuleStopsDeleting(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(rule *models.Automation)
		keeps  bool
	}{
		{name: "renamed", change: func(rule *models.Automation) { rule.Name = "renamed" }, keeps: true},
		{name: "disabled", change: func(rule *models.Automation) { rule.Enabled = false }},
		{name: "switched to dry run", change: func(rule *models.Automation) { rule.DryRun = true }},
		{name: "lost its delete action", change: func(rule *models.Automation) {
			rule.Conditions.Delete = nil
			rule.Conditions.Tags = []*models.TagAction{{Enabled: true, Tags: []string{"unreg"}, Mode: "add", Condition: unregisteredIs("true")}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rig := newPendingRig(t, waitRunTorrents)
			rule := rig.createRule(waitDeleteRule(0, new(pendingYear), unregisteredIs("true")))
			rig.run(rule)
			require.Len(t, rig.rows(), 3)

			tt.change(rule)
			_, err := rig.rules.Update(t.Context(), rule)
			require.NoError(t, err)
			if tt.keeps {
				require.Len(t, rig.rows(), 3)
			} else {
				require.Empty(t, rig.rows())
			}
		})
	}
}
