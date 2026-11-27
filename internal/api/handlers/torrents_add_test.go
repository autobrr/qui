// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
)

type addTorrentCall struct {
	instanceID  int
	fileContent []byte
	options     map[string]string
}

type addTorrentFromURLsCall struct {
	instanceID int
	urls       []string
	options    map[string]string
}

type jackettResponse struct {
	data []byte
	err  error
}

func TestAddTorrentHandler_InvalidIndexerID_Returns400(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandler(nil, nil, nil, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", "http://example.com/torrent.torrent")
	_ = writer.WriteField("indexer_id", "not-a-number")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid indexer_id")
	assert.Contains(t, w.Body.String(), "not-a-number")
}

func TestAddTorrentHandler_ZeroIndexerID_Returns400(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandler(nil, nil, nil, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", "http://example.com/torrent.torrent")
	_ = writer.WriteField("indexer_id", "0")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "must be a positive integer")
}

func TestAddTorrentHandler_JackettServiceUnavailable_Returns503(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandler(nil, nil, nil, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", "http://example.com/torrent.torrent")
	_ = writer.WriteField("indexer_id", "42")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "Indexer service is not available")
}

func TestAddTorrentHandler_NoURLsOrFiles_Returns400(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandler(nil, nil, nil, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Either torrent files or URLs are required")
}

func TestAddTorrentHandler_InvalidInstanceID_Returns400(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandler(nil, nil, nil, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", "http://example.com/torrent.torrent")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/invalid/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "invalid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid instance ID")
}

type fullMockSyncManager struct {
	addTorrentCalls         []addTorrentCall
	addTorrentFromURLsCalls []addTorrentFromURLsCall
	addTorrentErr           error
	addTorrentFromURLsErr   error
	addTorrentFromURLsFunc  func(urls []string) (*qbt.TorrentAddResponse, error)
	preferencesJSON         string
}

func (m *fullMockSyncManager) AddTorrent(_ context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	m.addTorrentCalls = append(m.addTorrentCalls, addTorrentCall{
		instanceID:  instanceID,
		fileContent: fileContent,
		options:     options,
	})
	return nil, m.addTorrentErr
}

func (m *fullMockSyncManager) AddTorrentFromURLs(_ context.Context, instanceID int, urls []string, options map[string]string) (*qbt.TorrentAddResponse, error) {
	m.addTorrentFromURLsCalls = append(m.addTorrentFromURLsCalls, addTorrentFromURLsCall{
		instanceID: instanceID,
		urls:       urls,
		options:    options,
	})
	if m.addTorrentFromURLsFunc != nil {
		return m.addTorrentFromURLsFunc(urls)
	}
	return nil, m.addTorrentFromURLsErr
}

func (m *fullMockSyncManager) GetAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error) {
	var prefs qbt.AppPreferences
	if m.preferencesJSON == "" {
		return prefs, nil
	}
	err := json.Unmarshal([]byte(m.preferencesJSON), &prefs)
	return prefs, err
}

type fullMockJackettService struct {
	downloadTorrentCalls []jackett.TorrentDownloadRequest
	downloadTorrentData  []byte
	downloadTorrentErr   error
}

func (m *fullMockJackettService) DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error) {
	m.downloadTorrentCalls = append(m.downloadTorrentCalls, req)
	return m.downloadTorrentData, m.downloadTorrentErr
}

func TestAddTorrentHandler_SuccessfulIndexerDownload_Returns201(t *testing.T) {
	t.Parallel()

	mockSync := &fullMockSyncManager{}
	mockJackett := &fullMockJackettService{
		downloadTorrentData: []byte("fake torrent data"),
	}

	handler := &TorrentsHandler{torrentAdder: mockSync, torrentDownloader: mockJackett}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", "http://indexer.example.com/download/123")
	_ = writer.WriteField("indexer_id", "42")
	_ = writer.WriteField("category", "movies")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":1`)
	assert.Contains(t, w.Body.String(), `"failed":0`)

	require.Len(t, mockJackett.downloadTorrentCalls, 1)
	assert.Equal(t, 42, mockJackett.downloadTorrentCalls[0].IndexerID)
	assert.Equal(t, "http://indexer.example.com/download/123", mockJackett.downloadTorrentCalls[0].DownloadURL)

	require.Len(t, mockSync.addTorrentCalls, 1)
	assert.Equal(t, 1, mockSync.addTorrentCalls[0].instanceID)
	assert.Equal(t, []byte("fake torrent data"), mockSync.addTorrentCalls[0].fileContent)
	assert.Equal(t, "movies", mockSync.addTorrentCalls[0].options["category"])

	assert.Empty(t, mockSync.addTorrentFromURLsCalls)
}

func TestAddTorrentHandler_SuccessfulMagnetWithIndexer_Returns201(t *testing.T) {
	t.Parallel()

	mockSync := &fullMockSyncManager{}
	mockJackett := &fullMockJackettService{
		downloadTorrentData: []byte("should not be used"),
	}

	handler := &TorrentsHandler{torrentAdder: mockSync, torrentDownloader: mockJackett}

	magnetURL := "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678"
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", magnetURL)
	_ = writer.WriteField("indexer_id", "42")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":1`)
	assert.Contains(t, w.Body.String(), `"failed":0`)

	assert.Empty(t, mockJackett.downloadTorrentCalls)

	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, []string{magnetURL}, mockSync.addTorrentFromURLsCalls[0].urls)

	assert.Empty(t, mockSync.addTorrentCalls)
}

