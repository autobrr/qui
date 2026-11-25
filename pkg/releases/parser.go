// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/autobrr/go-cache/ttlcache"
	"github.com/autobrr/rls"
	"github.com/autobrr/rls/taginfo"

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

var releaseBuilder = rls.NewTagBuilder().Init(taginfo.All())

type Parser struct {
	cache         *ttlcache.Cache[string, *rls.Release]
	keyNormalizer *stringutils.Normalizer[string, string]
}

func NewParser(ttl time.Duration) *Parser {
	cache := ttlcache.New[string, *rls.Release](
		ttlcache.SetDefaultTTL(ttl))
	return &Parser{
		cache:         cache,
		keyNormalizer: stringutils.NewNormalizer(ttl, strings.TrimSpace),
	}
}

func NewDefaultParser() *Parser {
	return NewParser(defaultParserTTL)
}

var DefaultParser = NewDefaultParser()

func (p *Parser) Parse(name string) *rls.Release {
	if p == nil {
		return &rls.Release{}
	}
	key := strings.TrimSpace(name)
	if p.keyNormalizer != nil {
		key = p.keyNormalizer.Normalize(name)
	}
	key = stringutils.SanitizeUTF8(key)
	if key == "" {
		return &rls.Release{}
	}

	if cached, ok := p.cache.Get(key); ok {
		return cached
	}
	if release := p.listingRelease(key); release != nil {
		p.cache.Set(key, release, ttlcache.DefaultTTL)
		return release
	}

	tags, end := rls.ParseTagsString(key)
	release := releaseBuilder.Build(slices.Clone(tags), end)
	enrichReleaseHDR(key, &release)
	enrichUHDWebSource(&release)
	enrichEpisodeRange(&release)
	p.cache.Set(key, &release, ttlcache.DefaultTTL)
	return &release
}

func enrichUHDWebSource(release *rls.Release) {
	if release.Source != "UHD.BluRay" {
		return
	}
	bareUHD := false
	for _, tag := range release.Tags() {
		if !tag.Is(rls.TagTypeSource) {
			continue
		}
		if strings.EqualFold(fmt.Sprintf("%o", tag), "UHD") {
			bareUHD = true
			continue
		}
		if source := tag.Normalize(); bareUHD && strings.HasPrefix(strings.ToUpper(source), "WEB") {
			release.Source = "UHD." + source
		}
		return
	}
}

func enrichEpisodeRange(release *rls.Release) {
	if release == nil || release.Episode == 0 {
		return
	}
	if !IsEpisodeRange(release) {
		return
	}
	release.Episode = 0
	release.Type = rls.Series
}

func IsEpisodeRange(release *rls.Release) bool {
	if release == nil {
		return false
	}
	var tags, scheme []rls.Tag
	for _, tag := range release.Tags() {
		if !tag.Is(rls.TagTypeSeries) {
			continue
		}
		tags = append(tags, tag)
		if seasonEpisodeScheme.MatchString(fmt.Sprintf("%o", tag)) {
			scheme = append(scheme, tag)
		}
	}
	if len(scheme) > 0 {
		tags = scheme
	}
	first := [2]int{-1, -1}
	for _, tag := range tags {
		series, _ := tag.Series()
		for _, episode := range tag.Episodes() {
			switch cur := [2]int{series, episode}; {
			case first[0] < 0:
				first = cur
			case cur != first:
				return true
			}
		}
	}
	return false
}

var seasonEpisodeScheme = regexp.MustCompile(`(?i)^(?:[se]|\d+x)\d`)

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

var hdrTagSeparators = strings.NewReplacer(" ", "", ".", "", "_", "", "-", "")

func CanonicalHDRTag(tag string) string {
	upper := strings.ToUpper(strings.TrimSpace(tag))
	if upper == "" {
		return ""
	}

	key := hdrTagSeparators.Replace(upper)

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

var listingYear = regexp.MustCompile(`\S (\d{4}) [^\d\[\]]*$`)

func (p *Parser) listingRelease(name string) *rls.Release {
	rest, ok := strings.CutSuffix(name, "]")
	if !ok {
		return nil
	}
	title, inner, ok := strings.CutLast(rest, "[")
	if !ok || !strings.Contains(inner, ".") || strings.ContainsFunc(inner, unicode.IsSpace) {
		return nil
	}
	match := listingYear.FindStringSubmatchIndex(title)
	if match == nil {
		return nil
	}
	release := p.Parse(inner)
	listing := titleWords(title[:match[2]])
	words := titleWords(release.Title)
	if len(words) == 0 || slices.ContainsFunc(words, func(word string) bool { return !slices.Contains(listing, word) }) {
		return nil
	}
	if release.Year == 0 && release.Series == 0 {
		withYear := *release
		withYear.Year, _ = strconv.Atoi(title[match[2]:match[3]])
		return &withYear
	}
	return release
}

func titleWords(name string) []string {
	return strings.FieldsFunc(stringutils.NormalizeForMatching(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
