// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogger_Success(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf).Level(zerolog.TraceLevel)

	handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "success", rec.Body.String())

	// Check log output
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, `"type":"access"`)
	assert.Contains(t, logOutput, `"url":"/test"`)
	assert.Contains(t, logOutput, `"method":"GET"`)
	assert.Contains(t, logOutput, `"status":200`)
}

func TestLogger_DifferentStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"200 OK", http.StatusOK},
		{"201 Created", http.StatusCreated},
		{"400 Bad Request", http.StatusBadRequest},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			logger := zerolog.New(&logBuf).Level(zerolog.TraceLevel)

			handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.statusCode, rec.Code)
		})
	}
}

func TestLogger_DifferentMethods(t *testing.T) {
	methods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			var logBuf bytes.Buffer
			logger := zerolog.New(&logBuf).Level(zerolog.TraceLevel)

			handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(method, "/", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			logOutput := logBuf.String()
			assert.Contains(t, logOutput, `"method":"`+method+`"`)
		})
	}
}

func TestLogger_PanicRecovery(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf).Level(zerolog.TraceLevel)

	handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	// Should not panic, middleware should recover
	require.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	// Check log output for panic recovery
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, `"type":"error"`)
	assert.Contains(t, logOutput, "test panic")
}

func TestLogger_TracesLatency(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf).Level(zerolog.TraceLevel)

	handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Check that latency is logged
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "latency_ms")
}

func TestLogger_BytesInOut(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf).Level(zerolog.TraceLevel)

	handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response body"))
	}))

	body := bytes.NewReader([]byte("request body"))
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Length", "12")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "bytes_in")
	assert.Contains(t, logOutput, "bytes_out")
}

func TestLogger_UserAgent(t *testing.T) {
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf).Level(zerolog.TraceLevel)

	handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "TestClient/1.0")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "TestClient/1.0")
}

func TestChiMiddlewareExports(t *testing.T) {
	// Verify middleware exports are not nil
	assert.NotNil(t, RequestID)
	assert.NotNil(t, Recoverer)
	assert.NotNil(t, RealIP)
	assert.NotNil(t, ThrottleBacklog)
}
