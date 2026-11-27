// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/api/ctxkeys"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/secrets"
)

func TestAudienceOf(t *testing.T) {
	t.Parallel()
	withKey := func(permissions ...models.Permission) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		return r.WithContext(context.WithValue(r.Context(), ctxkeys.APIKey, &models.APIKey{Permissions: permissions}))
	}
	for _, tc := range []struct {
		name string
		r    *http.Request
		want secrets.Audience
	}{
		{"no request", nil, secrets.Audience{}},
		{"no API key", httptest.NewRequest(http.MethodGet, "/", nil), secrets.Full()},
		{"a key with read Permissions", withKey(models.Permission{Area: models.AreaTorrents, Level: models.LevelRead}, models.Permission{Area: models.AreaNotifications, Level: models.LevelRead}), secrets.Audience{}},
		{"a key with Torrents: write", withKey(models.Permission{Area: models.AreaTorrents, Level: models.LevelWrite}), secrets.NewAudience(secrets.TrackerPasskey)},
		{"a key with Notifications: write", withKey(models.Permission{Area: models.AreaNotifications, Level: models.LevelWrite}), secrets.NewAudience(secrets.NotificationToken)},
		{"a key with both", withKey(models.Permission{Area: models.AreaTorrents, Level: models.LevelWrite}, models.Permission{Area: models.AreaNotifications, Level: models.LevelWrite}), secrets.Full()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, AudienceOf(tc.r))
		})
	}
}
