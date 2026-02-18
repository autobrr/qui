// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

type mockContentResolver struct {
	files         *qbt.TorrentFiles
	filesErr      error
	properties    *qbt.TorrentProperties
	propertiesErr error
	torrents      []qbt.Torrent
	torrentsErr   error
	filesCalls    int
	propsCalls    int
	torrentsCalls int
}

func (m *mockContentResolver) GetTorrentFiles(_ context.Context, _ int, _ string) (*qbt.TorrentFiles, error) {
	m.filesCalls++
	return m.files, m.filesErr
}

func (m *mockContentResolver) GetTorrentProperties(_ context.Context, _ int, _ string) (*qbt.TorrentProperties, error) {
	m.propsCalls++
	return m.properties, m.propertiesErr
}

func (m *mockContentResolver) GetTorrents(_ context.Context, _ int, _ qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	m.torrentsCalls++
	return m.torrents, m.torrentsErr
}

func createInstanceStoreWithInstance(t *testing.T, hasLocalAccess bool) (*models.InstanceStore, int) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	instance, err := instanceStore.Create(
		t.Context(),
		"test-instance",
		"http://localhost:8080",
		"admin",
		"admin",
		nil,
		nil,
		false,
		&hasLocalAccess,
	)
	require.NoError(t, err)

	return instanceStore, instance.ID
}

func newDownloadRequest(t *testing.T, instanceID int, hash, fileIndex string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/instances/"+strconv.Itoa(instanceID)+"/torrents/"+hash+"/files/"+fileIndex+"/download", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("instanceID", strconv.Itoa(instanceID))
	routeCtx.URLParams.Add("hash", hash)
	routeCtx.URLParams.Add("fileIndex", fileIndex)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func TestDownloadTorrentContentFile_ReturnsForbiddenWithoutLocalAccess(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, false)
	resolver := &mockContentResolver{}
	handler := &TorrentsHandler{
		instanceStore:   instanceStore,
		contentResolver: resolver,
	}

	rec := httptest.NewRecorder()
	req := newDownloadRequest(t, instanceID, "hash123", "0")

	handler.DownloadTorrentContentFile(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "local filesystem access")
	require.Equal(t, 0, resolver.filesCalls)
}

func TestDownloadTorrentContentFile_ReturnsNotFoundForUnknownFileIndex(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	files := qbt.TorrentFiles{
		{Index: 1, Name: "known.mkv"},
	}
	resolver := &mockContentResolver{files: &files}
	handler := &TorrentsHandler{
		instanceStore:   instanceStore,
		contentResolver: resolver,
	}

	rec := httptest.NewRecorder()
	req := newDownloadRequest(t, instanceID, "hash123", "9")

	handler.DownloadTorrentContentFile(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "File index not found")
	require.Equal(t, 1, resolver.filesCalls)
	require.Equal(t, 0, resolver.propsCalls)
}

