// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/buildinfo"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/experimental"
)

func TestApplicationHandler_GetInfo(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, config.WriteDefaultConfig(configPath))

	appCfg, err := config.New(configPath, "test-version")
	require.NoError(t, err)
	appCfg.Config.BaseURL = "/qui"
	appCfg.Config.Host = "127.0.0.1"
	appCfg.Config.Port = 7476
	appCfg.Config.DatabaseEngine = "sqlite"
	appCfg.Config.CheckForUpdates = true
	appCfg.Config.OIDCEnabled = true
	appCfg.Config.OIDCDisableBuiltInLogin = true
	appCfg.Config.OIDCIssuer = "https://id.example.com/realms/main"
	appCfg.PublishConfig()

	origVersion := buildinfo.Version
	origCommit := buildinfo.Commit
	origDate := buildinfo.Date
	t.Cleanup(func() {
		buildinfo.Version = origVersion
		buildinfo.Commit = origCommit
		buildinfo.Date = origDate
	})

	buildinfo.Version = "v1.2.3"
	buildinfo.Commit = "1234567890abcdef"
	buildinfo.Date = "2026-03-02T10:00:00Z"

	startedAt := time.Now().Add(-42 * time.Second).UTC()
	handler := NewApplicationHandler(appCfg, experimental.NewWithKnown(
		[]experimental.Feature{"testAlpha", "testBravo"},
		[]string{"testBravo", "nothing"},
	), startedAt)

	req := httptest.NewRequest(http.MethodGet, "/api/application/info", nil)
	rec := httptest.NewRecorder()
	handler.GetInfo(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp ApplicationInfoResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "v1.2.3", resp.Version)
	require.Equal(t, "1234567890abcdef", resp.Commit)
	require.Equal(t, "12345678", resp.CommitShort)
	require.Equal(t, "2026-03-02T10:00:00Z", resp.BuildDate)
	require.Equal(t, "/qui", resp.BaseURL)
	require.Equal(t, "127.0.0.1", resp.Host)
	require.Equal(t, 7476, resp.Port)
	require.Equal(t, "oidc", resp.AuthMode)
	require.True(t, resp.OIDCEnabled)
	require.False(t, resp.BuiltInLogin)
	require.Equal(t, "id.example.com", resp.OIDCIssuerHost)
	require.Equal(t, "sqlite", resp.Database.Engine)
	require.Equal(t, appCfg.GetDatabasePath(), resp.Database.Target)
	require.Positive(t, resp.UptimeSeconds)
	require.Equal(t, startedAt.Format(time.RFC3339), resp.StartedAt)
	require.Equal(t, []string{"testBravo"}, resp.ExperimentalFeatures)
}

func TestApplicationHandler_GetInfoListsNoExperimentalFeatureAsEmptyList(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, config.WriteDefaultConfig(configPath))
	appCfg, err := config.New(configPath, "test-version")
	require.NoError(t, err)
	handler := NewApplicationHandler(appCfg, experimental.NewWithKnown([]experimental.Feature{"testAlpha"}, nil), time.Now())

	rec := httptest.NewRecorder()
	handler.GetInfo(rec, httptest.NewRequest(http.MethodGet, "/api/application/info", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"experimentalFeatures":[]`)
}

func TestPostgresTarget_DSNRemovesCredentials(t *testing.T) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "PG") {
			t.Setenv(name, "")
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())

	for _, tc := range []struct {
		name, dsn, want string
	}{
		{name: "URL", dsn: "postgres://db.example.com:5432/qui_prod?sslmode=require", want: "db.example.com:5432/qui_prod"},
		{name: "URL with a login and no port", dsn: "postgres://qui:hunter2@db.example.com/qui_prod", want: "db.example.com:5432/qui_prod"},
		{name: "key/value", dsn: "host=db.example.com port=6432 user=qui password=hunter2 dbname=qui_prod", want: "db.example.com:6432/qui_prod"},
		{name: "IPv6 host", dsn: "host=::1 user=qui password=hunter2 dbname=qui_prod", want: "[::1]:5432/qui_prod"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, postgresTarget(&domain.Config{DatabaseDSN: tc.dsn}))
		})
	}
}

func TestAuthMode(t *testing.T) {
	require.Equal(t, "builtin", authMode(&domain.Config{}))
	require.Equal(t, "oidc", authMode(&domain.Config{OIDCEnabled: true}))
	require.Equal(t, "disabled", authMode(&domain.Config{
		AuthDisabled:               true,
		IAcknowledgeThisIsABadIdea: true,
	}))
}

func TestOIDCIssuerHost(t *testing.T) {
	require.Equal(t, "auth.example.com", oidcIssuerHost("https://auth.example.com/oidc"))
	require.Empty(t, oidcIssuerHost(""))
	require.Empty(t, oidcIssuerHost("not-a-url"))
}
