// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

type mockMediaInfoPreferencesAdder struct {
	prefs     qbt.AppPreferences
	prefsErr  error
	prefsCall int
}

type mediaInfoJSONPayload struct {
	Media mediaInfoJSONMedia `json:"media"`
}

type mediaInfoJSONMedia struct {
	Track []json.RawMessage `json:"track"`
}

func (m *mockMediaInfoPreferencesAdder) AddTorrent(context.Context, int, []byte, map[string]string) error {
	return nil
}

func (m *mockMediaInfoPreferencesAdder) AddTorrentFromURLs(context.Context, int, []string, map[string]string) error {
	return nil
}

func (m *mockMediaInfoPreferencesAdder) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	m.prefsCall++
	return m.prefs, m.prefsErr
}

func newContentPathMediaInfoRequest(t *testing.T, instanceID int, contentPath string) *http.Request {
	t.Helper()

	target := "/api/instances/" + strconv.Itoa(instanceID) + "/mediainfo"
	if contentPath != "" {
		target += "?contentPath=" + url.QueryEscape(contentPath)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("instanceID", strconv.Itoa(instanceID))
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func TestGetContentPathMediaInfo_ReturnsServerErrorWithoutInstanceStore(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandlerForTesting(nil, nil)
	rec := httptest.NewRecorder()
	req := newContentPathMediaInfoRequest(t, 1, "folder/file.bin")

	handler.GetContentPathMediaInfo(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "Instance store not configured")
}

func TestGetContentPathMediaInfo_RejectsInvalidInstanceID(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandlerForTesting(nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/instances/not-an-int/mediainfo?contentPath=folder%2Ffile.bin", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("instanceID", "not-an-int")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	handler.GetContentPathMediaInfo(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid instance ID")
}

func TestGetContentPathMediaInfo_ReturnsNotFoundForMissingInstance(t *testing.T) {
	t.Parallel()

	instanceStore, _ := createInstanceStoreWithInstance(t, true)
	handler := &TorrentsHandler{instanceStore: instanceStore, torrentAdder: &mockMediaInfoPreferencesAdder{}}

	rec := httptest.NewRecorder()
	req := newContentPathMediaInfoRequest(t, 999999, "folder/file.bin")

	handler.GetContentPathMediaInfo(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "Instance not found")
}

func TestGetContentPathMediaInfo_ReturnsForbiddenWithoutLocalAccess(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, false)
	mockAdder := &mockMediaInfoPreferencesAdder{}
	handler := &TorrentsHandler{instanceStore: instanceStore, torrentAdder: mockAdder}

	rec := httptest.NewRecorder()
	req := newContentPathMediaInfoRequest(t, instanceID, "folder/file.bin")

	handler.GetContentPathMediaInfo(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "local filesystem access")
	require.Equal(t, 0, mockAdder.prefsCall)
}

func TestGetContentPathMediaInfo_RejectsMissingContentPath(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	mockAdder := &mockMediaInfoPreferencesAdder{}
	handler := &TorrentsHandler{instanceStore: instanceStore, torrentAdder: mockAdder}

	rec := httptest.NewRecorder()
	req := newContentPathMediaInfoRequest(t, instanceID, "")

	handler.GetContentPathMediaInfo(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid content path")
	require.Equal(t, 0, mockAdder.prefsCall)
}

func TestGetContentPathMediaInfo_RejectsInvalidContentPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		contentPath string
	}{
		{name: "traversal", contentPath: "../escape.bin"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
			mockAdder := &mockMediaInfoPreferencesAdder{}
			handler := &TorrentsHandler{instanceStore: instanceStore, torrentAdder: mockAdder}

			rec := httptest.NewRecorder()
			req := newContentPathMediaInfoRequest(t, instanceID, tc.contentPath)

			handler.GetContentPathMediaInfo(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), "Invalid content path")
			require.Equal(t, 0, mockAdder.prefsCall)
		})
	}
}

func TestGetContentPathMediaInfo_RejectsAbsoluteContentPath(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	mockAdder := &mockMediaInfoPreferencesAdder{}
	handler := &TorrentsHandler{instanceStore: instanceStore, torrentAdder: mockAdder}

	absolutePath := filepath.Join(t.TempDir(), "movie.bin")
	rec := httptest.NewRecorder()
	req := newContentPathMediaInfoRequest(t, instanceID, absolutePath)

	handler.GetContentPathMediaInfo(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid content path")
	require.Equal(t, 0, mockAdder.prefsCall)
}

func TestGetContentPathMediaInfo_ReturnsErrorWhenPreferencesUnavailable(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	handler := &TorrentsHandler{instanceStore: instanceStore}

	rec := httptest.NewRecorder()
	req := newContentPathMediaInfoRequest(t, instanceID, "folder/file.bin")

	handler.GetContentPathMediaInfo(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "Failed to get app preferences")
}

func TestGetContentPathMediaInfo_ReturnsBadRequestWhenRootsNotConfigured(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	mockAdder := &mockMediaInfoPreferencesAdder{prefs: qbt.AppPreferences{}}
	handler := &TorrentsHandler{instanceStore: instanceStore, torrentAdder: mockAdder}

	rec := httptest.NewRecorder()
	req := newContentPathMediaInfoRequest(t, instanceID, "folder/file.bin")

	handler.GetContentPathMediaInfo(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "No content roots configured")
	require.Equal(t, 1, mockAdder.prefsCall)
}

func TestGetContentPathMediaInfo_ReturnsNotFoundWhenFileMissingOnDisk(t *testing.T) {
	t.Parallel()

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	root := t.TempDir()
	mockAdder := &mockMediaInfoPreferencesAdder{prefs: qbt.AppPreferences{SavePath: root}}
	handler := &TorrentsHandler{instanceStore: instanceStore, torrentAdder: mockAdder}

	rec := httptest.NewRecorder()
	req := newContentPathMediaInfoRequest(t, instanceID, "folder/file.bin")

	handler.GetContentPathMediaInfo(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "File not found on disk")
	require.Equal(t, 1, mockAdder.prefsCall)
}

func TestGetContentPathMediaInfo_ReturnsSummaryAndJSON(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	relativePath := filepath.Join("movies", "sample.bin")
	fullPath := filepath.Join(root, relativePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte("hello world"), 0o600))

	instanceStore, instanceID := createInstanceStoreWithInstance(t, true)
	mockAdder := &mockMediaInfoPreferencesAdder{prefs: qbt.AppPreferences{SavePath: root}}
	handler := &TorrentsHandler{instanceStore: instanceStore, torrentAdder: mockAdder}

	rec := httptest.NewRecorder()
	req := newContentPathMediaInfoRequest(t, instanceID, filepath.ToSlash(relativePath))

	handler.GetContentPathMediaInfo(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		ContentPath   string               `json:"contentPath"`
		SummaryTxt    string               `json:"summaryTxt"`
		MediaInfoJSON mediaInfoJSONPayload `json:"mediaInfoJson"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, filepath.ToSlash(relativePath), filepath.ToSlash(resp.ContentPath))
	require.NotEmpty(t, resp.SummaryTxt)
	require.NotEmpty(t, resp.MediaInfoJSON.Media.Track)
	require.Equal(t, 1, mockAdder.prefsCall)
}
