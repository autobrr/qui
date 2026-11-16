// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/rollout"
	"github.com/autobrr/qui/internal/services/automations"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// The automations API saves a rule with a Transfer to Instance action only
// while the switch for Transfers and Shares is on, and then keeps each field
// of the action.
func TestAutomationRuleWithATransferSavesOnlyWithTheSwitch(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "automation-transfer")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	source, err := instances.Create(t.Context(), "seedbox", "http://example.invalid", "", "", nil, nil, false, nil)
	require.NoError(t, err)
	target, err := instances.Create(t.Context(), "homelab", "http://example.invalid:8081", "", "", nil, nil, false, nil)
	require.NoError(t, err)
	store := models.NewAutomationStore(db)

	rule := func(action string) string {
		return `{"name":"move old seeds","trackerPattern":"*","conditions":{"schemaVersion":"1","transferToInstance":` + action + `}}`
	}
	action := func(targetID int, after, way string) string {
		return fmt.Sprintf(`{"enabled":true,"targetInstanceId":%d,"savePath":"/data/{{ .Category }}","category":"archive",`+
			`"tags":["moved"],"after":%q,"leaveStopped":true,"way":%q,`+
			`"condition":{"field":"SEEDING_TIME","operator":"GREATER_THAN","value":"7776000"}}`, targetID, after, way)
	}
	create := func(switches rollout.Switches, body string) *httptest.ResponseRecorder {
		handler := NewAutomationHandler(store, nil, instances, nil, &automations.Service{}, switches)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(source.ID))
		rec := httptest.NewRecorder()
		handler.Create(rec, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
		return rec
	}
	on := rollout.Switches{TransfersAndShares: true}

	rec := create(rollout.Production(), rule(action(target.ID, "keepFiles", "bittorrent")))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "Transfer to instance is not available")

	rec = create(on, rule(action(target.ID, "keepFiles", "bittorrent")))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created models.Automation
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	stored, err := store.Get(t.Context(), source.ID, created.ID)
	require.NoError(t, err)
	require.Equal(t, &models.TransferToInstanceAction{
		Enabled:          true,
		TargetInstanceID: target.ID,
		SavePath:         "/data/{{ .Category }}",
		Category:         "archive",
		Tags:             []string{"moved"},
		After:            "keepFiles",
		LeaveStopped:     true,
		Way:              "bittorrent",
		Condition:        &models.RuleCondition{Field: models.FieldSeedingTime, Operator: models.OperatorGreaterThan, Value: "7776000"},
	}, stored.Conditions.TransferToInstance)

	for _, tc := range []struct {
		name   string
		action string
		want   string
	}{
		{"unknown after", action(target.ID, "later", "auto"),
			`conditions.transferToInstance.after: after does not allow "later"; it allows removeWithFiles, keepFiles, keepSeeding`},
		{"unknown way", action(target.ID, "", "rsync"),
			`conditions.transferToInstance.way: way does not allow "rsync"; it allows auto, copy, bittorrent`},
		{"missing target", action(target.ID+100, "", ""), "Target instance not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := create(on, rule(tc.action))
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			var body ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, tc.want, body.Error)
		})
	}
}
