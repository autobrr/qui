// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
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
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/dirscan"
)

func TestPathMatchesDirectory(t *testing.T) {
	t.Parallel()

	root := string(filepath.Separator)
	tests := []struct {
		name     string
		path     string
		dir      string
		expected bool
	}{
		{
			name:     "same path matches",
			path:     filepath.Clean("/data/media/tv"),
			dir:      filepath.Clean("/data/media/tv"),
			expected: true,
		},
		{
			name:     "child path matches",
			path:     filepath.Clean("/data/media/tv/Show Name"),
			dir:      filepath.Clean("/data/media/tv"),
			expected: true,
		},
		{
			name:     "sibling path does not match",
			path:     filepath.Clean("/data/media/tv-shows"),
			dir:      filepath.Clean("/data/media/tv"),
			expected: false,
		},
		{
			name:     "filesystem root matches descendants",
			path:     filepath.Join(root, "data", "media", "movies"),
			dir:      root,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, pathMatchesDirectory(tt.path, tt.dir))
		})
	}
}

func TestTriggerScan_ReturnsMatchedDirectoryMetadata(t *testing.T) {
	ctx := t.Context()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	createdInstance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	service := dirscan.NewService(
		dirscan.DefaultConfig(),
		models.NewDirScanStore(db),
		nil,
		instanceStore,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
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

	var resp dirScanTriggerResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Positive(t, resp.RunID)
	require.Equal(t, created.ID, resp.DirectoryID)
	require.Equal(t, dirPath, resp.DirectoryPath)
	require.Equal(t, dirPath, resp.ScanRoot)

	require.Eventually(t, func() bool {
		run, getErr := service.GetActiveRun(ctx, created.ID)
		require.NoError(t, getErr)
		return run == nil
	}, 5*time.Second, 50*time.Millisecond)
}

func TestWebhookTriggerScan_RejectsAmbiguousDuplicateDirectoryPaths(t *testing.T) {
	ctx := t.Context()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	instance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	service := dirscan.NewService(
		dirscan.DefaultConfig(),
		models.NewDirScanStore(db),
		nil,
		instanceStore,
		nil,
		nil,
		nil,
		nil,
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
		strings.NewReader(`{"path":"`+filepath.Join(dupePath, "Show Name")+`"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.WebhookTriggerScan(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "Multiple directories match the given path")
}

func TestWebhookTriggerScan_AcceptsArrTestPayloadWithoutScan(t *testing.T) {
	ctx := t.Context()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)

	service := dirscan.NewService(
		dirscan.DefaultConfig(),
		models.NewDirScanStore(db),
		nil,
		instanceStore,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	handler := NewDirScanHandler(service, instanceStore)

	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/api/dir-scan/webhook/scan",
		strings.NewReader(`{"eventType":"test","series":{"path":"C:\\testpath"}}`),
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

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	localAccess := true
	instance, err := instanceStore.Create(ctx, "test", "http://localhost:8080", "", "", nil, nil, false, &localAccess)
	require.NoError(t, err)

	service := dirscan.NewService(
		dirscan.DefaultConfig(),
		models.NewDirScanStore(db),
		nil,
		instanceStore,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
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
		strings.NewReader(`{"path":"`+first+`"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.WebhookTriggerScan(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp dirScanTriggerResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Positive(t, resp.RunID)
	require.Equal(t, created.ID, resp.DirectoryID)
	require.Equal(t, root, resp.DirectoryPath)
	require.Equal(t, first, resp.ScanRoot)

	require.Eventually(t, func() bool {
		run, getErr := service.GetActiveRun(ctx, created.ID)
		require.NoError(t, getErr)
		return run == nil
	}, 5*time.Second, 50*time.Millisecond)

	run, err := models.NewDirScanStore(db).GetRun(ctx, resp.RunID)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, "webhook", run.TriggeredBy)
	require.Equal(t, first, run.ScanRoot)
	require.Equal(t, 1, run.FilesFound)
}
