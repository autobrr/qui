// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllowedHostsAdmission(t *testing.T) {
	deps := newTestDependencies(t)
	deps.Config.Config.AuthDisabled = true
	deps.Config.Config.IAcknowledgeThisIsABadIdea = true
	deps.Config.Config.AuthDisabledAllowedCIDRs = []string{"127.0.0.1/32"}
	deps.Config.Config.AllowedHosts = []string{"localhost"}
	deps.Config.Config.CORSAllowedOrigins = []string{"https://panel.test"}
	router, err := NewServer(deps).Handler()
	require.NoError(t, err)

	// The router keeps its list even if the source configuration changes.
	deps.Config.Config.AllowedHosts[0] = "attacker.test"
	for _, tc := range []struct {
		name, method, path, host, peer string
		want                           int
	}{
		{"permitted loopback admin", "GET", "/api/auth/me", "localhost:7476", "127.0.0.1:1234", 200},
		{"rebound loopback admin", "GET", "/api/auth/me", "attacker.test:7476", "127.0.0.1:1234", 400},
		{"IP restriction still applies", "GET", "/api/auth/me", "localhost", "192.0.2.1:1234", 403},
		{"proxy", "GET", "/proxy/synthetic-key/api/v2/torrents/info", "attacker.test", "127.0.0.1:1234", 400},
		{"static file", "GET", "/assets/app.js", "attacker.test", "127.0.0.1:1234", 400},
		{"login", "POST", "/api/auth/login", "attacker.test", "127.0.0.1:1234", 400},
		{"preflight", "OPTIONS", "/api/auth/me", "attacker.test", "127.0.0.1:1234", 400},
		{"local health", "GET", "/health", "attacker.test", "127.0.0.1:1234", 200},
		{"IPv6 health outside IP list", "GET", "/healthz/liveness", "attacker.test", "[::1]:1234", 200},
		{"remote health", "GET", "/health", "attacker.test", "192.0.2.1:1234", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil)
			req.Host, req.RemoteAddr = tc.host, tc.peer
			req.Header.Set("X-Forwarded-Host", "localhost")
			req.Header.Set("X-Forwarded-For", "127.0.0.1")
			req.Header.Set("X-Real-IP", "127.0.0.1")
			req.Header.Set("Forwarded", "for=127.0.0.1;host=localhost")
			req.Header.Set("Origin", "https://panel.test")
			req.Header.Set("Access-Control-Request-Method", "GET")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			require.Equal(t, tc.want, res.Code)
			if tc.want == 400 {
				require.Contains(t, res.Body.String(), "allowedHosts")
				require.Empty(t, res.Header().Get("Access-Control-Allow-Origin"))
			}
			if tc.path == "/api/auth/me" && tc.want == 200 {
				require.JSONEq(t, `{"username":"admin","auth_method":"none"}`, res.Body.String())
			}
		})
	}
}

func TestAllowedHostsWithCredentials(t *testing.T) {
	deps := newTestDependencies(t)
	deps.Config.Config.AllowedHosts = []string{"localhost"}
	router, err := NewServer(deps).Handler()
	require.NoError(t, err)
	apiKey, _, err := deps.AuthService.CreateAPIKey(t.Context(), "allowed-hosts-test")
	require.NoError(t, err)
	session, err := deps.SessionManager.Load(t.Context(), "")
	require.NoError(t, err)
	deps.SessionManager.Put(session, "authenticated", true)
	deps.SessionManager.Put(session, "username", "test-user")
	token, _, err := deps.SessionManager.Commit(session)
	require.NoError(t, err)

	for _, credential := range []string{"session", "API key", "none"} {
		for _, host := range []string{"localhost", "attacker.test"} {
			t.Run(credential+"/"+host, func(t *testing.T) {
				// This endpoint uses only the local database, with no client warm-up.
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/api-keys", nil)
				req.Host = host
				if credential == "session" {
					req.AddCookie(&http.Cookie{Name: deps.SessionManager.Cookie.Name, Value: token})
				}
				if credential == "API key" {
					req.Header.Set("X-API-Key", apiKey)
				}
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				want := http.StatusBadRequest
				if host == "localhost" {
					if credential == "session" || credential == "API key" {
						want = http.StatusOK
					} else {
						want = http.StatusForbidden
					}
				}
				require.Equal(t, want, res.Code)
			})
		}
	}
}
