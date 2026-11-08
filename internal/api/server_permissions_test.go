// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/models"
)

const needsPermissionPrefix = "API key needs "

// Routes that a key without any Permission reaches. Every other /api route
// must refuse it.
var openToEveryKey = map[routeKey]struct{}{
	// No authentication.
	{http.MethodPost, "/api/auth/setup"}:      {},
	{http.MethodPost, "/api/auth/login"}:      {},
	{http.MethodGet, "/api/auth/check-setup"}: {},
	{http.MethodGet, "/api/auth/validate"}:    {},
	{http.MethodGet, "/api/themes"}:           {},
	{http.MethodGet, "/api/themes/settings"}:  {},
	{http.MethodGet, "/api/docs"}:             {},
	{http.MethodGet, "/api/openapi.json"}:     {},

	{http.MethodGet, "/api/auth/me"}:                  {},
	{http.MethodGet, "/api/tracker-icons"}:            {},
	{http.MethodGet, "/api/peer-client-icons/{name}"}: {},
	{http.MethodGet, "/api/themes/custom"}:            {},
	{http.MethodGet, "/api/version"}:                  {},
	{http.MethodGet, "/api/version/latest"}:           {},
	// The event-only stream. Torrent streams check "Torrents: read" inside.
	{http.MethodGet, "/api/stream"}:                {},
	{http.MethodPost, "/api/stream/subscriptions"}: {},
}

var routeParam = regexp.MustCompile(`\{[^}]+\}`)

type permissionRig struct {
	deps *Dependencies
	url  string
}

func newPermissionRig(t *testing.T, setup ...func(*Dependencies)) *permissionRig {
	t.Helper()
	deps := newTestDependencies(t)
	// The walk reaches the log handlers, which need a config that can persist.
	var err error
	deps.Config, err = config.New(t.TempDir(), "test")
	require.NoError(t, err)
	for _, f := range setup {
		f(deps)
	}
	router, err := NewServer(deps).Handler()
	require.NoError(t, err)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &permissionRig{deps: deps, url: srv.URL}
}

func (rig *permissionRig) key(t *testing.T, perms ...models.Permission) string {
	t.Helper()
	if perms == nil {
		perms = []models.Permission{}
	}
	raw, _, err := rig.deps.AuthService.CreateAPIKey(t.Context(), "script", perms, nil)
	require.NoError(t, err)
	return raw
}

// send returns the status and body. A request that is still running after a
// second returns status 0: the handler passed every check and holds the request.
func (rig *permissionRig) send(t *testing.T, method, path, key string, cookie *http.Cookie) (int, string) {
	t.Helper()
	return rig.sendBody(t, method, path, key, cookie, "{}")
}

func (rig *permissionRig) sendBody(t *testing.T, method, path, key string, cookie *http.Cookie, reqBody string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, rig.url+path, strings.NewReader(reqBody))
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
	if errors.Is(err, context.DeadlineExceeded) {
		return 0, ""
	}
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if errors.Is(err, context.DeadlineExceeded) {
		return resp.StatusCode, string(body)
	}
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

func (rig *permissionRig) sessionCookie(t *testing.T) *http.Cookie {
	t.Helper()
	sessions := rig.deps.SessionManager
	signIn := sessions.LoadAndSave(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sessions.Put(r.Context(), "authenticated", true)
		sessions.Put(r.Context(), "username", "test-user")
	}))
	rec := httptest.NewRecorder()
	signIn.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	return cookies[0]
}

func apiRoutes(t *testing.T, router chi.Routes) []routeKey {
	t.Helper()
	var routes []routeKey
	require.NoError(t, chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(path, "/api/") {
			routes = append(routes, routeKey{method, path})
		}
		return nil
	}))
	return routes
}