func TestAddTorrentHandler_MixedURLsAndMagnets_Returns201(t *testing.T) {
	t.Parallel()

	mockSync := &fullMockSyncManager{}
	mockJackett := &fullMockJackettService{
		downloadTorrentData: []byte("downloaded torrent data"),
	}

	handler := &TorrentsHandler{torrentAdder: mockSync, torrentDownloader: mockJackett}

	magnetURL := "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678"
	httpURL := "http://indexer.example.com/download/456"
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", magnetURL+"\n"+httpURL)
	_ = writer.WriteField("indexer_id", "99")
	_ = writer.Close()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/instances/2/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "2")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":2`)
	assert.Contains(t, w.Body.String(), `"failed":0`)

	require.Len(t, mockJackett.downloadTorrentCalls, 1)
	assert.Equal(t, 99, mockJackett.downloadTorrentCalls[0].IndexerID)
	assert.Equal(t, httpURL, mockJackett.downloadTorrentCalls[0].DownloadURL)

	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, []string{magnetURL}, mockSync.addTorrentFromURLsCalls[0].urls)

	require.Len(t, mockSync.addTorrentCalls, 1)
	assert.Equal(t, []byte("downloaded torrent data"), mockSync.addTorrentCalls[0].fileContent)
}

func TestAddTorrentHandler_MagnetWithIndexerPartialFailure_Returns201WithFailedURLs(t *testing.T) {
	t.Parallel()

	magnetURL := "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678"
	httpURL := "http://indexer.example.com/download/456"
	mockSync := &fullMockSyncManager{
		addTorrentFromURLsFunc: func(urls []string) (*qbt.TorrentAddResponse, error) {
			if urls[0] == magnetURL {
				return &qbt.TorrentAddResponse{FailureCount: 1}, nil
			}
			return &qbt.TorrentAddResponse{SuccessCount: 1}, nil
		},
	}
	mockJackett := &fullMockJackettService{
		downloadTorrentData: []byte("downloaded torrent data"),
	}

	handler := &TorrentsHandler{torrentAdder: mockSync, torrentDownloader: mockJackett}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", magnetURL+"\n"+httpURL)
	_ = writer.WriteField("indexer_id", "99")
	_ = writer.Close()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/instances/2/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "2")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":1`)
	assert.Contains(t, w.Body.String(), `"failed":1`)
	assert.Contains(t, w.Body.String(), `"failedURLs"`)
	assert.Contains(t, w.Body.String(), magnetURL)
	assert.Contains(t, w.Body.String(), "qBittorrent rejected torrent URL")

	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, []string{magnetURL}, mockSync.addTorrentFromURLsCalls[0].urls)
	require.Len(t, mockSync.addTorrentCalls, 1)
	assert.Equal(t, []byte("downloaded torrent data"), mockSync.addTorrentCalls[0].fileContent)
}

