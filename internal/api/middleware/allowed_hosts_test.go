// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequireAllowedHosts(t *testing.T) {
	guard, err := RequireAllowedHosts([]string{"QUI.Example.TEST.", "*.home.test", "bücher.test", "127.0.0.1", "[::1]", "2001:db8::1"})
	require.NoError(t, err)
	handler := guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		host string
		want int
	}{
		{"qui.example.test:7476", http.StatusNoContent},
		{"attacker.test:7476", http.StatusBadRequest},
		{"QUI.EXAMPLE.TEST.:443", http.StatusNoContent},
		{"qui.home.test", http.StatusNoContent},
		{"a.qui.home.test", http.StatusNoContent},
		{"home.test", http.StatusBadRequest},
		{"badhome.test", http.StatusBadRequest},
		{"qui.home.test.attacker.test", http.StatusBadRequest},
		{"xn--bcher-kva.test", http.StatusNoContent},
		{"BÜCHER.test.", http.StatusNoContent},
		{"bücher.test。", http.StatusNoContent},
		{"bücher.test．", http.StatusNoContent},
		{"bücher.test｡", http.StatusNoContent},
		{"bücher.test。.", http.StatusBadRequest},
		{"127.0.0.1:80", http.StatusNoContent},
		{"[::1]", http.StatusNoContent},
		{"[0:0:0:0:0:0:0:1]:7476", http.StatusNoContent},
		{"[2001:0db8:0:0:0:0:0:1]:80", http.StatusNoContent},
		{"[::ffff:127.0.0.1]:80", http.StatusNoContent},
		{"::1", http.StatusBadRequest},
		{"localhost", http.StatusBadRequest},
		{"", http.StatusBadRequest},
		{"qui.example.test:", http.StatusBadRequest},
		{"qui.example.test:abc", http.StatusBadRequest},
		{"qui.example.test:+80", http.StatusBadRequest},
		{"qui.example.test:65536", http.StatusBadRequest},
		{"qui.example.test:80:80", http.StatusBadRequest},
		{"[qui.example.test]:80", http.StatusBadRequest},
		{"[127.0.0.1]:80", http.StatusBadRequest},
		{"[::1%lo]:80", http.StatusBadRequest},
		{"https://qui.example.test", http.StatusBadRequest},
		{"qui.example.test/path", http.StatusBadRequest},
		{"qui.example.test@attacker.test", http.StatusBadRequest},
		{"qui.example.test..", http.StatusBadRequest},
		{" qui.example.test", http.StatusBadRequest},
		{"qui.example.test\n", http.StatusBadRequest},
	} {
		t.Run(tc.host, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/me", nil)
			req.Host = tc.host
			req.RemoteAddr = "127.0.0.1:1234"
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			require.Equal(t, tc.want, res.Code)
			if tc.want == http.StatusBadRequest {
				require.Equal(t, "Host is not permitted by allowedHosts\n", res.Body.String())
			}
		})
	}
}

func TestRequireAllowedHostsRejectsInvalidEntries(t *testing.T) {
	for _, entry := range []string{
		"", " ", "*", "*qui.test", "qui.*.test", "*.*.test", "*.127.0.0.1", "*.::1",
		"https://qui.test", "qui.test:80", "[::1]:80", "qui.test/", "127.0.0.0/8", "::1/128",
		"qui.test?query", "qui.test#fragment", "user@qui.test", "[qui.test]", "[127.0.0.1]",
		"[::1%lo]", "::1%lo", ".qui.test", "qui..test", "qui.test..", "-qui.test", "qui-.test",
		"qui_test", "xn--.test", strings.Repeat("a", 64) + ".test", strings.Repeat("a.", 127) + "test",
	} {
		t.Run(entry, func(t *testing.T) {
			_, err := RequireAllowedHosts([]string{"qui.test", entry})
			require.ErrorContains(t, err, "allowedHosts")
		})
	}
}

func TestRequireAllowedHostsEmptyList(t *testing.T) {
	guard, err := RequireAllowedHosts(nil)
	require.NoError(t, err)
	handler := guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, host := range []string{"", "attacker.test:7476"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/me", nil)
		req.Host = host
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		require.Equal(t, http.StatusNoContent, res.Code)
	}
}

func TestRequireAllowedHostsHealthProbes(t *testing.T) {
	guard, err := RequireAllowedHosts([]string{"qui.test"})
	require.NoError(t, err)
	handler := guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, path := range []string{"/health", "/healthz/readiness", "/healthz/liveness", "/health/", "/qui/health", "/api/auth/me"} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodOptions} {
			for _, peer := range []string{"127.0.0.1:1234", "[::1]:1234", "[::ffff:127.0.0.1]:1234", "192.0.2.1:1234", "invalid"} {
				t.Run(method+path+peer, func(t *testing.T) {
					req := httptest.NewRequestWithContext(t.Context(), method, path+"?probe=1", nil)
					req.Host = "attacker.test"
					req.RemoteAddr = peer
					req.Header.Set("X-Forwarded-For", "127.0.0.1")
					req.Header.Set("X-Real-IP", "::1")
					req.Header.Set("X-Forwarded-Host", "qui.test")
					req.Header.Set("Forwarded", "for=127.0.0.1;host=qui.test")
					res := httptest.NewRecorder()
					handler.ServeHTTP(res, req)
					want := http.StatusBadRequest
					if (path == "/health" || path == "/healthz/readiness" || path == "/healthz/liveness") &&
						(method == http.MethodGet || method == http.MethodHead) &&
						peer != "192.0.2.1:1234" && peer != "invalid" {
						want = http.StatusNoContent
					}
					require.Equal(t, want, res.Code)
				})
			}
		}
	}
}
