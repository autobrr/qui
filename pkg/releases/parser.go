// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
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

var trailingTokenRegexCache sync.Map

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

// DefaultParser is a statically allocated default parser; constructing one per call
// leaks a ttlcache goroutine.
var DefaultParser = NewDefaultParser()

// Parse returns the parsed release metadata for name.
func (p *Parser) Parse(name string) *rls.Release {
	if p == nil {
		return &rls.Release{}
	}
	key := strings.TrimSpace(name)
	if p.keyNormalizer != nil {
		key = p.keyNormalizer.Normalize(name)
	}
	// Sanitize here: raw non-UTF-8 bytes (e.g. "á" as Latin-1 0xe1) otherwise reach
	// regexp.Compile in enrichReleaseHDR and panic via MustCompile. Names read from
	// torrent files and qBt file lists are already sanitized at their ingestion
	// boundaries.
	key = stringutils.SanitizeUTF8(key)
	if key == "" {
		return &rls.Release{}
	}

	if cached, ok := p.cache.Get(key); ok {
		return cached
	}

	release := rls.ParseString(key)
	enrichReleaseHDR(key, &release)
	enrichEpisodeRange(&release)
	p.cache.Set(key, &release, ttlcache.DefaultTTL)
	return &release
}

// enrichEpisodeRange turns a multi-episode release such as "S00E02-E05" into a
// pack. rls reports it as its first episode, so the pack looks like a single
// episode, but it keeps the whole list in SeriesEpisodes. Done at parse time so
// the result lands in the cache.
func enrichEpisodeRange(release *rls.Release) {
	if release == nil || release.Episode == 0 {
		return
	}
	if !IsEpisodeRange(release) {
		return
	}
	// A season 0 pack carries no season number, so Series alone cannot mark it as
	// a pack. Set the type as well and let the episode number go.
	release.Episode = 0
	release.Type = rls.Series
}

// IsEpisodeRange reports whether the release names two or more distinct
// episodes, such as "S01E05E06". Parse turns such a release into a pack
// (Episode 0), and this predicate is what still tells it apart from a full
// season pack: a range keeps its episode list in SeriesEpisodes, a full pack
// has none.
func IsEpisodeRange(release *rls.Release) bool {
	if release == nil {
		return false
	}
	// Count distinct episodes, not entries: a file path names the same episode
	// twice ("Show.S11E11-GRP/Show.S11E11-GRP.mkv") and that is still one episode.
	episodes := release.SeriesEpisodes()
	if len(episodes) < 2 {
		return false
	}
	return slices.ContainsFunc(episodes, func(e []int) bool {
		return !slices.Equal(e, episodes[0])
	})
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

	release.HDR = NormalizeHDRTags(tags)
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
		switch CanonicalHDRTag(codec) {
		case "DV", "HDR", "HDR10", "HDR10+", "HLG":
			return true
		}
		upper := strings.ToUpper(strings.TrimSpace(codec))
		switch upper {
		case "X264", "H264", "H.264", "AVC", "X265", "H265", "H.265", "HEVC", "AV1", "XVID", "DIVX":
			return true
		}
	}

	return false
}

// NormalizeHDRTags deduplicates and canonicalizes a slice of HDR tag strings.
// HDR10 is subsumed by HDR10+ when both are present. Returns nil for empty input.
func NormalizeHDRTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	hasHDR10Plus := false

	for _, tag := range tags {
		canonical := CanonicalHDRTag(tag)
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

	if len(seen) == 0 {
		return nil
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

	if trimmed, ok := trimTrailingDelimitedToken(rawName, token); ok {
		return trimmed
	}
	if trimmed, ok := trimTrailingWrappedToken(rawName, "["+token+"]"); ok {
		return trimmed
	}
	if trimmed, ok := trimTrailingWrappedToken(rawName, "("+token+")"); ok {
		return trimmed
	}

	trimmed := rawName
	for _, re := range trailingTokenRegexes(token) {
		if idx := re.FindStringIndex(trimmed); idx != nil {
			trimmed = strings.TrimRight(trimmed[:idx[0]], " ._-")
		}
	}

	return strings.TrimSpace(trimmed)
}

func trimTrailingDelimitedToken(rawName, token string) (string, bool) {
	if len(rawName) <= len(token) {
		return "", false
	}

	start := len(rawName) - len(token)
	if !strings.EqualFold(rawName[start:], token) {
		return "", false
	}

	prefix := rawName[:start]
	trimmed := strings.TrimRight(prefix, " ._-")
	if len(trimmed) == len(prefix) {
		return "", false
	}

	return strings.TrimSpace(trimmed), true
}

func trimTrailingWrappedToken(rawName, wrapped string) (string, bool) {
	if len(rawName) < len(wrapped) {
		return "", false
	}
	if !strings.EqualFold(rawName[len(rawName)-len(wrapped):], wrapped) {
		return "", false
	}
	return strings.TrimSpace(rawName[:len(rawName)-len(wrapped)]), true
}

func trailingTokenRegexes(token string) []*regexp.Regexp {
	if cached, ok := trailingTokenRegexCache.Load(token); ok {
		return cached.([]*regexp.Regexp)
	}

	quoted := regexp.QuoteMeta(token)
	ext := `(?:\.[^./\\]+)?$`
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)[\s._-]+` + quoted + ext),
		regexp.MustCompile(`(?i)\[` + quoted + `\]` + ext),
		regexp.MustCompile(`(?i)\(` + quoted + `\)` + ext),
	}

	actual, _ := trailingTokenRegexCache.LoadOrStore(token, patterns)
	return actual.([]*regexp.Regexp)
}

// CanonicalHDRTag maps an HDR tag string to its canonical form.
// For example, "HDR10P", "HDR10PLUS", and "HDR10+" all map to "HDR10+".
func CanonicalHDRTag(tag string) string {
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
