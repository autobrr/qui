// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"encoding/json"
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
	c.preferencesJSON = nil
	c.preferencesFetchedAt = time.Now()
	c.preferencesMu.Unlock()

	return cloneAppPreferences(cloned), nil
}

// cachedAppPreferencesJSON returns the cached preferences already rendered to
// JSON, or nil when nothing is cached. The ~150-field struct reflects to the
// same bytes on every list response between refreshes, so it is rendered once
// per refresh instead of once per response.
func (c *Client) cachedAppPreferencesJSON() (json.RawMessage, error) {
	c.preferencesMu.Lock()
	defer c.preferencesMu.Unlock()

	if c.preferencesCache == nil {
		return nil, nil
	}
	if c.preferencesJSON == nil {
		data, err := json.Marshal(c.preferencesCache)
		if err != nil {
			return nil, err
		}
		c.preferencesJSON = data
	}
	return c.preferencesJSON, nil
}

// GetCachedAppPreferences returns the last cached app preferences without triggering a refresh.
func (c *Client) GetCachedAppPreferences() *qbt.AppPreferences {
	c.preferencesMu.RLock()
	defer c.preferencesMu.RUnlock()

	return cloneAppPreferences(c.preferencesCache)
}

// InvalidateAppPreferencesCache clears the cached preferences to force a refresh on next access.
func (c *Client) InvalidateAppPreferencesCache() {
	c.preferencesMu.Lock()
	c.preferencesCache = nil
	c.preferencesJSON = nil
	c.preferencesFetchedAt = time.Time{}
	c.preferencesMu.Unlock()
}
