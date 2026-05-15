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

func TestLoggerRedactsSensitiveURLQueryParams(t *testing.T) {
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
	require.Contains(t, logLine, `"url":"/api/cross-seed/apply?apikey=REDACTED&format=json"`)
}
