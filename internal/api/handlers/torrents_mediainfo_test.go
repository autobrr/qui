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
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func newMediaInfoRequest(t *testing.T, instanceID int, hash, fileIndex string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/instances/"+strconv.Itoa(instanceID)+"/torrents/"+hash+"/files/"+fileIndex+"/mediainfo", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("instanceID", strconv.Itoa(instanceID))
	routeCtx.URLParams.Add("hash", hash)
	routeCtx.URLParams.Add("fileIndex", fileIndex)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func TestGetTorrentFileMediaInfo_ReturnsServerErrorWithoutInstanceStore(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandlerForTesting(nil, nil)
	rec := httptest.NewRecorder()
	req := newMediaInfoRequest(t, 1, "hash123", "0")

	handler.GetTorrentFileMediaInfo(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "Instance store not configured")
}

func TestGetTorrentFileMediaInfo_RejectsInvalidFileIndex(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		fileIndex string
	}{
		{name: "negative", fileIndex: "-1"},
		{name: "not_integer", fileIndex: "abc"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
			resolver := &mockContentResolver{}
			handler := &TorrentsHandler{
				instanceStore:   instanceStore,
				contentResolver: resolver,
			}

			rec := httptest.NewRecorder()
			req := newMediaInfoRequest(t, instanceID, "hash123", tc.fileIndex)

			handler.GetTorrentFileMediaInfo(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), "Invalid file index")
			require.Equal(t, 0, resolver.filesCalls)
		})
	}
}

func TestGetTorrentFileMediaInfo_ReturnsNotFoundForMissingInstance(t *testing.T) {
	t.Parallel()

	instanceStore, _ := createInstanceStoreWithInstance(t, true)
	resolver := &mockContentResolver{}
	handler := &TorrentsHandler{
		instanceStore:   instanceStore,
		contentResolver: resolver,
	}

	rec := httptest.NewRecorder()
	req := newMediaInfoRequest(t, 999999, "hash123", "0")

	handler.GetTorrentFileMediaInfo(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "Instance not found")
	require.Equal(t, 0, resolver.filesCalls)
}

func TestGetTorrentFileMediaInfo_ReturnsForbiddenWithoutLocalAccess(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, false)
	resolver := &mockContentResolver{}
	handler := &TorrentsHandler{
		instanceStore:   instanceStore,
		contentResolver: resolver,
	}

	rec := httptest.NewRecorder()
	req := newMediaInfoRequest(t, instanceID, "hash123", "0")

	handler.GetTorrentFileMediaInfo(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "local filesystem access")
	require.Equal(t, 0, resolver.filesCalls)
}

func TestGetTorrentFileMediaInfo_ReturnsNotFoundForUnknownFileIndex(t *testing.T) {
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
	req := newMediaInfoRequest(t, instanceID, "hash123", "9")

	handler.GetTorrentFileMediaInfo(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "File index not found")
	require.Equal(t, 1, resolver.filesCalls)
	require.Equal(t, 0, resolver.propsCalls)
}

func TestGetTorrentFileMediaInfo_RejectsTraversalPaths(t *testing.T) {
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
	req := newMediaInfoRequest(t, instanceID, "hash123", "5")

	handler.GetTorrentFileMediaInfo(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid file path")
}

func TestGetTorrentFileMediaInfo_ReturnsNotFoundWhenFileMissingOnDisk(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	files := qbt.TorrentFiles{
		{Index: 2, Name: "movie.bin"},
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
	req := newMediaInfoRequest(t, instanceID, "hash123", "2")

	handler.GetTorrentFileMediaInfo(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "File not found on disk")
}

func TestGetTorrentFileMediaInfo_ReturnsReport(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	baseDir := t.TempDir()
	relativePath := "folder/file.bin"
	fullPath := filepath.Join(baseDir, relativePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte("hello world"), 0o600))

	files := qbt.TorrentFiles{{Index: 3, Name: relativePath}}
	resolver := &mockContentResolver{
		files:      &files,
		properties: &qbt.TorrentProperties{SavePath: baseDir},
	}
	handler := &TorrentsHandler{
		instanceStore:   instanceStore,
		contentResolver: resolver,
	}

	rec := httptest.NewRecorder()
	req := newMediaInfoRequest(t, instanceID, "hash123", "3")

	handler.GetTorrentFileMediaInfo(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		FileIndex    int    `json:"fileIndex"`
		RelativePath string `json:"relativePath"`
		Streams      []struct {
			Kind   string `json:"kind"`
			Fields []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"fields"`
		} `json:"streams"`
		RawJSON string `json:"rawJSON"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 3, resp.FileIndex)
	require.Equal(t, relativePath, resp.RelativePath)
	require.NotEmpty(t, resp.Streams)
	require.Equal(t, "General", resp.Streams[0].Kind)
	require.NotEmpty(t, resp.Streams[0].Fields)
	require.True(t, json.Valid([]byte(resp.RawJSON)))
}
