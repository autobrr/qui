// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

const activeTaskCountTTL = 10 * time.Second

func (c *Client) GetActiveTaskCount(ctx context.Context) int {
	if c == nil || c.Client == nil || !c.Supports(CapTorrentCreation) {
		return 0
	}

	c.activeTaskMu.Lock()
	fresh := !c.activeTaskCountAt.IsZero() && time.Since(c.activeTaskCountAt) < activeTaskCountTTL
	if fresh || c.activeTaskRefreshing {
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
