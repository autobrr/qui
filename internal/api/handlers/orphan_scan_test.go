// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/orphanscan"
	"github.com/autobrr/qui/internal/testutil/sshtest"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestOrphanScanSettings_CategoryPathsRequireDefaultSavePath(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "orphan-scan-settings")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(t.Context(), "test", "http://example.invalid", "", "", nil, nil, false, new(true))
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)
	handler := NewOrphanScanHandler(store, instances, nil)
	router := chi.NewRouter()
	router.Put("/api/instances/{instanceID}/orphan-scan/settings", handler.UpdateSettings)
	url := fmt.Sprintf("/api/instances/%d/orphan-scan/settings", instance.ID)

	for _, step := range []struct {
		name string
		body string
		want bool
	}{
		{"enable both", `{"scanDefaultSavePath":true,"scanCategoryPaths":true}`, true},
		{"disable default only", `{"scanDefaultSavePath":false}`, false},
		{"enable categories only", `{"scanCategoryPaths":true}`, false},
	} {
		t.Run(step.name, func(t *testing.T) {
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequestWithContext(t.Context(), http.MethodPut, url, strings.NewReader(step.body)))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			var updated models.OrphanScanSettings
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &updated))
			require.Equal(t, step.want, updated.ScanCategoryPaths)
			stored, err := store.GetSettings(t.Context(), instance.ID)
			require.NoError(t, err)
			require.Equal(t, step.want, stored.ScanCategoryPaths)
		})
	}
}

func newOrphanScanInstance(t *testing.T, instances *models.InstanceStore, mode models.FilesystemMode) *models.Instance {
	t.Helper()

	instance, err := instances.Create(t.Context(), "test", "http://example.invalid", "", "", nil, nil, false, new(mode == models.FilesystemModeLocal))
	require.NoError(t, err)
	if mode == models.FilesystemModeRemote {
		require.NoError(t, instances.SetSSHCredentials(t.Context(), instance.ID, "box.example", 22, "qui", sshtest.PrivateKey("")))
		require.NoError(t, instances.SetHostKeyPin(t.Context(), instance.ID, "box.example", 22, sshtest.NewSigner().PublicKey().Marshal()))
	}
	instance, err = instances.Get(t.Context(), instance.ID)
	require.NoError(t, err)
	require.Equal(t, mode, models.FilesystemAccessMode(instance))
	return instance
}

func newOrphanScanRouter(handler *OrphanScanHandler) *chi.Mux {
	router := chi.NewRouter()
	router.Route("/api/instances/{instanceID}/orphan-scan", func(r chi.Router) {
		r.Get("/settings", handler.GetSettings)
		r.Put("/settings", handler.UpdateSettings)
		r.Post("/scan", handler.TriggerScan)
		r.Get("/runs", handler.ListRuns)
		r.Get("/runs/{runID}", handler.GetRun)
		r.Post("/runs/{runID}/confirm", handler.ConfirmDeletion)
		r.Delete("/runs/{runID}", handler.CancelRun)
	})
	return router
}

func TestOrphanScan_RoutesAdmitLocalAndRemoteInstances(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "orphan-scan-gate")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	router := newOrphanScanRouter(NewOrphanScanHandler(models.NewOrphanScanStore(db), instances, nil))

	for _, mode := range []models.FilesystemMode{models.FilesystemModeLocal, models.FilesystemModeRemote, models.FilesystemModeNone} {
		instance := newOrphanScanInstance(t, instances, mode)
		base := fmt.Sprintf("/api/instances/%d/orphan-scan", instance.ID)
		for _, route := range []struct{ method, path, body string }{
			{http.MethodGet, "/settings", ""},
			{http.MethodPut, "/settings", "{}"},
			{http.MethodPost, "/scan", ""},
			{http.MethodGet, "/runs", ""},
			{http.MethodGet, "/runs/999", ""},
			{http.MethodPost, "/runs/999/confirm", ""},
			{http.MethodDelete, "/runs/999", ""},
		} {
			t.Run(string(mode)+" "+route.method+" "+route.path, func(t *testing.T) {
				resp := httptest.NewRecorder()
				router.ServeHTTP(resp, httptest.NewRequestWithContext(t.Context(), route.method, base+route.path, strings.NewReader(route.body)))
				if mode == models.FilesystemModeNone {
					require.Equal(t, http.StatusForbidden, resp.Code, resp.Body.String())
					return
				}
				require.NotEqual(t, http.StatusForbidden, resp.Code, resp.Body.String())
			})
		}
	}
}

