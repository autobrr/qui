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

func TestAutomationDecodePayload_Category(t *testing.T) {
	for _, category := range []string{"", "archive"} {
		t.Run("target="+category, func(t *testing.T) {
			body := `{"name":"Category rule","trackerPattern":"*","conditions":{"category":{"enabled":true,"category":"` + category + `"}}}`

			payload, err := decodeAutomationPayload(strings.NewReader(body), 1, nil, nil)
			require.NoError(t, err)
			require.Equal(t, category, payload.Conditions.Category.Category)
		})
	}
}

func TestAutomationDryRunNow(t *testing.T) {
	newRequest := func(body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/instances/1/automations/dry-run", strings.NewReader(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", "1")
		return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	validPayload := `{
		"name":"Dry run test",
		"trackerPattern":"*",
		"conditions":{"schemaVersion":"1","pause":{"enabled":true}}
	}`

	t.Run("returns 503 when service is unavailable", func(t *testing.T) {
		handler := NewAutomationHandler(nil, nil, nil, nil, nil)
		rec := httptest.NewRecorder()

		handler.DryRunNow(rec, newRequest(validPayload))

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("returns 400 on invalid JSON payload", func(t *testing.T) {
		handler := NewAutomationHandler(nil, nil, nil, nil, &automations.Service{})
		rec := httptest.NewRecorder()

		handler.DryRunNow(rec, newRequest("{"))

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("runs dry-run and returns accepted status", func(t *testing.T) {
		handler := NewAutomationHandler(nil, nil, nil, nil, &automations.Service{})
		rec := httptest.NewRecorder()

		handler.DryRunNow(rec, newRequest(validPayload))

		require.Equal(t, http.StatusAccepted, rec.Code)
		var response AutomationDryRunResult
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Equal(t, "dry-run-completed", response.Status)
	})
}

func TestAutomationTrackerPattern(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "automation-tracker-pattern")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(t.Context(), "test", "http://example.invalid", "", "", nil, nil, false, nil)
	require.NoError(t, err)
	handler := NewAutomationHandler(models.NewAutomationStore(db), nil, nil, nil, nil)
	instanceID := strconv.Itoa(instance.ID)

	serve := func(method, ruleID, body string, fn http.HandlerFunc) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/", strings.NewReader(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("instanceID", instanceID)
		if ruleID != "" {
			rctx.URLParams.Add("ruleID", ruleID)
		}
		rec := httptest.NewRecorder()
		fn(rec, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
		return rec
	}
	payload := func(trackerFields string) string {
		return `{"name":"rule",` + trackerFields + `,"conditions":{"schemaVersion":"1","pause":{"enabled":true}}}`
	}
	decode := func(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.NotContains(t, body, "trackerDomains")
		return body
	}

	tests := []struct {
		name          string
		trackerFields string
		want          string
	}{
		{name: "pattern only", trackerFields: `"trackerPattern":"a.example,b.example"`, want: "a.example,b.example"},
		{name: "domains only", trackerFields: `"trackerDomains":[" a.example ","","b.example"]`, want: "a.example,b.example"},
		{name: "pattern wins over domains", trackerFields: `"trackerPattern":"a.example","trackerDomains":["b.example"]`, want: "a.example"},
		{name: "all trackers", trackerFields: `"trackerPattern":"*"`, want: "*"},
		{name: "exclude tokens", trackerFields: `"trackerPattern":"!a.example,!b.example"`, want: "!a.example,!b.example"},
		{name: "mixed pattern", trackerFields: `"trackerPattern":"a.example,!b.example,*.c.example"`, want: "a.example,!b.example,*.c.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created := serve(http.MethodPost, "", payload(tt.trackerFields), handler.Create)
			require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
			createdRule := decode(t, created)
			require.Equal(t, tt.want, createdRule["trackerPattern"])

			ruleID := strconv.Itoa(int(createdRule["id"].(float64)))
			updated := serve(http.MethodPut, ruleID, payload(tt.trackerFields), handler.Update)
			require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
			require.Equal(t, tt.want, decode(t, updated)["trackerPattern"])

			listed := serve(http.MethodGet, "", "", handler.List)
			require.Equal(t, http.StatusOK, listed.Code)
			var rules []map[string]any
			require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &rules))
			found := false
			for _, rule := range rules {
				require.NotContains(t, rule, "trackerDomains")
				if strconv.Itoa(int(rule["id"].(float64))) == ruleID {
					found = true
					require.Equal(t, tt.want, rule["trackerPattern"])
				}
			}
			require.True(t, found)
		})
	}

	t.Run("no tracker", func(t *testing.T) {
		rec := serve(http.MethodPost, "", payload(`"trackerPattern":" ","trackerDomains":[" "]`), handler.Create)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "Select at least one tracker or enable 'Apply to all'")
	})
}
