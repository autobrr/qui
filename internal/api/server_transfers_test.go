// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"encoding/json/v2"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/transfer"
)

func TestTransferStartRoute(t *testing.T) {
	rig := newPermissionRig(t)
	cookie := rig.sessionCookie(t)

	status, _ := rig.sendBody(t, http.MethodPost, "/api/transfers", "", cookie, `{"sourceInstanceId":1,"targetInstanceId":2}`)
	require.Equal(t, http.StatusBadRequest, status, "a body without the torrent")

	t.Run("a refusal answers 409 with its reason", func(t *testing.T) {
		// No way and each way reach the checks.
		for _, way := range []string{"", "auto", "copy", "bittorrent"} {
			status, body := rig.sendBody(t, http.MethodPost, "/api/transfers", "", cookie,
				`{"sourceInstanceId":1,"hash":"aaaa","targetInstanceId":2,"way":"`+way+`"}`)
			require.Equal(t, http.StatusConflict, status, way)
			require.Contains(t, body, "Instance 1 does not exist.", way)
		}
	})

	t.Run("an unknown way answers 400", func(t *testing.T) {
		status, body := rig.sendBody(t, http.MethodPost, "/api/transfers", "", cookie,
			`{"sourceInstanceId":1,"hash":"aaaa","targetInstanceId":2,"way":"sneakernet"}`)
		require.Equal(t, http.StatusBadRequest, status)
		require.Contains(t, body, "way must be auto, copy, or bittorrent")
	})
}

// addInstances adds the instances 1 and 2 that the Transfers of a test name.
func addInstances(t *testing.T, rig *permissionRig) {
	t.Helper()
	for _, name := range []string{"seedbox", "homelab"} {
		_, err := rig.deps.InstanceStore.Create(t.Context(), name, "http://127.0.0.1:1", "admin", "secret", nil, nil, false, nil)
		require.NoError(t, err)
	}
}

// addTransfer stores a Transfer of hash in state. A finished one has ended.
func addTransfer(t *testing.T, rig *permissionRig, hash string, state transfer.State, finished bool, edit func(*transfer.Record)) int64 {
	t.Helper()
	record := &transfer.Record{
		Kind: transfer.KindTransfer, Way: transfer.WaySFTP, State: transfer.StateWaiting, StartedBy: transfer.StartedByUser,
		SourceInstanceID: 1, TargetInstanceID: 2,
		Options: transfer.Options{After: transfer.AfterRemoveWithFiles},
		Steps:   []transfer.Step{{Name: "start"}},
		Members: []transfer.Member{{Hash: hash, Name: "Show.S01.1080p-GRP", SourceSavePath: "/data",
			TargetInstanceID: 2, Placement: transfer.Placement{SavePath: "/srv/tv", Category: "tv", Tags: []string{"moved"}}}},
	}
	if edit != nil {
		edit(record)
	}
	store := rig.deps.Transfers
	require.NoError(t, store.Create(t.Context(), record))
	record.State = state
	require.NoError(t, store.Save(t.Context(), record))
	if finished {
		require.NoError(t, store.Finish(t.Context(), record.ID, state))
	}
	return record.ID
}

