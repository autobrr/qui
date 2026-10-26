// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package timeouts

import (
	"context"
	"time"
)

const (
	// DefaultSearchTimeout matches both Jackett and cross-seed automation base timeout.
	DefaultSearchTimeout = 9 * time.Second
	// MaxSearchTimeout caps how far we extend the adaptive timeout budget.
	MaxSearchTimeout = 45 * time.Second
	// PerIndexerSearchTimeout is the additional budget granted per indexer; keep in sync with automation scheduler assumptions.
	PerIndexerSearchTimeout = 1 * time.Second
)

var defaultSearchTimeout = DefaultSearchTimeout

// SetDefaultSearchTimeoutForTest sets the default search timeout and returns
// a func that restores it. Product code does not call it.
func SetDefaultSearchTimeoutForTest(timeout time.Duration) (restore func()) {
	previous := defaultSearchTimeout
	defaultSearchTimeout = timeout
	return func() { defaultSearchTimeout = previous }
}

// AdaptiveSearchTimeout scales the timeout linearly per indexer (starting from defaultSearchTimeout)
// and caps the total budget at MaxSearchTimeout.
func AdaptiveSearchTimeout(indexerCount int) time.Duration {
	if indexerCount <= 1 {
		return defaultSearchTimeout
	}
	extra := max(time.Duration(indexerCount-1)*PerIndexerSearchTimeout, 0)
	timeout := defaultSearchTimeout + extra
	if timeout > MaxSearchTimeout {
		return MaxSearchTimeout
	}
	return timeout
}

// WithSearchTimeout enforces a timeout only when the parent context lacks a deadline.
func WithSearchTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = defaultSearchTimeout
	}
	if ctx == nil {
		return context.WithTimeout(context.Background(), timeout)
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}
