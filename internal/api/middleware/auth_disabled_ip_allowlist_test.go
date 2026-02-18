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

	tests := []struct {
		name       string
		cfg        *domain.Config
		remoteAddr string
		wantStatus int
	}{
		{
			name:       "passes through when config is nil",
			cfg:        nil,
			remoteAddr: "203.0.113.10:12345",
			wantStatus: http.StatusOK,
		},
		{
			name:       "passes when auth-disabled mode is off",
			cfg:        &domain.Config{},
			remoteAddr: "203.0.113.10:12345",
			wantStatus: http.StatusOK,
		},
		{
			name: "allows request from configured CIDR",
			cfg: &domain.Config{
				AuthDisabled:               true,
				IAcknowledgeThisIsABadIdea: true,
				AuthDisabledAllowedCIDRs:   []string{"127.0.0.1/32"},
			},
			remoteAddr: "127.0.0.1:54321",
			wantStatus: http.StatusOK,
		},
		{
			name: "blocks request outside CIDR",
			cfg: &domain.Config{
				AuthDisabled:               true,
				IAcknowledgeThisIsABadIdea: true,
				AuthDisabledAllowedCIDRs:   []string{"127.0.0.1/32"},
			},
			remoteAddr: "203.0.113.10:54321",
			wantStatus: http.StatusForbidden,
		},
		{
			name: "blocks when configured list is invalid",
			cfg: &domain.Config{
				AuthDisabled:               true,
				IAcknowledgeThisIsABadIdea: true,
				AuthDisabledAllowedCIDRs:   []string{"invalid-cidr"},
			},
			remoteAddr: "127.0.0.1:54321",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := RequireAuthDisabledIPAllowlist(tc.cfg)(inner)

			req := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
			req.RemoteAddr = tc.remoteAddr
			resp := httptest.NewRecorder()

			handler.ServeHTTP(resp, req)
			assert.Equal(t, tc.wantStatus, resp.Code)
		})
	}
}
