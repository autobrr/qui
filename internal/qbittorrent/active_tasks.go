// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

// activeTaskCountTTL bounds how often the running/queued torrent-creation task
// count is refreshed from qBittorrent. The SSE sync loop rebuilds every group's
// payload on each tick (default 2s); without this cache each group would issue an
// uncached HTTP request for the task count, scaling with the number of open views.
const activeTaskCountTTL = 10 * time.Second

// GetActiveTaskCount returns the number of running or queued torrent-creation
// tasks, served from a short-TTL per-instance cache with single-flight refresh.
//
// It returns 0 without any network call when the client is unavailable or the
// qBittorrent version does not support torrent creation. On a refresh error the
// last known value is returned and the cache timestamp is left stale so the next
// call retries.
func (c *Client) GetActiveTaskCount(ctx context.Context) int {
	if c == nil || c.Client == nil || !c.SupportsTorrentCreation() {
		return 0
	}

	c.activeTaskMu.Lock()
	fresh := !c.activeTaskCountAt.IsZero() && time.Since(c.activeTaskCountAt) < activeTaskCountTTL
	if fresh || c.activeTaskRefreshing {
		// Serve the cached value when it is fresh, or while another goroutine is
		// already refreshing it, so concurrent group builds collapse to one fetch.
		count := c.activeTaskCount
		c.activeTaskMu.Unlock()
		return count
	}
	c.activeTaskRefreshing = true
	c.activeTaskMu.Unlock()

	count, err := c.fetchActiveTaskCount(ctx)

	c.activeTaskMu.Lock()
	c.activeTaskRefreshing = false
	if err == nil {
		c.activeTaskCount = count
		c.activeTaskCountAt = time.Now()
	}
	result := c.activeTaskCount
	c.activeTaskMu.Unlock()
	return result
}

func (c *Client) fetchActiveTaskCount(ctx context.Context) (int, error) {
	tasks, err := c.GetTorrentCreationStatusCtx(ctx, "")
	if err != nil {
		return 0, err
	}

	count := 0
	for _, task := range tasks {
		if task.Status == qbt.TorrentCreationStatusRunning || task.Status == qbt.TorrentCreationStatusQueued {
			count++
		}
	}
	return count, nil
}
