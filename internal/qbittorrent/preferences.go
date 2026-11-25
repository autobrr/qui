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

func (c *Client) GetCachedAppPreferences() *qbt.AppPreferences {
	c.preferencesMu.RLock()
	defer c.preferencesMu.RUnlock()

	return cloneAppPreferences(c.preferencesCache)
}

func (c *Client) GetLastAppPreferences(ctx context.Context) (*qbt.AppPreferences, error) {
	if cached := c.GetCachedAppPreferences(); cached != nil {
		return cached, nil
	}
	return c.GetAppPreferences(ctx)
}

func (c *Client) InvalidateAppPreferencesCache() {
	c.preferencesMu.Lock()
	c.preferencesCache = nil
	c.preferencesFetchedAt = time.Time{}
	c.preferencesMu.Unlock()
}
