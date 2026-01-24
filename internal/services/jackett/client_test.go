// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadError_Error(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		url        string
		wantMsg    string
	}{
		{
			name:       "404 not found",
			statusCode: http.StatusNotFound,
			url:        "https://example.com/download/123",
			wantMsg:    "torrent download from https://example.com/download/123 returned status 404",
		},
		{
			name:       "429 rate limited",
			statusCode: http.StatusTooManyRequests,
			url:        "https://indexer.com/torrent/abc",
			wantMsg:    "torrent download from https://indexer.com/torrent/abc returned status 429",
		},
		{
			name:       "500 server error",
			statusCode: http.StatusInternalServerError,
			url:        "https://tracker.com/dl",
			wantMsg:    "torrent download from https://tracker.com/dl returned status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &DownloadError{StatusCode: tt.statusCode, URL: tt.url}
			assert.Equal(t, tt.wantMsg, err.Error())
		})
	}
}

func TestDownloadError_IsRateLimited(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{
			name:       "429 is rate limited",
			statusCode: http.StatusTooManyRequests,
			want:       true,
		},
		{
			name:       "404 is not rate limited",
			statusCode: http.StatusNotFound,
			want:       false,
		},
		{
			name:       "500 is not rate limited",
			statusCode: http.StatusInternalServerError,
			want:       false,
		},
		{
			name:       "503 is not rate limited",
			statusCode: http.StatusServiceUnavailable,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &DownloadError{StatusCode: tt.statusCode}
			assert.Equal(t, tt.want, err.IsRateLimited())
		})
	}
}

func TestDownloadError_Is(t *testing.T) {
	tests := []struct {
		name   string
		target error
		want   bool
	}{
		{
			name:   "matches DownloadError",
			target: &DownloadError{},
			want:   true,
		},
		{
			name:   "matches DownloadError with different values",
			target: &DownloadError{StatusCode: 500, URL: "other"},
			want:   true,
		},
		{
			name:   "does not match other error types",
			target: errors.New("some error"),
			want:   false,
		},
		{
			name:   "does not match nil",
			target: nil,
			want:   false,
		},
	}

	err := &DownloadError{StatusCode: 404, URL: "https://example.com"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, err.Is(tt.target))
		})
	}
}

func TestDownloadError_ErrorsIs(t *testing.T) {
	err := &DownloadError{StatusCode: 404, URL: "https://example.com"}
	wrapped := errors.Join(errors.New("wrapper"), err)

	assert.True(t, errors.Is(wrapped, &DownloadError{}), "errors.Is should find DownloadError in wrapped error")
}

func TestDownloadError_ErrorsAs(t *testing.T) {
	err := &DownloadError{StatusCode: 429, URL: "https://example.com"}
	wrapped := errors.Join(errors.New("wrapper"), err)

	var dlErr *DownloadError
	require.True(t, errors.As(wrapped, &dlErr), "errors.As should extract DownloadError from wrapped error")
	assert.Equal(t, 429, dlErr.StatusCode)
	assert.Equal(t, "https://example.com", dlErr.URL)
}

// TestDiscoverJackettIndexers_RedactsAPIKey is a regression test for issue #839.
// It verifies that API keys in error messages are redacted when discovery fails.
func TestDiscoverJackettIndexers_RedactsAPIKey(t *testing.T) {
	const secretAPIKey = "SUPERSECRETAPIKEY12345"

	// Create a test server that will be immediately closed to guarantee connection failure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should never reach here since we close the server
		t.Error("Request should not reach handler")
	}))
	// Close immediately to guarantee connection failure
	server.Close()

	// Try to discover indexers against the closed server
	_, err := DiscoverJackettIndexers(context.Background(), server.URL, secretAPIKey)

	// We expect an error since the server is closed
	require.Error(t, err, "Expected an error when connecting to closed server")

	errStr := err.Error()

	// The error message must NOT contain the secret API key
	assert.False(t, strings.Contains(errStr, secretAPIKey),
		"Error message should not contain the secret API key. Got: %s", errStr)

	// The error message SHOULD contain REDACTED if it includes the URL with apikey param
	// Note: depending on where the error occurs, it may or may not include URL params.
	// If it does include URL params, they should be redacted.
	if strings.Contains(errStr, "apikey=") {
		assert.True(t, strings.Contains(errStr, "apikey=REDACTED"),
			"Error message with apikey param should have value redacted. Got: %s", errStr)
	}
}
