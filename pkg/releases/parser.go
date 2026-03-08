// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	"github.com/moistari/rls"

	"github.com/autobrr/qui/pkg/stringutils"
)

const defaultParserTTL = 5 * time.Minute

var hdrTagMatchers = []struct {
	tag string
	re  *regexp.Regexp
}{
	{tag: "DV", re: regexp.MustCompile(`(?i)(?:^|[^A-Z0-9])(?:DV|DOVI|DOLBY[ ._-]?VISION)(?:$|[^A-Z0-9])`)},
	{tag: "HDR10+", re: regexp.MustCompile(`(?i)(?:^|[^A-Z0-9])HDR(?:[ ._-]?10(?:\+|P|PLUS))(?:$|[^A-Z0-9])`)},
	{tag: "HDR10", re: regexp.MustCompile(`(?i)(?:^|[^A-Z0-9])HDR(?:[ ._-]?10)(?:$|[^A-Z0-9+P])`)},
	{tag: "HDR", re: regexp.MustCompile(`(?i)(?:^|[^A-Z0-9])HDR(?:$|[^A-Z0-9+])`)},
	{tag: "HLG", re: regexp.MustCompile(`(?i)(?:^|[^A-Z0-9])HLG(?:$|[^A-Z0-9])`)},
}

// Parser caches rls parsing results so we do not repeatedly parse the same release names.
type Parser struct {
	cache         *ttlcache.Cache[string, *rls.Release]
	keyNormalizer *stringutils.Normalizer[string, string]
}

// NewParser returns a parser with the provided TTL for cached entries.
func NewParser(ttl time.Duration) *Parser {
	cache := ttlcache.New(ttlcache.Options[string, *rls.Release]{}.
		SetDefaultTTL(ttl))
	return &Parser{
		cache:         cache,
		keyNormalizer: stringutils.NewNormalizer(ttl, strings.TrimSpace),
	}
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
	if p.keyNormalizer != nil {
		key = p.keyNormalizer.Normalize(name)
	}
	if key == "" {
		return &rls.Release{}
	}

	if cached, ok := p.cache.Get(key); ok {
		return cached
	}

	release := rls.ParseString(key)
	enrichReleaseHDR(key, &release)
	p.cache.Set(key, &release, ttlcache.DefaultTTL)
	return &release
}

func enrichReleaseHDR(rawName string, release *rls.Release) {
	if release == nil {
		return
	}

	normalized := make([]string, 0, len(release.HDR)+2)
	seen := make(map[string]struct{}, len(release.HDR)+2)

	add := func(tag string) {
		canonical := canonicalHDRTag(tag)
		if canonical == "" {
			return
		}
		if _, ok := seen[canonical]; ok {
			return
		}
		seen[canonical] = struct{}{}
		normalized = append(normalized, canonical)
	}

	for _, tag := range release.HDR {
		add(tag)
	}

	for _, matcher := range hdrTagMatchers {
		if matcher.re.MatchString(rawName) {
			add(matcher.tag)
		}
	}

	sort.Strings(normalized)
	release.HDR = normalized
}

func canonicalHDRTag(tag string) string {
	upper := strings.ToUpper(strings.TrimSpace(tag))
	if upper == "" {
		return ""
	}

	key := strings.NewReplacer(" ", "", ".", "", "_", "", "-", "").Replace(upper)

	switch key {
	case "DOVI", "DOLBYVISION", "DV":
		return "DV"
	case "HDR10PLUS", "HDR10P", "HDR10+":
		return "HDR10+"
	case "HDR10":
		return "HDR10"
	case "HDR":
		return "HDR"
	case "HLG":
		return "HLG"
	default:
		return upper
	}
}

// Clear removes a cached entry.
func (p *Parser) Clear(name string) {
	if p == nil {
		return
	}
	key := strings.TrimSpace(name)
	if p.keyNormalizer != nil {
		key = p.keyNormalizer.Normalize(name)
	}
	if key == "" {
		return
	}
	p.cache.Delete(key)
}
