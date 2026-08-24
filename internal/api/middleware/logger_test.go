// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// The production logger already carries a timestamp in its context, so a
// Timestamp() call here emits a duplicate "time" key.
func TestLoggerEmitsSingleTimestamp(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger().Level(zerolog.TraceLevel)
	handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/instances", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	logLine := strings.TrimSpace(buf.String())
	require.Equal(t, 1, strings.Count(logLine, `"time":`), "duplicate time key in %s", logLine)

	var decoded struct {
		Message string `json:"message"`
		Time    string `json:"time"`
	}
	require.NoError(t, json.Unmarshal([]byte(logLine), &decoded))
	require.Equal(t, "incoming_request", decoded.Message)
	require.NotEmpty(t, decoded.Time)
}

// A DEBUG-level logger stays silent for healthy traffic.
func TestLoggerLevelBySeverity(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		delay     time.Duration
		threshold time.Duration
		wantLine  bool
	}{
		{name: "fast success stays at trace", status: http.StatusOK, threshold: time.Second},
		{name: "server error surfaces", status: http.StatusInternalServerError, threshold: time.Second, wantLine: true},
		{name: "client error stays at trace", status: http.StatusUnauthorized, threshold: time.Second},
		{name: "slow request surfaces", status: http.StatusOK, delay: 5 * time.Millisecond, threshold: time.Millisecond, wantLine: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := slowRequestThreshold
			slowRequestThreshold = tt.threshold
			t.Cleanup(func() { slowRequestThreshold = original })

			var buf bytes.Buffer
			logger := zerolog.New(&buf).Level(zerolog.DebugLevel)
			handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				time.Sleep(tt.delay)
				w.WriteHeader(tt.status)
			}))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/instances", nil)
			handler.ServeHTTP(httptest.NewRecorder(), req)

			logLine := strings.TrimSpace(buf.String())
			if !tt.wantLine {
				require.Empty(t, logLine)
				return
			}
			require.Contains(t, logLine, `"level":"debug"`)
			require.Contains(t, logLine, `"message":"incoming_request"`)
		})
	}
}

func TestLoggerLogsPathOnly(t *testing.T) {
	const secretAPIKey = "SECRET-API-KEY"

	var buf bytes.Buffer
	logger := zerolog.New(&buf).Level(zerolog.TraceLevel)
	handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/cross-seed/apply?apikey="+secretAPIKey+"&format=json",
		nil,
	)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	logLine := buf.String()
	require.NotContains(t, logLine, secretAPIKey)
	require.NotContains(t, logLine, "format=json")
	require.Contains(t, logLine, `"url":"/api/cross-seed/apply"`)
}

func TestLoggerDoesNotLogOIDCCallbackQuerySecrets(t *testing.T) {
	const (
		oauthState = "SECRET-OAUTH-STATE"
		oauthCode  = "SECRET-AUTH-CODE"
	)

	var buf bytes.Buffer
	logger := zerolog.New(&buf).Level(zerolog.TraceLevel)
	handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/auth/oidc/callback?state="+oauthState+"&code="+oauthCode,
		nil,
	)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	logLine := buf.String()
	require.NotContains(t, logLine, oauthState)
	require.NotContains(t, logLine, oauthCode)
	require.NotContains(t, logLine, "state=")
	require.NotContains(t, logLine, "code=")
	require.Contains(t, logLine, `"url":"/api/auth/oidc/callback"`)
}

func TestLoggerRedactsProxyAPIKeyPath(t *testing.T) {
	const proxyAPIKey = "SECRET-PROXY-API-KEY"

	var buf bytes.Buffer
	logger := zerolog.New(&buf).Level(zerolog.TraceLevel)
	handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/proxy/"+proxyAPIKey+"/api/v2/torrents/info",
		nil,
	)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	logLine := buf.String()
	require.NotContains(t, logLine, proxyAPIKey)
	require.Contains(t, logLine, `"url":"/proxy/REDACTED/api/v2/torrents/info"`)
}
