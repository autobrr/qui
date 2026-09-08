// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestRateLimitError_Error(t *testing.T) {
	err := &RateLimitError{
		IndexerID:   1,
		IndexerName: "TorrentLeech",
		Scope:       rateLimitScopeGrab,
		RetryAt:     time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	assert.Equal(t, "indexer TorrentLeech grab rate-limited until 2025-01-15T10:30:00Z", err.Error())
}

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
			name: "502 Bad Gateway is retryable",
			err:  &DownloadError{StatusCode: http.StatusBadGateway},
			want: true,
		},
		{
			name: "503 Service Unavailable is retryable",
			err:  &DownloadError{StatusCode: http.StatusServiceUnavailable},
			want: true,
		},
		{
			name: "504 Gateway Timeout is retryable",
			err:  &DownloadError{StatusCode: http.StatusGatewayTimeout},
			want: true,
		},
		{
			name: "400 Bad Request is not retryable",
			err:  &DownloadError{StatusCode: http.StatusBadRequest},
			want: false,
		},
		{
			name: "401 Unauthorized is not retryable",
			err:  &DownloadError{StatusCode: http.StatusUnauthorized},
			want: false,
		},
		{
			name: "403 Forbidden is not retryable",
			err:  &DownloadError{StatusCode: http.StatusForbidden},
			want: false,
		},
		{
			name: "404 Not Found is not retryable",
			err:  &DownloadError{StatusCode: http.StatusNotFound},
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

func TestRateLimitError_ErrorsAs(t *testing.T) {
	err := &RateLimitError{
		IndexerID:   42,
		IndexerName: "TestIndexer",
		RetryAt:     time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}
	wrapped := errors.Join(errors.New("wrapper"), err)

	var rateLimitErr *RateLimitError
	require.ErrorAs(t, wrapped, &rateLimitErr, "errors.As should extract RateLimitError from wrapped error")
	assert.Equal(t, 42, rateLimitErr.IndexerID)
	assert.Equal(t, "TestIndexer", rateLimitErr.IndexerName)
}

func TestDownloadRateLimitUsesGrabScopeAndRetryAfter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	queryLimited, _ := service.rateLimiter.IsInCooldown(indexer.ID, rateLimitScopeQuery)
	grabLimited, _ := service.rateLimiter.IsInCooldown(indexer.ID, rateLimitScopeGrab)
	assert.False(t, queryLimited)
	assert.True(t, grabLimited)
}

// timeoutError implements net.Error for testing timeout detection
type timeoutError struct {
	timeout bool
}

func (e *timeoutError) Error() string   { return "timeout error" }
func (e *timeoutError) Timeout() bool   { return e.timeout }
func (e *timeoutError) Temporary() bool { return false }
