// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	"github.com/moistari/rls"
)

// ReleaseCache provides cached rls parsing to avoid expensive re-parsing
type ReleaseCache struct {
	cache *ttlcache.Cache[string, rls.Release]
}

// NewReleaseCache creates a new release cache with 5 minute expiration
func NewReleaseCache() *ReleaseCache {
	cache := ttlcache.New(ttlcache.Options[string, rls.Release]{}.
		SetDefaultTTL(5 * time.Minute))

	return &ReleaseCache{
		cache: cache,
	}
}

// Parse parses a release name using rls, with caching
func (rc *ReleaseCache) Parse(name string) rls.Release {
	// Check cache first
	if cached, found := rc.cache.Get(name); found {
		return cached
	}

	// Parse and cache
	release := rls.ParseString(name)
	rc.cache.Set(name, release, ttlcache.DefaultTTL)

	return release
}

// Clear removes a specific entry from cache
func (rc *ReleaseCache) Clear(name string) {
	rc.cache.Delete(name)
}
