// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package httphelpers

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeBasePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Empty and root cases
		{"empty string", "", ""},
		{"just slash", "/", ""},
		{"just whitespace", "   ", ""},

		// Normal paths
		{"simple path", "/api", "/api"},
		{"path with trailing slash", "/api/", "/api"},
		{"path without leading slash", "api", "/api"},
		{"path without leading slash with trailing", "api/", "/api"},

		// Nested paths
		{"nested path", "/api/v1", "/api/v1"},
		{"nested path with trailing slash", "/api/v1/", "/api/v1"},
		{"nested path without leading slash", "api/v1", "/api/v1"},

		// Whitespace handling
		{"leading whitespace", "  /api", "/api"},
		{"trailing whitespace", "/api  ", "/api"},
		{"both whitespace", "  /api  ", "/api"},

		// Edge cases
		{"multiple trailing slashes", "/api///", "/api"},
		{"just multiple slashes", "///", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := NormalizeBasePath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestJoinBasePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		basePath string
		suffix   string
		expected string
	}{
		// Empty base path
		{"empty base, empty suffix", "", "", "/"},
		{"empty base, root suffix", "", "/", "/"},
		{"empty base, relative suffix", "", "foo", "/foo"},
		{"empty base, absolute suffix", "", "/foo", "/foo"},

		// Non-empty base path
		{"with base, empty suffix", "/api", "", "/api"},
		{"with base, relative suffix", "/api", "v1", "/api/v1"},
		{"with base, absolute suffix", "/api", "/v1", "/api/v1"},

		// Nested paths
		{"nested base, relative suffix", "/api/v1", "users", "/api/v1/users"},
		{"nested base, absolute suffix", "/api/v1", "/users", "/api/v1/users"},
		{"nested base and suffix", "/api/v1", "users/123", "/api/v1/users/123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := JoinBasePath(tt.basePath, tt.suffix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDrainAndClose(t *testing.T) {
	t.Parallel()

	t.Run("nil response", func(t *testing.T) {
		t.Parallel()
		// Should not panic
		DrainAndClose(nil)
	})

	t.Run("nil body", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{Body: nil}
		// Should not panic
		DrainAndClose(resp)
	})

	t.Run("drains and closes body", func(t *testing.T) {
		t.Parallel()

		body := io.NopCloser(bytes.NewReader([]byte("test body content")))
		resp := &http.Response{Body: body}

		DrainAndClose(resp)

		// After draining and closing, reading should return EOF or error
		_, err := resp.Body.Read(make([]byte, 1))
		assert.Error(t, err)
	})

	t.Run("closes body after drain", func(t *testing.T) {
		t.Parallel()

		closed := false
		body := &mockReadCloser{
			reader:  bytes.NewReader([]byte("test")),
			onClose: func() { closed = true },
		}
		resp := &http.Response{Body: body}

		DrainAndClose(resp)

		assert.True(t, closed)
	})
}

// mockReadCloser is a test helper that tracks Close calls
type mockReadCloser struct {
	reader  io.Reader
	onClose func()
}

func (m *mockReadCloser) Read(p []byte) (n int, err error) {
	return m.reader.Read(p)
}

func (m *mockReadCloser) Close() error {
	if m.onClose != nil {
		m.onClose()
	}
	return nil
}
