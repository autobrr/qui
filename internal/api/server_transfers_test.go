// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransferStartRoute(t *testing.T) {
	rig := newPermissionRig(t)
	cookie := rig.sessionCookie(t)

	status, _ := rig.sendBody(t, http.MethodPost, "/api/transfers", "", cookie, `{"sourceInstanceId":1,"targetInstanceId":2}`)
	require.Equal(t, http.StatusBadRequest, status, "a body without the torrent")

	t.Run("a refusal answers 409 with its reason", func(t *testing.T) {
		// No way and each way reach the checks.
		for _, way := range []string{"", "auto", "copy", "bittorrent"} {
			status, body := rig.sendBody(t, http.MethodPost, "/api/transfers", "", cookie,
				`{"sourceInstanceId":1,"hash":"aaaa","targetInstanceId":2,"way":"`+way+`"}`)
			require.Equal(t, http.StatusConflict, status, way)
			require.Contains(t, body, "Instance 1 does not exist.", way)
		}
	})

	t.Run("an unknown way answers 400", func(t *testing.T) {
		status, body := rig.sendBody(t, http.MethodPost, "/api/transfers", "", cookie,
			`{"sourceInstanceId":1,"hash":"aaaa","targetInstanceId":2,"way":"sneakernet"}`)
		require.Equal(t, http.StatusBadRequest, status)
		require.Contains(t, body, "way must be auto, copy, or bittorrent")
	})
}
