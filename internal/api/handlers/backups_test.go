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
	"io/fs"
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
	"github.com/autobrr/qui/internal/testutil/testdb"
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

func setupTestBackupHandler(t *testing.T) (*BackupsHandler, *database.DB, string) {
	t.Helper()

	// Create test database
	db := testdb.NewMigratedSQLite(t, "backups-handler")

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

	return handler, db, dataDir
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
	require.NoError(t, os.WriteFile(absManifestPath, manifestData, 0o600))

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

	require.NoError(t, os.WriteFile(file1, testData, 0o600))
	require.NoError(t, os.WriteFile(file2, testData, 0o600))
}

func TestDownloadRun_InvalidInstanceID(t *testing.T) {
	handler, _, _ := setupTestBackupHandler(t)

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
	handler, _, _ := setupTestBackupHandler(t)

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
	handler, _, _ := setupTestBackupHandler(t)

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
	handler, db, _ := setupTestBackupHandler(t)

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
	handler, db, dataDir := setupTestBackupHandler(t)

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
	handler, db, dataDir := setupTestBackupHandler(t)

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
	handler, db, dataDir := setupTestBackupHandler(t)

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
	handler, db, dataDir := setupTestBackupHandler(t)

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
	handler, db, dataDir := setupTestBackupHandler(t)

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
	handler, db, dataDir := setupTestBackupHandler(t)

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
	handler, db, dataDir := setupTestBackupHandler(t)

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
	handler, db, dataDir := setupTestBackupHandler(t)

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
	url := getBackupDownloadURL(1, 123)
	expected := "http://localhost:7476/api/instances/1/backups/runs/123/download"
	assert.Equal(t, expected, url)

	// Test with zip format (should not add query param since it's default)
	url = getBackupDownloadURL(1, 123, "zip")
	assert.Equal(t, expected, url)

	// Test with other formats
	url = getBackupDownloadURL(1, 123, "tar.gz")
	expected = "http://localhost:7476/api/instances/1/backups/runs/123/download?format=tar.gz"
	assert.Equal(t, expected, url)

	url = getBackupDownloadURL(1, 123, "tar.zst")
	expected = "http://localhost:7476/api/instances/1/backups/runs/123/download?format=tar.zst"
	assert.Equal(t, expected, url)
}

// Mock window.location for testing
var windowLocation *url.URL

func getBackupDownloadURL(instanceID, runID int, format ...string) string {
	u := &url.URL{
		Scheme: windowLocation.Scheme,
		Host:   windowLocation.Host,
		Path:   fmt.Sprintf("/api/instances/%d/backups/runs/%d/download", instanceID, runID),
	}
	if len(format) > 0 && format[0] != "" && format[0] != "zip" {
		q := u.Query()
		q.Set("format", format[0])
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// Archive entry names are attacker-controlled, so the check must reject POSIX
// and Windows escapes regardless of which OS the extraction runs on.
func TestSafeArchiveEntryPath(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		want  string
	}{
		{"plain name", "example.torrent", "example.torrent"},
		{"nested name", "torrents/example.torrent", filepath.Join("torrents", "example.torrent")},
		{"dot segments that stay inside", "torrents/./example.torrent", filepath.Join("torrents", "example.torrent")},
		{"posix traversal", "../../etc/passwd", ""},
		{"posix traversal mid-path", "torrents/../../etc/passwd", ""},
		{"posix absolute", "/etc/passwd", ""},
		{"windows traversal", `..\..\Windows\System32\evil.torrent`, ""},
		{"windows absolute", `C:\Windows\System32\evil.torrent`, ""},
		{"windows unc", `\\server\share\evil.torrent`, ""},
		{"empty", "   ", ""},
		{"bare dot", ".", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, safeArchiveEntryPath(tt.entry))
		})
	}
}

// archiveEntry is one file to put in a test archive.
type archiveEntry struct {
	name string
	body []byte
}

func buildTestZip(t *testing.T, entries []archiveEntry) string {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range entries {
		w, err := zw.Create(entry.name)
		require.NoError(t, err)
		_, err = w.Write(entry.body)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())

	archivePath := filepath.Join(t.TempDir(), "backup.zip")
	require.NoError(t, os.WriteFile(archivePath, buf.Bytes(), 0o600))
	return archivePath
}

func buildTestTar(t *testing.T, entries []archiveEntry) string {
	t.Helper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, entry := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     entry.name,
			Mode:     0o600,
			Size:     int64(len(entry.body)),
			Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write(entry.body)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())

	archivePath := filepath.Join(t.TempDir(), "backup.tar")
	require.NoError(t, os.WriteFile(archivePath, buf.Bytes(), 0o600))
	return archivePath
}

