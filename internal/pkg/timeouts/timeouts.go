// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package timeouts

import (
	"context"
	"time"
)

const (
	DefaultSearchTimeout    = 9 * time.Second
	MaxSearchTimeout        = 45 * time.Second
	PerIndexerSearchTimeout = 1 * time.Second
)

var defaultSearchTimeout = DefaultSearchTimeout

func SetDefaultSearchTimeoutForTest(timeout time.Duration) (restore func()) {
	previous := defaultSearchTimeout
	defaultSearchTimeout = timeout
	return func() { defaultSearchTimeout = previous }
}

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
