// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/dirscan"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type webhookPayloadSimple struct {
	Path string `json:"path"`
}

type webhookPayloadSeries struct {
	Path string `json:"path"`
}

type webhookPayloadArr struct {
	Series         webhookPayloadSeries `json:"series"`
	DownloadClient string               `json:"downloadClient,omitempty"`
	EventType      string               `json:"eventType,omitempty"`
}

func dirScanWebhookJSONBody(t *testing.T, payload any) *bytes.Reader {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return bytes.NewReader(body)
}

func TestDownloadClientAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		allowed        []string
		downloadClient string
		expected       bool
	}{
		{
			name:           "empty allowlist allows all",
			allowed:        nil,
			downloadClient: "",
			expected:       true,
		},
		{
			name:           "blank allowlist entries are ignored",
			allowed:        []string{"", "   "},
			downloadClient: "",
			expected:       true,
		},
		{
			name:           "matching is case insensitive",
			allowed:        []string{" SABnzbd "},
			downloadClient: "sabNZBD",
			expected:       true,
		},
		{
			name:           "missing client is rejected when nonblank allowlist exists",
			allowed:        []string{"SABnzbd"},
			downloadClient: "",
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, downloadClientAllowed(tt.allowed, tt.downloadClient))
		})
	}
}

func TestNormalizeAllowedDownloadClients(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		[]string{"SABnzbd", "qBittorrent"},
		normalizeAllowedDownloadClients([]string{" SABnzbd ", "", "sabnzbd", "   ", "qBittorrent"}),
	)
}

type singleDirScanRun struct {
	*database.DB
	terminal               chan notifications.Event
	latestPruneReadStarted chan struct{}
}

func newSingleDirScanRun(db *database.DB, pool *fsops.Pool) (*dirscan.Service, *singleDirScanRun) {
	run := &singleDirScanRun{
		DB:                     db,
		terminal:               make(chan notifications.Event, 1),
		latestPruneReadStarted: make(chan struct{}, 1),
	}
	service := dirscan.NewService(dirscan.DefaultConfig(), models.NewDirScanStore(run),
		nil, nil, nil, nil, nil, run, pool, nil)
	return service, run
}

func (r *singleDirScanRun) Notify(_ context.Context, event notifications.Event) {
	if event.Type == notifications.EventDirScanCompleted || event.Type == notifications.EventDirScanFailed {
		r.terminal <- event
	}
}

func (r *singleDirScanRun) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if strings.Contains(query, "FROM dir_scan_files") && strings.Contains(query, "last_processed_at >=") &&
		len(args) == 4 && args[3] == 0 {
		row := r.Conn().QueryRowContext(ctx, query, args...)
		r.latestPruneReadStarted <- struct{}{}
		return row
	}
	return r.DB.QueryRowContext(ctx, query, args...)
}

func (r *singleDirScanRun) response(t *testing.T, rec *httptest.ResponseRecorder) dirScanTriggerResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var event notifications.Event
	select {
	case <-ctx.Done():
		t.Fatal("Dir Scan did not finish notifying")
	case event = <-r.terminal:
	}
	if event.Type == notifications.EventDirScanCompleted {
		store := models.NewDirScanStore(r.DB)
		run, err := store.GetRun(ctx, event.DirScanRunID)
		require.NoError(t, err)
		directory, err := store.GetDirectory(ctx, run.DirectoryID)
		require.NoError(t, err)
		if run.ScanRoot == "" || run.ScanRoot == directory.Path {
			select {
			case <-ctx.Done():
				t.Fatal("Dir Scan did not reach its final prune read")
			case <-r.latestPruneReadStarted:
			}
			require.NoError(t, r.Conn().PingContext(ctx), "the prune query must release the single writer connection")
		}
	}

	var response dirScanTriggerResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, event.DirScanRunID, response.RunID)
	return response
}

func TestTriggerScan_ReturnsMatchedDirectoryMetadata(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "dirscan-webhook")

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	createdInstance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	service, scan := newSingleDirScanRun(db, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	handler := NewDirScanHandler(service, instanceStore)

	dirPath := t.TempDir()
	created, err := service.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                dirPath,
		Enabled:             true,
		TargetInstanceID:    createdInstance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/dir-scan/directories/"+strconv.Itoa(created.ID)+"/scan", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("directoryID", strconv.Itoa(created.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rec := httptest.NewRecorder()

	handler.TriggerScan(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	resp := scan.response(t, rec)
	require.Positive(t, resp.RunID)
	require.Equal(t, created.ID, resp.DirectoryID)
	require.Equal(t, dirPath, resp.DirectoryPath)
	require.Equal(t, dirPath, resp.ScanRoot)
}

func TestWebhookTriggerScan_RejectsAmbiguousDuplicateDirectoryPaths(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "dirscan-webhook")

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	instance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	service := dirscan.NewService(
		dirscan.DefaultConfig(),
		models.NewDirScanStore(db),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		fsops.NewPool(instanceStore, localbackend.NewBackend()),
		nil,
	)
	handler := NewDirScanHandler(service, instanceStore)

	dupePath := filepath.Join(t.TempDir(), "library")
	_, err = db.ExecContext(ctx, `
		INSERT INTO dir_scan_directories
			(path, enabled, target_instance_id, scan_interval_minutes)
		VALUES (?, ?, ?, ?), (?, ?, ?, ?)
	`, dupePath, 1, instance.ID, 60, dupePath+string(filepath.Separator), 1, instance.ID, 60)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/api/dir-scan/webhook/scan",
		dirScanWebhookJSONBody(t, webhookPayloadSimple{
			Path: filepath.Join(dupePath, "Show Name"),
		}),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.WebhookTriggerScan(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "Multiple directories match the given path")
}

