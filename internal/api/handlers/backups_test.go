// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ulikunitz/xz"

	"github.com/go-chi/chi/v5"

	"github.com/autobrr/qui/internal/backups"
	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func newRequestWithParams(method, path string, params map[string]string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	ctx := chi.NewRouteContext()
	for key, value := range params {
		ctx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
}

func newRequestWithParamsAndQuery(method, path string, params map[string]string, query map[string]string) *http.Request {
	req := newRequestWithParams(method, path, params)
	if len(query) > 0 {
		q := url.Values{}
		for key, value := range query {
			q.Add(key, value)
		}
		req.URL.RawQuery = q.Encode()
	}
	return req
}

func setupTestBackupHandler(t *testing.T) (*BackupsHandler, *database.DB, string, func()) {
	t.Helper()

	// Create test database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)

	// Create test data directory
	dataDir := t.TempDir()

	// Create backup service
	backupStore := models.NewBackupStore(db)
	cfg := backups.Config{
		DataDir:      dataDir,
		PollInterval: 0,
		WorkerCount:  1,
	}
	service := backups.NewService(backupStore, nil, nil, cfg, nil)

	handler := NewBackupsHandler(service)

	cleanup := func() {
		require.NoError(t, db.Close())
	}

	return handler, db, dataDir, cleanup
}

func createTestBackupRun(t *testing.T, db *database.DB, dataDir string, instanceID int) *models.BackupRun {
	t.Helper()
	ctx := context.Background()

	run := &models.BackupRun{
		InstanceID:   instanceID,
		Kind:         models.BackupRunKindManual,
		Status:       models.BackupRunStatusSuccess,
		RequestedBy:  "test",
		TotalBytes:   1024,
		TorrentCount: 2,
	}

	store := models.NewBackupStore(db)
	err := store.CreateRun(ctx, run)
	require.NoError(t, err)

	// Create manifest manually
	manifest := &backups.Manifest{
		InstanceID:   instanceID,
		Kind:         "manual",
		TorrentCount: 2,
		Items: []backups.ManifestItem{
			{
				Hash:        "test-hash-1",
				Name:        "Test Torrent 1",
				ArchivePath: "Test Torrent 1.torrent",
				SizeBytes:   512,
				TorrentBlob: "backups/torrents/ab/cd/abcd123456789.torrent",
			},
			{
				Hash:        "test-hash-2",
				Name:        "Test Torrent 2",
				ArchivePath: "Test Torrent 2.torrent",
				SizeBytes:   512,
				TorrentBlob: "backups/torrents/ef/gh/efgh987654321.torrent",
			},
		},
	}

	// Insert backup items into database
	items := []models.BackupItem{
		{
			RunID:           run.ID,
			TorrentHash:     "test-hash-1",
			Name:            "Test Torrent 1",
			SizeBytes:       512,
			ArchiveRelPath:  &manifest.Items[0].ArchivePath,
			TorrentBlobPath: &manifest.Items[0].TorrentBlob,
		},
		{
			RunID:           run.ID,
			TorrentHash:     "test-hash-2",
			Name:            "Test Torrent 2",
			SizeBytes:       512,
			ArchiveRelPath:  &manifest.Items[1].ArchivePath,
			TorrentBlobPath: &manifest.Items[1].TorrentBlob,
		},
	}
	err = store.InsertItems(ctx, run.ID, items)
	require.NoError(t, err)

	// Save manifest to file
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)

	manifestPath := filepath.Join("backups", fmt.Sprintf("instance-%d", instanceID), "manual", fmt.Sprintf("run-%d", run.ID), "manifest.json")
	absManifestPath := filepath.Join(dataDir, manifestPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(absManifestPath), 0755))
	require.NoError(t, os.WriteFile(absManifestPath, manifestData, 0644))

	// Update run with manifest path
	err = store.UpdateRunMetadata(ctx, run.ID, func(r *models.BackupRun) error {
		r.ManifestPath = &manifestPath
		return nil
	})
	require.NoError(t, err)

	return run
}

func createTestTorrentFiles(t *testing.T, dataDir string) {
	t.Helper()

	// Create test torrent files in the cache directory
	cacheDir := filepath.Join(dataDir, "backups", "torrents")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create test torrent data
	testData := []byte("test torrent data")

	// Create files with subdirectories (using the new structure)
	subdir1 := filepath.Join(cacheDir, "ab", "cd")
	subdir2 := filepath.Join(cacheDir, "ef", "gh")
	require.NoError(t, os.MkdirAll(subdir1, 0755))
	require.NoError(t, os.MkdirAll(subdir2, 0755))

	file1 := filepath.Join(subdir1, "abcd123456789.torrent")
	file2 := filepath.Join(subdir2, "efgh987654321.torrent")

	require.NoError(t, os.WriteFile(file1, testData, 0644))
	require.NoError(t, os.WriteFile(file2, testData, 0644))
}

