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

	"github.com/autobrr/qui/internal/experimental"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/problemreport"
	"github.com/autobrr/qui/internal/testutil/fakebrrbin"
)

const browserOnlyBody = "API key not allowed; use the web UI\n"

type browserOnlyRoute struct {
	Method, Path  string
	BrowserStatus int
}

var browserOnlyRoutes = []browserOnlyRoute{
	{http.MethodGet, "/api/api-keys", http.StatusOK},
	{http.MethodPost, "/api/api-keys", http.StatusBadRequest},
	{http.MethodPut, "/api/api-keys/1", http.StatusBadRequest},
	{http.MethodDelete, "/api/api-keys/1", http.StatusOK},
	{http.MethodGet, "/api/client-api-keys", http.StatusOK},
	{http.MethodPost, "/api/client-api-keys", http.StatusBadRequest},
	{http.MethodDelete, "/api/client-api-keys/1", http.StatusNotFound},
	{http.MethodPost, "/api/system/update", http.StatusForbidden},
	{http.MethodPost, "/api/system/restart", http.StatusForbidden},
	{http.MethodPost, "/api/database/shrink", http.StatusAccepted},
	{http.MethodPost, "/api/transfers", http.StatusBadRequest},
	{http.MethodPost, "/api/shares", http.StatusBadRequest},
	{http.MethodGet, "/api/shares", http.StatusOK},
	{http.MethodGet, "/api/shares/settings", http.StatusOK},
	{http.MethodPut, "/api/shares/settings", http.StatusBadRequest},
	{http.MethodPost, "/api/shares/1/stop", http.StatusNotFound},
	{http.MethodPost, "/api/shares/1/unlock", http.StatusNotFound},
	{http.MethodGet, "/api/shares/1/torrent", http.StatusNotFound},
	{http.MethodDelete, "/api/shares/1", http.StatusNotFound},
	{http.MethodPut, "/api/instances/1/ssh-credentials", http.StatusNotFound},
	{http.MethodDelete, "/api/instances/1/ssh-credentials", http.StatusNotFound},
	{http.MethodPost, "/api/instances/1/ssh-test", http.StatusNotFound},
	{http.MethodPost, "/api/instances/1/ssh-host-key", http.StatusNotFound},
	{http.MethodPost, "/api/instances/1/ssh-host-key/replace", http.StatusNotFound},
	{http.MethodPut, "/api/auth/change-password", http.StatusUnauthorized},
	{http.MethodGet, "/api/license/licensed", http.StatusOK},
	{http.MethodGet, "/api/license/licenses", http.StatusOK},
	{http.MethodPost, "/api/license/activate", http.StatusBadRequest},
	{http.MethodPost, "/api/license/validate", http.StatusBadRequest},
	{http.MethodPost, "/api/license/refresh", http.StatusOK},
	{http.MethodDelete, "/api/license/some-key", http.StatusInternalServerError},
	{http.MethodPost, "/api/external-programs", http.StatusBadRequest},
	{http.MethodPut, "/api/external-programs/1", http.StatusBadRequest},
	{http.MethodDelete, "/api/external-programs/1", http.StatusNotFound},
	{http.MethodGet, "/api/problem-reports/facts", http.StatusBadRequest},
	{http.MethodPost, "/api/problem-reports", http.StatusBadRequest},
	{http.MethodGet, "/api/problem-reports", http.StatusOK},
	{http.MethodDelete, "/api/problem-reports/r1", http.StatusNotFound},
	{http.MethodGet, "/api/problem-reports/sender", http.StatusOK},
	{http.MethodPost, "/api/problem-reports/sign-in", http.StatusBadRequest},
	{http.MethodGet, "/api/problem-reports/sign-in", http.StatusOK},
	{http.MethodDelete, "/api/problem-reports/sign-in", http.StatusNoContent},
	{http.MethodPost, "/api/auth/logout", http.StatusOK},
}

func TestBrowserOnlyRoutesRefuseAPIKeys(t *testing.T) {
	deps, db := newTestDependenciesWithDB(t)
	store, err := models.NewProblemReportStore(db, make([]byte, 32))
	require.NoError(t, err)
	brrbin := httptest.NewServer(fakebrrbin.New())
	t.Cleanup(brrbin.Close)
	deps.ProblemReports = problemreport.NewService(brrbin.URL, brrbin.Client(), store, problemreport.Sources{})
	deps.Experimental = experimental.New([]string{string(experimental.ProblemReports)})
	deps.Config.Config.ProblemReports = true
	apiKey := newUsedAPIKey(t, db, "script", models.AllPermissions(), nil)

	router, err := newServerCheckingHandlerPanics(t, deps).Handler()
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
			require.NotEqual(t, handlerPanicBody, body, "%s %s", route.Method, route.Path)
			require.Equal(t, route.BrowserStatus, status, "%s %s", route.Method, route.Path)
			require.NotEqual(t, "404 page not found\n", body, "%s %s", route.Method, route.Path)
			require.NotEqual(t, browserOnlyBody, body, "%s %s", route.Method, route.Path)
			require.NotEqual(t, "Unauthorized\n", body, "%s %s: session lost (status %d)", route.Method, route.Path, status)
		}
	})
}
