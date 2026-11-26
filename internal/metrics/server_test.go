// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestMetricsServerAllowsColonInPassword(t *testing.T) {
	server := NewMetricsServer(
		&MetricsManager{registry: prometheus.NewRegistry()},
		"127.0.0.1",
		9074,
		"alice:secret:with:colons",
	)

	tests := []struct {
		name     string
		password string
		wantCode int
	}{
		{name: "rejects wrong password", password: "wrong", wantCode: http.StatusUnauthorized},
		{name: "accepts configured password", password: "secret:with:colons", wantCode: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			request.SetBasicAuth("alice", tt.password)
			response := httptest.NewRecorder()

			server.server.Handler.ServeHTTP(response, request)

			assert.Equal(t, tt.wantCode, response.Code)
		})
	}
}

func TestMetricsServerMalformedBasicAuthFailsClosed(t *testing.T) {
	server := NewMetricsServer(
		&MetricsManager{registry: prometheus.NewRegistry()},
		"127.0.0.1",
		9074,
		"missing-delimiter",
	)
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
}
