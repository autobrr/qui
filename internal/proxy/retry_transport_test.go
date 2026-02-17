// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRoundTripper is a mock implementation of http.RoundTripper for testing
type mockRoundTripper struct {
	attempts   int
	maxAttempt int
	err        error
	response   *http.Response
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m.attempts++
	if m.attempts <= m.maxAttempt {
		return nil, m.err
	}
	if m.response != nil {
		return m.response, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("OK")),
	}, nil
}

func TestRetryTransport_Success(t *testing.T) {
	mock := &mockRoundTripper{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("success")),
		},
	}

	transport := NewRetryTransport(mock)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, mock.attempts)
}

func TestRetryTransport_SelectorOverridesBase(t *testing.T) {
	base := &mockRoundTripper{
		response: &http.Response{
			StatusCode: http.StatusTeapot,
			Body:       io.NopCloser(strings.NewReader("base")),
		},
	}

	selected := &mockRoundTripper{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("selected")),
		},
	}

	transport := NewRetryTransportWithSelector(base, func(req *http.Request) http.RoundTripper {
		return selected
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.com", http.NoBody)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 0, base.attempts)
	assert.Equal(t, 1, selected.attempts)
}

func TestRetryTransport_RetryableError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		expectRetry   bool
		maxFailures   int
		expectedCalls int
	}{
		{
			name:          "Connection refused - should retry",
			err:           syscall.ECONNREFUSED,
			expectRetry:   true,
			maxFailures:   2,
			expectedCalls: 3, // 2 failures + 1 success
		},
		{
			name:          "Connection reset - should retry",
			err:           syscall.ECONNRESET,
			expectRetry:   true,
			maxFailures:   1,
			expectedCalls: 2,
		},
		{
			name:          "EOF - should retry",
			err:           io.EOF,
			expectRetry:   true,
			maxFailures:   1,
			expectedCalls: 2,
		},
		{
			name: "OpError dial - should retry",
			err: &net.OpError{
				Op:  "dial",
				Err: errors.New("connection refused"),
			},
			expectRetry:   true,
			maxFailures:   1,
			expectedCalls: 2,
		},
		{
			name: "OpError read - should retry",
			err: &net.OpError{
				Op:  "read",
				Err: errors.New("connection reset by peer"),
			},
			expectRetry:   true,
			maxFailures:   1,
			expectedCalls: 2,
		},
		{
			name: "URL error with retryable underlying error",
			err: &url.Error{
				Op:  "Get",
				URL: "http://example.com",
				Err: syscall.ECONNREFUSED,
			},
			expectRetry:   true,
			maxFailures:   1,
			expectedCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRoundTripper{
				err:        tt.err,
				maxAttempt: tt.maxFailures,
			}

			transport := NewRetryTransport(mock)

			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)
			require.NoError(t, err)

			resp, err := transport.RoundTrip(req)
			if resp != nil {
				t.Cleanup(func() { _ = resp.Body.Close() })
			}

			if tt.expectRetry && tt.maxFailures < maxRetries {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, http.StatusOK, resp.StatusCode)
			}

			assert.Equal(t, tt.expectedCalls, mock.attempts)
		})
	}
}

func TestRetryTransport_NonRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "Context canceled",
			err:  context.Canceled,
		},
		{
			name: "Generic error",
			err:  errors.New("some generic error"),
		},
		{
			name: "Unexpected EOF",
			err:  errors.New("unexpected EOF"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRoundTripper{
				err:        tt.err,
				maxAttempt: maxRetries + 1,
			}

			transport := NewRetryTransport(mock)

			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)
			require.NoError(t, err)

			resp, err := transport.RoundTrip(req)
			if resp != nil {
				t.Cleanup(func() { _ = resp.Body.Close() })
			}
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Equal(t, 1, mock.attempts, "Should not retry non-retryable errors")
		})
	}
}

func TestRetryTransport_NonIdempotentMethod(t *testing.T) {
	methods := []string{http.MethodPost, http.MethodPatch}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			mock := &mockRoundTripper{
				err:        syscall.ECONNREFUSED,
				maxAttempt: maxRetries + 1,
			}

			transport := NewRetryTransport(mock)

			req, err := http.NewRequestWithContext(context.Background(), method, "http://example.com", http.NoBody)
			require.NoError(t, err)

			resp, err := transport.RoundTrip(req)
			if resp != nil {
				t.Cleanup(func() { _ = resp.Body.Close() })
			}
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Equal(t, 1, mock.attempts, "Should not retry non-idempotent methods")
		})
	}
}

func TestRetryTransport_IdempotentMethods(t *testing.T) {
	methods := []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			mock := &mockRoundTripper{
				err:        syscall.ECONNREFUSED,
				maxAttempt: 1,
			}

			transport := NewRetryTransport(mock)

			req, err := http.NewRequestWithContext(context.Background(), method, "http://example.com", http.NoBody)
			require.NoError(t, err)

			resp, err := transport.RoundTrip(req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			t.Cleanup(func() { _ = resp.Body.Close() })
			assert.Equal(t, 2, mock.attempts, "Should retry idempotent methods")
		})
	}
}

func TestRetryTransport_MaxRetries(t *testing.T) {
	mock := &mockRoundTripper{
		err:        syscall.ECONNREFUSED,
		maxAttempt: maxRetries + 10, // More than max retries
	}

	transport := NewRetryTransport(mock)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)
	require.NoError(t, err)

	start := time.Now()
	resp, err := transport.RoundTrip(req)
	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	duration := time.Since(start)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, maxRetries+1, mock.attempts, "Should stop after max retries")

	// Verify that backoff was applied (should take at least some time)
	// With exponential backoff: 50ms + 100ms + 200ms = 350ms minimum
	minExpected := initialRetryWait + (initialRetryWait * 2) + (initialRetryWait * 4)
	assert.GreaterOrEqual(t, duration, minExpected, "Should apply backoff between retries")
}