func TestWebhookTriggerScan_AcceptsArrTestPayloadWithoutScan(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "dirscan-webhook")

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)

	service := dirscan.NewService(
		dirscan.DefaultConfig(),
		models.NewDirScanStore(db),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		fsops.NewPool(instanceStore, localbackend.NewBackend()),
		nil,
	)
	handler := NewDirScanHandler(service, instanceStore)

	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/api/dir-scan/webhook/scan",
		dirScanWebhookJSONBody(t, webhookPayloadArr{
			EventType: "test",
			Series: webhookPayloadSeries{
				Path: `C:\testpath`,
			},
		}),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.WebhookTriggerScan(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)

	runs, err := service.ListRuns(ctx, 1, 10)
	require.NoError(t, err)
	require.Empty(t, runs)
}

func TestWebhookTriggerScan_ScansOnlyRequestedSubtree(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "dirscan-webhook")

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	instance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	service, scan := newSingleDirScanRun(db, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	handler := NewDirScanHandler(service, instanceStore)

	root := t.TempDir()
	first := filepath.Join(root, "Movie One (2024)")
	second := filepath.Join(root, "Movie Two (2024)")
	require.NoError(t, os.Mkdir(first, 0o755))
	require.NoError(t, os.Mkdir(second, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(first, "movie-one.mkv"), []byte("one"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(second, "movie-two.mkv"), []byte("two"), 0o600))

	created, err := service.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                root,
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/api/dir-scan/webhook/scan",
		dirScanWebhookJSONBody(t, webhookPayloadSimple{
			Path: first,
		}),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.WebhookTriggerScan(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	resp := scan.response(t, rec)
	require.Positive(t, resp.RunID)
	require.Equal(t, created.ID, resp.DirectoryID)
	require.Equal(t, root, resp.DirectoryPath)
	require.Equal(t, first, resp.ScanRoot)

	run, err := models.NewDirScanStore(db).GetRun(ctx, resp.RunID)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, "webhook", run.TriggeredBy)
	require.Equal(t, first, run.ScanRoot)
	require.Equal(t, 1, run.FilesFound)
}

func TestWebhookTriggerScan_SkipsWhenDownloadClientNotAllowed(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "dirscan-webhook")

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	instance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	service := dirscan.NewService(
		dirscan.DefaultConfig(),
		models.NewDirScanStore(db),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		fsops.NewPool(instanceStore, localbackend.NewBackend()),
		nil,
	)
	handler := NewDirScanHandler(service, instanceStore)

	root := t.TempDir()
	_, err = service.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                root,
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
		AllowedDownloadClients: []string{
			"SABnzbd",
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/api/dir-scan/webhook/scan",
		dirScanWebhookJSONBody(t, webhookPayloadArr{
			Series: webhookPayloadSeries{
				Path: root,
			},
			DownloadClient: "qBittorrent",
		}),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.WebhookTriggerScan(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"skipped":true,"reason":"download client not allowed"}`, rec.Body.String())

	runs, err := service.ListRuns(ctx, 1, 10)
	require.NoError(t, err)
	require.Empty(t, runs)
}

func TestWebhookTriggerScan_SkipsWhenDownloadClientMissingButFilterExists(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "dirscan-webhook")

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	instance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	service := dirscan.NewService(
		dirscan.DefaultConfig(),
		models.NewDirScanStore(db),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		fsops.NewPool(instanceStore, localbackend.NewBackend()),
		nil,
	)
	handler := NewDirScanHandler(service, instanceStore)

	root := t.TempDir()
	_, err = service.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                root,
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
		AllowedDownloadClients: []string{
			"SABnzbd",
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/api/dir-scan/webhook/scan",
		dirScanWebhookJSONBody(t, webhookPayloadArr{
			Series: webhookPayloadSeries{
				Path: root,
			},
		}),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.WebhookTriggerScan(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"skipped":true,"reason":"download client not allowed"}`, rec.Body.String())

	runs, err := service.ListRuns(ctx, 1, 10)
	require.NoError(t, err)
	require.Empty(t, runs)
}

func TestWebhookTriggerScan_MatchesDownloadClientCaseInsensitively(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "dirscan-webhook")

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	instance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	service, scan := newSingleDirScanRun(db, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	handler := NewDirScanHandler(service, instanceStore)

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "episode.mkv"), []byte("one"), 0o600))

	created, err := service.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                root,
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
		AllowedDownloadClients: []string{
			" SABnzbd ",
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/api/dir-scan/webhook/scan",
		dirScanWebhookJSONBody(t, webhookPayloadArr{
			Series: webhookPayloadSeries{
				Path: root,
			},
			DownloadClient: "sabNZBD",
		}),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.WebhookTriggerScan(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	resp := scan.response(t, rec)
	require.Equal(t, created.ID, resp.DirectoryID)
}

func TestWebhookTriggerScan_SimpleModeBypassesDownloadClientFilter(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "dirscan-webhook")

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	instance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	service, scan := newSingleDirScanRun(db, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	handler := NewDirScanHandler(service, instanceStore)

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "movie.mkv"), []byte("one"), 0o600))

	created, err := service.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                root,
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
		AllowedDownloadClients: []string{
			"SABnzbd",
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/api/dir-scan/webhook/scan",
		dirScanWebhookJSONBody(t, webhookPayloadSimple{
			Path: root,
		}),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.WebhookTriggerScan(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	resp := scan.response(t, rec)
	require.Equal(t, created.ID, resp.DirectoryID)
}
