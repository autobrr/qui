// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"path"
	"strings"
	"unicode/utf8"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/pkg/stringutils"
)

func searchTorrents(torrents []*qbt.Torrent, query string) []*qbt.Torrent {
	search := parseTorrentSearch(query)
	if search.include == nil && len(search.excluded) == 0 {
		return torrents
	}
	normalizedCache := make(map[string]string)

	var matched []int
	for i, torrent := range torrents {
		row := searchRow{torrent: torrent, normalizedCache: normalizedCache}
		if search.matches(&row) {
			matched = append(matched, i)
		}
	}

	if len(matched) == len(torrents) {
		return torrents
	}

	filtered := make([]*qbt.Torrent, len(matched))
	for i, idx := range matched {
		filtered[i] = torrents[idx]
	}

	log.Trace().
		Str("search", query).
		Int("totalTorrents", len(torrents)).
		Int("matchedTorrents", len(filtered)).
		Msg("Search completed")

	return filtered
}

type torrentSearch struct {
	include  *includedSearch
	excluded []excludedTerm
}

func parseTorrentSearch(query string) torrentSearch {
	var search torrentSearch
	var included []string
	plain := true
	for word := range strings.FieldsSeq(query) {
		prefix, rest := word[0], word[1:]
		switch {
		case prefix == '-' || prefix == '!':
			plain = false
			if rest != "" {
				search.excluded = append(search.excluded, newExcludedTerm(rest))
			}
		case prefix == '+' && rest != "":
			plain = false
			included = append(included, rest)
		default:
			included = append(included, word)
		}
	}
	switch {
	case plain && len(included) > 0:
		search.include = new(newIncludedSearch(query))
	case len(included) > 0:
		search.include = new(newIncludedSearch(strings.Join(included, " ")))
	}
	return search
}

func (s *torrentSearch) matches(row *searchRow) bool {
	if s.include != nil && !s.include.matches(row) {
		return false
	}
	for i := range s.excluded {
		if s.excluded[i].hides(row) {
			return false
		}
	}
	return true
}

type excludedTerm struct {
	lower      string
	normalized string
	glob       bool
}

func newExcludedTerm(text string) excludedTerm {
	return excludedTerm{
		lower:      strings.ToLower(text),
		normalized: normalizeForSearch(text),
		glob:       strings.ContainsAny(text, "*?["),
	}
}

func (t *excludedTerm) hides(row *searchRow) bool {
	if t.glob && torrentMatchesGlob(row.torrent, t.lower) {
		return true
	}
	if row.containsFold(t.lower) {
		return true
	}
	return t.normalized != "" && t.normalized != t.lower && row.containsNormalized(t.normalized)
}

type includedSearch struct {
	lower       string
	normalized  string
	words       []string
	glob        bool
	folded      bool
	couldBeHash bool
}

func newIncludedSearch(text string) includedSearch {
	lower := strings.ToLower(text)
	normalized := normalizeForSearch(text)
	return includedSearch{
		lower:      lower,
		normalized: normalized,
		words:      strings.Fields(normalized),
		glob:       strings.ContainsAny(text, "*?"),
		folded:     isASCIIFolded(normalized),
		couldBeHash: !strings.ContainsFunc(lower, func(r rune) bool {
			return (r < '0' || r > '9') && (r < 'a' || r > 'f')
		}),
	}
}

func (s *includedSearch) matches(row *searchRow) bool {
	torrent := row.torrent
	if s.glob && torrentMatchesGlob(torrent, s.lower) {
		return true
	}
	if row.containsFold(s.lower) {
		return true
	}
	if s.couldBeHash &&
		(stringutils.ContainsFold(torrent.Hash, s.lower) ||
			stringutils.ContainsFold(torrent.InfohashV1, s.lower) ||
			stringutils.ContainsFold(torrent.InfohashV2, s.lower)) {
		return true
	}
	if row.containsNormalized(s.normalized) {
		return true
	}
	if len(s.words) > 1 && row.containsEachWord(s.words) {
		return true
	}
	name, _, _ := row.normalizedFields()
	score := rankFuzzy(s.normalized, name, s.folded)
	return score >= 0 && score < 10
}