func TestRetryTransport_ContextCancellation(t *testing.T) {
	mock := &mockRoundTripper{
		err:        syscall.ECONNREFUSED,
		maxAttempt: maxRetries + 1,
	}

	transport := NewRetryTransport(mock)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", http.NoBody)
	require.NoError(t, err)

	// Cancel context after first failure
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Less(t, mock.attempts, maxRetries+1, "Should stop on context cancellation")
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection refused syscall",
			err:      syscall.ECONNREFUSED,
			expected: true,
		},
		{
			name:     "connection reset syscall",
			err:      syscall.ECONNRESET,
			expected: true,
		},
		{
			name:     "broken pipe syscall",
			err:      syscall.EPIPE,
			expected: true,
		},
		{
			name:     "EOF",
			err:      io.EOF,
			expected: true,
		},
		{
			name:     "connection refused string",
			err:      errors.New("connection refused"),
			expected: true,
		},
		{
			name:     "connection reset string",
			err:      errors.New("connection reset by peer"),
			expected: true,
		},
		{
			name:     "broken pipe string",
			err:      errors.New("broken pipe"),
			expected: true,
		},
		{
			name:     "no such host",
			err:      errors.New("no such host"),
			expected: true,
		},
		{
			name:     "network unreachable",
			err:      errors.New("network is unreachable"),
			expected: true,
		},
		{
			name:     "unexpected EOF",
			err:      errors.New("unexpected EOF"),
			expected: false,
		},
		{
			name:     "generic error",
			err:      errors.New("some random error"),
			expected: false,
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsIdempotentMethod(t *testing.T) {
	tests := []struct {
		method   string
		expected bool
	}{
		{http.MethodGet, true},
		{http.MethodHead, true},
		{http.MethodOptions, true},
		{http.MethodTrace, true},
		{http.MethodPost, false},
		{http.MethodPut, false},
		{http.MethodDelete, false},
		{http.MethodPatch, false},
		{"CUSTOM", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			result := isIdempotentMethod(tt.method)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		name     string
		attempt  int
		initial  time.Duration
		max      time.Duration
		expected time.Duration
	}{
		{
			name:     "First attempt",
			attempt:  0,
			initial:  50 * time.Millisecond,
			max:      500 * time.Millisecond,
			expected: 50 * time.Millisecond,
		},
		{
			name:     "Second attempt",
			attempt:  1,
			initial:  50 * time.Millisecond,
			max:      500 * time.Millisecond,
			expected: 100 * time.Millisecond,
		},
		{
			name:     "Third attempt",
			attempt:  2,
			initial:  50 * time.Millisecond,
			max:      500 * time.Millisecond,
			expected: 200 * time.Millisecond,
		},
		{
			name:     "Fourth attempt (capped)",
			attempt:  3,
			initial:  50 * time.Millisecond,
			max:      500 * time.Millisecond,
			expected: 400 * time.Millisecond,
		},
		{
			name:     "Fifth attempt (capped at max)",
			attempt:  4,
			initial:  50 * time.Millisecond,
			max:      500 * time.Millisecond,
			expected: 500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateBackoff(tt.attempt, tt.initial, tt.max)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// mockTransportWithIdleClose is a mock that tracks CloseIdleConnections calls
type mockTransportWithIdleClose struct {
	mockRoundTripper
	closeIdleCalled int
}

func (m *mockTransportWithIdleClose) CloseIdleConnections() {
	m.closeIdleCalled++
}

func TestRetryTransport_ClosesIdleConnections(t *testing.T) {
	mock := &mockTransportWithIdleClose{
		mockRoundTripper: mockRoundTripper{
			err:        syscall.ECONNREFUSED,
			maxAttempt: 1, // Fail once, then succeed
		},
	}

	transport := NewRetryTransport(mock)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	t.Cleanup(func() { _ = resp.Body.Close() })

	// Should have called CloseIdleConnections once for the failed attempt
	assert.Equal(t, 1, mock.closeIdleCalled, "Should close idle connections on network error")
	assert.Equal(t, 2, mock.attempts, "Should have retried after network error")
}

func TestRetryTransport_ClosesIdleConnectionsMultipleTimes(t *testing.T) {
	mock := &mockTransportWithIdleClose{
		mockRoundTripper: mockRoundTripper{
			err:        io.EOF,
			maxAttempt: 2, // Fail twice, then succeed
		},
	}

	transport := NewRetryTransport(mock)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	t.Cleanup(func() { _ = resp.Body.Close() })

	// Should have called CloseIdleConnections twice for the two failed attempts
	assert.Equal(t, 2, mock.closeIdleCalled, "Should close idle connections for each network error")
	assert.Equal(t, 3, mock.attempts, "Should have retried after network errors")
}

func TestRetryTransport_DoesNotCloseOnNonRetryableError(t *testing.T) {
	mock := &mockTransportWithIdleClose{
		mockRoundTripper: mockRoundTripper{
			err:        errors.New("some non-retryable error"),
			maxAttempt: 10,
		},
	}

	transport := NewRetryTransport(mock)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	require.Error(t, err)

	// Should not have called CloseIdleConnections for non-retryable error
	assert.Equal(t, 0, mock.closeIdleCalled, "Should not close idle connections on non-retryable error")
	assert.Equal(t, 1, mock.attempts, "Should not retry non-retryable error")
}
