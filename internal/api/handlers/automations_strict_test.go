// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestAutomationStrictDecode(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "automation-strict-decode")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(t.Context(), "test", "http://example.invalid", "", "", nil, nil, false, nil)
	require.NoError(t, err)
	store := models.NewAutomationStore(db)
	handler := NewAutomationHandler(store, nil, instances, nil, &automations.Service{})
	instanceID := strconv.Itoa(instance.ID)

	existing, err := store.Create(t.Context(), &models.Automation{
		InstanceID:     instance.ID,
		Name:           "existing",
		TrackerPattern: "*",
		Conditions:     &models.ActionConditions{SchemaVersion: "1", Pause: &models.PauseAction{Enabled: true}},
	})
	require.NoError(t, err)

	serveRule := func(fn http.HandlerFunc, ruleID int, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", instanceID)
		rctx.URLParams.Add("ruleID", strconv.Itoa(ruleID))
		rec := httptest.NewRecorder()
		fn(rec, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
		return rec
	}
	serve := func(fn http.HandlerFunc, body string) *httptest.ResponseRecorder {
		return serveRule(fn, existing.ID, body)
	}
	endpoints := []struct {
		name   string
		fn     http.HandlerFunc
		status int
	}{
		{name: "create", fn: handler.Create, status: http.StatusCreated},
		{name: "update", fn: handler.Update, status: http.StatusOK},
		{name: "dry run", fn: handler.DryRunNow, status: http.StatusAccepted},
	}
	errorLines := func(t *testing.T, rec *httptest.ResponseRecorder) []string {
		t.Helper()
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		var body ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		return strings.Split(body.Error, "\n")
	}

	valid := []struct {
		name string
		body string
	}{
		{
			name: "server-set keys",
			body: `{"id":99,"instanceId":42,"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
				"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true}}}`,
		},
		{
			name: "legacy trackerDomains and conditions.tag",
			body: `{"name":"rule","trackerDomains":["a.example"],
				"conditions":{"schemaVersion":"1","tag":{"enabled":true,"tags":["x"],"mode":"add"}}}`,
		},
		{
			name: "legacy TRACKERS field",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true,
				"condition":{"field":"TRACKERS","operator":"CONTAINS","value":"example"}}}}`,
		},
		{
			name: "content type with any case, a non-equal operator, regex, or no value",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true,
				"condition":{"operator":"OR","conditions":[
					{"field":"CONTENT_TYPE","operator":"EQUAL","value":"TV"},
					{"field":"CONTENT_TYPE","operator":"CONTAINS","value":"mov"},
					{"field":"CONTENT_TYPE","operator":"EQUAL","value":"mov.*","regex":true},
					{"field":"STATE","operator":"EQUAL","value":""}]}}}}`,
		},
		{
			name: "contentLayout values",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true},
				"exportToInstance":{"enabled":false,"targetInstanceId":0,"savePath":"","contentLayout":"NoSubfolder"}}}`,
		},
		{
			name: "query builder clientId",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true,
				"condition":{"clientId":"c_1","operator":"AND","conditions":[{"clientId":"c_2","field":"NAME","operator":"CONTAINS","value":"x"}]}}}}`,
		},
	}
	invalid := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "unknown top-level key",
			body: `{"name":"rule","trackerPattern":"*","enabeld":true,"conditions":{"schemaVersion":"1","pause":{"enabled":true}}}`,
			want: []string{"enabeld: unknown key"},
		},
		{
			name: "unknown key under conditions",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pasue":{"enabled":true},"pause":{"enabled":true}}}`,
			want: []string{"conditions.pasue: unknown key"},
		},
		{
			name: "unknown key in an action",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","tags":[{"enabled":true,"tags":["x"],"mode":"add","tag":"y"}]}}`,
			want: []string{"conditions.tags[0].tag: unknown key"},
		},
		{
			name: "unknown key in sortingConfig",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true}},
				"sortingConfig":{"schemaVersion":"1","type":"simple","direction":"ASC","field":"SIZE","order":"x"}}`,
			want: []string{"sortingConfig.order: unknown key"},
		},
		{
			name: "unknown key in freeSpaceSource",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true}},
				"freeSpaceSource":{"type":"path","dir":"/data"}}`,
			want: []string{"freeSpaceSource.dir: unknown key"},
		},
		{
			name: "wrong type",
			body: `{"name":"rule","trackerPattern":"*","intervalSeconds":"900","conditions":{"schemaVersion":"1","pause":{"enabled":true}}}`,
			want: []string{"intervalSeconds: expected a whole number, got a string"},
		},
		{
			name: "unknown field",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true,
				"condition":{"field":"NAEM","operator":"CONTAINS","value":"x"}}}}`,
			want: []string{`conditions.pause.condition.field: unknown field "NAEM"`},
		},
		{
			name: "unknown operator",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true,
				"condition":{"field":"PRIVATE","operator":"IS","value":"true"}}}}`,
			want: []string{`conditions.pause.condition.operator: unknown operator "IS"; PRIVATE allows EQUAL, NOT_EQUAL`},
		},
		{
			name: "operator that does not fit the field",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true,
				"condition":{"field":"PRIVATE","operator":"GREATER_THAN","value":"true"}}}}`,
			want: []string{"conditions.pause.condition.operator: PRIVATE does not allow GREATER_THAN; it allows EQUAL, NOT_EQUAL"},
		},
		{
			name: "unknown enum values",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true,
				"condition":{"operator":"AND","conditions":[
					{"field":"STATE","operator":"EQUAL","value":"seeding"},
					{"field":"TRACKER_STATUS","operator":"NOT_EQUAL","value":"ok"},
					{"field":"CONTENT_TYPE","operator":"EQUAL","value":"movies"},
					{"field":"PRIVATE","operator":"EQUAL","value":"yes"}]}}}}`,
			want: []string{
				`conditions.pause.condition.conditions[0].value: STATE does not allow "seeding"; it allows downloading, uploading, completed, stopped, active, inactive, running, stalled, stalled_uploading, stalled_downloading, errored, tracker_down, tracker_error, checking, checkingResumeData, moving, missingFiles`,
				`conditions.pause.condition.conditions[1].value: TRACKER_STATUS does not allow "ok"; it allows not_contacted, working, updating, error, tracker_error, unreachable`,
				`conditions.pause.condition.conditions[2].value: CONTENT_TYPE does not allow "movies"; it allows movie, tv, music, audiobook, book, comic, game, app, adult, unknown`,
				`conditions.pause.condition.conditions[3].value: PRIVATE does not allow "yes"; it allows true, false`,
			},
		},
		{
			name: "regex on a field that matches without regex",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true,
				"condition":{"field":"PRIVATE","operator":"EQUAL","value":"yes","regex":true}}}}`,
			want: []string{`conditions.pause.condition.value: PRIVATE does not allow "yes"; it allows true, false`},
		},
		{
			name: "yes/no field without a value",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true,
				"condition":{"field":"PRIVATE","operator":"EQUAL"}}}}`,
			want: []string{`conditions.pause.condition.value: PRIVATE does not allow ""; it allows true, false`},
		},
		{
			name: "logical operator without child conditions",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true,
				"condition":{"field":"NAEM","operator":"AND"}}}}`,
			want: []string{`conditions.pause.condition.field: unknown field "NAEM"`},
		},
		{
			name: "unknown contentLayout",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true},
				"exportToInstance":{"enabled":false,"targetInstanceId":0,"savePath":"","contentLayout":"Create subfolder"}}}`,
			want: []string{`conditions.exportToInstance.contentLayout: contentLayout does not allow "Create subfolder"; it allows Original, Subfolder, NoSubfolder`},
		},
		{
			name: "condition in a score rule",
			body: `{"name":"rule","trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true}},
				"sortingConfig":{"schemaVersion":"1","type":"score","direction":"DESC","scoreRules":[
					{"type":"conditional","conditional":{"score":1,"condition":{"field":"RATIO","operator":"CONTAINS","value":"1"}}}]}}`,
			want: []string{"sortingConfig.scoreRules[0].conditional.condition.operator: RATIO does not allow CONTAINS; it allows EQUAL, NOT_EQUAL, GREATER_THAN, GREATER_THAN_OR_EQUAL, LESS_THAN, LESS_THAN_OR_EQUAL, BETWEEN"},
		},
		{
			name: "several problems",
			body: `{"name":5,"trackerPattern":"*","extra":1,"conditions":{"schemaVersion":"1","pause":{"enabled":"yes","condition":{"operator":"AND","conditions":[{"field":"NAME","operator":"EQUAL","valeu":"x"}]}}}}`,
			want: []string{
				"conditions.pause.condition.conditions[0].valeu: unknown key",
				"conditions.pause.enabled: expected true or false, got a string",
				"extra: unknown key",
				"name: expected a string, got a number",
			},
		},
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, tt := range valid {
				t.Run(tt.name, func(t *testing.T) {
					rec := serve(endpoint.fn, tt.body)
					require.Equal(t, endpoint.status, rec.Code, rec.Body.String())
				})
			}
			for _, tt := range invalid {
				t.Run(tt.name, func(t *testing.T) {
					require.Equal(t, tt.want, errorLines(t, serve(endpoint.fn, tt.body)))
				})
			}
		})
	}

	t.Run("legacy keys still apply", func(t *testing.T) {
		rec := serve(handler.Create, valid[1].body)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var rule models.Automation
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rule))
		require.Equal(t, "a.example", rule.TrackerPattern)
		require.Len(t, rule.Conditions.TagActions(), 1)
		require.Equal(t, []string{"x"}, rule.Conditions.TagActions()[0].Tags)
	})

	t.Run("on/off switch sends the stored rule", func(t *testing.T) {
		stored, err := json.Marshal(existing)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(stored, &body))
		body["enabled"] = false
		toggled, err := json.Marshal(body)
		require.NoError(t, err)

		rec := serve(handler.Update, string(toggled))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var rule models.Automation
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rule))
		require.False(t, rule.Enabled)
		require.Equal(t, existing.ID, rule.ID)
		require.Equal(t, instance.ID, rule.InstanceID)
	})

	t.Run("update skips the field check when the conditions did not change", func(t *testing.T) {
		// A rule saved before the field check, with an operator that never matches.
		bad, err := store.Create(t.Context(), &models.Automation{
			InstanceID:     instance.ID,
			Name:           "bad operator",
			TrackerPattern: "*",
			Conditions: &models.ActionConditions{SchemaVersion: "1", Pause: &models.PauseAction{
				Enabled:   true,
				Condition: &models.RuleCondition{Field: models.FieldPrivate, Operator: models.OperatorGreaterThan, Value: "true"},
			}},
		})
		require.NoError(t, err)
		stored, err := json.Marshal(bad)
		require.NoError(t, err)
		edit := func(change func(body map[string]any)) string {
			var body map[string]any
			require.NoError(t, json.Unmarshal(stored, &body))
			change(body)
			out, err := json.Marshal(body)
			require.NoError(t, err)
			return string(out)
		}

		rec := serveRule(handler.Update, bad.ID, edit(func(body map[string]any) {
			body["enabled"] = false
			body["name"] = "renamed"
		}))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		rec = serveRule(handler.Update, bad.ID, edit(func(body map[string]any) {
			condition := body["conditions"].(map[string]any)["pause"].(map[string]any)["condition"].(map[string]any)
			condition["value"] = "false"
		}))
		require.Equal(t, []string{"conditions.pause.condition.operator: PRIVATE does not allow GREATER_THAN; it allows EQUAL, NOT_EQUAL"}, errorLines(t, rec))

		// The sorting changes and the conditions stay the same, so only the sorting is checked.
		rec = serveRule(handler.Update, bad.ID, edit(func(body map[string]any) {
			body["sortingConfig"] = map[string]any{"schemaVersion": "1", "type": "simple", "direction": "ASC", "field": "SIZE"}
		}))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		rec = serveRule(handler.Update, bad.ID, edit(func(body map[string]any) {
			body["sortingConfig"] = map[string]any{"schemaVersion": "1", "type": "score", "direction": "DESC", "scoreRules": []any{
				map[string]any{"type": "conditional", "conditional": map[string]any{"score": 1,
					"condition": map[string]any{"field": "RATIO", "operator": "CONTAINS", "value": "1"}}},
			}}
		}))
		require.Equal(t, []string{
			"sortingConfig.scoreRules[0].conditional.condition.operator: RATIO does not allow CONTAINS; it allows EQUAL, NOT_EQUAL, GREATER_THAN, GREATER_THAN_OR_EQUAL, LESS_THAN, LESS_THAN_OR_EQUAL, BETWEEN",
		}, errorLines(t, rec))
	})

	t.Run("rule check runs after decode", func(t *testing.T) {
		require.Equal(t, []string{"Name is required"}, errorLines(t, serve(handler.Create,
			`{"trackerPattern":"*","conditions":{"schemaVersion":"1","pause":{"enabled":true}}}`)))
	})

	t.Run("unknown instance", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(valid[0].body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", "999")
		rec := httptest.NewRecorder()
		handler.Create(rec, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})
}
