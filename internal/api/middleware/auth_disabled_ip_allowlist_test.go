// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/domain"
)

func TestRequireAuthDisabledIPAllowlist(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("passes when auth-disabled mode is off", func(t *testing.T) {
		cfg := &domain.Config{}
		handler := RequireAuthDisabledIPAllowlist(cfg)(inner)

		req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
		req.RemoteAddr = "203.0.113.10:12345"
		resp := httptest.NewRecorder()

		handler.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusOK, resp.Code)
	})

	t.Run("allows request from configured CIDR", func(t *testing.T) {
		cfg := &domain.Config{
			AuthDisabled:             true,
			IfIGetBannedItsMyFault:   true,
			AuthDisabledAllowedCIDRs: []string{"127.0.0.1/32"},
		}
		handler := RequireAuthDisabledIPAllowlist(cfg)(inner)

		req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		resp := httptest.NewRecorder()

		handler.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusOK, resp.Code)
	})

	t.Run("blocks request outside CIDR", func(t *testing.T) {
		cfg := &domain.Config{
			AuthDisabled:             true,
			IfIGetBannedItsMyFault:   true,
			AuthDisabledAllowedCIDRs: []string{"127.0.0.1/32"},
		}
		handler := RequireAuthDisabledIPAllowlist(cfg)(inner)

		req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
		req.RemoteAddr = "203.0.113.10:54321"
		resp := httptest.NewRecorder()

		handler.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusForbidden, resp.Code)
	})

	t.Run("blocks when configured list is invalid", func(t *testing.T) {
		cfg := &domain.Config{
			AuthDisabled:             true,
			IfIGetBannedItsMyFault:   true,
			AuthDisabledAllowedCIDRs: []string{"invalid-cidr"},
		}
		handler := RequireAuthDisabledIPAllowlist(cfg)(inner)

		req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		resp := httptest.NewRecorder()

		handler.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusForbidden, resp.Code)
	})
}
