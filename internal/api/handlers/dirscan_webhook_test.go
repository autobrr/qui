// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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

	require.Eventually(t, func() bool {
		run, getErr := service.GetActiveRun(ctx, created.ID)
		require.NoError(t, getErr)
		return run == nil
	}, 5*time.Second, 50*time.Millisecond)
}
