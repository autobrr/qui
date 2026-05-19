// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import "context"

type contextKey string

const skipTrackerHydrationKey contextKey = "qui_skip_tracker_hydration"

// WithSkipTrackerHydration marks the context so tracker enrichment/hydration is skipped.
func WithSkipTrackerHydration(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, skipTrackerHydrationKey, true)
}

// shouldSkipTrackerHydration returns true when the context requests tracker enrichment to be skipped.
func shouldSkipTrackerHydration(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	val, ok := ctx.Value(skipTrackerHydrationKey).(bool)
	return ok && val
}