func TestAddTorrentHandler_MagnetRedirectPartialFailure_Returns201WithFailedURLs(t *testing.T) {
	t.Parallel()

	sourceURL := "http://indexer.example.com/download/magnet"
	successURL := "http://indexer.example.com/download/success"
	magnetURL := "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678"
	mockSync := &fullMockSyncManager{
		addTorrentFromURLsFunc: func(urls []string) (*qbt.TorrentAddResponse, error) {
			if urls[0] == magnetURL {
				return &qbt.TorrentAddResponse{FailureCount: 1}, nil
			}
			return &qbt.TorrentAddResponse{SuccessCount: 1}, nil
		},
	}
	mockJackett := &customMockJackettServiceForHandler{
		responses: []jackettResponse{
			{err: &jackett.MagnetDownloadError{MagnetURL: magnetURL}},
			{data: []byte("success torrent data")},
		},
	}

	handler := &TorrentsHandler{torrentAdder: mockSync, torrentDownloader: mockJackett}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", sourceURL+"\n"+successURL)
	_ = writer.WriteField("indexer_id", "99")
	_ = writer.Close()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/instances/2/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "2")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":1`)
	assert.Contains(t, w.Body.String(), `"failed":1`)
	assert.Contains(t, w.Body.String(), `"failedURLs"`)
	assert.Contains(t, w.Body.String(), magnetURL)
	assert.Contains(t, w.Body.String(), "qBittorrent rejected torrent URL")

	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, []string{magnetURL}, mockSync.addTorrentFromURLsCalls[0].urls)
	require.Len(t, mockSync.addTorrentCalls, 1)
	assert.Equal(t, []byte("success torrent data"), mockSync.addTorrentCalls[0].fileContent)
}

func TestAddTorrentHandler_PartialFailure_Returns201WithFailedURLs(t *testing.T) {
	t.Parallel()

	mockSync := &fullMockSyncManager{}
	mockJackett := &customMockJackettServiceForHandler{
		responses: []jackettResponse{
			{err: errors.New("indexer unavailable")},
			{data: []byte("success torrent data")},
		},
	}

	handler := &TorrentsHandler{torrentAdder: mockSync, torrentDownloader: mockJackett}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", "http://fail.example.com/1\nhttp://success.example.com/2")
	_ = writer.WriteField("indexer_id", "1")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":1`)
	assert.Contains(t, w.Body.String(), `"failed":1`)
	assert.Contains(t, w.Body.String(), `"failedURLs"`)
	assert.Contains(t, w.Body.String(), "http://fail.example.com/1")
	assert.Contains(t, w.Body.String(), "indexer unavailable")

	assert.Len(t, mockJackett.calls, 2)

	require.Len(t, mockSync.addTorrentCalls, 1)
	assert.Equal(t, []byte("success torrent data"), mockSync.addTorrentCalls[0].fileContent)
}

func TestAddTorrentHandler_DirectMultiURLPartialFailure_Returns201WithFailedURLs(t *testing.T) {
	t.Parallel()

	failURL := "http://tracker.example.com/fail.torrent"
	successURL := "http://tracker.example.com/success.torrent"
	mockSync := &fullMockSyncManager{
		addTorrentFromURLsFunc: func(urls []string) (*qbt.TorrentAddResponse, error) {
			if urls[0] == failURL {
				return &qbt.TorrentAddResponse{
					FailureCount: 1,
				}, nil
			}
			return &qbt.TorrentAddResponse{SuccessCount: 1}, nil
		},
	}

	handler := &TorrentsHandler{torrentAdder: mockSync}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", failURL+"\n"+successURL)
	_ = writer.Close()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":1`)
	assert.Contains(t, w.Body.String(), `"failed":1`)
	assert.Contains(t, w.Body.String(), `"failedURLs"`)
	assert.Contains(t, w.Body.String(), failURL)
	assert.Contains(t, w.Body.String(), "qBittorrent rejected torrent URL")

	require.Len(t, mockSync.addTorrentFromURLsCalls, 2)
	assert.Equal(t, []string{failURL}, mockSync.addTorrentFromURLsCalls[0].urls)
	assert.Equal(t, []string{successURL}, mockSync.addTorrentFromURLsCalls[1].urls)
}

type customMockJackettServiceForHandler struct {
	calls     []jackett.TorrentDownloadRequest
	responses []jackettResponse
	callIndex int
}

func (m *customMockJackettServiceForHandler) DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error) {
	m.calls = append(m.calls, req)
	if m.callIndex < len(m.responses) {
		resp := m.responses[m.callIndex]
		m.callIndex++
		return resp.data, resp.err
	}
	return nil, errors.New("no more responses configured")
}

