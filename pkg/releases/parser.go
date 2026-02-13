// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"strings"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	"github.com/moistari/rls"
)

const defaultParserTTL = 5 * time.Minute

// Parser caches rls parsing results so we do not repeatedly parse the same release names.
type Parser struct {
	cache *ttlcache.Cache[string, *rls.Release]
}

// NewParser returns a parser with the provided TTL for cached entries.
func NewParser(ttl time.Duration) *Parser {
	cache := ttlcache.New(ttlcache.Options[string, *rls.Release]{}.
		SetDefaultTTL(ttl))
	return &Parser{cache: cache}
}

// NewDefaultParser returns a parser using the default TTL.
func NewDefaultParser() *Parser {
	return NewParser(defaultParserTTL)
}

// Parse returns the parsed release metadata for name.
func (p *Parser) Parse(name string) *rls.Release {
	if p == nil {
		return &rls.Release{}
	}
	key := strings.TrimSpace(name)
	if key == "" {
		return &rls.Release{}
	}

	if cached, ok := p.cache.Get(key); ok {
		return cached
	}

	release := rls.ParseString(key)
	p.cache.Set(key, &release, ttlcache.DefaultTTL)
	return &release
}

// Clear removes a cached entry.
func (p *Parser) Clear(name string) {
	if p == nil {
		return
	}
	key := strings.TrimSpace(name)
	if key == "" {
		return
	}
	p.cache.Delete(key)
}