func TestOrphanScan_RoutesAnswerNotFoundForUnknownInstance(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "orphan-scan-unknown-instance")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	router := newOrphanScanRouter(NewOrphanScanHandler(models.NewOrphanScanStore(db), instances, nil))

	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/settings", ""},
		{http.MethodPut, "/settings", "{}"},
		{http.MethodPost, "/scan", ""},
		{http.MethodGet, "/runs", ""},
		{http.MethodGet, "/runs/999", ""},
		{http.MethodPost, "/runs/999/confirm", ""},
		{http.MethodDelete, "/runs/999", ""},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequestWithContext(t.Context(), route.method, "/api/instances/404/orphan-scan"+route.path, strings.NewReader(route.body)))
			require.Equal(t, http.StatusNotFound, resp.Code, resp.Body.String())
			require.JSONEq(t, `{"error":"Instance not found"}`, resp.Body.String())
		})
	}
}

func TestOrphanScan_ConfirmConflictsNameTheirReason(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "orphan-scan-confirm-conflicts")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)
	service := orphanscan.NewService(orphanscan.DefaultConfig(), instances, store, nil, nil, nil)
	router := newOrphanScanRouter(NewOrphanScanHandler(store, instances, service))
	instance := newOrphanScanInstance(t, instances, models.FilesystemModeRemote)

	previewRun := func(mode models.FilesystemMode) int64 {
		runID, err := store.CreateRunIfNoActive(t.Context(), instance.ID, "manual")
		require.NoError(t, err)
		require.NoError(t, store.UpdateRunFilesystemMode(t.Context(), runID, mode))
		require.NoError(t, store.UpdateRunStatus(t.Context(), runID, "preview_ready"))
		return runID
	}
	confirm := func(runID int64) *httptest.ResponseRecorder {
		resp := httptest.NewRecorder()
		url := fmt.Sprintf("/api/instances/%d/orphan-scan/runs/%d/confirm", instance.ID, runID)
		router.ServeHTTP(resp, httptest.NewRequestWithContext(t.Context(), http.MethodPost, url, nil))
		return resp
	}

	remoteRun := previewRun(models.FilesystemModeRemote)
	unsupported := confirm(remoteRun)
	require.Equal(t, http.StatusConflict, unsupported.Code, unsupported.Body.String())
	require.Contains(t, unsupported.Body.String(), "remote instances")
	kept, err := store.GetRun(t.Context(), remoteRun)
	require.NoError(t, err)
	require.Equal(t, "preview_ready", kept.Status)
	require.NoError(t, store.UpdateRunStatus(t.Context(), remoteRun, "canceled"))

	localRun := previewRun(models.FilesystemModeLocal)
	changed := confirm(localRun)
	require.Equal(t, http.StatusConflict, changed.Code, changed.Body.String())
	require.Contains(t, changed.Body.String(), orphanscan.FilesystemModeChangedMessage)
	require.NotEqual(t, unsupported.Body.String(), changed.Body.String())
}

func TestOrphanScan_TriggerReplacesOnlyARemotePreview(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "orphan-scan-replace-preview")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)
	service := orphanscan.NewService(orphanscan.DefaultConfig(), instances, store, nil, nil, nil)
	router := newOrphanScanRouter(NewOrphanScanHandler(store, instances, service))

	previewWithFiles := func(instanceID int, mode models.FilesystemMode) int64 {
		runID, err := store.CreateRunIfNoActive(t.Context(), instanceID, "manual")
		require.NoError(t, err)
		require.NoError(t, store.UpdateRunFilesystemMode(t.Context(), runID, mode))
		require.NoError(t, store.UpdateRunFoundStats(t.Context(), runID, 2, false, 20))
		require.NoError(t, store.UpdateRunStatus(t.Context(), runID, "preview_ready"))
		return runID
	}
	trigger := func(instanceID int) *httptest.ResponseRecorder {
		resp := httptest.NewRecorder()
		url := fmt.Sprintf("/api/instances/%d/orphan-scan/scan", instanceID)
		router.ServeHTTP(resp, httptest.NewRequestWithContext(t.Context(), http.MethodPost, url, nil))
		return resp
	}
	status := func(runID int64) string {
		run, err := store.GetRun(t.Context(), runID)
		require.NoError(t, err)
		return run.Status
	}

	remote := newOrphanScanInstance(t, instances, models.FilesystemModeRemote)
	oldRemote := previewWithFiles(remote.ID, models.FilesystemModeRemote)
	accepted := trigger(remote.ID)
	require.Equal(t, http.StatusAccepted, accepted.Code, accepted.Body.String())
	var body struct {
		RunID int64 `json:"runId"`
	}
	require.NoError(t, json.Unmarshal(accepted.Body.Bytes(), &body))
	require.Equal(t, "canceled", status(oldRemote))
	// The service has no backend pool, so the new run fails fast. Wait for it before the database closes.
	require.Eventually(t, func() bool { return status(body.RunID) == "failed" }, 5*time.Second, 10*time.Millisecond)

	local := newOrphanScanInstance(t, instances, models.FilesystemModeLocal)
	oldLocal := previewWithFiles(local.ID, models.FilesystemModeLocal)
	conflict := trigger(local.ID)
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	require.Contains(t, conflict.Body.String(), fmt.Sprintf("runId=%d status=preview_ready", oldLocal))
	require.Equal(t, "preview_ready", status(oldLocal))
}
