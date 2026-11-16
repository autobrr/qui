// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/rollout"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// While the switch for Transfers and Shares is off, the Transfer events reach
// no caller: the list of events, the event types of a target saved with
// none, and the check of the event types of a save. With the switch on, they
// reach each.
func TestNotificationsHandlerShowsTransferEventsOnlyWithTheSwitch(t *testing.T) {
	transferEvents := []string{string(notifications.EventTransferFailed), string(notifications.EventTransferFinished)}
	for _, tc := range []struct {
		name     string
		switches rollout.Switches
		reached  bool
	}{
		{"switch off", rollout.Production(), false},
		{"switch on", rollout.Switches{TransfersAndShares: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := models.NewNotificationTargetStore(testdb.NewMigratedSQLite(t, "notifications-handler"))
			h := NewNotificationsHandler(store, nil, tc.switches)
			send := func(handle http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				handle(rec, httptest.NewRequestWithContext(t.Context(), method, "/api/notifications/targets", strings.NewReader(body)))
				return rec
			}

			rec := send(h.ListEvents, http.MethodGet, "")
			require.Equal(t, http.StatusOK, rec.Code)
			var events []notifications.EventDefinition
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &events))
			listed := make([]string, 0, len(events))
			for _, event := range events {
				listed = append(listed, string(event.Type))
			}

			rec = send(h.CreateTarget, http.MethodPost, `{"name":"every event","url":"generic://127.0.0.1/hook"}`)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			var target models.NotificationTarget
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &target))

			for _, event := range transferEvents {
				require.Equal(t, tc.reached, slices.Contains(listed, event), "listed %s", event)
				require.Equal(t, tc.reached, slices.Contains(target.EventTypes, event), "default list has %s", event)
			}
			require.Contains(t, target.EventTypes, string(notifications.EventTorrentAdded))

			rec = send(h.CreateTarget, http.MethodPost,
				`{"name":"transfers","url":"generic://127.0.0.1/hook","eventTypes":["`+string(notifications.EventTransferFailed)+`"]}`)
			if tc.reached {
				require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			} else {
				require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			}
		})
	}
}
