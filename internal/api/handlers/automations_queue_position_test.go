// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestAutomationSave_QueuePositionNeedsQueueing(t *testing.T) {
	const (
		queueTop      = `"queuePosition":{"enabled":true,"position":"top"}`
		queueDisabled = `"queuePosition":{"enabled":false,"position":"top"},"pause":{"enabled":true}`
		pauseOnly     = `"pause":{"enabled":true}`
	)

	tests := []struct {
		name              string
		queueingEnabled   bool
		ruleEnabled       *bool
		conditions        string
		wantStatus        int
		wantPrefsRequests bool
	}{
		{name: "rejects enabled action when queueing is off", queueingEnabled: false, conditions: queueTop, wantStatus: http.StatusBadRequest, wantPrefsRequests: true},
		{name: "accepts enabled action when queueing is on", queueingEnabled: true, conditions: queueTop, wantStatus: http.StatusCreated, wantPrefsRequests: true},
		{name: "skips the check for a disabled action", queueingEnabled: false, conditions: queueDisabled, wantStatus: http.StatusCreated},
		{name: "skips the check without the action", queueingEnabled: false, conditions: pauseOnly, wantStatus: http.StatusCreated},
		{name: "skips the check for a disabled rule", queueingEnabled: false, ruleEnabled: new(false), conditions: queueTop, wantStatus: http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, instanceID, prefsRequests := newQueuePositionHandler(t, tt.queueingEnabled)
			enabled := ""
			if tt.ruleEnabled != nil {
				enabled = fmt.Sprintf(`"enabled":%t,`, *tt.ruleEnabled)
			}
			body := fmt.Sprintf(`{"name":"queue","trackerPattern":"*",%s"conditions":{"schemaVersion":"1",%s}}`, enabled, tt.conditions)

			rec := httptest.NewRecorder()
			handler.Create(rec, automationRequest(http.MethodPost, instanceID, 0, body))
			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			require.Equal(t, tt.wantPrefsRequests, prefsRequests.Load() > 0)

			saved, err := handler.store.ListByInstance(t.Context(), instanceID)
			require.NoError(t, err)
			if tt.wantStatus == http.StatusBadRequest {
				require.Empty(t, saved, "rejected rule must not be persisted")
				return
			}
			require.Len(t, saved, 1)
		})
	}

	t.Run("update rejects enabling the action when queueing is off", func(t *testing.T) {
		handler, instanceID, _ := newQueuePositionHandler(t, false)
		created, err := handler.store.Create(t.Context(), &models.Automation{
			InstanceID:     instanceID,
			Name:           "queue",
			TrackerPattern: "*",
			Enabled:        true,
			Conditions:     &models.ActionConditions{SchemaVersion: "1", Pause: &models.PauseAction{Enabled: true}},
		})
		require.NoError(t, err)

		body := `{"name":"queue","trackerPattern":"*","conditions":{"schemaVersion":"1",` + queueTop + `}}`
		rec := httptest.NewRecorder()
		handler.Update(rec, automationRequest(http.MethodPut, instanceID, created.ID, body))
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

		saved, err := handler.store.ListByInstance(t.Context(), instanceID)
		require.NoError(t, err)
		require.Len(t, saved, 1)
		require.Nil(t, saved[0].Conditions.QueuePosition, "rejected update must not be persisted")
	})

	t.Run("update disables a rule with the action while queueing is off", func(t *testing.T) {
		handler, instanceID, prefsRequests := newQueuePositionHandler(t, false)
		created, err := handler.store.Create(t.Context(), &models.Automation{
			InstanceID:     instanceID,
			Name:           "queue",
			TrackerPattern: "*",
			Enabled:        true,
			Conditions: &models.ActionConditions{
				SchemaVersion: "1",
				QueuePosition: &models.QueuePositionAction{Enabled: true, Position: models.QueuePositionTop},
			},
		})
		require.NoError(t, err)

		body := `{"name":"queue","trackerPattern":"*","enabled":false,"conditions":{"schemaVersion":"1",` + queueTop + `}}`
		rec := httptest.NewRecorder()
		handler.Update(rec, automationRequest(http.MethodPut, instanceID, created.ID, body))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Zero(t, prefsRequests.Load())

		saved, err := handler.store.ListByInstance(t.Context(), instanceID)
		require.NoError(t, err)
		require.Len(t, saved, 1)
		require.False(t, saved[0].Enabled)
	})
}

func TestAutomationSave_QueuePositionRejectsUnknownPosition(t *testing.T) {
	handler, instanceID, _ := newQueuePositionHandler(t, true)
	body := `{"name":"queue","trackerPattern":"*","conditions":{"schemaVersion":"1","queuePosition":{"enabled":true,"position":"middle"}}}`

	rec := httptest.NewRecorder()
	handler.Create(rec, automationRequest(http.MethodPost, instanceID, 0, body))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// newQueuePositionHandler wires an AutomationHandler to a stub qBittorrent that reports queueingEnabled.
func newQueuePositionHandler(t *testing.T, queueingEnabled bool) (*AutomationHandler, int, *atomic.Int32) {
	t.Helper()

	var prefsRequests atomic.Int32
	qbtServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.10.0"))
		case "/api/v2/app/preferences":
			prefsRequests.Add(1)
			_, _ = fmt.Fprintf(w, `{"queueing_enabled":%t}`, queueingEnabled)
		case "/api/v2/sync/maindata":
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"torrents":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(qbtServer.Close)

	db := testdb.NewMigratedSQLite(t, t.Name())
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "queue", qbtServer.URL, "", "", nil, nil, false, nil)
	require.NoError(t, err)
	clientPool, err := internalqbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	handler := NewAutomationHandler(models.NewAutomationStore(db), nil, instanceStore, nil, nil, internalqbittorrent.NewSyncManager(clientPool, nil))
	return handler, instance.ID, &prefsRequests
}

func automationRequest(method string, instanceID, ruleID int, body string) *http.Request {
	req := httptest.NewRequest(method, "/api/instances/"+strconv.Itoa(instanceID)+"/automations", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", strconv.Itoa(instanceID))
	if ruleID > 0 {
		rctx.URLParams.Add("ruleID", strconv.Itoa(ruleID))
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
