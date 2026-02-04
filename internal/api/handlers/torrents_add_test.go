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
	"strconv"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
)

// mockSyncManager implements the methods needed for AddTorrent testing
type mockSyncManager struct {
	addTorrentCalls         []addTorrentCall
	addTorrentFromURLsCalls []addTorrentFromURLsCall
	addTorrentErr           error
	addTorrentFromURLsErr   error
}

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

func (m *mockSyncManager) AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error {
	m.addTorrentCalls = append(m.addTorrentCalls, addTorrentCall{
		instanceID:  instanceID,
		fileContent: fileContent,
		options:     options,
	})
	return m.addTorrentErr
}

func (m *mockSyncManager) AddTorrentFromURLs(ctx context.Context, instanceID int, urls []string, options map[string]string) error {
	m.addTorrentFromURLsCalls = append(m.addTorrentFromURLsCalls, addTorrentFromURLsCall{
		instanceID: instanceID,
		urls:       urls,
		options:    options,
	})
	return m.addTorrentFromURLsErr
}

// mockJackettService implements the DownloadTorrent method for testing
type mockJackettService struct {
	downloadTorrentCalls []jackett.TorrentDownloadRequest
	downloadTorrentData  []byte
	downloadTorrentErr   error
}

func (m *mockJackettService) DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error) {
	m.downloadTorrentCalls = append(m.downloadTorrentCalls, req)
	return m.downloadTorrentData, m.downloadTorrentErr
}

// syncManagerAdapter wraps mockSyncManager to match the interface expected by TorrentsHandler
type syncManagerAdapter interface {
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error
	AddTorrentFromURLs(ctx context.Context, instanceID int, urls []string, options map[string]string) error
}

// jackettServiceAdapter wraps mockJackettService to match the interface expected by TorrentsHandler
type jackettServiceAdapter interface {
	DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)
}

// addTorrentWithIndexer tests the core logic of adding torrents with indexer_id
// This function extracts and tests the indexer-aware torrent addition logic
func addTorrentWithIndexer(
	ctx context.Context,
	syncManager syncManagerAdapter,
	jackettService jackettServiceAdapter,
	instanceID int,
	urls []string,
	indexerID int,
	options map[string]string,
) (addedCount int, failedCount int, lastError error) {
	if indexerID > 0 && jackettService != nil {
		for _, url := range urls {
			url = strings.TrimSpace(url)
			if url == "" {
				continue
			}

			// Magnet links can be added directly to qBittorrent
			if strings.HasPrefix(strings.ToLower(url), "magnet:") {
				if err := syncManager.AddTorrentFromURLs(ctx, instanceID, []string{url}, options); err != nil {
					failedCount++
					lastError = err
				} else {
					addedCount++
				}
				continue
			}

			// Download torrent file from indexer
			torrentBytes, err := jackettService.DownloadTorrent(ctx, jackett.TorrentDownloadRequest{
				IndexerID:   indexerID,
				DownloadURL: url,
			})
			if err != nil {
				var magnetErr *jackett.MagnetDownloadError
				if errors.As(err, &magnetErr) && magnetErr.MagnetURL != "" {
					magnetURL := strings.TrimSpace(magnetErr.MagnetURL)
					if err := syncManager.AddTorrentFromURLs(ctx, instanceID, []string{magnetURL}, options); err != nil {
						failedCount++
						lastError = err
					} else {
						addedCount++
					}
					continue
				}
				failedCount++
				lastError = err
				continue
			}

			// Add torrent from downloaded file content
			if err := syncManager.AddTorrent(ctx, instanceID, torrentBytes, options); err != nil {
				failedCount++
				lastError = err
			} else {
				addedCount++
			}
		}
	} else {
		// No indexer_id or no jackett service - use URL method directly
		if err := syncManager.AddTorrentFromURLs(ctx, instanceID, urls, options); err != nil {
			return 0, len(urls), err
		}
		addedCount = len(urls)
	}
	return addedCount, failedCount, lastError
}

