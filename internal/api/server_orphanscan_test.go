// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/orphanscan"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// The server hands the orphan scan service to the SSH routes, so an SSH change
// waits for a running orphan deletion.
func TestServerSSHRoutesWaitForAnOrphanDeletion(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "api-orphan-deletion-guard")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	orphanStore := models.NewOrphanScanStore(db)
	runID, err := orphanStore.CreateRunIfNoActive(t.Context(), instance.ID, "manual")
	require.NoError(t, err)
	require.NoError(t, orphanStore.UpdateRunStatus(t.Context(), runID, "preview_ready"))
	started, err := orphanStore.StartRunDeletion(t.Context(), runID)
	require.NoError(t, err)
	require.True(t, started)

	deps := newTestDependencies(t)
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("logPath = 'qui.log'\n"), 0o600))
	deps.Config, err = config.New(configDir, "test")
	require.NoError(t, err)
	deps.Config.Config.AuthDisabled = true
	deps.Config.Config.IAcknowledgeThisIsABadIdea = true
	deps.Config.Config.AuthDisabledAllowedCIDRs = []string{"127.0.0.1/32"}
	deps.InstanceStore = instanceStore
	deps.OrphanScanService = orphanscan.NewService(orphanscan.DefaultConfig(), instanceStore, orphanStore, nil, nil, nil)
	router, err := NewServer(deps).Handler()
	require.NoError(t, err)

	body := `{"host":"box.example","port":22,"username":"qui","privateKey":"x"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/instances/"+strconv.Itoa(instance.ID)+"/ssh-credentials", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "orphan scan is deleting")
}
