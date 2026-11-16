// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/rollout"
)

func TestTransferStartRoute(t *testing.T) {
	const start = `{"sourceInstanceId":1,"hash":"aaaa","targetInstanceId":2}`

	t.Run("switch off answers 404", func(t *testing.T) {
		rig := newPermissionRig(t, func(deps *Dependencies) { deps.Switches = rollout.Production() })
		status, body := rig.sendBody(t, http.MethodPost, "/api/transfers", "", rig.sessionCookie(t), start)
		require.Equal(t, http.StatusNotFound, status)
		require.Equal(t, "404 page not found\n", body)
	})

	rig := newPermissionRig(t)
	cookie := rig.sessionCookie(t)

	t.Run("a body without the torrent answers 400", func(t *testing.T) {
		status, _ := rig.sendBody(t, http.MethodPost, "/api/transfers", "", cookie, `{"sourceInstanceId":1,"targetInstanceId":2}`)
		require.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("a refusal answers 409 with its reason", func(t *testing.T) {
		status, body := rig.sendBody(t, http.MethodPost, "/api/transfers", "", cookie, start)
		require.Equal(t, http.StatusConflict, status)
		require.Contains(t, body, "Instance 1 does not exist.")
	})
}