func walkAPIRoutes(t *testing.T, rig *permissionRig, visit func(route routeKey, path string)) {
	t.Helper()
	router, err := NewServer(rig.deps).Handler()
	require.NoError(t, err)
	routes := apiRoutes(t, router)
	// The router has about 260 /api routes. A broken walk must not pass with none.
	require.GreaterOrEqual(t, len(routes), 260)
	for _, route := range routes {
		visit(route, routeParam.ReplaceAllString(route.Path, "1"))
	}
}

func TestRoutesRefuseKeyWithoutPermission(t *testing.T) {
	rig := newPermissionRig(t)
	key := rig.key(t)

	walkAPIRoutes(t, rig, func(route routeKey, path string) {
		if _, open := openToEveryKey[route]; open {
			return
		}
		status, body := rig.send(t, route.Method, path, key, nil)
		require.Equal(t, http.StatusForbidden, status, "%s %s: %s", route.Method, route.Path, body)
		if body != browserOnlyBody {
			require.True(t, strings.HasPrefix(body, needsPermissionPrefix), "%s %s: %q", route.Method, route.Path, body)
		}
	})
}

// Existing keys got every Permission on upgrade, so they must keep every route
// except the Browser-only routes.
func TestEveryPermissionReachesEveryRouteExceptBrowserOnly(t *testing.T) {
	rig := newPermissionRig(t)
	key := rig.key(t, models.AllPermissions()...)

	walkAPIRoutes(t, rig, func(route routeKey, path string) {
		status, body := rig.send(t, route.Method, path, key, nil)
		require.False(t, strings.HasPrefix(body, needsPermissionPrefix), "%s %s: %d %q", route.Method, route.Path, status, body)
		require.False(t, isInstanceLimitRefusal(body), "%s %s: a key with no Instance limit reaches every instance: %d %q", route.Method, route.Path, status, body)
	})
}

func TestPermissionLevels(t *testing.T) {
	rig := newPermissionRig(t)
	const (
		readRoute  = "GET /api/dir-scan/settings"
		runRoute   = "POST /api/dir-scan/webhook/scan"
		writeRoute = "PATCH /api/dir-scan/settings"
	)
	dirScan := func(level models.PermissionLevel) string {
		return rig.key(t, models.Permission{Area: models.AreaDirScan, Level: level})
	}
	readKey, runKey, writeKey := dirScan(models.LevelRead), dirScan(models.LevelRun), dirScan(models.LevelWrite)

	for _, tc := range []struct {
		name, route, key string
		want             string
	}{
		{"read allows a read route", readRoute, readKey, ""},
		{"read refuses a run route", runRoute, readKey, "API key needs Dir Scan: run\n"},
		{"read refuses a write route", writeRoute, readKey, "API key needs Dir Scan: write\n"},
		{"run allows a run route", runRoute, runKey, ""},
		{"run refuses a read route", readRoute, runKey, "API key needs Dir Scan: read\n"},
		{"run refuses a write route", writeRoute, runKey, "API key needs Dir Scan: write\n"},
		{"write allows a read route", readRoute, writeKey, ""},
		{"write allows a run route", runRoute, writeKey, ""},
		{"write allows a write route", writeRoute, writeKey, ""},
		{"another area does not count", readRoute, rig.key(t, models.Permission{Area: models.AreaTorrents, Level: models.LevelWrite}), "API key needs Dir Scan: read\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method, path, _ := strings.Cut(tc.route, " ")
			status, body := rig.send(t, method, path, tc.key, nil)
			if tc.want != "" {
				require.Equal(t, http.StatusForbidden, status)
				require.Equal(t, tc.want, body)
				return
			}
			require.NotEqual(t, http.StatusForbidden, status, body)
		})
	}

	t.Run("a signed-in session passes every level", func(t *testing.T) {
		cookie := rig.sessionCookie(t)
		for _, route := range []string{readRoute, runRoute, writeRoute} {
			method, path, _ := strings.Cut(route, " ")
			status, body := rig.send(t, method, path, "", cookie)
			require.NotEqual(t, http.StatusForbidden, status, "%s: %s", route, body)
			require.NotEqual(t, http.StatusUnauthorized, status, "%s: %s", route, body)
		}
	})
}