func TestAddTorrentHandler_NoIndexerID_UsesDirectURL(t *testing.T) {
	t.Parallel()

	mockSync := &fullMockSyncManager{}
	mockJackett := &fullMockJackettService{
		downloadTorrentData: []byte("should not be used"),
	}

	handler := &TorrentsHandler{torrentAdder: mockSync, torrentDownloader: mockJackett}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", "http://example.com/torrent.torrent")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":1`)

	assert.Empty(t, mockJackett.downloadTorrentCalls)

	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, []string{"http://example.com/torrent.torrent"}, mockSync.addTorrentFromURLsCalls[0].urls)

	assert.Empty(t, mockSync.addTorrentCalls)
}

func TestAddTorrentHandler_StartPausedChoiceApplies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		preferences string
		paused      string
		wantPaused  string
	}{
		{
			name:        "5.x adds stopped, request starts",
			preferences: `{"add_stopped_enabled":true}`,
			paused:      "false",
			wantPaused:  "false",
		},
		{
			name:        "4.x adds stopped, request starts",
			preferences: `{"start_paused_enabled":true}`,
			paused:      "false",
			wantPaused:  "false",
		},
		{
			name:        "5.x adds started, request pauses",
			preferences: `{"add_stopped_enabled":false}`,
			paused:      "true",
			wantPaused:  "true",
		},
		{
			name:        "5.3 session start_paused is not the add preference",
			preferences: `{"add_stopped_enabled":false,"start_paused":true}`,
			paused:      "true",
			wantPaused:  "true",
		},
		{
			name:        "request matches the kept preference, choice still sent",
			preferences: `{"add_stopped_enabled":true}`,
			paused:      "true",
			wantPaused:  "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockSync := &fullMockSyncManager{preferencesJSON: tt.preferences}
			handler := &TorrentsHandler{torrentAdder: mockSync}

			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			_ = writer.WriteField("urls", "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678")
			_ = writer.WriteField("paused", tt.paused)
			_ = writer.Close()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/instances/1/torrents", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("instanceID", "1")
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			w := httptest.NewRecorder()
			handler.AddTorrent(w, req)

			require.Equal(t, http.StatusCreated, w.Code)
			require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
			options := mockSync.addTorrentFromURLsCalls[0].options
			assert.Equal(t, tt.wantPaused, options["paused"])
			assert.Equal(t, tt.wantPaused, options["stopped"])
		})
	}
}

func TestAddTorrentHandler_TooManyFormParts_NamesThePartLimit(t *testing.T) {
	t.Parallel()

	mockSync := &fullMockSyncManager{}
	handler := &TorrentsHandler{torrentAdder: mockSync}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for i := range 996 {
		part, err := writer.CreateFormFile("torrent", fmt.Sprintf("Example.Show.S01E%04d.torrent", i))
		require.NoError(t, err)
		_, err = part.Write([]byte("d4:infod4:name1:xee"))
		require.NoError(t, err)
	}
	for _, field := range []string{"paused", "autoTMM", "skip_checking", "sequentialDownload", "firstLastPiecePrio"} {
		require.NoError(t, writer.WriteField(field, "false"))
	}
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Contains(t, w.Body.String(), "1000 files and fields")
	assert.NotContains(t, w.Body.String(), "MB")
	assert.Empty(t, mockSync.addTorrentCalls)
}

type failSecondFileAdder struct {
	fullMockSyncManager
}

func (m *failSecondFileAdder) AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	_, _ = m.fullMockSyncManager.AddTorrent(ctx, instanceID, fileContent, options)
	if len(m.addTorrentCalls) == 2 {
		return nil, errors.New("torrent file is not valid")
	}
	return nil, nil
}

func TestAddTorrentHandler_FailedFile_NamesTheUploadedFile(t *testing.T) {
	t.Parallel()

	handler := &TorrentsHandler{torrentAdder: &failSecondFileAdder{}}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for _, name := range []string{"Example.Show.S01E01.torrent", "Example.Show.S01E02.torrent", "Example.Show.S01E03.torrent"} {
		part, err := writer.CreateFormFile("torrent", name)
		require.NoError(t, err)
		_, err = part.Write([]byte("d4:infod4:name1:xee"))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"failedFiles":[{"filename":"Example.Show.S01E02.torrent","error":"torrent file is not valid"}]`)
}