// An archive whose entries resolve to one destination is ambiguous about which
// payload belongs to which manifest item, so extraction refuses it rather than
// letting the later entry replace the earlier one. Case-only differences count:
// they are the same file on Windows and on a default macOS volume, and an
// archive that imports on one host and not another is worse than a refusal.
func TestExtractRejectsCollidingEntryNames(t *testing.T) {
	manifest := archiveEntry{name: "manifest.json", body: []byte(`{"items":[]}`)}

	tests := []struct {
		name    string
		entries []archiveEntry
		wantErr string
	}{
		{
			name: "slash style",
			entries: []archiveEntry{
				manifest,
				{name: "a/b.torrent", body: []byte("alpha")},
				{name: `a\b.torrent`, body: []byte("beta")},
			},
			wantErr: "extract to the same path",
		},
		{
			name: "case only",
			entries: []archiveEntry{
				manifest,
				{name: "a/b.torrent", body: []byte("alpha")},
				{name: "a/B.torrent", body: []byte("beta")},
			},
			wantErr: "extract to the same path",
		},
		{
			name: "duplicate manifests",
			entries: []archiveEntry{
				{name: "a/manifest.json", body: []byte(`{"items":[{"hash":"alpha"}]}`)},
				{name: "b/manifest.json", body: []byte(`{"items":[{"hash":"beta"}]}`)},
			},
			wantErr: "more than one manifest.json",
		},
		{
			name: "duplicate manifests across slash styles",
			entries: []archiveEntry{
				{name: "a/manifest.json", body: []byte(`{"items":[{"hash":"alpha"}]}`)},
				{name: `b\manifest.json`, body: []byte(`{"items":[{"hash":"beta"}]}`)},
			},
			wantErr: "more than one manifest.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("zip", func(t *testing.T) {
				_, err := extractZipToDisk(buildTestZip(t, tt.entries))
				require.ErrorContains(t, err, tt.wantErr)
			})

			t.Run("tar", func(t *testing.T) {
				_, err := extractTarToDisk(buildTestTar(t, tt.entries))
				require.ErrorContains(t, err, tt.wantErr)
			})
		})
	}
}

// A valid archive still extracts: the guards above must not reject entries that
// only look similar.
func TestExtractAcceptsDistinctEntryNames(t *testing.T) {
	entries := []archiveEntry{
		{name: "manifest.json", body: []byte(`{"items":[]}`)},
		{name: "a/one.torrent", body: []byte("one")},
		{name: "b/two.torrent", body: []byte("two")},
	}

	t.Run("zip", func(t *testing.T) {
		extracted, err := extractZipToDisk(buildTestZip(t, entries))
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(extracted.TempDir) })
		assert.Len(t, extracted.TorrentPaths, 2)
	})

	t.Run("tar", func(t *testing.T) {
		extracted, err := extractTarToDisk(buildTestTar(t, entries))
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(extracted.TempDir) })
		assert.Len(t, extracted.TorrentPaths, 2)
	})
}

// The claim map catches destinations that collide as strings, case folded. The
// writers are the backstop for whatever else a filesystem folds together, so
// they must refuse an existing destination rather than truncate it.
func TestExtractionWritersRefuseExistingDestination(t *testing.T) {
	t.Run("copyStreamToFile", func(t *testing.T) {
		destPath := filepath.Join(t.TempDir(), "one.torrent")
		require.NoError(t, os.WriteFile(destPath, []byte("first"), 0o600))

		err := copyStreamToFile(bytes.NewReader([]byte("second")), destPath)
		require.ErrorIs(t, err, fs.ErrExist)

		kept, err := os.ReadFile(destPath)
		require.NoError(t, err)
		assert.Equal(t, "first", string(kept), "the existing file must survive")
	})

	t.Run("extractZipFileToDisk", func(t *testing.T) {
		archivePath := buildTestZip(t, []archiveEntry{{name: "one.torrent", body: []byte("second")}})
		reader, err := zip.OpenReader(archivePath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = reader.Close() })

		destPath := filepath.Join(t.TempDir(), "one.torrent")
		require.NoError(t, os.WriteFile(destPath, []byte("first"), 0o600))

		require.ErrorIs(t, extractZipFileToDisk(reader.File[0], destPath), fs.ErrExist)

		kept, err := os.ReadFile(destPath)
		require.NoError(t, err)
		assert.Equal(t, "first", string(kept), "the existing file must survive")
	})
}
