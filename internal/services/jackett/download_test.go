// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestIsRetryableDownloadError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error is not retryable",
			err:  nil,
			want: false,
		},
		{
			name: "500 Internal Server Error is retryable",
			err:  &DownloadError{StatusCode: http.StatusInternalServerError},
			want: true,
		},
		{
			name: "400 Bad Request is not retryable",
			err:  &DownloadError{StatusCode: http.StatusBadRequest},
			want: false,
		},
		{
			name: "429 Too Many Requests is not retryable (handled separately)",
			err:  &DownloadError{StatusCode: http.StatusTooManyRequests},
			want: false,
		},
		{
			name: "generic error is not retryable",
			err:  errors.New("something went wrong"),
			want: false,
		},
		{
			name: "timeout error is retryable",
			err:  &timeoutError{timeout: true},
			want: true,
		},
		{
			name: "net.OpError is retryable",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isRetryableDownloadError(tt.err))
		})
	}
}

func TestIsRetryableDownloadError_WrappedErrors(t *testing.T) {
	t.Run("wrapped 500 error is retryable", func(t *testing.T) {
		wrapped := errors.Join(errors.New("context"), &DownloadError{StatusCode: 500})
		assert.True(t, isRetryableDownloadError(wrapped))
	})

	t.Run("wrapped timeout error is retryable", func(t *testing.T) {
		wrapped := errors.Join(errors.New("context"), &timeoutError{timeout: true})
		assert.True(t, isRetryableDownloadError(wrapped))
	})

	t.Run("wrapped net.OpError is retryable", func(t *testing.T) {
		wrapped := errors.Join(errors.New("context"), &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")})
		assert.True(t, isRetryableDownloadError(wrapped))
	})
}

func TestDownloadRateLimitUsesGrabScopeAndRetryAfter(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	indexer := &models.TorznabIndexer{ID: 7, Name: "GrabLimited", BaseURL: server.URL, Backend: models.TorznabBackendProwlarr}
	service := NewService(&mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{indexer}})
	t.Cleanup(service.searchScheduler.Stop)
	before := time.Now().Add(41 * time.Second)

	_, err := service.DownloadTorrent(context.Background(), TorrentDownloadRequest{IndexerID: indexer.ID, DownloadURL: server.URL + "/download"})
	var rateLimitErr *RateLimitError
	require.ErrorAs(t, err, &rateLimitErr)
	assert.Equal(t, rateLimitScopeGrab, rateLimitErr.Scope)
	assert.WithinRange(t, rateLimitErr.RetryAt, before, time.Now().Add(43*time.Second))
	assert.Equal(t, int32(1), requests.Load(), "a 429 must end the download with no retry")
	_, _, searchErr := service.slots.tryAcquire(indexer, requestSearch)
	_, _, downloadErr := service.slots.tryAcquire(indexer, requestDownload)
	require.NoError(t, searchErr)
	assert.ErrorAs(t, downloadErr, &rateLimitErr)
}

func TestDownloadTorrentDoesNotCacheRejectedPayload(t *testing.T) {
	const torrentBody = testTorrentPayload
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>Login required</html>"))
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(torrentBody))
	}))
	t.Cleanup(server.Close)

	db := testdb.NewMigratedSQLite(t, "torrent-download-cache")
	store, err := models.NewTorznabIndexerStore(db, make([]byte, 32))
	require.NoError(t, err)
	indexer, err := store.Create(t.Context(), "Test indexer", server.URL, "test-key", nil, nil, true, 0, 5)
	require.NoError(t, err)
	cache := models.NewTorznabTorrentCacheStore(db)
	service := NewService(store, WithTorrentCache(cache))
	t.Cleanup(service.searchScheduler.Stop)
	t.Cleanup(func() {
		service.background.Wait()
		cache.WaitTouches()
	})
	req := TorrentDownloadRequest{IndexerID: indexer.ID, DownloadURL: server.URL + "/download", GUID: "test-release"}

	data, err := service.DownloadTorrent(t.Context(), req)
	require.ErrorIs(t, err, ErrInvalidTorrentPayload)
	assert.Nil(t, data)
	assert.Equal(t, int32(1), calls.Load(), "rejected payload must not trigger a retry")
	_, found, err := cache.Fetch(t.Context(), indexer.ID, req.GUID, defaultTorrentCacheTTL)
	require.NoError(t, err)
	assert.False(t, found)

	for range 2 {
		data, err = service.DownloadTorrent(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, []byte(torrentBody), data)
		assert.Equal(t, int32(2), calls.Load(), "valid payload must be fetched once and then cached")
	}
}

type timeoutError struct {
	timeout bool
}

func (e *timeoutError) Error() string   { return "timeout error" }
func (e *timeoutError) Timeout() bool   { return e.timeout }
func (e *timeoutError) Temporary() bool { return false }
