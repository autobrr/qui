// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMetricsServer(t *testing.T) {
	t.Parallel()

	manager := NewMetricsManager(nil, nil)

	tests := []struct {
		name             string
		host             string
		port             int
		basicAuthUsers   string
		expectedAddr     string
		expectedAuthSize int
	}{
		{
			name:             "default config",
			host:             "127.0.0.1",
			port:             9090,
			basicAuthUsers:   "",
			expectedAddr:     "127.0.0.1:9090",
			expectedAuthSize: 0,
		},
		{
			name:             "with single basic auth user",
			host:             "0.0.0.0",
			port:             8080,
			basicAuthUsers:   "user:password",
			expectedAddr:     "0.0.0.0:8080",
			expectedAuthSize: 1,
		},
		{
			name:             "with multiple basic auth users",
			host:             "localhost",
			port:             9191,
			basicAuthUsers:   "user1:pass1,user2:pass2",
			expectedAddr:     "localhost:9191",
			expectedAuthSize: 2,
		},
		{
			name:             "with invalid auth entry skipped",
			host:             "localhost",
			port:             9090,
			basicAuthUsers:   "user1:pass1,invalidentry,user2:pass2",
			expectedAddr:     "localhost:9090",
			expectedAuthSize: 2,
		},
		{
			name:             "with whitespace in auth entries",
			host:             "localhost",
			port:             9090,
			basicAuthUsers:   " user1:pass1 , user2:pass2 ",
			expectedAddr:     "localhost:9090",
			expectedAuthSize: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := NewMetricsServer(manager, tt.host, tt.port, tt.basicAuthUsers)

			require.NotNil(t, server)
			assert.NotNil(t, server.server)
			assert.Equal(t, tt.expectedAddr, server.server.Addr)
			assert.Equal(t, tt.expectedAuthSize, len(server.basicAuthUsers))
			assert.Equal(t, manager, server.manager)
		})
	}
}

func TestMetricsServer_MetricsEndpoint(t *testing.T) {
	t.Parallel()

	manager := NewMetricsManager(nil, nil)
	server := NewMetricsServer(manager, "localhost", 9090, "")

	// Create a test request
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	// Serve the request
	server.server.Handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
	// Should contain at least Go runtime metrics
	body := rec.Body.String()
	assert.Contains(t, body, "go_", "Should contain Go runtime metrics")
}

func TestMetricsServer_MetricsEndpointWithBasicAuth(t *testing.T) {
	t.Parallel()

	manager := NewMetricsManager(nil, nil)
	server := NewMetricsServer(manager, "localhost", 9090, "admin:secret")

	t.Run("without credentials", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()

		server.server.Handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("with wrong credentials", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.SetBasicAuth("admin", "wrong")
		rec := httptest.NewRecorder()

		server.server.Handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("with correct credentials", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.SetBasicAuth("admin", "secret")
		rec := httptest.NewRecorder()

		server.server.Handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestMetricsServer_NonMetricsEndpoint(t *testing.T) {
	t.Parallel()

	manager := NewMetricsManager(nil, nil)
	server := NewMetricsServer(manager, "localhost", 9090, "")

	req := httptest.NewRequest(http.MethodGet, "/other", nil)
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMetricsServer_Stop(t *testing.T) {
	manager := NewMetricsManager(nil, nil)
	server := NewMetricsServer(manager, "localhost", 0, "") // port 0 for random available port

	// Start server in background
	go func() {
		_ = server.ListenAndServe()
	}()

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Stop should not error
	err := server.Stop()
	assert.NoError(t, err)
}

func TestMetricsServer_Shutdown(t *testing.T) {
	manager := NewMetricsManager(nil, nil)
	server := NewMetricsServer(manager, "localhost", 0, "")

	// Start server in background
	go func() {
		_ = server.ListenAndServe()
	}()

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Shutdown should not error
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := server.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestBasicAuth(t *testing.T) {
	t.Parallel()

	users := map[string]string{
		"user1": "pass1",
		"user2": "pass2",
	}

	handler := BasicAuth("test-realm", users)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name         string
		username     string
		password     string
		expectedCode int
	}{
		{
			name:         "valid credentials user1",
			username:     "user1",
			password:     "pass1",
			expectedCode: http.StatusOK,
		},
		{
			name:         "valid credentials user2",
			username:     "user2",
			password:     "pass2",
			expectedCode: http.StatusOK,
		},
		{
			name:         "invalid password",
			username:     "user1",
			password:     "wrongpass",
			expectedCode: http.StatusUnauthorized,
		},
		{
			name:         "unknown user",
			username:     "unknown",
			password:     "anypass",
			expectedCode: http.StatusUnauthorized,
		},
		{
			name:         "no credentials",
			username:     "",
			password:     "",
			expectedCode: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.username != "" || tt.password != "" {
				req.SetBasicAuth(tt.username, tt.password)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedCode, rec.Code)
		})
	}
}
