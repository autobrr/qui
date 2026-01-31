// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/pkg/redact"
)

const (
	// Retry configuration for transient network errors
	maxRetries       = 3
	initialRetryWait = 50 * time.Millisecond
	maxRetryWait     = 500 * time.Millisecond
)

// RetryTransport wraps an http.RoundTripper with retry logic for transient network errors
type RetryTransport struct {
	base http.RoundTripper
	// baseSelector allows choosing a per-request transport (e.g. instance-specific TLS settings).
	// When nil or returning nil, base is used.
	baseSelector func(*http.Request) http.RoundTripper
}

// NewRetryTransport creates a new RetryTransport that wraps the given RoundTripper
func NewRetryTransport(base http.RoundTripper) *RetryTransport {
	return &RetryTransport{
		base: base,
	}
}

// NewRetryTransportWithSelector creates a new RetryTransport using a per-request selector.
// The selector may return nil to fall back to the default base transport.
func NewRetryTransportWithSelector(base http.RoundTripper, selector func(*http.Request) http.RoundTripper) *RetryTransport {
	return &RetryTransport{
		base:         base,
		baseSelector: selector,
	}
}

func (t *RetryTransport) transportFor(req *http.Request) http.RoundTripper {
	if t.baseSelector == nil {
		return t.base
	}
	if selected := t.baseSelector(req); selected != nil {
		return selected
	}
	return t.base
}

func (t *RetryTransport) retryBackoff(req *http.Request, attempt int, err error, base http.RoundTripper) (time.Duration, bool) {
	// Check if the error is retryable
	if !isRetryableError(err) {
		log.Debug().
			Str("error", redact.String(err.Error())).
			Str("method", req.Method).
			Str("url", redact.URLString(req.URL.String())).
			Msg("Proxy request failed with non-retryable error")
		return 0, false
	}

	// Close idle connections to clear potentially stale connections from the pool
	// This helps recover from connection pool issues
	t.closeIdleConnections(base)

	// Check if the request method is safe to retry
	if !isIdempotentMethod(req.Method) {
		log.Debug().
			Str("error", redact.String(err.Error())).
			Str("method", req.Method).
			Str("url", redact.URLString(req.URL.String())).
			Msg("Proxy request failed but method is not idempotent, not retrying")
		return 0, false
	}

	// Don't retry if we've exhausted our attempts
	if attempt >= maxRetries {
		log.Warn().
			Str("error", redact.String(err.Error())).
			Str("method", req.Method).
			Str("url", redact.URLString(req.URL.String())).
			Int("attempts", attempt+1).
			Msg("Proxy request failed after max retries")
		return 0, false
	}

	// Calculate backoff duration with exponential increase
	backoff := calculateBackoff(attempt, initialRetryWait, maxRetryWait)

	log.Debug().
		Str("error", redact.String(err.Error())).
		Str("method", req.Method).
		Str("url", redact.URLString(req.URL.String())).
		Int("attempt", attempt+1).
		Dur("backoff", backoff).
		Msg("Proxy request failed with retryable error, retrying")

	return backoff, true
}

//nolint:wrapcheck // Callers expect unwrapped context errors from transports.
func waitForRetry(req *http.Request, backoff time.Duration) error {
	// Wait before retry
	select {
	case <-req.Context().Done():
		return req.Context().Err()
	case <-time.After(backoff):
		return nil
	}
}

// RoundTrip implements http.RoundTripper with retry logic for transient errors
//
//nolint:wrapcheck // RoundTrip should not wrap errors - callers expect unwrapped transport errors
func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error

	base := t.transportFor(req)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Clone the request for retry attempts to ensure body can be replayed if needed
		reqClone := req.Clone(req.Context())

		resp, err := base.RoundTrip(reqClone)

		// Success case
		if err == nil {
			if attempt > 0 {
				log.Debug().
					Str("method", req.Method).
					Str("url", redact.URLString(req.URL.String())).
					Int("attempt", attempt+1).
					Msg("Proxy request succeeded after retry")
			}
			return resp, nil
		}

		lastErr = err

		backoff, shouldRetry := t.retryBackoff(req, attempt, err, base)
		if !shouldRetry {
			return nil, err
		}

		if err := waitForRetry(req, backoff); err != nil {
			return nil, err
		}
	}

	return nil, lastErr
}

// closeIdleConnections closes idle connections in the transport if supported
// This helps clear potentially stale connections from the connection pool
func (t *RetryTransport) closeIdleConnections(base http.RoundTripper) {
	type closeIdler interface {
		CloseIdleConnections()
	}

	if tr, ok := base.(closeIdler); ok {
		tr.CloseIdleConnections()
		log.Debug().Msg("Closed idle connections after network error")
	}
}

// isRetryableError determines if an error is a transient network error that should be retried
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for URL errors first - unwrap and check underlying error
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isRetryableError(urlErr.Err)
	}

	// Check typed errors
	if isRetryableNetError(err) || isRetryableSyscallError(err) || errors.Is(err, io.EOF) {
		return true
	}

	// Check error message patterns as fallback
	return isRetryableErrorMessage(err)
}

// isRetryableNetError checks if a net.Error or net.OpError indicates a retryable condition
func isRetryableNetError(err error) bool {
	// Check OpError first - dial errors (including dial timeouts) are retryable
	// since they indicate transient connection issues, not slow server responses
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Op == "dial" {
			return true
		}
		// Read operations are retryable unless they're timeouts
		if opErr.Op == "read" {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return false // Read timeout = slow server, don't retry
			}
			return true
		}
	}

	// Non-dial/read timeouts shouldn't retry
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}

	return false
}

// isRetryableSyscallError checks for retryable syscall errors
func isRetryableSyscallError(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE)
}

// isRetryableErrorMessage checks error message patterns for retryable conditions
func isRetryableErrorMessage(err error) bool {
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "network is unreachable") ||
		(strings.Contains(errStr, "eof") && !strings.Contains(errStr, "unexpected eof"))
}

// isIdempotentMethod checks if the HTTP method is safe to retry
func isIdempotentMethod(method string) bool {
	// Safe methods according to RFC 7231
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	case http.MethodPut, http.MethodDelete:
		// PUT and DELETE are technically idempotent but we should be more conservative
		// in a proxy scenario. For qBittorrent API, most mutations are POST anyway.
		return false
	default:
		return false
	}
}

// calculateBackoff calculates exponential backoff duration with a cap
func calculateBackoff(attempt int, initial, maxBackoff time.Duration) time.Duration {
	backoff := initial
	for range attempt {
		backoff *= 2
		if backoff > maxBackoff {
			return maxBackoff
		}
	}
	return backoff
}
