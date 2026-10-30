// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/update"
)

type stubRestarter struct {
	sync.Mutex
	err      error
	requests int
}

func (s *stubRestarter) Request() error {
	s.requests++
	return s.err
}

type stubInstaller struct {
	result update.Result
	err    error
	tags   []string
	// during runs inside Install, while the update holds the lock.
	during func()
}

func (s *stubInstaller) Install(_ context.Context, tag string) (update.Result, error) {
	s.tags = append(s.tags, tag)
	if s.during != nil {
		s.during()
	}
	return s.result, s.err
}

func postUpdate(t *testing.T, h *SystemHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/system/update", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	return rec
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
		rec := postRestart(t, NewSystemHandler(update.Availability{Restart: true}, restarter, &stubInstaller{}))

		require.Equal(t, http.StatusAccepted, rec.Code)
		require.Equal(t, 1, restarter.requests)
	})

	t.Run("not available", func(t *testing.T) {
		restarter := &stubRestarter{}
		rec := postRestart(t, NewSystemHandler(update.Availability{SelfUpdate: true}, restarter, &stubInstaller{}))

		requireErrorBody(t, rec, http.StatusForbidden, "Restart is not available")
		require.Zero(t, restarter.requests)
	})

	t.Run("already running", func(t *testing.T) {
		restarter := &stubRestarter{}
		h := NewSystemHandler(update.Availability{Restart: true}, restarter, &stubInstaller{})
		require.Equal(t, http.StatusAccepted, postRestart(t, h).Code)

		requireErrorBody(t, postRestart(t, h), http.StatusConflict, "An update or restart is already running")
		require.Equal(t, 1, restarter.requests)
	})

	t.Run("pre-check failed", func(t *testing.T) {
		restarter := &stubRestarter{err: errors.New("/opt/qui/qui is not executable")}
		h := NewSystemHandler(update.Availability{Restart: true}, restarter, &stubInstaller{})

		requireErrorBody(t, postRestart(t, h), http.StatusInternalServerError, "/opt/qui/qui is not executable")

		// The refusal releases the lock, so a fixed binary can restart.
		restarter.err = nil
		require.Equal(t, http.StatusAccepted, postRestart(t, h).Code)
	})
}

func TestSystemHandler_Update(t *testing.T) {
	available := update.Availability{SelfUpdate: true, Restart: true}
	body := `{"version":"v1.31.0"}`

	t.Run("ok", func(t *testing.T) {
		restarter := &stubRestarter{}
		want := update.Result{Version: "1.31.0", RollbackCommand: `mv '/opt/qui/qui-v1.30.0.bak' '/opt/qui/qui'`}
		installer := &stubInstaller{result: want}
		rec := postUpdate(t, NewSystemHandler(available, restarter, installer), body)

		require.Equal(t, http.StatusOK, rec.Code)
		var got update.Result
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Equal(t, want, got)
		require.Equal(t, []string{"v1.31.0"}, installer.tags)
		require.Equal(t, 1, restarter.requests)
	})

	t.Run("not available", func(t *testing.T) {
		restarter := &stubRestarter{}
		installer := &stubInstaller{}
		rec := postUpdate(t, NewSystemHandler(update.Availability{Restart: true}, restarter, installer), body)

		requireErrorBody(t, rec, http.StatusForbidden, "Self-update is not available")
		require.Empty(t, installer.tags)
		require.Zero(t, restarter.requests)
	})

	for _, bad := range []string{``, `{}`, `{"version":""}`, `{"version":"latest"}`, `{"version":"1.31"}`, `not json`} {
		t.Run(fmt.Sprintf("bad request %q", bad), func(t *testing.T) {
			installer := &stubInstaller{}
			rec := postUpdate(t, NewSystemHandler(available, &stubRestarter{}, installer), bad)

			requireErrorBody(t, rec, http.StatusBadRequest, "version must be a release tag, such as v1.31.0")
			require.Empty(t, installer.tags)
		})
	}

	failures := []struct {
		name   string
		err    error
		status int
	}{
		{name: "release not found", err: fmt.Errorf("%w: no release v1.31.0 for linux/amd64", update.ErrReleaseNotFound), status: http.StatusNotFound},
		{name: "not newer", err: fmt.Errorf("%w: v1.30.0 is not newer than 1.30.0", update.ErrNotNewer), status: http.StatusConflict},
		{name: "github error", err: errors.New("could not find release v1.31.0: GET https://api.github.com/repos/autobrr/qui/releases: 403 API rate limit exceeded"), status: http.StatusBadGateway},
		{name: "swap failed", err: fmt.Errorf("%w: open /opt/qui/.qui.new: permission denied", update.ErrSwap), status: http.StatusInternalServerError},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			restarter := &stubRestarter{}
			installer := &stubInstaller{err: tt.err}
			h := NewSystemHandler(available, restarter, installer)

			requireErrorBody(t, postUpdate(t, h, body), tt.status, tt.err.Error())
			require.Zero(t, restarter.requests)

			// The failure releases the lock.
			installer.err = nil
			require.Equal(t, http.StatusOK, postUpdate(t, h, body).Code)
		})
	}

	t.Run("restart refused", func(t *testing.T) {
		restarter := &stubRestarter{err: errors.New("/opt/qui/qui is not executable")}
		installer := &stubInstaller{result: update.Result{Version: "1.31.0"}}
		h := NewSystemHandler(available, restarter, installer)

		requireErrorBody(t, postUpdate(t, h, body), http.StatusInternalServerError, "installed 1.31.0, but the restart was refused: /opt/qui/qui is not executable")

		// The refusal releases the lock, so a fixed binary can restart.
		restarter.err = nil
		require.Equal(t, http.StatusAccepted, postRestart(t, h).Code)
	})

	t.Run("restart during update", func(t *testing.T) {
		restarter := &stubRestarter{}
		installer := &stubInstaller{}
		h := NewSystemHandler(available, restarter, installer)
		installer.during = func() {
			requireErrorBody(t, postRestart(t, h), http.StatusConflict, "An update or restart is already running")
			requireErrorBody(t, postUpdate(t, h, body), http.StatusConflict, "An update or restart is already running")
		}

		require.Equal(t, http.StatusOK, postUpdate(t, h, body).Code)
		require.Equal(t, []string{"v1.31.0"}, installer.tags)
		require.Equal(t, 1, restarter.requests)
	})

	t.Run("tray restart during update", func(t *testing.T) {
		restarter := &stubRestarter{}
		installer := &stubInstaller{}
		installer.during = func() {
			// The Tray takes the same lock before it calls Request.
			require.False(t, restarter.TryLock())
		}

		require.Equal(t, http.StatusOK, postUpdate(t, NewSystemHandler(available, restarter, installer), body).Code)
		require.Equal(t, 1, restarter.requests)
	})

	t.Run("update during restart", func(t *testing.T) {
		installer := &stubInstaller{}
		h := NewSystemHandler(available, &stubRestarter{}, installer)
		require.Equal(t, http.StatusAccepted, postRestart(t, h).Code)

		requireErrorBody(t, postUpdate(t, h, body), http.StatusConflict, "An update or restart is already running")
		require.Empty(t, installer.tags)
	})
}
