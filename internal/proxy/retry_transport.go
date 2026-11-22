// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"bytes"
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
	maxRetries       = 3
	initialRetryWait = 50 * time.Millisecond
	maxRetryWait     = 500 * time.Millisecond
	// Small qBittorrent form posts should be replayable after re-login.
	maxAuthReplayBodySize = 10 * 1024 * 1024
)

type RetryTransport struct {
	base         http.RoundTripper
	baseSelector func(*http.Request) http.RoundTripper
}

func NewRetryTransport(base http.RoundTripper) *RetryTransport {
	return &RetryTransport{
		base: base,
	}
}

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
	if !isRetryableError(err) {
		log.Debug().
			Str("error", redact.String(err.Error())).
			Str("method", req.Method).
			Str("url", redact.URLString(req.URL.String())).
			Msg("Proxy request failed with non-retryable error")
		return 0, false
	}

	t.closeIdleConnections(base)

	if !isIdempotentMethod(req.Method) {
		log.Debug().
			Str("error", redact.String(err.Error())).
			Str("method", req.Method).
			Str("url", redact.URLString(req.URL.String())).
			Msg("Proxy request failed but method is not idempotent, not retrying")
		return 0, false
	}

	if attempt >= maxRetries {
		log.Warn().
			Str("error", redact.String(err.Error())).
			Str("method", req.Method).
			Str("url", redact.URLString(req.URL.String())).
			Int("attempts", attempt+1).
			Msg("Proxy request failed after max retries")
		return 0, false
	}

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

func waitForRetry(req *http.Request, backoff time.Duration) error {
	select {
	case <-req.Context().Done():
		return req.Context().Err()
	case <-time.After(backoff):
		return nil
	}
}

type requestReplayState struct {
	replayable bool
}

func prepareRequestReplay(req *http.Request) (requestReplayState, error) {
	if req == nil || req.Body == nil {
		return requestReplayState{replayable: true}, nil
	}

	if req.ContentLength < 0 || req.ContentLength > maxAuthReplayBodySize {
		return requestReplayState{}, nil
	}

	if req.GetBody != nil {
		return requestReplayState{replayable: true}, nil
	}

	originalBody := req.Body
	body, err := io.ReadAll(originalBody)
	closeErr := originalBody.Close()
	if err != nil {
		return requestReplayState{}, err
	}
	if closeErr != nil {
		return requestReplayState{}, closeErr
	}

	req.Body = io.NopCloser(bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}

	return requestReplayState{replayable: true}, nil
}

func cloneRequestForAttempt(req *http.Request, attempt int) (*http.Request, error) {
	reqClone := req.Clone(req.Context())
	if attempt == 0 || req.Body == nil {
		return reqClone, nil
	}

	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		reqClone.Body = body
	}

	return reqClone, nil
}

func closeResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func canRetryForbidden(req *http.Request, proxyCtx *proxyContext, replay requestReplayState, authRetried bool) bool {
	if authRetried || proxyCtx == nil || proxyCtx.session == nil {
		return false
	}

	if req == nil || req.Body == nil {
		return true
	}

	return replay.replayable && req.GetBody != nil
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error

	base := t.transportFor(req)
	proxyCtx, _ := getProxyContext(req.Context())
	replay, err := prepareRequestReplay(req)
	if err != nil {
		return nil, err
	}
	authRetried := false

	for attempt, sendAttempt := 0, 0; attempt <= maxRetries; sendAttempt++ {
		reqClone, err := cloneRequestForAttempt(req, sendAttempt)
		if err != nil {
			return nil, err
		}
		if proxyCtx != nil {
			proxyCtx.applyAuthHeaders(reqClone)
		}

		resp, err := base.RoundTrip(reqClone)

		if err == nil {
			if resp != nil && resp.StatusCode == http.StatusForbidden && canRetryForbidden(req, proxyCtx, replay, authRetried) {
				closeResponseBody(resp)
				if err := proxyCtx.refreshSession(req.Context()); err != nil {
					return nil, err
				}
				authRetried = true
				continue
			}
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

		attempt++
	}

	return nil, lastErr
}

func (t *RetryTransport) closeIdleConnections(base http.RoundTripper) {
	type closeIdler interface {
		CloseIdleConnections()
	}

	if tr, ok := base.(closeIdler); ok {
		tr.CloseIdleConnections()
		log.Debug().Msg("Closed idle connections after network error")
	}
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return isRetryableError(urlErr.Err)
	}

	if isRetryableNetError(err) || isRetryableSyscallError(err) || errors.Is(err, io.EOF) {
		return true
	}

	return isRetryableErrorMessage(err)
}

func isRetryableNetError(err error) bool {
	// Check OpError first - dial errors (including dial timeouts) are retryable
	// since they indicate transient connection issues, not slow server responses
	if opErr, ok := errors.AsType[*net.OpError](err); ok {
		if opErr.Op == "dial" {
			return true
		}
		if opErr.Op == "read" {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return false // Read timeout = slow server, don't retry
			}
			return true
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}

	return false
}

func isRetryableSyscallError(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE)
}

func isRetryableErrorMessage(err error) bool {
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "network is unreachable") ||
		(strings.Contains(errStr, "eof") && !strings.Contains(errStr, "unexpected eof"))
}

func isIdempotentMethod(method string) bool {
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
