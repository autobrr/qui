// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

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