func TestDownloadRun_InvalidInstanceID(t *testing.T) {
	handler, _, _, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	req := newRequestWithParams(http.MethodGet, "/api/instances/invalid/backups/runs/1/download", map[string]string{
		"instanceID": "invalid",
		"runID":      "1",
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid instance ID")
}

func TestDownloadRun_InvalidRunID(t *testing.T) {
	handler, _, _, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	req := newRequestWithParams(http.MethodGet, "/api/instances/1/backups/runs/invalid/download", map[string]string{
		"instanceID": "1",
		"runID":      "invalid",
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid run ID")
}

func TestDownloadRun_BackupNotFound(t *testing.T) {
	handler, _, _, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	req := newRequestWithParams(http.MethodGet, "/api/instances/1/backups/runs/999/download", map[string]string{
		"instanceID": "1",
		"runID":      "999",
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Backup run not found")
}

func TestDownloadRun_BackupNotAvailable(t *testing.T) {
	handler, db, _, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	// Create a test instance
	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (1, 1, 1, 'pass')")
	require.NoError(t, err)
	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	// Create a backup run with pending status
	run := &models.BackupRun{
		InstanceID:   instanceID,
		Kind:         models.BackupRunKindManual,
		Status:       models.BackupRunStatusPending,
		RequestedBy:  "test",
		TotalBytes:   1024,
		TorrentCount: 1,
	}

	store := models.NewBackupStore(db)
	err = store.CreateRun(ctx, run)
	require.NoError(t, err)

	req := newRequestWithParams(http.MethodGet, fmt.Sprintf("/api/instances/%d/backups/runs/%d/download", instanceID, run.ID), map[string]string{
		"instanceID": strconv.Itoa(instanceID),
		"runID":      strconv.FormatInt(run.ID, 10),
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Backup not available")
}

func TestDownloadRun_UnsupportedFormat(t *testing.T) {
	handler, db, dataDir, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	// Create a test instance and successful backup run
	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (1, 1, 1, 'pass')")
	require.NoError(t, err)
	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	run := createTestBackupRun(t, db, dataDir, instanceID)

	req := newRequestWithParamsAndQuery(http.MethodGet, fmt.Sprintf("/api/instances/%d/backups/runs/%d/download", instanceID, run.ID), map[string]string{
		"instanceID": strconv.Itoa(instanceID),
		"runID":      strconv.FormatInt(run.ID, 10),
	}, map[string]string{
		"format": "invalid",
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Unsupported format")
}

func TestDownloadRun_ZIPFormat(t *testing.T) {
	handler, db, dataDir, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	// Create a test instance and successful backup run
	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (1, 1, 1, 'pass')")
	require.NoError(t, err)
	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	run := createTestBackupRun(t, db, dataDir, instanceID)
	createTestTorrentFiles(t, dataDir)

	req := newRequestWithParamsAndQuery(http.MethodGet, fmt.Sprintf("/api/instances/%d/backups/runs/%d/download", instanceID, run.ID), map[string]string{
		"instanceID": strconv.Itoa(instanceID),
		"runID":      strconv.FormatInt(run.ID, 10),
	}, map[string]string{
		"format": "zip",
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/zip", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, w.Header().Get("Content-Disposition"), ".zip")

	// Verify ZIP content
	zipReader, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	require.NoError(t, err)

	// Should contain manifest.json and torrent files
	files := make(map[string]bool)
	for _, file := range zipReader.File {
		files[file.Name] = true
	}

	assert.True(t, files["manifest.json"])
	assert.True(t, files["Test Torrent 1.torrent"])
	assert.True(t, files["Test Torrent 2.torrent"])
}

func TestDownloadRun_TarGzFormat(t *testing.T) {
	handler, db, dataDir, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	// Create a test instance and successful backup run
	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (1, 1, 1, 'pass')")
	require.NoError(t, err)
	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	run := createTestBackupRun(t, db, dataDir, instanceID)
	createTestTorrentFiles(t, dataDir)

	req := newRequestWithParamsAndQuery(http.MethodGet, fmt.Sprintf("/api/instances/%d/backups/runs/%d/download", instanceID, run.ID), map[string]string{
		"instanceID": strconv.Itoa(instanceID),
		"runID":      strconv.FormatInt(run.ID, 10),
	}, map[string]string{
		"format": "tar.gz",
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/gzip", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, w.Header().Get("Content-Disposition"), ".tar.gz")

	// Verify tar.gz content
	gzipReader, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err)
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	files := make(map[string]bool)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		files[header.Name] = true
	}

	assert.True(t, files["manifest.json"])
	assert.True(t, files["Test Torrent 1.torrent"])
	assert.True(t, files["Test Torrent 2.torrent"])
}

func TestDownloadRun_TarZstFormat(t *testing.T) {
	handler, db, dataDir, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	// Create a test instance and successful backup run
	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (1, 1, 1, 'pass')")
	require.NoError(t, err)
	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	run := createTestBackupRun(t, db, dataDir, instanceID)
	createTestTorrentFiles(t, dataDir)

	req := newRequestWithParamsAndQuery(http.MethodGet, fmt.Sprintf("/api/instances/%d/backups/runs/%d/download", instanceID, run.ID), map[string]string{
		"instanceID": strconv.Itoa(instanceID),
		"runID":      strconv.FormatInt(run.ID, 10),
	}, map[string]string{
		"format": "tar.zst",
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/zstd", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, w.Header().Get("Content-Disposition"), ".tar.zst")

	// Verify tar.zst content
	zstdReader, err := zstd.NewReader(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err)
	defer zstdReader.Close()

	tarReader := tar.NewReader(zstdReader)
	files := make(map[string]bool)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		files[header.Name] = true
	}

	assert.True(t, files["manifest.json"])
	assert.True(t, files["Test Torrent 1.torrent"])
	assert.True(t, files["Test Torrent 2.torrent"])
}

func TestDownloadRun_TarBrFormat(t *testing.T) {
	handler, db, dataDir, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	// Create a test instance and successful backup run
	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (1, 1, 1, 'pass')")
	require.NoError(t, err)
	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	run := createTestBackupRun(t, db, dataDir, instanceID)
	createTestTorrentFiles(t, dataDir)

	req := newRequestWithParamsAndQuery(http.MethodGet, fmt.Sprintf("/api/instances/%d/backups/runs/%d/download", instanceID, run.ID), map[string]string{
		"instanceID": strconv.Itoa(instanceID),
		"runID":      strconv.FormatInt(run.ID, 10),
	}, map[string]string{
		"format": "tar.br",
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/x-brotli", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, w.Header().Get("Content-Disposition"), ".tar.br")

	// Verify tar.br content
	brotliReader := brotli.NewReader(bytes.NewReader(w.Body.Bytes()))
	tarReader := tar.NewReader(brotliReader)
	files := make(map[string]bool)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		files[header.Name] = true
	}

	assert.True(t, files["manifest.json"])
	assert.True(t, files["Test Torrent 1.torrent"])
	assert.True(t, files["Test Torrent 2.torrent"])
}

func TestDownloadRun_TarXzFormat(t *testing.T) {
	handler, db, dataDir, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	// Create a test instance and successful backup run
	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (1, 1, 1, 'pass')")
	require.NoError(t, err)
	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	run := createTestBackupRun(t, db, dataDir, instanceID)
	createTestTorrentFiles(t, dataDir)

	req := newRequestWithParamsAndQuery(http.MethodGet, fmt.Sprintf("/api/instances/%d/backups/runs/%d/download", instanceID, run.ID), map[string]string{
		"instanceID": strconv.Itoa(instanceID),
		"runID":      strconv.FormatInt(run.ID, 10),
	}, map[string]string{
		"format": "tar.xz",
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/x-xz", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, w.Header().Get("Content-Disposition"), ".tar.xz")

	// Verify tar.xz content
	xzReader, err := xz.NewReader(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err)

	tarReader := tar.NewReader(xzReader)
	files := make(map[string]bool)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		files[header.Name] = true
	}

	assert.True(t, files["manifest.json"])
	assert.True(t, files["Test Torrent 1.torrent"])
	assert.True(t, files["Test Torrent 2.torrent"])
}

func TestDownloadRun_TarFormat(t *testing.T) {
	handler, db, dataDir, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	// Create a test instance and successful backup run
	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (1, 1, 1, 'pass')")
	require.NoError(t, err)
	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	run := createTestBackupRun(t, db, dataDir, instanceID)
	createTestTorrentFiles(t, dataDir)

	req := newRequestWithParamsAndQuery(http.MethodGet, fmt.Sprintf("/api/instances/%d/backups/runs/%d/download", instanceID, run.ID), map[string]string{
		"instanceID": strconv.Itoa(instanceID),
		"runID":      strconv.FormatInt(run.ID, 10),
	}, map[string]string{
		"format": "tar",
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/x-tar", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, w.Header().Get("Content-Disposition"), ".tar")

	// Verify tar content
	tarReader := tar.NewReader(bytes.NewReader(w.Body.Bytes()))
	files := make(map[string]bool)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		files[header.Name] = true
	}

	assert.True(t, files["manifest.json"])
	assert.True(t, files["Test Torrent 1.torrent"])
	assert.True(t, files["Test Torrent 2.torrent"])
}

func TestDownloadRun_DefaultFormat(t *testing.T) {
	handler, db, dataDir, cleanup := setupTestBackupHandler(t)
	defer cleanup()

	// Create a test instance and successful backup run
	ctx := context.Background()
	result, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (1, 1, 1, 'pass')")
	require.NoError(t, err)
	instanceID64, err := result.LastInsertId()
	require.NoError(t, err)
	instanceID := int(instanceID64)

	run := createTestBackupRun(t, db, dataDir, instanceID)
	createTestTorrentFiles(t, dataDir)

	// Test without format parameter (should default to zip)
	req := newRequestWithParams(http.MethodGet, fmt.Sprintf("/api/instances/%d/backups/runs/%d/download", instanceID, run.ID), map[string]string{
		"instanceID": strconv.Itoa(instanceID),
		"runID":      strconv.FormatInt(run.ID, 10),
	})
	w := httptest.NewRecorder()

	handler.DownloadRun(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/zip", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), ".zip")
}

func TestGetBackupDownloadUrl(t *testing.T) {
	// Test the API URL generation function

	// Mock window.location
	originalLocation := windowLocation
	windowLocation = &url.URL{Scheme: "http", Host: "localhost:7476"}
	defer func() { windowLocation = originalLocation }()

	// Test without format (should not add query param)
	url := getBackupDownloadUrl(1, 123)
	expected := "http://localhost:7476/api/instances/1/backups/runs/123/download"
	assert.Equal(t, expected, url)

	// Test with zip format (should not add query param since it's default)
	url = getBackupDownloadUrl(1, 123, "zip")
	assert.Equal(t, expected, url)

	// Test with other formats
	url = getBackupDownloadUrl(1, 123, "tar.gz")
	expected = "http://localhost:7476/api/instances/1/backups/runs/123/download?format=tar.gz"
	assert.Equal(t, expected, url)

	url = getBackupDownloadUrl(1, 123, "tar.zst")
	expected = "http://localhost:7476/api/instances/1/backups/runs/123/download?format=tar.zst"
	assert.Equal(t, expected, url)
}

// Mock window.location for testing
var windowLocation *url.URL

func getBackupDownloadUrl(instanceId, runId int, format ...string) string {
	u := &url.URL{
		Scheme: windowLocation.Scheme,
		Host:   windowLocation.Host,
		Path:   fmt.Sprintf("/api/instances/%d/backups/runs/%d/download", instanceId, runId),
	}
	if len(format) > 0 && format[0] != "" && format[0] != "zip" {
		q := u.Query()
		q.Set("format", format[0])
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func TestValidateBlobPath(t *testing.T) {
	baseDir := t.TempDir()

	tests := []struct {
		name     string
		blobPath string
		wantSafe bool
	}{
		{
			name:     "valid relative path",
			blobPath: "backups/torrents/ab/cd/test.torrent",
			wantSafe: true,
		},
		{
			name:     "valid simple path",
			blobPath: "test.torrent",
			wantSafe: true,
		},
		{
			name:     "empty path",
			blobPath: "",
			wantSafe: false,
		},
		{
			name:     "traversal with ../",
			blobPath: "../../../etc/passwd",
			wantSafe: false,
		},
		{
			name:     "traversal in middle",
			blobPath: "backups/../../../etc/passwd",
			wantSafe: false,
		},
		{
			name:     "absolute path unix",
			blobPath: "/etc/passwd",
			wantSafe: false,
		},
		{
			name:     "double dot only",
			blobPath: "..",
			wantSafe: false,
		},
		{
			name:     "dot dot slash",
			blobPath: "../",
			wantSafe: false,
		},
		{
			name:     "hidden traversal with dot segments",
			blobPath: "backups/./../../etc/passwd",
			wantSafe: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateBlobPath(baseDir, tt.blobPath)
			if tt.wantSafe {
				assert.NotEmpty(t, result, "expected safe path to return non-empty result")
				assert.True(t, filepath.IsAbs(result), "expected absolute path")
			} else {
				assert.Empty(t, result, "expected unsafe path to return empty result")
			}
		})
	}

	// Test empty baseDir separately (not using shared baseDir)
	t.Run("empty baseDir", func(t *testing.T) {
		result := validateBlobPath("", "some/valid/path.torrent")
		assert.Empty(t, result, "expected empty baseDir to return empty result")
	})
}
