// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encodingJSONLine(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b) + "\n"
}

func TestRespondJSON_NoContent(t *testing.T) {
	rr := httptest.NewRecorder()

	RespondJSON(rr, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusNoContent, map[string]string{"ignored": "value"})

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Empty(t, rr.Body.String(), "body must be empty for 204")
	assert.Empty(t, rr.Header().Get("Content-Type"), "Content-Type must not be set for 204")
}

func TestRespondJSON_NotModified(t *testing.T) {
	rr := httptest.NewRecorder()

	RespondJSON(rr, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusNotModified, map[string]string{"ignored": "value"})

	assert.Equal(t, http.StatusNotModified, rr.Code)
	assert.Empty(t, rr.Body.String(), "body must be empty for 304")
	assert.Empty(t, rr.Header().Get("Content-Type"), "Content-Type must not be set for 304")
}

func TestRespondJSON_WritesTheBytesOfEncodingJSON(t *testing.T) {
	tests := []struct {
		name   string
		status int
		data   any
		want   string
	}{
		{name: "map", status: http.StatusOK, data: map[string]string{"key": "value"}, want: `{"key":"value"}` + "\n"},
		{name: "HTML characters", status: http.StatusCreated, data: map[string]string{"a": "<b> & é"}, want: `{"a":"\u003cb\u003e \u0026 é"}` + "\n"},
		{name: "slice", status: http.StatusOK, data: []int{1, 2}, want: "[1,2]\n"},
		{name: "typed nil slice", status: http.StatusOK, data: []string(nil), want: "null\n"},
		{name: "typed nil pointer", status: http.StatusOK, data: (*ErrorResponse)(nil), want: "null\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()

			RespondJSON(rr, httptest.NewRequest(http.MethodGet, "/", nil), tt.status, tt.data)

			assert.Equal(t, tt.status, rr.Code)
			assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
			assert.Equal(t, tt.want, rr.Body.String())
		})
	}
}

func TestRespondJSON_NilData(t *testing.T) {
	rr := httptest.NewRecorder()

	RespondJSON(rr, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, rr.Header().Get("Content-Type"), "Content-Type should not be set for nil data")
	assert.Empty(t, rr.Body.String())
}

func TestRespondError_WritesTheErrorBody(t *testing.T) {
	rr := httptest.NewRecorder()

	RespondError(rr, http.StatusBadRequest, "bad <input>")

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.Equal(t, encodingJSONLine(t, ErrorResponse{Error: "bad <input>"}), rr.Body.String())
}

type failingWriter struct {
	header http.Header
	status int
	err    error
}

func (w *failingWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *failingWriter) WriteHeader(status int) { w.status = status }

func (w *failingWriter) Write([]byte) (int, error) { return 0, w.err }

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	origLogger := log.Logger
	origLevel := zerolog.GlobalLevel()
	log.Logger = zerolog.New(&buf).Level(zerolog.TraceLevel)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		log.Logger = origLogger
		zerolog.SetGlobalLevel(origLevel)
	})

	return &buf
}

func TestRespondJSON_ClientDisconnectLogsDebug(t *testing.T) {
	logs := captureLogs(t)
	w := &failingWriter{err: &net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.EPIPE)}}

	RespondJSON(w, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, map[string]string{"key": "value"})

	assert.Equal(t, http.StatusOK, w.status)
	assert.NotContains(t, logs.String(), `"level":"error"`)
	assert.Contains(t, logs.String(), `"level":"debug"`)
	assert.Contains(t, logs.String(), "Client disconnected")
}

func TestRespondJSON_EncodeFailureLogsError(t *testing.T) {
	tests := []struct {
		name string
		data any
	}{
		{name: "channel", data: make(chan int)},
		{name: "marshaler error", data: failingMarshaler{err: syscall.EPIPE}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			rr := httptest.NewRecorder()

			RespondJSON(rr, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, tt.data)

			assert.Equal(t, http.StatusInternalServerError, rr.Code)
			assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
			assert.Equal(t, encodingJSONLine(t, ErrorResponse{Error: "Failed to encode response"}), rr.Body.String())
			assert.Contains(t, logs.String(), `"level":"error"`)
			assert.Contains(t, logs.String(), "Failed to encode JSON response")
		})
	}
}

func TestRespondJSON_UnrelatedWriterErrorLogsError(t *testing.T) {
	logs := captureLogs(t)
	w := &failingWriter{err: io.ErrShortWrite}

	RespondJSON(w, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, map[string]string{"key": "value"})

	assert.Contains(t, logs.String(), `"level":"error"`)
	assert.Contains(t, logs.String(), "Failed to write JSON response")
}

type failingMarshaler struct{ err error }

func (m failingMarshaler) MarshalJSON() ([]byte, error) { return nil, m.err }
