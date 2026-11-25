// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchCapsPreservesRateLimitResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "45")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "", nil, nil, "prowlarr", 5)
	_, err := client.FetchCaps(context.Background(), "14")
	require.Error(t, err)

	var responseErr interface {
		HTTPStatusCode() int
		RetryAfterHeader() string
	}
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusTooManyRequests, responseErr.HTTPStatusCode())
	assert.Equal(t, "45", responseErr.RetryAfterHeader())
}

func TestFetchCapsPreservesBodyRateLimitResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "47")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><error code="429" description="Too many requests"/>`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "", nil, nil, "native", 5)
	_, err := client.FetchCaps(context.Background(), "")
	require.Error(t, err)

	var responseErr interface {
		HTTPStatusCode() int
		RetryAfterHeader() string
	}
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusTooManyRequests, responseErr.HTTPStatusCode())
	assert.Equal(t, "47", responseErr.RetryAfterHeader())
}

func TestDownloadPreservesRateLimitResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "52")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "", nil, nil, "jackett", 5)
	_, err := client.Download(context.Background(), server.URL+"/download")
	require.Error(t, err)

	var responseErr interface {
		HTTPStatusCode() int
		RetryAfterHeader() string
	}
	require.ErrorAs(t, err, &responseErr)
	assert.Equal(t, http.StatusTooManyRequests, responseErr.HTTPStatusCode())
	assert.Equal(t, "52", responseErr.RetryAfterHeader())
}

const testTorrentPayload = "d4:infod6:lengthi1e4:name8:test.bin12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaaee"

func TestDownloadRejectsNonTorrentPayloads(t *testing.T) {
	const torrentBody = testTorrentPayload
	tests := []struct {
		name        string
		contentType string
		body        string
		wantError   bool
	}{
		{name: "HTML login", contentType: "text/html; charset=utf-8", body: "<html>Login required</html>", wantError: true},
		{name: "JSON error", contentType: "application/json", body: `{"error":"access denied"}`, wantError: true},
		{name: "empty", contentType: "application/x-bittorrent", wantError: true},
		{name: "magnet body", contentType: "text/plain", body: "magnet:?xt=urn:btih:0123456789012345678901234567890123456789", wantError: true},
		{name: "HTML with torrent type", contentType: "application/x-bittorrent", body: "<html>Challenge</html>", wantError: true},
		{name: "bencoded list", contentType: "application/x-bittorrent", body: "le", wantError: true},
		{name: "dict-prefixed garbage", contentType: "application/x-bittorrent", body: "dnot-a-valid-torrent", wantError: true},
		{name: "bencoded dict without info", contentType: "application/x-bittorrent", body: "d4:name8:test.bine", wantError: true},
		{name: "non-dictionary info", contentType: "application/x-bittorrent", body: "d4:info4:spame", wantError: true},
		{name: "torrent", contentType: "application/x-bittorrent", body: torrentBody},
		{name: "plain text torrent", contentType: "text/plain", body: torrentBody},
		{name: "torrent without content type", body: torrentBody},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header()["Content-Type"] = []string{tt.contentType}
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)

			client := NewClient(server.URL, "test-api-secret", nil, nil, "jackett", 5)
			data, err := client.Download(t.Context(), server.URL+"/download?apikey=test-api-secret&passkey=test-pass-secret&id=private-query-value")
			if !tt.wantError {
				require.NoError(t, err)
				assert.Equal(t, []byte(tt.body), data)
				return
			}

			require.ErrorIs(t, err, ErrInvalidTorrentPayload)
			assert.Nil(t, data)
			assert.Contains(t, err.Error(), "Content-Type: ")
			assert.Contains(t, err.Error(), tt.contentType)
			assert.Contains(t, err.Error(), "web page")
			assert.NotContains(t, err.Error(), "test-api-secret")
			assert.NotContains(t, err.Error(), "test-pass-secret")
			assert.NotContains(t, err.Error(), "private-query-value")
			assert.False(t, isRetryableDownloadError(err))
		})
	}
}

func TestDownloadPreservesMagnetRedirect(t *testing.T) {
	const magnetURL = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, magnetURL, http.StatusFound)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "", nil, nil, "jackett", 5)
	data, err := client.Download(t.Context(), server.URL+"/download")
	magnetErr, ok := errors.AsType[*MagnetDownloadError](err)
	require.True(t, ok, "expected MagnetDownloadError, got %v", err)
	assert.Equal(t, magnetURL, magnetErr.MagnetURL)
	assert.Nil(t, data)
}

func TestFetchCapsWithRetryDoesNotRetryRateLimit(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	_, err := fetchCapsWithRetry(t.Context(), server.URL, "", nil, nil, "prowlarr", "14", 2, time.Millisecond, time.Second)
	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load())
}

func TestDiscoverJackettIndexers_RedactsAPIKey(t *testing.T) {
	const secretAPIKey = "SUPERSECRETAPIKEY12345"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Request should not reach handler")
	}))
	server.Close()

	_, err := DiscoverJackettIndexers(context.Background(), server.URL, secretAPIKey, nil, nil)

	require.Error(t, err, "Expected an error when connecting to closed server")

	errStr := err.Error()

	assert.NotContains(t, errStr, secretAPIKey,
		"Error message should not contain the secret API key. Got: %s", errStr)

	if strings.Contains(errStr, "apikey=") {
		assert.Contains(t, errStr, "apikey=REDACTED",
			"Error message with apikey param should have value redacted. Got: %s", errStr)
	}
}

func TestMagnetDownloadError_RedactsTrackerPasskeys(t *testing.T) {
	rawMagnet := "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a&tr=https%3A%2F%2Ftracker.example.com%2Fannounce%3Fpasskey%3DDEADBEEF01"
	err := &MagnetDownloadError{MagnetURL: rawMagnet}

	msg := err.Error()
	assert.NotContains(t, msg, "DEADBEEF01")
	assert.Contains(t, msg, "tracker.example.com")
	assert.Contains(t, msg, "c12fe1c06bba254a9dc9f519b335aa7c1367a88a")

	assert.Equal(t, rawMagnet, err.MagnetURL)
}

func TestDownloadHidesTheKeysOfALinkThatDoesNotParse(t *testing.T) {
	client := NewClient("https://indexer.example.invalid", "test-api-secret", nil, nil, "jackett", 5)

	tests := []struct {
		name string
		link string
	}{
		{name: "keys in the query", link: "https://indexer.example.invalid/download?apikey=test-api-secret&passkey=test-pass-secret\x7f"},
		{name: "key in the path", link: "https://indexer.example.invalid/test-pass-secret/download\x7f"},
		{name: "key in the place of the port", link: "https://indexer.example.invalid:test-pass-secret/download"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Download(t.Context(), tt.link)

			require.ErrorContains(t, err, "build download request")
			assert.NotContains(t, err.Error(), "test-api-secret")
			assert.NotContains(t, err.Error(), "test-pass-secret")
		})
	}
}
