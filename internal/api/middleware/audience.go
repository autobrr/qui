// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"net/http"

	"github.com/autobrr/qui/internal/api/ctxkeys"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/secrets"
)

var secretKindPermissions = [...]struct {
	kind secrets.Kind
	area models.PermissionArea
}{
	{secrets.TrackerPasskey, models.AreaTorrents},
	{secrets.NotificationToken, models.AreaNotifications},
}

func AudienceOf(r *http.Request) secrets.Audience {
	if r == nil {
		return secrets.Audience{}
	}
	key, ok := r.Context().Value(ctxkeys.APIKey).(*models.APIKey)
	if !ok {
		return secrets.Full()
	}
	sees := make([]secrets.Kind, 0, len(secretKindPermissions))
	for _, p := range secretKindPermissions {
		if key.Allows(p.area, models.LevelWrite) {
			sees = append(sees, p.kind)
		}
	}
	return secrets.NewAudience(sees...)
}