func TestFilesWithPasskeysNeedWrite(t *testing.T) {
	rig := newPermissionRig(t)
	key := rig.key(t,
		models.Permission{Area: models.AreaTorrents, Level: models.LevelRead},
		models.Permission{Area: models.AreaBackups, Level: models.LevelRead},
	)

	for _, tc := range []struct{ path, want string }{
		{"/api/instances/1/torrents/abc/export", "API key needs Torrents: write\n"},
		{"/api/instances/1/torrent-creator/1/file", "API key needs Torrents: write\n"},
		{"/api/instances/1/backups/runs/1/download", "API key needs Backups: write\n"},
		{"/api/instances/1/backups/runs/1/items/abc/download", "API key needs Backups: write\n"},
	} {
		status, body := rig.send(t, http.MethodGet, tc.path, key, nil)
		require.Equal(t, http.StatusForbidden, status, tc.path)
		require.Equal(t, tc.want, body, tc.path)
	}

	status, body := rig.send(t, http.MethodGet, "/api/instances/1/torrents/abc/files/0/download", key, nil)
	require.False(t, strings.HasPrefix(body, needsPermissionPrefix), "content file download needs only read: %d %q", status, body)
}

func TestStreamsNeedPermissions(t *testing.T) {
	rig := newPermissionRig(t)
	noPermission := rig.key(t)
	torrentsRead := rig.key(t, models.Permission{Area: models.AreaTorrents, Level: models.LevelRead})
	logsRead := rig.key(t, models.Permission{Area: models.AreaLogs, Level: models.LevelRead})
	rssRead := rig.key(t, models.Permission{Area: models.AreaRSS, Level: models.LevelRead})
	torrentStream := "/api/stream?streams=" + url.QueryEscape(`[{"key":"a","instanceId":1}]`)

	for _, tc := range []struct {
		name, method, path, key string
		want                    string
	}{
		{"torrent stream refuses a key without Torrents: read", http.MethodGet, torrentStream, logsRead, "API key needs Torrents: read\n"},
		{"torrent stream accepts Torrents: read", http.MethodGet, torrentStream, torrentsRead, ""},
		{"subscription change refuses a key without Torrents: read", http.MethodPost, "/api/stream/subscriptions", logsRead, "API key needs Torrents: read\n"},
		{"log stream refuses a key without Logs: read", http.MethodGet, "/api/logs/stream", torrentsRead, "API key needs Logs: read\n"},
		{"log stream accepts Logs: read", http.MethodGet, "/api/logs/stream", logsRead, ""},
		{"RSS events refuse a key without RSS: read", http.MethodGet, "/api/instances/1/rss/events", torrentsRead, "API key needs RSS: read\n"},
		{"RSS events accept RSS: read", http.MethodGet, "/api/instances/1/rss/events", rssRead, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if tc.method == http.MethodPost {
				// The add list holds a torrent stream; the body replaces the default "{}".
				status, body := rig.sendBody(t, tc.method, path, tc.key, nil, `{"connectionId":"x","add":[{"key":"a","instanceId":1}]}`)
				require.Equal(t, http.StatusForbidden, status)
				require.Equal(t, tc.want, body)
				return
			}
			status, body := rig.send(t, tc.method, path, tc.key, nil)
			if tc.want != "" {
				require.Equal(t, http.StatusForbidden, status)
				require.Equal(t, tc.want, body)
				return
			}
			require.False(t, strings.HasPrefix(body, needsPermissionPrefix), "%d %q", status, body)
		})
	}

	t.Run("event-only stream accepts every key", func(t *testing.T) {
		status, body := rig.send(t, http.MethodGet, "/api/stream?activity=1", noPermission, nil)
		require.NotEqual(t, http.StatusForbidden, status, body)
	})
}
