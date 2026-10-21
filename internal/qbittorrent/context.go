// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import "context"

type contextKey string

const (
	skipTrackerHydrationKey    contextKey = "qui_skip_tracker_hydration"
	cachedCountsWithSkippedKey contextKey = "qui_cached_counts_with_skipped_tracker_hydration"
	skipFreshDataKey           contextKey = "qui_skip_fresh_data"
)

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

// SkipTrackerHydrationRequested reports whether ctx disables tracker enrichment.
func SkipTrackerHydrationRequested(ctx context.Context) bool {
	return shouldSkipTrackerHydration(ctx)
}

// WithCachedCountsWhenSkippingTrackerHydration marks the context so cached
// aggregate counts are still returned when tracker hydration is skipped.
func WithCachedCountsWhenSkippingTrackerHydration(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, cachedCountsWithSkippedKey, true)
}

// shouldIncludeCachedCountsWhenSkippingTrackerHydration returns true when callers
// want cached aggregate metadata without allowing inline tracker enrichment.
func shouldIncludeCachedCountsWhenSkippingTrackerHydration(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	val, ok := ctx.Value(cachedCountsWithSkippedKey).(bool)
	return ok && val
}

// CachedCountsWhenSkippingTrackerHydrationRequested reports whether ctx asks for
// cached counts even when tracker enrichment is disabled.
func CachedCountsWhenSkippingTrackerHydrationRequested(ctx context.Context) bool {
	return shouldIncludeCachedCountsWhenSkippingTrackerHydration(ctx)
}

// WithSkipFreshData marks the context so qBittorrent cache reads avoid triggering fresh syncs.
func WithSkipFreshData(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, skipFreshDataKey, true)
}

// shouldSkipFreshData returns true when the context prefers cached qBittorrent data.
func shouldSkipFreshData(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	val, ok := ctx.Value(skipFreshDataKey).(bool)
	return ok && val
}

// SkipFreshDataRequested reports whether ctx prefers cached qBittorrent data
// over request-triggered fresh sync work.
func SkipFreshDataRequested(ctx context.Context) bool {
	return shouldSkipFreshData(ctx)
}
