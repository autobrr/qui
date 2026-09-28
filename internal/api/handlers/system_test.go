// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/update"
)

type stubRestarter struct {
	err      error
	requests int
}

func (s *stubRestarter) Request() error {
	s.requests++
	return s.err
}

func postRestart(t *testing.T, h *SystemHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/system/restart", nil)
	rec := httptest.NewRecorder()
	h.Restart(rec, req)
	return rec
}

func requireErrorBody(t *testing.T, rec *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	require.Equal(t, status, rec.Code)
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, message, body.Error)
}

func TestSystemHandler_Restart(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		restarter := &stubRestarter{}
		rec := postRestart(t, NewSystemHandler(update.Availability{Restart: true}, restarter))

		require.Equal(t, http.StatusAccepted, rec.Code)
		require.Equal(t, 1, restarter.requests)
	})

	t.Run("not available", func(t *testing.T) {
		restarter := &stubRestarter{}
		rec := postRestart(t, NewSystemHandler(update.Availability{SelfUpdate: true}, restarter))

		requireErrorBody(t, rec, http.StatusForbidden, "Restart is not available")
		require.Zero(t, restarter.requests)
	})

	t.Run("already running", func(t *testing.T) {
		restarter := &stubRestarter{}
		h := NewSystemHandler(update.Availability{Restart: true}, restarter)
		require.Equal(t, http.StatusAccepted, postRestart(t, h).Code)

		requireErrorBody(t, postRestart(t, h), http.StatusConflict, "An update or restart is already running")
		require.Equal(t, 1, restarter.requests)
	})

	t.Run("pre-check failed", func(t *testing.T) {
		restarter := &stubRestarter{err: errors.New("/opt/qui/qui is not executable")}
		h := NewSystemHandler(update.Availability{Restart: true}, restarter)

		requireErrorBody(t, postRestart(t, h), http.StatusInternalServerError, "/opt/qui/qui is not executable")

		// The refusal releases the lock, so a fixed binary can restart.
		restarter.err = nil
		require.Equal(t, http.StatusAccepted, postRestart(t, h).Code)
	})
}
