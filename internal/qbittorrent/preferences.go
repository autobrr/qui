// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

const appPreferencesCacheTTL = 30 * time.Second
const appPreferencesRequestTimeout = 10 * time.Second

func cloneAppPreferences(prefs *qbt.AppPreferences) *qbt.AppPreferences {
	if prefs == nil {
		return nil
	}

	clone := *prefs
	return &clone
}

// GetAppPreferences returns cached qBittorrent app preferences, refreshing them when stale.
func (c *Client) GetAppPreferences(ctx context.Context) (*qbt.AppPreferences, error) {
	if c == nil || c.Client == nil {
		return nil, errors.New("qbittorrent client unavailable")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	c.preferencesMu.RLock()
	if c.preferencesCache != nil && time.Since(c.preferencesFetchedAt) < appPreferencesCacheTTL {
		cached := cloneAppPreferences(c.preferencesCache)
		c.preferencesMu.RUnlock()
		return cached, nil
	}
	c.preferencesMu.RUnlock()

	return c.refreshAppPreferences(ctx)
}

func (c *Client) refreshAppPreferences(ctx context.Context) (*qbt.AppPreferences, error) {
	requestCtx, cancel := context.WithTimeout(ctx, appPreferencesRequestTimeout)
	defer cancel()

	prefs, err := c.GetAppPreferencesCtx(requestCtx)
	if err != nil {
		return nil, fmt.Errorf("get app preferences: %w", err)
	}

	cloned := cloneAppPreferences(&prefs)

	c.preferencesMu.Lock()
	c.preferencesCache = cloned
	c.preferencesFetchedAt = time.Now()
	c.preferencesMu.Unlock()

	return cloneAppPreferences(cloned), nil
}

// GetCachedAppPreferences returns the last cached app preferences without triggering a refresh.
func (c *Client) GetCachedAppPreferences() *qbt.AppPreferences {
	c.preferencesMu.RLock()
	defer c.preferencesMu.RUnlock()

	return cloneAppPreferences(c.preferencesCache)
}

// GetCachedAppPreferencesAt returns the time the cached app preferences were last
// fetched, or the zero time if they have never been fetched.
func (c *Client) GetCachedAppPreferencesAt() time.Time {
	c.preferencesMu.RLock()
	defer c.preferencesMu.RUnlock()

	return c.preferencesFetchedAt
}

// InvalidateAppPreferencesCache clears the cached preferences to force a refresh on next access.
func (c *Client) InvalidateAppPreferencesCache() {
	c.preferencesMu.Lock()
	c.preferencesCache = nil
	c.preferencesFetchedAt = time.Time{}
	c.preferencesMu.Unlock()
}

// GetAlternativeSpeedLimitsMode fetches the live alternative-speed-limits mode and,
// on success, records it as the last-known-good value so the API can fall back to it
// when a later live call times out or errors.
func (c *Client) GetAlternativeSpeedLimitsMode(ctx context.Context) (bool, error) {
	if c == nil || c.Client == nil {
		return false, errors.New("qbittorrent client unavailable")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	enabled, err := c.GetAlternativeSpeedLimitsModeCtx(ctx)
	if err != nil {
		return false, fmt.Errorf("get alternative speed limits mode: %w", err)
	}

	c.storeAlternativeSpeedLimitsMode(enabled)
	return enabled, nil
}

// storeAlternativeSpeedLimitsMode records the latest observed mode and fetch time.
func (c *Client) storeAlternativeSpeedLimitsMode(enabled bool) {
	c.altSpeedMu.Lock()
	c.altSpeedMode = enabled
	c.altSpeedFetched = true
	c.altSpeedFetchedAt = time.Now()
	c.altSpeedMu.Unlock()
}

// GetCachedAlternativeSpeedLimitsMode returns the last-known-good alternative-speed-limits
// mode. The second return value reports whether a value has ever been cached.
func (c *Client) GetCachedAlternativeSpeedLimitsMode() (bool, bool) {
	c.altSpeedMu.RLock()
	defer c.altSpeedMu.RUnlock()

	return c.altSpeedMode, c.altSpeedFetched
}

// GetCachedAlternativeSpeedLimitsModeAt returns the time the alternative-speed-limits
// mode was last successfully fetched, or the zero time if it has never been fetched.
func (c *Client) GetCachedAlternativeSpeedLimitsModeAt() time.Time {
	c.altSpeedMu.RLock()
	defer c.altSpeedMu.RUnlock()

	return c.altSpeedFetchedAt
}
