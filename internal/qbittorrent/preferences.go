// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
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

// InvalidateAppPreferencesCache clears the cached preferences to force a refresh on next access.
func (c *Client) InvalidateAppPreferencesCache() {
	c.preferencesMu.Lock()
	c.preferencesCache = nil
	c.preferencesFetchedAt = time.Time{}
	c.preferencesMu.Unlock()
}
