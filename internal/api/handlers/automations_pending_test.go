// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestListPendingDeletesGroupsTheWaitingTorrentsByRule(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "automation-pending-deletes")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(t.Context(), "test", "http://example.invalid", "", "", nil, nil, false, nil)
	require.NoError(t, err)
	rules := models.NewAutomationStore(db)
	unregistered := &models.RuleCondition{Field: automations.FieldIsUnregistered, Operator: models.OperatorEqual, Value: "true"}
	rule, err := rules.Create(t.Context(), &models.Automation{
		InstanceID: instance.ID, Name: "Clean unregistered", TrackerPattern: "*", Enabled: true,
		Conditions: &models.ActionConditions{SchemaVersion: "1", Delete: &models.DeleteAction{Enabled: true, Mode: automations.DeleteModeKeepFiles, Condition: unregistered}},
	})
	require.NoError(t, err)
	pending := models.NewPendingDeleteStore(db)
	fresh := time.Now().Add(-time.Hour).Truncate(time.Second).UTC()
	old := time.Now().Add(-3 * 24 * time.Hour).Truncate(time.Second).UTC()
	require.NoError(t, pending.ApplyPendingDeletes(t.Context(), instance.ID, models.PendingDeleteChange{Put: []models.PendingDelete{
		{RuleID: rule.ID, Hash: "1111111111111111111111111111111111111111", UnregisteredSince: fresh},
		{RuleID: rule.ID, Hash: "2222222222222222222222222222222222222222", UnregisteredSince: old},
	}}))
	service := automations.NewService(automations.DefaultConfig(), instances, rules, nil, nil, nil, nil, nil, nil, nil)
	service.SetPendingDeleteStore(pending)

	serve := func(handler *AutomationHandler, instanceID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", instanceID)
		rec := httptest.NewRecorder()
		handler.ListPendingDeletes(rec, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
		return rec
	}

	t.Run("lists the stored torrents of each rule, the first delete first", func(t *testing.T) {
		rec := serve(NewAutomationHandler(rules, nil, instances, nil, service), strconv.Itoa(instance.ID))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var list automations.PendingDeleteList
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
		wait := int64(models.DefaultUnregisteredWait / time.Second)
		require.Equal(t, automations.PendingDeleteList{Rules: []automations.PendingDeleteGroup{{
			RuleID: rule.ID, RuleName: "Clean unregistered", Torrents: []automations.PendingDeleteTorrent{
				{Hash: "2222222222222222222222222222222222222222", UnregisteredSince: old.Unix(), DeletesAt: old.Unix() + wait, AwaitsTracker: true},
				{Hash: "1111111111111111111111111111111111111111", UnregisteredSince: fresh.Unix(), DeletesAt: fresh.Unix() + wait},
			},
		}}}, list)
	})

	t.Run("answers 503 without the automations service", func(t *testing.T) {
		rec := serve(NewAutomationHandler(rules, nil, instances, nil, nil), strconv.Itoa(instance.ID))

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})
}
