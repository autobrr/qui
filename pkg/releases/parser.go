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
	{tag: "HDR10+", re: regexp.MustCompile(`(?i)(?:^|[^A-Z0-9])HDR(?:[ ._-]?10(?:[ ._-]?(?:\+|P(?:LUS)?)))(?:$|[^A-Z0-9])`)},
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

	tags := make([]string, 0, len(release.HDR)+2)
	tags = append(tags, release.HDR...)

	if shouldScanRawHDR(release) {
		scanName := trimTrailingGroupOrSite(rawName, release)
		for _, matcher := range hdrTagMatchers {
			if matcher.re.MatchString(scanName) {
				tags = append(tags, matcher.tag)
			}
		}
	}

	release.HDR = normalizeHDRTags(tags)
}

func shouldScanRawHDR(release *rls.Release) bool {
	if release == nil {
		return false
	}

	if release.Type.Is(rls.Movie, rls.Series, rls.Episode) {
		return true
	}

	if release.Resolution != "" || release.Source != "" {
		return true
	}

	for _, codec := range release.Codec {
		switch canonicalHDRTag(codec) {
		case "DV", "HDR", "HDR10", "HDR10+", "HLG":
			return true
		}
	}

	for _, codec := range release.Codec {
		upper := strings.ToUpper(strings.TrimSpace(codec))
		switch upper {
		case "X264", "H264", "H.264", "AVC", "X265", "H265", "H.265", "HEVC", "AV1", "XVID", "DIVX":
			return true
		}
	}

	return false
}

func normalizeHDRTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	hasHDR10Plus := false

	for _, tag := range tags {
		canonical := canonicalHDRTag(tag)
		if canonical == "" {
			continue
		}
		if canonical == "HDR10+" {
			hasHDR10Plus = true
		}
		seen[canonical] = struct{}{}
	}

	if hasHDR10Plus {
		delete(seen, "HDR10")
	}

	normalized := make([]string, 0, len(seen))
	for tag := range seen {
		normalized = append(normalized, tag)
	}

	sort.Strings(normalized)
	return normalized
}

func trimTrailingGroupOrSite(rawName string, release *rls.Release) string {
	if release == nil {
		return rawName
	}

	trimmed := strings.TrimSpace(rawName)
	if trimmed == "" {
		return trimmed
	}

	for {
		prev := trimmed
		for _, token := range []string{release.Group, release.Site} {
			trimmed = trimTrailingParsedToken(trimmed, token)
		}
		if trimmed == prev {
			break
		}
	}

	return trimmed
}

func trimTrailingParsedToken(rawName, token string) string {
	token = strings.TrimSpace(token)
	if rawName == "" || token == "" {
		return rawName
	}

	quoted := regexp.QuoteMeta(token)
	ext := `(?:\.[^./\\]+)?$`
	patterns := []string{
		`(?i)[\s._-]+` + quoted + ext,
		`(?i)\[` + quoted + `\]` + ext,
		`(?i)\(` + quoted + `\)` + ext,
	}

	trimmed := rawName
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if idx := re.FindStringIndex(trimmed); idx != nil {
			trimmed = strings.TrimRight(trimmed[:idx[0]], " ._-")
		}
	}

	return strings.TrimSpace(trimmed)
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
