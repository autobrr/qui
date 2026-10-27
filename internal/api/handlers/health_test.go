// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHealthResponses(t *testing.T) {
	t.Parallel()

	handler := NewHealthHandler()
	for _, tt := range []struct {
		path   string
		handle http.HandlerFunc
		status string
	}{
		{"/health", handler.HandleHealth, "ok"},
		{"/healthz/readiness", handler.HandleReady, "ready"},
		{"/healthz/liveness", handler.HandleLiveness, "alive"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			tt.handle(response, httptest.NewRequest(http.MethodGet, tt.path, nil))
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, "application/json", response.Header().Get("Content-Type"))
			require.JSONEq(t, `{"status":"`+tt.status+`"}`, response.Body.String())
		})
	}
}
