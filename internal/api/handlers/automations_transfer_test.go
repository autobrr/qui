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

	"github.com/autobrr/qui/internal/api/ctxkeys"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestAutomationRuleWithATransferSaves(t *testing.T) {
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
	create := func(body string) *httptest.ResponseRecorder {
		handler := NewAutomationHandler(store, nil, instances, nil, &automations.Service{})
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(source.ID))
		rec := httptest.NewRecorder()
		handler.Create(rec, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
		return rec
	}
	rec := create(rule(action(target.ID, "keepFiles", "bittorrent")))
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
		{"unknown after and way", action(target.ID, "later", "rsync"),
			`conditions.transferToInstance.after: after does not allow "later"; it allows removeWithFiles, keepFiles, keepSeeding` + "\n" +
				`conditions.transferToInstance.way: way does not allow "rsync"; it allows auto, copy, bittorrent`},
		{"missing target", action(target.ID+100, "", ""), "Target instance not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := create(rule(tc.action))
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			var body ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, tc.want, body.Error)
		})
	}
}

func TestATransferRuleNeedsTheCrossSeedInstanceOfItsTargetInTheKeyLimit(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "automation-transfer-route")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	add := func(name, url string) int {
		inst, err := instances.Create(t.Context(), name, url, "", "", nil, nil, false, nil)
		require.NoError(t, err)
		return inst.ID
	}
	source, target, crossSeed := add("seedbox", "http://example.invalid:8080"), add("homelab", "http://example.invalid:8081"), add("archive", "http://example.invalid:8082")
	routes := models.NewCrossSeedRouteStore(db)
	require.NoError(t, routes.Upsert(t.Context(), &models.CrossSeedRoute{SourceInstanceID: target, CrossSeedInstanceID: crossSeed}))
	store := models.NewAutomationStore(db)
	handler := NewAutomationHandler(store, nil, instances, nil, &automations.Service{})
	handler.SetCrossSeedRoutes(crossseedroute.NewService(routes, instances, nil, nil))
	serve := func(serve http.HandlerFunc, ruleID string, key *models.APIKey, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", strconv.Itoa(source))
		rctx.URLParams.Add("ruleID", ruleID)
		ctx := context.WithValue(context.WithValue(req.Context(), chi.RouteCtxKey, rctx), ctxkeys.APIKey, key)
		rec := httptest.NewRecorder()
		serve(rec, req.WithContext(ctx))
		return rec
	}
	rule := fmt.Sprintf(`{"name":"move old seeds","trackerPattern":"*","conditions":{"schemaVersion":"1",`+
		`"transferToInstance":{"enabled":true,"targetInstanceId":%d}}}`, target)
	limited := &models.APIKey{Permissions: models.AllPermissions(), InstanceIDs: []int{source, target}}
	refused := `{"error":"API key cannot reach instance ` + strconv.Itoa(crossSeed) + `"}`

	rec := serve(handler.Create, "", limited, rule)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.JSONEq(t, refused, rec.Body.String())

	stored, err := store.Create(t.Context(), &models.Automation{
		InstanceID: source, Name: "saved Transfer", TrackerPattern: "*", Enabled: true,
		Conditions: &models.ActionConditions{SchemaVersion: "1", TransferToInstance: &models.TransferToInstanceAction{Enabled: true, TargetInstanceID: target}},
	})
	require.NoError(t, err)
	rec = serve(handler.ApplyRuleNow, strconv.Itoa(stored.ID), limited, "{}")
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.JSONEq(t, refused, rec.Body.String())

	limited.InstanceIDs = append(limited.InstanceIDs, crossSeed)
	rec = serve(handler.Create, "", limited, rule)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}
