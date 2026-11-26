// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/api/ctxkeys"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestListTargetsShowsTheFullURLOnlyWithNotificationsWrite(t *testing.T) {
	store := models.NewNotificationTargetStore(testdb.NewMigratedSQLite(t, "notification-targets"))
	stored := []string{
		"discord://madeUpWebhookToken@123456789",
		"generic+https://hooks.example.invalid/hook/madeUpPathToken?secret=madeUpQueryToken",
		"ntfy://ntfy.example.invalid/madeUpTopic",
		"telegram://123456:madeUpBotToken@telegram?chats=@madeUpChannel",
	}
	for _, url := range stored {
		_, err := store.Create(t.Context(), &models.NotificationTargetCreate{Name: url, URL: url, Enabled: true})
		require.NoError(t, err)
	}
	handler := NewNotificationsHandler(store, nil)
	permission := func(level models.PermissionLevel) *models.APIKey {
		return &models.APIKey{Permissions: []models.Permission{{Area: models.AreaNotifications, Level: level}}}
	}

	tests := []struct {
		name string
		key  *models.APIKey
		want []string
	}{
		{name: "a read key gets each URL as its scheme", key: permission(models.LevelRead), want: []string{"discord://", "generic+https://", "ntfy://", "telegram://"}},
		{name: "a write key gets the full URLs", key: permission(models.LevelWrite), want: stored},
		{name: "a session gets the full URLs", want: stored},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			if tt.key != nil {
				ctx = context.WithValue(ctx, ctxkeys.APIKey, tt.key)
			}
			rec := httptest.NewRecorder()
			handler.ListTargets(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/notifications/targets", http.NoBody))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var targets []models.NotificationTarget
			require.NoError(t, json.UnmarshalRead(rec.Body, &targets))
			urls := make([]string, 0, len(targets))
			for _, target := range targets {
				urls = append(urls, target.URL)
			}
			require.Equal(t, tt.want, urls)
		})
	}
}
