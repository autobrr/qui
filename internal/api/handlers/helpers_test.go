// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRespondJSON_NoContent(t *testing.T) {
	rr := httptest.NewRecorder()

	// Pass a payload that would normally be encoded
	RespondJSON(rr, http.StatusNoContent, map[string]string{"ignored": "value"})

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Empty(t, rr.Body.String(), "body must be empty for 204")
	assert.Empty(t, rr.Header().Get("Content-Type"), "Content-Type must not be set for 204")
}

func TestRespondJSON_NotModified(t *testing.T) {
	rr := httptest.NewRecorder()

	RespondJSON(rr, http.StatusNotModified, map[string]string{"ignored": "value"})

	assert.Equal(t, http.StatusNotModified, rr.Code)
	assert.Empty(t, rr.Body.String(), "body must be empty for 304")
	assert.Empty(t, rr.Header().Get("Content-Type"), "Content-Type must not be set for 304")
}

func TestRespondJSON_OK(t *testing.T) {
	rr := httptest.NewRecorder()

	RespondJSON(rr, http.StatusOK, map[string]string{"key": "value"})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"key":"value"}`, rr.Body.String())
}

func TestRespondJSON_NilData(t *testing.T) {
	rr := httptest.NewRecorder()

	RespondJSON(rr, http.StatusOK, nil)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, rr.Header().Get("Content-Type"), "Content-Type should not be set for nil data")
	assert.Empty(t, rr.Body.String())
}

// failingWriter is a response writer whose body writes always fail with err.
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
	tests := []struct {
		name string
		err  error
	}{
		{name: "epipe", err: fmt.Errorf("write tcp: %w", syscall.EPIPE)},
		{name: "econnreset", err: fmt.Errorf("write tcp: %w", syscall.ECONNRESET)},
		{name: "windows aborted", err: errors.New("wsasend: An established connection was aborted by the software in your host machine.")},
		{name: "windows forcibly closed", err: errors.New("wsasend: An existing connection was forcibly closed by the remote host.")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			w := &failingWriter{err: tt.err}

			RespondJSON(w, http.StatusOK, map[string]string{"key": "value"})

			assert.Equal(t, http.StatusOK, w.status)
			assert.NotContains(t, logs.String(), `"level":"error"`)
			assert.NotContains(t, logs.String(), "Failed to encode JSON response")
			assert.Contains(t, logs.String(), `"level":"debug"`)
			assert.Contains(t, logs.String(), "Client disconnected")
		})
	}
}

func TestRespondJSON_EncodeFailureLogsError(t *testing.T) {
	tests := []struct {
		name string
		data any
	}{
		{name: "channel", data: make(chan int)},
		{name: "nan", data: math.NaN()},
		{name: "marshaler error", data: failingMarshaler{err: syscall.EPIPE}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			rr := httptest.NewRecorder()

			RespondJSON(rr, http.StatusOK, tt.data)

			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Contains(t, logs.String(), `"level":"error"`)
			assert.Contains(t, logs.String(), "Failed to encode JSON response")
		})
	}
}

func TestRespondJSON_UnrelatedWriterErrorLogsError(t *testing.T) {
	logs := captureLogs(t)
	w := &failingWriter{err: io.ErrShortWrite}

	RespondJSON(w, http.StatusOK, map[string]string{"key": "value"})

	assert.Contains(t, logs.String(), `"level":"error"`)
	assert.Contains(t, logs.String(), "Failed to encode JSON response")
}

func TestRespondJSON_NoBodyPathsLogNothing(t *testing.T) {
	logs := captureLogs(t)

	RespondJSON(httptest.NewRecorder(), http.StatusNoContent, map[string]string{"a": "b"})
	RespondJSON(httptest.NewRecorder(), http.StatusNotModified, map[string]string{"a": "b"})
	RespondJSON(&failingWriter{err: syscall.EPIPE}, http.StatusOK, nil)

	require.Empty(t, logs.String())
}

// failingMarshaler fails to serialize with err, which looks like a disconnect.
type failingMarshaler struct{ err error }

func (m failingMarshaler) MarshalJSON() ([]byte, error) { return nil, m.err }