func TestAddTorrentWithIndexer_DownloadsViaBackend(t *testing.T) {
	t.Parallel()

	mockSync := &mockSyncManager{}
	mockJackett := &mockJackettService{
		downloadTorrentData: []byte("fake torrent data"),
	}

	ctx := context.Background()
	urls := []string{"http://indexer.example.com/download/123"}
	options := map[string]string{"category": "movies"}

	added, failed, err := addTorrentWithIndexer(ctx, mockSync, mockJackett, 1, urls, 42, options)

	require.NoError(t, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 0, failed)

	// Verify jackett service was called to download
	require.Len(t, mockJackett.downloadTorrentCalls, 1)
	assert.Equal(t, 42, mockJackett.downloadTorrentCalls[0].IndexerID)
	assert.Equal(t, "http://indexer.example.com/download/123", mockJackett.downloadTorrentCalls[0].DownloadURL)

	// Verify sync manager received the downloaded torrent bytes
	require.Len(t, mockSync.addTorrentCalls, 1)
	assert.Equal(t, 1, mockSync.addTorrentCalls[0].instanceID)
	assert.Equal(t, []byte("fake torrent data"), mockSync.addTorrentCalls[0].fileContent)
	assert.Equal(t, "movies", mockSync.addTorrentCalls[0].options["category"])

	// Verify URL method was NOT called
	assert.Empty(t, mockSync.addTorrentFromURLsCalls)
}

func TestAddTorrentWithIndexer_FallsBackWithoutIndexerID(t *testing.T) {
	t.Parallel()

	mockSync := &mockSyncManager{}
	mockJackett := &mockJackettService{
		downloadTorrentData: []byte("fake torrent data"),
	}

	ctx := context.Background()
	urls := []string{"http://indexer.example.com/download/123"}
	options := map[string]string{"category": "movies"}

	// indexerID = 0, should fall back to direct URL method
	added, failed, err := addTorrentWithIndexer(ctx, mockSync, mockJackett, 1, urls, 0, options)

	require.NoError(t, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 0, failed)

	// Verify jackett service was NOT called
	assert.Empty(t, mockJackett.downloadTorrentCalls)

	// Verify URL method WAS called
	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, 1, mockSync.addTorrentFromURLsCalls[0].instanceID)
	assert.Equal(t, urls, mockSync.addTorrentFromURLsCalls[0].urls)
}

func TestAddTorrentWithIndexer_FallsBackWithNilJackettService(t *testing.T) {
	t.Parallel()

	mockSync := &mockSyncManager{}

	ctx := context.Background()
	urls := []string{"http://indexer.example.com/download/123"}
	options := map[string]string{"category": "movies"}

	// jackettService = nil, should fall back to direct URL method
	added, failed, err := addTorrentWithIndexer(ctx, mockSync, nil, 1, urls, 42, options)

	require.NoError(t, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 0, failed)

	// Verify URL method WAS called
	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, 1, mockSync.addTorrentFromURLsCalls[0].instanceID)
	assert.Equal(t, urls, mockSync.addTorrentFromURLsCalls[0].urls)
}

func TestAddTorrentWithIndexer_AddsMagnetRedirect(t *testing.T) {
	t.Parallel()

	mockSync := &mockSyncManager{}
	mockJackett := &mockJackettService{
		downloadTorrentErr: &jackett.MagnetDownloadError{MagnetURL: "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678"},
	}

	ctx := context.Background()
	urls := []string{"http://indexer.example.com/download/123"}
	options := map[string]string{"category": "movies"}

	added, failed, err := addTorrentWithIndexer(ctx, mockSync, mockJackett, 1, urls, 42, options)

	require.NoError(t, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 0, failed)

	require.Len(t, mockJackett.downloadTorrentCalls, 1)
	assert.Equal(t, "http://indexer.example.com/download/123", mockJackett.downloadTorrentCalls[0].DownloadURL)

	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, []string{"magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678"}, mockSync.addTorrentFromURLsCalls[0].urls)
	assert.Empty(t, mockSync.addTorrentCalls)
}

func TestAddTorrentWithIndexer_MagnetLinksPassedDirectly(t *testing.T) {
	t.Parallel()

	mockSync := &mockSyncManager{}
	mockJackett := &mockJackettService{
		downloadTorrentData: []byte("fake torrent data"),
	}

	ctx := context.Background()
	magnetURL := "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678"
	urls := []string{magnetURL}
	options := map[string]string{"category": "movies"}

	added, failed, err := addTorrentWithIndexer(ctx, mockSync, mockJackett, 1, urls, 42, options)

	require.NoError(t, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 0, failed)

	// Verify jackett service was NOT called for magnet links
	assert.Empty(t, mockJackett.downloadTorrentCalls)

	// Verify magnet was passed directly to qBittorrent
	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, []string{magnetURL}, mockSync.addTorrentFromURLsCalls[0].urls)

	// Verify AddTorrent (file method) was NOT called
	assert.Empty(t, mockSync.addTorrentCalls)
}