func TestDownloadTorrentContentFile_RejectsTraversalPaths(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	files := qbt.TorrentFiles{
		{Index: 5, Name: "../escape.txt"},
	}
	resolver := &mockContentResolver{
		files:      &files,
		properties: &qbt.TorrentProperties{SavePath: "/downloads"},
	}
	handler := &TorrentsHandler{
		instanceStore:   instanceStore,
		contentResolver: resolver,
	}

	rec := httptest.NewRecorder()
	req := newDownloadRequest(t, instanceID, "hash123", "5")

	handler.DownloadTorrentContentFile(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid file path")
}

func TestDownloadTorrentContentFile_ReturnsNotFoundWhenFileMissingOnDisk(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	files := qbt.TorrentFiles{
		{Index: 2, Name: "movie.txt"},
	}
	resolver := &mockContentResolver{
		files:      &files,
		properties: &qbt.TorrentProperties{SavePath: t.TempDir()},
	}
	handler := &TorrentsHandler{
		instanceStore:   instanceStore,
		contentResolver: resolver,
	}

	rec := httptest.NewRecorder()
	req := newDownloadRequest(t, instanceID, "hash123", "2")

	handler.DownloadTorrentContentFile(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "File not found on disk")
}

func TestDownloadTorrentContentFile_StreamsFile(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	baseDir := t.TempDir()
	relativePath := "folder/file.txt"
	fullPath := filepath.Join(baseDir, relativePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte("hello world"), 0o600))

	files := qbt.TorrentFiles{
		{Index: 3, Name: relativePath},
	}
	resolver := &mockContentResolver{
		files:      &files,
		properties: &qbt.TorrentProperties{SavePath: baseDir},
	}
	handler := &TorrentsHandler{
		instanceStore:   instanceStore,
		contentResolver: resolver,
	}

	rec := httptest.NewRecorder()
	req := newDownloadRequest(t, instanceID, "hash123", "3")

	handler.DownloadTorrentContentFile(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	require.Contains(t, rec.Header().Get("Content-Disposition"), "file.txt")
	require.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
	require.Equal(t, "hello world", rec.Body.String())
}

func TestFilePathCandidates_ContentPathSingleFileFallback(t *testing.T) {
	t.Parallel()

	savePath := "/downloads/tv"
	relativePath := "Show.S01E01.mkv"
	contentPath := "/downloads/tv/Show.S01E01/Show.S01E01.mkv"

	candidates := filePathCandidates(savePath, "", contentPath, relativePath, true)

	require.Contains(t, candidates, filepath.Clean(filepath.Join(savePath, relativePath)))
	require.Contains(t, candidates, filepath.Clean(contentPath))
	require.Contains(t, candidates, filepath.Clean(filepath.Join(filepath.Dir(contentPath), relativePath)))
}

func TestFilePathCandidates_ContentPathMultiFileFallback(t *testing.T) {
	t.Parallel()

	savePath := "/downloads"
	relativePath := "Show.S01/Show.S01E01.mkv"
	contentPath := "/downloads/Show.S01"

	candidates := filePathCandidates(savePath, "", contentPath, relativePath, false)

	require.Contains(t, candidates, filepath.Clean(filepath.Join(savePath, relativePath)))
	require.Contains(t, candidates, filepath.Clean(filepath.Join(contentPath, relativePath)))
}

func TestFilePathCandidates_DeduplicatesEquivalentPaths(t *testing.T) {
	t.Parallel()

	savePath := "/downloads"
	relativePath := "Movie.mkv"
	contentPath := "/downloads/Movie.mkv"

	candidates := filePathCandidates(savePath, "", contentPath, relativePath, true)

	want := filepath.Clean("/downloads/Movie.mkv")
	count := 0
	for _, candidate := range candidates {
		if candidate == want {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestFilePathCandidates_UsesDownloadPath(t *testing.T) {
	t.Parallel()

	savePath := "/downloads"
	downloadPath := "/tmp/incomplete"
	relativePath := "Show.S01/Show.S01E01.mkv"
	contentPath := "/downloads/Show.S01"

	candidates := filePathCandidates(savePath, downloadPath, contentPath, relativePath, false)

	require.GreaterOrEqual(t, len(candidates), 3)
	contentCandidate := filepath.Clean(filepath.Join(contentPath, relativePath))
	saveCandidate := filepath.Clean(filepath.Join(savePath, relativePath))
	downloadCandidate := filepath.Clean(filepath.Join(downloadPath, relativePath))
	contentIdx := slices.Index(candidates, contentCandidate)
	saveIdx := slices.Index(candidates, saveCandidate)
	downloadIdx := slices.Index(candidates, downloadCandidate)
	require.NotEqual(t, -1, contentIdx)
	require.NotEqual(t, -1, saveIdx)
	require.NotEqual(t, -1, downloadIdx)
	require.Less(t, contentIdx, saveIdx)
	require.Less(t, saveIdx, downloadIdx)
}