type searchRow struct {
	torrent            *qbt.Torrent
	normalizedCache    map[string]string
	normalizedName     string
	normalizedCategory string
	normalizedTags     string
	normalized         bool
}

func (r *searchRow) containsFold(lower string) bool {
	torrent := r.torrent
	return stringutils.ContainsFold(torrent.Name, lower) ||
		stringutils.ContainsFold(torrent.Category, lower) ||
		stringutils.ContainsFold(torrent.Tags, lower)
}

func (r *searchRow) containsNormalized(normalized string) bool {
	name, category, tags := r.normalizedFields()
	return strings.Contains(name, normalized) ||
		strings.Contains(category, normalized) ||
		strings.Contains(tags, normalized)
}

func (r *searchRow) containsEachWord(words []string) bool {
	name, category, tags := r.normalizedFields()
	for _, word := range words {
		if !strings.Contains(name, word) &&
			!strings.Contains(category, word) &&
			!strings.Contains(tags, word) {
			return false
		}
	}
	return true
}

func (r *searchRow) normalizedFields() (name, category, tags string) {
	if !r.normalized {
		torrent := r.torrent
		r.normalizedName = normalizeForSearch(torrent.Name)
		r.normalizedCategory = r.normalizeCached(torrent.Category)
		r.normalizedTags = r.normalizeCached(torrent.Tags)
		r.normalized = true
	}
	return r.normalizedName, r.normalizedCategory, r.normalizedTags
}

func (r *searchRow) normalizeCached(value string) string {
	if value == "" {
		return ""
	}
	if cached, ok := r.normalizedCache[value]; ok {
		return cached
	}
	normalized := normalizeForSearch(value)
	r.normalizedCache[value] = normalized
	return normalized
}

func torrentMatchesGlob(torrent *qbt.Torrent, patternLower string) bool {
	if matched, _ := path.Match(patternLower, strings.ToLower(torrent.Name)); matched {
		return true
	}
	if torrent.Category != "" {
		if matched, _ := path.Match(patternLower, strings.ToLower(torrent.Category)); matched {
			return true
		}
	}
	for tag := range strings.SplitSeq(strings.ToLower(torrent.Tags), ", ") {
		if matched, _ := path.Match(patternLower, strings.TrimSpace(tag)); matched {
			return true
		}
	}
	return false
}

func isSearchSeparator(c byte) bool {
	switch c {
	case '.', '_', '-', '[', ']', '(', ')', '{', '}', ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

func normalizeForSearch(text string) string {
	for i := range len(text) {
		if text[i] >= utf8.RuneSelf {
			return normalizeForSearchUnicode(text)
		}
	}

	var out strings.Builder
	out.Grow(len(text))
	pendingSpace := false
	for i := range len(text) {
		c := text[i]
		if isSearchSeparator(c) {
			pendingSpace = out.Len() > 0
			continue
		}
		if pendingSpace {
			out.WriteByte(' ')
			pendingSpace = false
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out.WriteByte(c)
	}
	return out.String()
}

func normalizeForSearchUnicode(text string) string {
	replacers := []string{".", "_", "-", "[", "]", "(", ")", "{", "}"}
	normalized := strings.ToLower(text)
	for _, r := range replacers {
		normalized = strings.ReplaceAll(normalized, r, " ")
	}
	return strings.Join(strings.Fields(normalized), " ")
}

func isASCIIFolded(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf || (s[i] >= 'A' && s[i] <= 'Z') {
			return false
		}
	}
	return true
}

func rankFuzzy(source, target string, sourceIsFolded bool) int {
	if sourceIsFolded && isASCIIFolded(target) {
		return fuzzy.RankMatch(source, target)
	}
	if rank := fuzzy.RankMatchNormalizedFold(source, target); rank >= 0 {
		return rank
	}
	if fuzzy.MatchNormalizedFold(source, target) {
		return 0
	}
	return -1
}
