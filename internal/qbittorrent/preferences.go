// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
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
	cached := cloneAppPreferences(c.preferencesCache)
	fresh := c.preferencesCache != nil && time.Since(c.preferencesFetchedAt) < appPreferencesCacheTTL
	c.preferencesMu.RUnlock()

	if fresh {
		return cached, nil
	}

	prefs, err := c.refreshAppPreferences(ctx)
	if err != nil {
		// Mirror GetAppInfo: a saturated qBittorrent WebUI times out even cheap
		// calls, so serve the last known preferences instead of failing the
		// torrent stream tick; only surface the error when nothing is cached.
		if cached != nil {
			log.Debug().
				Err(err).
				Int("instanceID", c.instanceID).
				Msg("Serving stale qBittorrent app preferences after refresh failure")
			return cached, nil
		}
		return nil, err
	}

	return prefs, nil
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

// GetCachedAppPreferencesSnapshot returns the last cached app preferences together
// with the time they were fetched, read under a single lock so the body and its
// timestamp are always a consistent pair.
func (c *Client) GetCachedAppPreferencesSnapshot() (*qbt.AppPreferences, time.Time) {
	c.preferencesMu.RLock()
	defer c.preferencesMu.RUnlock()

	return cloneAppPreferences(c.preferencesCache), c.preferencesFetchedAt
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

// GetCachedAlternativeSpeedLimitsModeSnapshot returns the last-known-good mode, the
// time it was fetched, and whether a value has ever been cached, all read under a
// single lock so they form a consistent snapshot.
func (c *Client) GetCachedAlternativeSpeedLimitsModeSnapshot() (mode bool, fetchedAt time.Time, ok bool) {
	c.altSpeedMu.RLock()
	defer c.altSpeedMu.RUnlock()

	return c.altSpeedMode, c.altSpeedFetchedAt, c.altSpeedFetched
}
