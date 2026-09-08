// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"errors"
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

// =============================================================================
// HTTP Handler Integration Tests
// =============================================================================
// These tests exercise the actual TorrentsHandler.AddTorrent method to verify
// error handling and response behavior matches the handler implementation.

// TestAddTorrentHandler_InvalidIndexerID_Returns400 verifies that providing
// an invalid (non-integer) indexer_id returns a 400 Bad Request error.
func TestAddTorrentHandler_InvalidIndexerID_Returns400(t *testing.T) {
	t.Parallel()

	// Create handler with nil dependencies - we won't reach them due to early return
	handler := NewTorrentsHandler(nil, nil, nil)

	// Create multipart form with invalid indexer_id
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", "http://example.com/torrent.torrent")
	_ = writer.WriteField("indexer_id", "not-a-number")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Add chi route context
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid indexer_id")
	assert.Contains(t, w.Body.String(), "not-a-number")
}

// TestAddTorrentHandler_NegativeIndexerID_Returns400 verifies that providing
// a negative indexer_id returns a 400 Bad Request error.
func TestAddTorrentHandler_NegativeIndexerID_Returns400(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandler(nil, nil, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", "http://example.com/torrent.torrent")
	_ = writer.WriteField("indexer_id", "-5")
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

// TestAddTorrentHandler_ZeroIndexerID_Returns400 verifies that providing
// indexer_id=0 returns a 400 Bad Request error.
func TestAddTorrentHandler_ZeroIndexerID_Returns400(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandler(nil, nil, nil)

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

// TestAddTorrentHandler_JackettServiceUnavailable_Returns503 verifies that
// providing a valid indexer_id when jackett service is nil returns 503.
func TestAddTorrentHandler_JackettServiceUnavailable_Returns503(t *testing.T) {
	t.Parallel()

	// Create handler with nil jackettService but valid syncManager
	// We need a non-nil syncManager to get past the URL processing,
	// but jackettService is nil to trigger the 503
	handler := NewTorrentsHandler(nil, nil, nil)

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

// TestAddTorrentHandler_NoURLsOrFiles_Returns400 verifies that providing
// neither URLs nor files returns a 400 Bad Request error.
func TestAddTorrentHandler_NoURLsOrFiles_Returns400(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandler(nil, nil, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	// Don't add urls or torrent files
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

// TestAddTorrentHandler_InvalidInstanceID_Returns400 verifies that providing
// an invalid instance ID in the URL returns a 400 Bad Request error.
func TestAddTorrentHandler_InvalidInstanceID_Returns400(t *testing.T) {
	t.Parallel()

	handler := NewTorrentsHandler(nil, nil, nil)

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

// =============================================================================
// Handler Integration Tests with Mocks (Success Paths)
// =============================================================================

// fullMockSyncManager implements torrentAdder interface for full handler testing
type fullMockSyncManager struct {
	addTorrentCalls         []addTorrentCall
	addTorrentFromURLsCalls []addTorrentFromURLsCall
	addTorrentErr           error
	addTorrentFromURLsErr   error
	addTorrentFromURLsFunc  func(urls []string) (*qbt.TorrentAddResponse, error)
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
	return qbt.AppPreferences{}, nil
}

// fullMockJackettService implements torrentDownloader interface for full handler testing
type fullMockJackettService struct {
	downloadTorrentCalls []jackett.TorrentDownloadRequest
	downloadTorrentData  []byte
	downloadTorrentErr   error
}

func (m *fullMockJackettService) DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error) {
	m.downloadTorrentCalls = append(m.downloadTorrentCalls, req)
	return m.downloadTorrentData, m.downloadTorrentErr
}

// TestAddTorrentHandler_SuccessfulIndexerDownload_Returns201 verifies the full success path:
// 1. Valid indexer_id provided
// 2. Torrent downloaded via jackettService
// 3. Torrent added to qBittorrent via syncManager
// 4. HTTP 201 response with correct counts
func TestAddTorrentHandler_SuccessfulIndexerDownload_Returns201(t *testing.T) {
	t.Parallel()

	mockSync := &fullMockSyncManager{}
	mockJackett := &fullMockJackettService{
		downloadTorrentData: []byte("fake torrent data"),
	}

	handler := NewTorrentsHandlerForTesting(mockSync, mockJackett)

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

	// Verify HTTP 201 Created response
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":1`)
	assert.Contains(t, w.Body.String(), `"failed":0`)

	// Verify jackettService.DownloadTorrent was called with correct parameters
	require.Len(t, mockJackett.downloadTorrentCalls, 1)
	assert.Equal(t, 42, mockJackett.downloadTorrentCalls[0].IndexerID)
	assert.Equal(t, "http://indexer.example.com/download/123", mockJackett.downloadTorrentCalls[0].DownloadURL)

	// Verify syncManager.AddTorrent was called with downloaded bytes
	require.Len(t, mockSync.addTorrentCalls, 1)
	assert.Equal(t, 1, mockSync.addTorrentCalls[0].instanceID)
	assert.Equal(t, []byte("fake torrent data"), mockSync.addTorrentCalls[0].fileContent)
	assert.Equal(t, "movies", mockSync.addTorrentCalls[0].options["category"])

	// Verify AddTorrentFromURLs was NOT called (since we downloaded via indexer)
	assert.Empty(t, mockSync.addTorrentFromURLsCalls)
}

// TestAddTorrentHandler_SuccessfulMagnetWithIndexer_Returns201 verifies that magnet links
// are passed directly to qBittorrent even when indexer_id is provided.
func TestAddTorrentHandler_SuccessfulMagnetWithIndexer_Returns201(t *testing.T) {
	t.Parallel()

	mockSync := &fullMockSyncManager{}
	mockJackett := &fullMockJackettService{
		downloadTorrentData: []byte("should not be used"),
	}

	handler := NewTorrentsHandlerForTesting(mockSync, mockJackett)

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

	// Verify HTTP 201 Created response
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":1`)
	assert.Contains(t, w.Body.String(), `"failed":0`)

	// Verify jackettService was NOT called for magnet links
	assert.Empty(t, mockJackett.downloadTorrentCalls)

	// Verify magnet was passed directly via AddTorrentFromURLs
	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, []string{magnetURL}, mockSync.addTorrentFromURLsCalls[0].urls)

	// Verify AddTorrent (file method) was NOT called
	assert.Empty(t, mockSync.addTorrentCalls)
}

// TestAddTorrentHandler_MixedURLsAndMagnets_Returns201 verifies handling of mixed
// HTTP URLs (downloaded via indexer) and magnet links (passed directly).
func TestAddTorrentHandler_MixedURLsAndMagnets_Returns201(t *testing.T) {
	t.Parallel()

	mockSync := &fullMockSyncManager{}
	mockJackett := &fullMockJackettService{
		downloadTorrentData: []byte("downloaded torrent data"),
	}

	handler := NewTorrentsHandlerForTesting(mockSync, mockJackett)

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

	// Verify HTTP 201 Created response
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":2`)
	assert.Contains(t, w.Body.String(), `"failed":0`)

	// Verify HTTP URL was downloaded via jackettService
	require.Len(t, mockJackett.downloadTorrentCalls, 1)
	assert.Equal(t, 99, mockJackett.downloadTorrentCalls[0].IndexerID)
	assert.Equal(t, httpURL, mockJackett.downloadTorrentCalls[0].DownloadURL)

	// Verify magnet was passed directly
	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, []string{magnetURL}, mockSync.addTorrentFromURLsCalls[0].urls)

	// Verify downloaded torrent was added via AddTorrent
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

	handler := NewTorrentsHandlerForTesting(mockSync, mockJackett)

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

	handler := NewTorrentsHandlerForTesting(mockSync, mockJackett)

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

// TestAddTorrentHandler_PartialFailure_Returns201WithFailedURLs verifies that
// partial failures return 201 with accurate counts and failedURLs details.
func TestAddTorrentHandler_PartialFailure_Returns201WithFailedURLs(t *testing.T) {
	t.Parallel()

	mockSync := &fullMockSyncManager{}
	// Use custom mock that fails on first download, succeeds on second
	mockJackett := &customMockJackettServiceForHandler{
		responses: []jackettResponse{
			{err: errors.New("indexer unavailable")},
			{data: []byte("success torrent data")},
		},
	}

	handler := NewTorrentsHandlerForTesting(mockSync, mockJackett)

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

	// Verify HTTP 201 Created (partial success)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":1`)
	assert.Contains(t, w.Body.String(), `"failed":1`)
	assert.Contains(t, w.Body.String(), `"failedURLs"`)
	assert.Contains(t, w.Body.String(), "http://fail.example.com/1")
	assert.Contains(t, w.Body.String(), "indexer unavailable")

	// Verify both URLs were attempted
	assert.Len(t, mockJackett.calls, 2)

	// Verify only successful torrent was added
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

	handler := NewTorrentsHandlerForTesting(mockSync, nil)

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

// customMockJackettServiceForHandler returns the configured download responses.
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

// TestAddTorrentHandler_NoIndexerID_UsesDirectURL verifies that when no indexer_id
// is provided, URLs are passed directly to qBittorrent.
func TestAddTorrentHandler_NoIndexerID_UsesDirectURL(t *testing.T) {
	t.Parallel()

	mockSync := &fullMockSyncManager{}
	mockJackett := &fullMockJackettService{
		downloadTorrentData: []byte("should not be used"),
	}

	handler := NewTorrentsHandlerForTesting(mockSync, mockJackett)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("urls", "http://example.com/torrent.torrent")
	// No indexer_id field
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.AddTorrent(w, req)

	// Verify HTTP 201 Created response
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"added":1`)

	// Verify jackettService was NOT called
	assert.Empty(t, mockJackett.downloadTorrentCalls)

	// Verify URL was passed directly via AddTorrentFromURLs
	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, []string{"http://example.com/torrent.torrent"}, mockSync.addTorrentFromURLsCalls[0].urls)

	// Verify AddTorrent (file method) was NOT called
	assert.Empty(t, mockSync.addTorrentCalls)
}