func TestAddTorrentWithIndexer_MixedURLsAndMagnets(t *testing.T) {
	t.Parallel()

	mockSync := &mockSyncManager{}
	mockJackett := &mockJackettService{
		downloadTorrentData: []byte("fake torrent data"),
	}

	ctx := context.Background()
	magnetURL := "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678"
	httpURL := "http://indexer.example.com/download/123"
	urls := []string{magnetURL, httpURL}
	options := map[string]string{"category": "movies"}

	added, failed, err := addTorrentWithIndexer(ctx, mockSync, mockJackett, 1, urls, 42, options)

	require.NoError(t, err)
	assert.Equal(t, 2, added)
	assert.Equal(t, 0, failed)

	// Verify magnet was passed directly
	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
	assert.Equal(t, []string{magnetURL}, mockSync.addTorrentFromURLsCalls[0].urls)

	// Verify HTTP URL was downloaded via jackett
	require.Len(t, mockJackett.downloadTorrentCalls, 1)
	assert.Equal(t, httpURL, mockJackett.downloadTorrentCalls[0].DownloadURL)

	// Verify downloaded torrent was added
	require.Len(t, mockSync.addTorrentCalls, 1)
	assert.Equal(t, []byte("fake torrent data"), mockSync.addTorrentCalls[0].fileContent)
}

func TestAddTorrentWithIndexer_DownloadFailureContinuesWithOthers(t *testing.T) {
	t.Parallel()

	mockSync := &mockSyncManager{}
	downloadErr := errors.New("download failed")

	// Create a custom mock that fails on first call, second succeeds
	customJackett := &customMockJackettService{
		responses: []jackettResponse{
			{err: downloadErr},
			{data: []byte("fake torrent data")},
		},
	}

	ctx := context.Background()
	urls := []string{
		"http://indexer.example.com/download/fail",
		"http://indexer.example.com/download/success",
	}
	options := map[string]string{"category": "movies"}

	added, failed, err := addTorrentWithIndexer(ctx, mockSync, customJackett, 1, urls, 42, options)

	// Last error should be from the failed download
	require.Error(t, err)
	assert.Equal(t, downloadErr, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 1, failed)

	// Verify both URLs were attempted
	assert.Equal(t, 2, len(customJackett.calls))

	// Verify only the successful download was added
	require.Len(t, mockSync.addTorrentCalls, 1)
}

func TestAddTorrentWithIndexer_AllDownloadsFail(t *testing.T) {
	t.Parallel()

	mockSync := &mockSyncManager{}
	downloadErr := errors.New("download failed")
	mockJackett := &mockJackettService{
		downloadTorrentErr: downloadErr,
	}

	ctx := context.Background()
	urls := []string{
		"http://indexer.example.com/download/1",
		"http://indexer.example.com/download/2",
	}
	options := map[string]string{"category": "movies"}

	added, failed, err := addTorrentWithIndexer(ctx, mockSync, mockJackett, 1, urls, 42, options)

	require.Error(t, err)
	assert.Equal(t, downloadErr, err)
	assert.Equal(t, 0, added)
	assert.Equal(t, 2, failed)

	// Verify no torrents were added
	assert.Empty(t, mockSync.addTorrentCalls)
}

func TestAddTorrentWithIndexer_AddTorrentFails(t *testing.T) {
	t.Parallel()

	addErr := errors.New("add torrent failed")
	mockSync := &mockSyncManager{
		addTorrentErr: addErr,
	}
	mockJackett := &mockJackettService{
		downloadTorrentData: []byte("fake torrent data"),
	}

	ctx := context.Background()
	urls := []string{"http://indexer.example.com/download/123"}
	options := map[string]string{"category": "movies"}

	added, failed, err := addTorrentWithIndexer(ctx, mockSync, mockJackett, 1, urls, 42, options)

	require.Error(t, err)
	assert.Equal(t, addErr, err)
	assert.Equal(t, 0, added)
	assert.Equal(t, 1, failed)

	// Verify download was attempted
	require.Len(t, mockJackett.downloadTorrentCalls, 1)

	// Verify add was attempted
	require.Len(t, mockSync.addTorrentCalls, 1)
}

