// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"encoding/json/v2"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/externalprograms"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

type everyTorrentIsAHelper struct {
	asked []string
}

func (h *everyTorrentIsAHelper) IsHelper(_ int, hash string) bool {
	h.asked = append(h.asked, hash)
	return true
}

func TestAnExternalProgramRunFindsItsTorrentByAnUppercaseHash(t *testing.T) {
	stored := strings.Repeat("ab", 20)
	stub := qbtstub.New(t, t.TempDir())
	stub.Add(qbtstub.Torrent{Hash: stored, Name: "Example.Show.S01.1080p.WEB-DL-GRP"})

	helpers := &everyTorrentIsAHelper{}
	rig := newPermissionRig(t, func(deps *Dependencies) {
		deps.ExternalProgramService = externalprograms.NewService(deps.ExternalProgramStore, nil, nil)
		deps.ExternalProgramService.SetHelperTorrents(helpers)
	})
	instance, err := rig.deps.InstanceStore.Create(t.Context(), "seedbox", stub.URL, "admin", "secret", nil, nil, false, nil)
	require.NoError(t, err)
	program, err := rig.deps.ExternalProgramStore.Create(t.Context(), &models.ExternalProgramCreate{
		Name: "recorder", Path: "/opt/recorder", Enabled: true,
	})
	require.NoError(t, err)

	asked := strings.ToUpper(stored)
	unknown := strings.Repeat("cd", 20)
	status, body := rig.sendBody(t, http.MethodPost, "/api/external-programs/execute", "", rig.sessionCookie(t),
		`{"program_id":`+strconv.Itoa(program.ID)+`,"instance_id":`+strconv.Itoa(instance.ID)+`,"hashes":["`+asked+`","`+unknown+`"]}`)
	require.Equal(t, http.StatusOK, status, body)

	var response struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &response))
	require.Equal(t, []map[string]any{
		{"hash": asked, "success": false, "error": "not started: qui added this torrent for a Transfer or a Share"},
		{"hash": unknown, "success": false, "error": "Torrent with hash " + unknown + " not found"},
	}, response.Results)
	require.Equal(t, []string{stored}, helpers.asked)
}
