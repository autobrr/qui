// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/config"
)

type trackerIconServiceStub struct {
	enabled bool
}

func (s *trackerIconServiceStub) GetIcon(context.Context, string, string) (string, error) {
	return "", nil
}

func (s *trackerIconServiceStub) ListIcons(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (s *trackerIconServiceStub) FetchEnabled() bool {
	return s.enabled
}

func (s *trackerIconServiceStub) SetFetchEnabled(enabled bool) {
	s.enabled = enabled
}

func TestGetTrackerIconSettings(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	require.NoError(t, config.WriteDefaultConfig(configPath))

	cfg, err := config.New(configPath)
	require.NoError(t, err)

	svc := &trackerIconServiceStub{enabled: true}
	handler := NewTrackerIconHandler(svc, cfg)

	req := httptest.NewRequest(http.MethodGet, "/tracker-icons/settings", nil)
	rr := httptest.NewRecorder()

	handler.GetTrackerIconSettings(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp trackerIconSettingsResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.True(t, resp.FetchEnabled)
}

func TestUpdateTrackerIconSettings(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	require.NoError(t, config.WriteDefaultConfig(configPath))

	cfg, err := config.New(configPath)
	require.NoError(t, err)

	svc := &trackerIconServiceStub{enabled: true}
	handler := NewTrackerIconHandler(svc, cfg)

	req := httptest.NewRequest(http.MethodPut, "/tracker-icons/settings", strings.NewReader(`{"fetchEnabled":false}`))
	rr := httptest.NewRecorder()

	handler.UpdateTrackerIconSettings(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.False(t, svc.FetchEnabled())
	require.False(t, cfg.Config.TrackerIconsFetchEnabled)

	var resp trackerIconSettingsResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.False(t, resp.FetchEnabled)

	cfgReloaded, err := config.New(configPath)
	require.NoError(t, err)
	require.False(t, cfgReloaded.Config.TrackerIconsFetchEnabled)
}