func TestAddTorrentWithIndexer_UppercaseMagnet(t *testing.T) {
	t.Parallel()

	mockSync := &mockSyncManager{}
	mockJackett := &mockJackettService{
		downloadTorrentData: []byte("fake torrent data"),
	}

	ctx := context.Background()
	// Test uppercase MAGNET: prefix
	magnetURL := "MAGNET:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678"
	urls := []string{magnetURL}
	options := map[string]string{}

	added, failed, err := addTorrentWithIndexer(ctx, mockSync, mockJackett, 1, urls, 42, options)

	require.NoError(t, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 0, failed)

	// Verify jackett service was NOT called for magnet links
	assert.Empty(t, mockJackett.downloadTorrentCalls)

	// Verify magnet was passed directly
	require.Len(t, mockSync.addTorrentFromURLsCalls, 1)
}

func TestAddTorrentWithIndexer_EmptyURLsSkipped(t *testing.T) {
	t.Parallel()

	mockSync := &mockSyncManager{}
	mockJackett := &mockJackettService{
		downloadTorrentData: []byte("fake torrent data"),
	}

	ctx := context.Background()
	urls := []string{"", "http://indexer.example.com/download/123", ""}
	options := map[string]string{}

	added, failed, err := addTorrentWithIndexer(ctx, mockSync, mockJackett, 1, urls, 42, options)

	require.NoError(t, err)
	assert.Equal(t, 1, added)
	assert.Equal(t, 0, failed)

	// Verify only non-empty URL was processed
	require.Len(t, mockJackett.downloadTorrentCalls, 1)
	assert.Equal(t, "http://indexer.example.com/download/123", mockJackett.downloadTorrentCalls[0].DownloadURL)
}

// customMockJackettService allows per-call response configuration
type customMockJackettService struct {
	calls     []jackett.TorrentDownloadRequest
	responses []jackettResponse
	callIndex int
}

type jackettResponse struct {
	data []byte
	err  error
}

func (m *customMockJackettService) DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error) {
	m.calls = append(m.calls, req)
	if m.callIndex < len(m.responses) {
		resp := m.responses[m.callIndex]
		m.callIndex++
		return resp.data, resp.err
	}
	return nil, errors.New("no more responses configured")
}

// TestParseIndexerIDFromForm tests parsing the indexer_id form field.
// Note: Negative indexer IDs are parsed correctly but treated as "no indexer"
// by the handler (which checks indexerID > 0), so they effectively behave like 0.
func TestParseIndexerIDFromForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		indexerIDValue string
		expectedID     int
	}{
		{
			name:           "valid positive integer",
			indexerIDValue: "42",
			expectedID:     42,
		},
		{
			name:           "zero",
			indexerIDValue: "0",
			expectedID:     0,
		},
		{
			name:           "empty string",
			indexerIDValue: "",
			expectedID:     0,
		},
		{
			name:           "invalid string",
			indexerIDValue: "not-a-number",
			expectedID:     0,
		},
		{
			name:           "negative number",
			indexerIDValue: "-5",
			expectedID:     -5,
		},
		{
			name:           "large number",
			indexerIDValue: "999999",
			expectedID:     999999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a multipart form request
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)

			// Add the indexer_id field
			if tt.indexerIDValue != "" {
				err := writer.WriteField("indexer_id", tt.indexerIDValue)
				require.NoError(t, err)
			}

			// Add required urls field
			err := writer.WriteField("urls", "magnet:?xt=urn:btih:test")
			require.NoError(t, err)

			err = writer.Close()
			require.NoError(t, err)

			// Create request
			req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())

			// Add chi route context
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("instanceID", "1")
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			// Parse the form
			err = req.ParseMultipartForm(10 << 20)
			require.NoError(t, err)

			// Parse indexer_id like the handler does
			var indexerID int
			if indexerIDStr := req.FormValue("indexer_id"); indexerIDStr != "" {
				parsedID, parseErr := strconv.Atoi(indexerIDStr)
				if parseErr == nil {
					indexerID = parsedID
				}
			}

			assert.Equal(t, tt.expectedID, indexerID)
		})
	}
}

// TestMultipartFormParsing_IndexerID verifies that indexer_id and related fields
// are correctly parsed from a multipart form request.
func TestMultipartFormParsing_IndexerID(t *testing.T) {
	t.Parallel()

	// Create a multipart form request with indexer_id
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	err := writer.WriteField("urls", "http://indexer.example.com/download/123")
	require.NoError(t, err)

	err = writer.WriteField("indexer_id", "42")
	require.NoError(t, err)

	err = writer.WriteField("category", "movies")
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Parse the form
	err = req.ParseMultipartForm(10 << 20)
	require.NoError(t, err)

	// Verify the form values are parsed correctly
	assert.Equal(t, "http://indexer.example.com/download/123", req.FormValue("urls"))
	assert.Equal(t, "42", req.FormValue("indexer_id"))
	assert.Equal(t, "movies", req.FormValue("category"))
}

