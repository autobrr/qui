// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestAutomationValidatePayload_Category(t *testing.T) {
	for _, category := range []string{"", "archive"} {
		t.Run("target="+category, func(t *testing.T) {
			handler := NewAutomationHandler(nil, nil, nil, nil, nil)
			payload := &AutomationPayload{
				Name:           "Category rule",
				TrackerPattern: "*",
				Conditions: &models.ActionConditions{
					Category: &models.CategoryAction{Enabled: true, Category: category},
				},
			}

			status, message, err := handler.validatePayload(t.Context(), 1, payload)
			require.NoError(t, err)
			require.Zero(t, status)
			require.Empty(t, message)
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

func TestAutomationValidatePayload_UnknownInstance(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "automation-unknown-instance")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	handler := NewAutomationHandler(nil, nil, instances, nil, nil)
	payload := &AutomationPayload{
		Name:           "Pause rule",
		TrackerPattern: "*",
		Conditions:     &models.ActionConditions{Pause: &models.PauseAction{Enabled: true}},
	}

	status, message, err := handler.validatePayload(t.Context(), 999, payload)

	require.Error(t, err)
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, "Instance not found", message)
}
