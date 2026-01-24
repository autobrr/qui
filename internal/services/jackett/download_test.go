// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadRateLimitError_Error(t *testing.T) {
	resumeAt := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name    string
		err     *DownloadRateLimitError
		wantMsg string
	}{
		{
			name: "not queued",
			err: &DownloadRateLimitError{
				IndexerID:   1,
				IndexerName: "TorrentLeech",
				ResumeAt:    resumeAt,
				Queued:      false,
			},
			wantMsg: "indexer TorrentLeech rate-limited until 2025-01-15T10:30:00Z",
		},
		{
			name: "queued for retry",
			err: &DownloadRateLimitError{
				IndexerID:   2,
				IndexerName: "IPTorrents",
				ResumeAt:    resumeAt,
				Queued:      true,
			},
			wantMsg: "indexer IPTorrents rate-limited, queued for retry at 2025-01-15T10:30:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantMsg, tt.err.Error())
		})
	}
}

func TestDownloadRateLimitError_Is(t *testing.T) {
	tests := []struct {
		name   string
		target error
		want   bool
	}{
		{
			name:   "matches DownloadRateLimitError",
			target: &DownloadRateLimitError{},
			want:   true,
		},
		{
			name: "matches DownloadRateLimitError with different values",
			target: &DownloadRateLimitError{
				IndexerID:   99,
				IndexerName: "Other",
				ResumeAt:    time.Now(),
			},
			want: true,
		},
		{
			name:   "does not match other error types",
			target: errors.New("some error"),
			want:   false,
		},
		{
			name:   "does not match DownloadError",
			target: &DownloadError{StatusCode: 429},
			want:   false,
		},
		{
			name:   "does not match nil",
			target: nil,
			want:   false,
		},
	}

	err := &DownloadRateLimitError{
		IndexerID:   1,
		IndexerName: "Test",
		ResumeAt:    time.Now(),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, err.Is(tt.target))
		})
	}
}

func TestDownloadRateLimitError_ErrorsIs(t *testing.T) {
	err := &DownloadRateLimitError{
		IndexerID:   1,
		IndexerName: "Test",
		ResumeAt:    time.Now(),
	}
	wrapped := errors.Join(errors.New("wrapper"), err)

	assert.True(t, errors.Is(wrapped, &DownloadRateLimitError{}), "errors.Is should find DownloadRateLimitError in wrapped error")
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

func TestDownloadRateLimitError_ErrorsAs(t *testing.T) {
	err := &DownloadRateLimitError{
		IndexerID:   42,
		IndexerName: "TestIndexer",
		ResumeAt:    time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		Queued:      false,
	}
	wrapped := errors.Join(errors.New("wrapper"), err)

	var dlErr *DownloadRateLimitError
	require.True(t, errors.As(wrapped, &dlErr), "errors.As should extract DownloadRateLimitError from wrapped error")
	assert.Equal(t, 42, dlErr.IndexerID)
	assert.Equal(t, "TestIndexer", dlErr.IndexerName)
}

// timeoutError implements net.Error for testing timeout detection
type timeoutError struct {
	timeout bool
}

func (e *timeoutError) Error() string   { return "timeout error" }
func (e *timeoutError) Timeout() bool   { return e.timeout }
func (e *timeoutError) Temporary() bool { return false }
