// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestGetAppPreferencesServesStaleCacheWhenRefreshFails(t *testing.T) {
	c := &Client{
		Client:     qbt.NewClient(qbt.Config{Host: "http://127.0.0.1:0"}),
		instanceID: 1,
	}
	c.preferencesCache = &qbt.AppPreferences{AnnounceIP: "203.0.113.7"}
	c.preferencesFetchedAt = time.Now().Add(-2 * appPreferencesCacheTTL) // force the cache stale

	// A canceled context makes the refresh fail immediately, standing in for a
	// saturated qBittorrent WebUI that times out every request.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	prefs, err := c.GetAppPreferences(ctx)
	require.NoError(t, err)
	require.NotNil(t, prefs)
	require.Equal(t, "203.0.113.7", prefs.AnnounceIP)
}

func TestGetAppPreferencesReturnsErrorWhenNoCacheAndRefreshFails(t *testing.T) {
	c := &Client{
		Client:     qbt.NewClient(qbt.Config{Host: "http://127.0.0.1:0"}),
		instanceID: 1,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	prefs, err := c.GetAppPreferences(ctx)
	require.Error(t, err)
	require.Nil(t, prefs)
}
