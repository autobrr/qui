// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/automations"
)

const (
	stubTorrentJSON = `{"name":"NAME","hash":"HASH","state":"uploading","added_on":1,"size":100,"total_size":100,"progress":1,` +
		`"save_path":"/data","content_path":"/data/Show.S01","tracker":"TRACKER","magnet_uri":"magnet:?xt=urn:btih:HASH&tr=ESCAPED",` +
		`"has_tracker_error":ERROR,"trackers":TRACKERS}`
)

func stubTorrent(hash, name, tracker, trackers string, trackerError bool) string {
	return strings.NewReplacer("HASH", hash, "NAME", name, "ERROR", strconv.FormatBool(trackerError), "TRACKERS", trackers, "TRACKER", tracker, "ESCAPED", url.QueryEscape(tracker)).Replace(stubTorrentJSON)
}

func stubTrackerRows(tracker string, status qbt.TrackerStatus, msg string) string {
	return `[{"url":"** [DHT] **","status":0},{"url":"` + tracker + `","status":` + strconv.Itoa(int(status)) + `,"msg":"` + msg + `"}]`
}

func (rig *secretsRig) trackerMessage(tracker string) string {
	announce, err := url.Parse(tracker)
	if err != nil {
		panic(err)
	}
	bareKey, _, _ := strings.Cut(strings.Trim(announce.Path, "/"), "/")
	mirror := "https://mirror.example.invalid/t?key=" + rig.plant("tracker message key", shownWithTorrentsWrite)
	return "Unregistered torrent " + bareKey + " see " + mirror
}

func (rig *secretsRig) plantUnregisteredDelete(t *testing.T, db *database.DB, instanceID int) {
	t.Helper()
	ctx := t.Context()
	deps := rig.deps
	deps.SyncManager.SetUnregisteredSinceStore(models.NewUnregisteredSinceStore(db))
	pendingDeletes := models.NewPendingDeleteStore(db)
	deps.AutomationService.SetPendingDeleteStore(pendingDeletes)

	rule, err := deps.AutomationStore.Create(ctx, &models.Automation{
		InstanceID: instanceID, Name: "unregistered", Enabled: true, TrackerPattern: "*",
		Conditions: &models.ActionConditions{SchemaVersion: "1", Delete: &models.DeleteAction{
			Enabled: true, Mode: automations.DeleteModeKeepFiles, UnregisteredWaitSeconds: new(0),
			Condition: &models.RuleCondition{Field: automations.FieldIsUnregistered, Operator: models.OperatorEqual, Value: "true"},
		}},
	})
	require.NoError(t, err)

	_, err = deps.ClientPool.GetClient(ctx, instanceID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return deps.SyncManager.GetTrackerHealthSnapshot(instanceID).Health(stubHashA) == qbittorrent.TrackerHealthUnregistered
	}, 10*time.Second, 10*time.Millisecond)
	activities, err := deps.AutomationService.ApplyRuleNow(ctx, instanceID, rule)
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(activities, func(activity *models.AutomationActivity) bool {
		return activity.Hash == stubHashA && activity.Action == models.ActivityActionDeletedCondition &&
			strings.Contains(string(activity.Details), `"unregistered":`)
	}), "the rule run wrote no delete row with the tracker facts for %s", stubHashA)

	require.NoError(t, pendingDeletes.ApplyPendingDeletes(ctx, instanceID, models.PendingDeleteChange{
		Put: []models.PendingDelete{{RuleID: rule.ID, Hash: stubHashA, UnregisteredSince: time.Now().Add(-time.Hour)}},
	}))
}
