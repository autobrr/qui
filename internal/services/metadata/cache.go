// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const cacheTTL = 1 * time.Hour

type cacheEntry struct {
	count   int
	expires time.Time
}

type resultCache struct {
	entries sync.Map
}

func newResultCache() *resultCache {
	return &resultCache{}
}

func cacheKey(title string, season int) string {
	return fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(title)), season)
}

// Get returns the cached episode count and true if a non-expired entry exists.
func (c *resultCache) Get(title string, season int) (int, bool) {
	key := cacheKey(title, season)

	val, ok := c.entries.Load(key)
	if !ok {
		return 0, false
	}

	entry := val.(cacheEntry)
	if time.Now().After(entry.expires) {
		c.entries.Delete(key)
		return 0, false
	}

	return entry.count, true
}

// Set stores the episode count with a TTL expiry.
func (c *resultCache) Set(title string, season int, count int) {
	key := cacheKey(title, season)
	c.entries.Store(key, cacheEntry{
		count:   count,
		expires: time.Now().Add(cacheTTL),
	})
}
