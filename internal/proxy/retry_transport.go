// Copyright (c) 2025, s0up and the autobrr contributors.
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
}

// NewRetryTransport creates a new RetryTransport that wraps the given RoundTripper
func NewRetryTransport(base http.RoundTripper) *RetryTransport {
	return &RetryTransport{
		base: base,
	}
}

// RoundTrip implements http.RoundTripper with retry logic for transient errors
func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Clone the request for retry attempts to ensure body can be replayed if needed
		reqClone := req.Clone(req.Context())

		resp, err := t.base.RoundTrip(reqClone)

		// Success case
		if err == nil {
			if attempt > 0 {
				log.Debug().
					Str("method", req.Method).
					Str("url", req.URL.String()).
					Int("attempt", attempt+1).
					Msg("Proxy request succeeded after retry")
			}
			return resp, nil
		}

		lastErr = err

		// Check if the error is retryable
		if !isRetryableError(err) {
			log.Debug().
				Err(err).
				Str("method", req.Method).
				Str("url", req.URL.String()).
				Msg("Proxy request failed with non-retryable error")
			return nil, err
		}

		// Close idle connections to clear potentially stale connections from the pool
		// This helps recover from connection pool issues
		t.closeIdleConnections()

		// Check if the request method is safe to retry
		if !isIdempotentMethod(req.Method) {
			log.Debug().
				Err(err).
				Str("method", req.Method).
				Str("url", req.URL.String()).
				Msg("Proxy request failed but method is not idempotent, not retrying")
			return nil, err
		}

		// Don't retry if we've exhausted our attempts
		if attempt >= maxRetries {
			log.Warn().
				Err(err).
				Str("method", req.Method).
				Str("url", req.URL.String()).
				Int("attempts", attempt+1).
				Msg("Proxy request failed after max retries")
			return nil, err
		}

		// Calculate backoff duration with exponential increase
		backoff := calculateBackoff(attempt, initialRetryWait, maxRetryWait)

		log.Debug().
			Err(err).
			Str("method", req.Method).
			Str("url", req.URL.String()).
			Int("attempt", attempt+1).
			Dur("backoff", backoff).
			Msg("Proxy request failed with retryable error, retrying")

		// Wait before retry
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(backoff):
		}
	}

	return nil, lastErr
}

// closeIdleConnections closes idle connections in the transport if supported
// This helps clear potentially stale connections from the connection pool
func (t *RetryTransport) closeIdleConnections() {
	type closeIdler interface {
		CloseIdleConnections()
	}

	if tr, ok := t.base.(closeIdler); ok {
		tr.CloseIdleConnections()
		log.Debug().Msg("Closed idle connections after network error")
	}
}

// isRetryableError determines if an error is a transient network error that should be retried
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for common transient network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		// Retry on temporary network errors, but be careful with timeouts
		// Only retry timeouts if they're connection timeouts, not long-running request timeouts
		if netErr.Temporary() {
			return true
		}
		// Don't retry on timeout errors as they might be legitimate slow responses
		if netErr.Timeout() {
			return false
		}
	}

	// Check for specific network errors that are retryable
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		// Retry on connection refused, connection reset, etc.
		if opErr.Op == "dial" || opErr.Op == "read" {
			return true
		}
	}

	// Check for URL errors
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// Recursively check the underlying error
		return isRetryableError(urlErr.Err)
	}

	// Check for syscall errors
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) {
		return true
	}

	// Check for common error messages
	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "network is unreachable") ||
		(strings.Contains(errStr, "eof") && !strings.Contains(errStr, "unexpected eof")) {
		return true
	}

	// Check for io.EOF which can indicate connection pool issues
	if errors.Is(err, io.EOF) {
		return true
	}

	return false
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
func calculateBackoff(attempt int, initial, max time.Duration) time.Duration {
	backoff := initial
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if backoff > max {
			return max
		}
	}
	return backoff
}