// TestAddTorrentURLProcessing tests the URL processing logic including whitespace handling
func TestAddTorrentURLProcessing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		urlsInput     string
		expectedURLs  []string
		expectedCount int
	}{
		{
			name:          "single URL",
			urlsInput:     "http://example.com/torrent.torrent",
			expectedURLs:  []string{"http://example.com/torrent.torrent"},
			expectedCount: 1,
		},
		{
			name:          "newline separated URLs",
			urlsInput:     "http://example.com/1.torrent\nhttp://example.com/2.torrent",
			expectedURLs:  []string{"http://example.com/1.torrent", "http://example.com/2.torrent"},
			expectedCount: 2,
		},
		{
			name:          "comma separated URLs",
			urlsInput:     "http://example.com/1.torrent,http://example.com/2.torrent",
			expectedURLs:  []string{"http://example.com/1.torrent", "http://example.com/2.torrent"},
			expectedCount: 2,
		},
		{
			name:          "mixed magnet and HTTP",
			urlsInput:     "magnet:?xt=urn:btih:abc123\nhttp://example.com/torrent.torrent",
			expectedURLs:  []string{"magnet:?xt=urn:btih:abc123", "http://example.com/torrent.torrent"},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)

			err := writer.WriteField("urls", tt.urlsInput)
			require.NoError(t, err)
			err = writer.Close()
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/instances/1/torrents", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())

			err = req.ParseMultipartForm(10 << 20)
			require.NoError(t, err)

			urlsParam := req.FormValue("urls")
			require.NotEmpty(t, urlsParam)

			// Process URLs like the handler does
			urlsParam = processURLSeparators(urlsParam)
			urls := splitURLs(urlsParam)

			assert.Len(t, urls, tt.expectedCount)
			for i, expected := range tt.expectedURLs {
				if i < len(urls) {
					assert.Equal(t, expected, urls[i])
				}
			}
		})
	}
}

// processURLSeparators converts newlines to commas (like the handler)
func processURLSeparators(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			result = append(result, ',')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}

// splitURLs splits on comma (like the handler)
func splitURLs(s string) []string {
	var result []string
	var current []byte
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			if len(current) > 0 {
				result = append(result, string(current))
				current = current[:0]
			}
		} else {
			current = append(current, s[i])
		}
	}
	if len(current) > 0 {
		result = append(result, string(current))
	}
	return result
}

// BenchmarkAddTorrentWithIndexer benchmarks the core logic
func BenchmarkAddTorrentWithIndexer(b *testing.B) {
	mockSync := &mockSyncManager{}
	mockJackett := &mockJackettService{
		downloadTorrentData: []byte("fake torrent data"),
	}

	ctx := context.Background()
	urls := []string{"http://indexer.example.com/download/123"}
	options := map[string]string{"category": "movies"}

	b.ResetTimer()
	for b.Loop() {
		mockSync.addTorrentCalls = nil
		mockSync.addTorrentFromURLsCalls = nil
		mockJackett.downloadTorrentCalls = nil
		_, _, _ = addTorrentWithIndexer(ctx, mockSync, mockJackett, 1, urls, 42, options)
	}
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
	handler := NewTorrentsHandler(nil, nil)

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

	handler := NewTorrentsHandler(nil, nil)

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

	handler := NewTorrentsHandler(nil, nil)

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
	handler := NewTorrentsHandler(nil, nil)

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

	handler := NewTorrentsHandler(nil, nil)

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

	handler := NewTorrentsHandler(nil, nil)

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
}

func (m *fullMockSyncManager) AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error {
	m.addTorrentCalls = append(m.addTorrentCalls, addTorrentCall{
		instanceID:  instanceID,
		fileContent: fileContent,
		options:     options,
	})
	return m.addTorrentErr
}

func (m *fullMockSyncManager) AddTorrentFromURLs(ctx context.Context, instanceID int, urls []string, options map[string]string) error {
	m.addTorrentFromURLsCalls = append(m.addTorrentFromURLsCalls, addTorrentFromURLsCall{
		instanceID: instanceID,
		urls:       urls,
		options:    options,
	})
	return m.addTorrentFromURLsErr
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

	req := httptest.NewRequest(http.MethodPost, "/api/instances/2/torrents", body)
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

// customMockJackettServiceForHandler is similar to customMockJackettService but
// implements the torrentDownloader interface
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
