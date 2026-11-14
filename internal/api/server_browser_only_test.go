// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

const browserOnlyBody = "API key not allowed; use the web UI\n"

var browserOnlyRoutes = []routeKey{
	{http.MethodGet, "/api/api-keys"},
	{http.MethodPost, "/api/api-keys"},
	{http.MethodPut, "/api/api-keys/1"},
	{http.MethodDelete, "/api/api-keys/1"},
	{http.MethodGet, "/api/client-api-keys"},
	{http.MethodPost, "/api/client-api-keys"},
	{http.MethodDelete, "/api/client-api-keys/1"},
	{http.MethodPost, "/api/system/update"},
	{http.MethodPost, "/api/system/restart"},
	{http.MethodPost, "/api/database/shrink"},
	{http.MethodPut, "/api/instances/1/ssh-credentials"},
	{http.MethodDelete, "/api/instances/1/ssh-credentials"},
	{http.MethodPost, "/api/instances/1/ssh-test"},
	{http.MethodPost, "/api/instances/1/ssh-host-key"},
	{http.MethodPost, "/api/instances/1/ssh-host-key/replace"},
	{http.MethodPut, "/api/auth/change-password"},
	{http.MethodGet, "/api/license/licensed"},
	{http.MethodGet, "/api/license/licenses"},
	{http.MethodPost, "/api/license/activate"},
	{http.MethodPost, "/api/license/validate"},
	{http.MethodPost, "/api/license/refresh"},
	{http.MethodDelete, "/api/license/some-key"},
	{http.MethodPost, "/api/external-programs"},
	{http.MethodPut, "/api/external-programs/1"},
	{http.MethodDelete, "/api/external-programs/1"},
	// Last: a session that reaches logout ends.
	{http.MethodPost, "/api/auth/logout"},
}

func TestBrowserOnlyRoutesRefuseAPIKeys(t *testing.T) {
	deps := newTestDependencies(t)
	apiKey, _, err := deps.AuthService.CreateAPIKey(t.Context(), "script", models.AllPermissions(), nil)
	require.NoError(t, err)

	router, err := NewServer(deps).Handler()
	require.NoError(t, err)
	srv := httptest.NewServer(router)
	defer srv.Close()

	send := func(t *testing.T, method, path, key string, cookie *http.Cookie) (int, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, strings.NewReader("{}"))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(body)
	}

	t.Run("API key gets 403", func(t *testing.T) {
		for _, route := range browserOnlyRoutes {
			status, body := send(t, route.Method, route.Path, apiKey, nil)
			require.Equal(t, http.StatusForbidden, status, "%s %s", route.Method, route.Path)
			require.Equal(t, browserOnlyBody, body, "%s %s", route.Method, route.Path)
		}
	})

	t.Run("API key reaches other routes", func(t *testing.T) {
		// 400: the handler read the empty body.
		for _, tc := range []struct {
			method, path, key string
			want              int
		}{
			{http.MethodGet, "/api/version", apiKey, http.StatusOK},
			{http.MethodPost, "/api/external-programs/execute", apiKey, http.StatusBadRequest},
			{http.MethodPost, "/api/cross-seed/webhook/check?apikey=" + apiKey, "", http.StatusBadRequest},
			{http.MethodPost, "/api/dir-scan/webhook/scan?apikey=" + apiKey, "", http.StatusBadRequest},
		} {
			status, _ := send(t, tc.method, tc.path, tc.key, nil)
			require.Equal(t, tc.want, status, tc.path)
		}
	})

	t.Run("signed-in browser reaches them", func(t *testing.T) {
		sessions := deps.SessionManager
		signIn := sessions.LoadAndSave(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			sessions.Put(r.Context(), "authenticated", true)
			sessions.Put(r.Context(), "username", "test-user")
		}))
		rec := httptest.NewRecorder()
		signIn.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1)

		for _, route := range browserOnlyRoutes {
			status, body := send(t, route.Method, route.Path, "", cookies[0])
			require.NotEqual(t, http.StatusMethodNotAllowed, status, "%s %s", route.Method, route.Path)
			require.NotEqual(t, "404 page not found\n", body, "%s %s", route.Method, route.Path)
			require.NotEqual(t, browserOnlyBody, body, "%s %s", route.Method, route.Path)
			require.NotEqual(t, "Unauthorized\n", body, "%s %s: session lost (status %d)", route.Method, route.Path, status)
		}
	})
}