func TestTransferListAndDetailRoutes(t *testing.T) {
	rig := newPermissionRig(t)
	cookie := rig.sessionCookie(t)
	addInstances(t, rig)
	rule, err := rig.deps.AutomationStore.Create(t.Context(), &models.Automation{InstanceID: 1, Name: "Move old seeds",
		TrackerPattern: "*", Conditions: &models.ActionConditions{SchemaVersion: "1", Pause: &models.PauseAction{Enabled: true}}})
	require.NoError(t, err)

	done := addTransfer(t, rig, "aaaa", transfer.StateDone, true, nil)
	failed := addTransfer(t, rig, "bbbb", transfer.StateFailed, true, func(r *transfer.Record) {
		r.StartedBy, r.RuleID = transfer.StartedByRule, &rule.ID
		r.Way = transfer.WayBitTorrent
		r.FailureClass, r.FailureReason = transfer.FailurePermanent, "A file on the target does not match."
		r.Log = []transfer.LogLine{{Message: "Passing 70 B by a Transfer torrent from seedbox to homelab."},
			{Message: "homelab uses TCP and μTP.", Hint: &transfer.Hint{Kind: transfer.HintTCPAndUTP, InstanceID: 2}}}
	})

	status, body := rig.sendBody(t, http.MethodGet, "/api/transfers?filter=failed", "", cookie, "")
	require.Equal(t, http.StatusOK, status, body)
	var list struct {
		Rows   []map[string]any `json:"rows"`
		Counts map[string]any   `json:"counts"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &list))
	require.Equal(t, map[string]any{"active": float64(0), "waiting": float64(0), "finished": float64(1), "failed": float64(1)}, list.Counts)
	require.Len(t, list.Rows, 1)
	row := list.Rows[0]
	require.EqualValues(t, failed, row["id"])
	require.Equal(t, "transfer", row["kind"])
	require.Equal(t, "bittorrent", row["way"])
	require.Equal(t, "failed", row["state"])
	require.Equal(t, "rule", row["startedBy"])
	require.EqualValues(t, rule.ID, row["ruleId"])
	require.Equal(t, "Move old seeds", row["ruleName"], "a row names the rule that started it")
	require.Equal(t, "bbbb", row["hash"])
	require.Equal(t, "Show.S01.1080p-GRP", row["name"])
	require.EqualValues(t, 1, row["torrents"])
	require.Equal(t, "permanent", row["failureClass"])
	require.Equal(t, "A file on the target does not match.", row["failureReason"])
	require.Nil(t, row["retryAt"])
	require.NotNil(t, row["finishedAt"])
	require.Nil(t, row["progress"], "a Transfer that does not run has no progress")
	require.Equal(t, []any{map[string]any{"kind": "tcpAndUtp", "instanceId": float64(2)}}, row["hints"])

	status, body = rig.sendBody(t, http.MethodGet, "/api/transfers", "", cookie, "")
	require.Equal(t, http.StatusOK, status, body)
	require.NoError(t, json.Unmarshal([]byte(body), &list))
	require.Len(t, list.Rows, 2)
	require.EqualValues(t, done, list.Rows[1]["id"])
	require.Equal(t, "user", list.Rows[1]["startedBy"])
	require.Nil(t, list.Rows[1]["ruleId"])
	require.Equal(t, []any{}, list.Rows[1]["hints"])

	status, _ = rig.sendBody(t, http.MethodGet, "/api/transfers?filter=sideways", "", cookie, "")
	require.Equal(t, http.StatusBadRequest, status)

	status, body = rig.sendBody(t, http.MethodGet, "/api/transfers/"+strconv.FormatInt(failed, 10), "", cookie, "")
	require.Equal(t, http.StatusOK, status, body)
	var detail map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &detail))
	require.Equal(t, "Move old seeds", detail["ruleName"])
	require.Equal(t, "start", detail["steps"].([]any)[0].(map[string]any)["name"])
	log := detail["log"].([]any)
	require.Len(t, log, 2)
	require.Equal(t, "homelab uses TCP and μTP.", log[1].(map[string]any)["message"])
	require.Equal(t, []any{map[string]any{"hash": "bbbb", "name": "Show.S01.1080p-GRP", "targetInstanceId": float64(2),
		"savePath": "/srv/tv", "category": "tv", "tags": []any{"moved"}}}, detail["members"])

	status, _ = rig.sendBody(t, http.MethodGet, "/api/transfers/999", "", cookie, "")
	require.Equal(t, http.StatusNotFound, status)
}

func TestTransferActionRoutes(t *testing.T) {
	rig := newPermissionRig(t)
	cookie := rig.sessionCookie(t)
	addInstances(t, rig)
	done := strconv.FormatInt(addTransfer(t, rig, "aaaa", transfer.StateDone, true, nil), 10)
	running := strconv.FormatInt(addTransfer(t, rig, "bbbb", transfer.StateCopying, false, nil), 10)

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/api/transfers/" + done + "/retry", http.StatusConflict},
		{http.MethodDelete, "/api/transfers/" + running, http.StatusConflict},
		{http.MethodPost, "/api/transfers/999/retry", http.StatusNotFound},
		{http.MethodPost, "/api/transfers/999/cancel", http.StatusNotFound},
		{http.MethodDelete, "/api/transfers/999", http.StatusNotFound},
		{http.MethodPost, "/api/transfers/x/retry", http.StatusBadRequest},
		{http.MethodDelete, "/api/transfers/" + done, http.StatusNoContent},
	} {
		status, body := rig.sendBody(t, tc.method, tc.path, "", cookie, "")
		require.Equal(t, tc.want, status, "%s %s: %s", tc.method, tc.path, body)
	}
	_, err := rig.deps.Transfers.Get(t.Context(), 1)
	require.ErrorIs(t, err, transfer.ErrNotFound, "the finished row is gone")
}

// The bulk route acts on the rows of ids, or on every row behind a chip. It
// counts the rows that the action does not fit as skipped.
func TestTransferBulkRoute(t *testing.T) {
	rig := newPermissionRig(t)
	cookie := rig.sessionCookie(t)
	addInstances(t, rig)
	first := addTransfer(t, rig, "aaaa", transfer.StateDone, true, nil)
	second := addTransfer(t, rig, "bbbb", transfer.StateCancelled, true, nil)
	running := addTransfer(t, rig, "cccc", transfer.StateCopying, false, nil)
	// A Transfer to its own source: a retry plans it again, and qui refuses it.
	refused := addTransfer(t, rig, "dddd", transfer.StateFailed, true, func(r *transfer.Record) {
		r.TargetInstanceID, r.Members[0].TargetInstanceID = 1, 1
	})
	type answer struct {
		Done    int      `json:"done"`
		Skipped int      `json:"skipped"`
		Failed  int      `json:"failed"`
		Reasons []string `json:"reasons"`
	}
	bulk := func(body string) (int, answer) {
		t.Helper()
		status, text := rig.sendBody(t, http.MethodPost, "/api/transfers/bulk", "", cookie, body)
		var res answer
		if status == http.StatusOK {
			require.NoError(t, json.Unmarshal([]byte(text), &res))
		}
		return status, res
	}

	status, res := bulk(`{"action":"remove","ids":[` + strconv.FormatInt(first, 10) + `,` + strconv.FormatInt(running, 10) + `,999]}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, answer{Done: 1, Skipped: 2, Reasons: []string{}}, res, "a running row and an unknown row are skipped")

	status, res = bulk(`{"action":"retry","ids":[` + strconv.FormatInt(refused, 10) + `,` + strconv.FormatInt(running, 10) + `]}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, answer{Skipped: 1, Failed: 1, Reasons: []string{"The Transfer source and the Transfer target are the same instance."}}, res,
		"a refusal fails with its reason, and a running row is skipped")

	status, res = bulk(`{"action":"remove","filter":"finished"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, answer{Done: 1, Reasons: []string{}}, res, "the filter picks the finished row that is left")
	_, err := rig.deps.Transfers.Get(t.Context(), second)
	require.ErrorIs(t, err, transfer.ErrNotFound)
	_, err = rig.deps.Transfers.Get(t.Context(), running)
	require.NoError(t, err, "the filter leaves the running row")

	for _, body := range []string{`{"action":"pause","ids":[1]}`, `{"action":"remove","filter":"sideways"}`, `{"action":"retry","filter":"failed"}`, `{`} {
		status, _ := bulk(body)
		require.Equal(t, http.StatusBadRequest, status, body)
	}
}

func TestTransferSummaryRoute(t *testing.T) {
	rig := newPermissionRig(t)
	cookie := rig.sessionCookie(t)

	status, _ := rig.sendBody(t, http.MethodPost, "/api/transfers/summary", "", cookie, `{"sourceInstanceId":1}`)
	require.Equal(t, http.StatusBadRequest, status)

	status, body := rig.sendBody(t, http.MethodPost, "/api/transfers/summary", "", cookie,
		`{"sourceInstanceId":1,"hash":"aaaa","targetInstanceId":1,"way":"auto"}`)
	require.Equal(t, http.StatusConflict, status, "a refusal answers 409 with its reason")
	require.JSONEq(t, `{"error":"The Transfer source and the Transfer target are the same instance."}`, body)
}
