// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package stringutils

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var (
	// unicodeNormalizer caches expensive NormalizeUnicode results to avoid repeated NFKD transformations.
	// This cuts CPU usage significantly in hot paths like crossseed alignment.
	unicodeNormalizer = NewNormalizer(defaultNormalizerTTL, normalizeUnicodeInner)

	// matchingNormalizer caches expensive NormalizeForMatching results to avoid repeated unicode transformations.
	// This cuts CPU usage significantly in hot paths like crossseed matching.
	matchingNormalizer = NewNormalizer(defaultNormalizerTTL, normalized)
)

// normalizeUnicodeInner is the inner transformation function used by unicodeNormalizer.
func normalizeUnicodeInner(s string) string {
	// Handle special characters that NFKD doesn't decompose to ASCII equivalents
	// (these are distinct letters in Nordic/Germanic languages, not composed characters)
	s = strings.ReplaceAll(s, "æ", "ae")
	s = strings.ReplaceAll(s, "Æ", "AE")
	s = strings.ReplaceAll(s, "œ", "oe")
	s = strings.ReplaceAll(s, "Œ", "OE")
	s = strings.ReplaceAll(s, "ø", "o")
	s = strings.ReplaceAll(s, "Ø", "O")
	s = strings.ReplaceAll(s, "ß", "ss")
	s = strings.ReplaceAll(s, "ð", "d")
	s = strings.ReplaceAll(s, "Ð", "D")
	s = strings.ReplaceAll(s, "þ", "th")
	s = strings.ReplaceAll(s, "Þ", "TH")

	// Create transformer fresh per-call (transform.Chain is not thread-safe for concurrent use).
	// Caching via unicodeNormalizer prevents repeated transformations for identical inputs.
	t := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)))
	result, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return result
}

// normalized is the inner transformation function used by matchingNormalizer.
func normalized(s string) string {
	// Start with cached unicode normalization
	s = unicodeNormalizer.Normalize(s)

	// Lowercase and trim
	s = strings.ToLower(strings.TrimSpace(s))

	// Remove apostrophes - "Bob's" → "Bobs"
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, "'", "") // Unicode right single quote U+2019
	s = strings.ReplaceAll(s, "'", "") // Unicode left single quote U+2018
	s = strings.ReplaceAll(s, "`", "") // Backtick

	// Remove colons - "csi: miami" → "csi miami"
	s = strings.ReplaceAll(s, ":", "")

	// Normalize ampersand to "and" - "His & Hers" → "His and Hers"
	s = strings.ReplaceAll(s, "&", " and ")

	// Normalize hyphens to spaces - "Spider-Man" → "spider man"
	s = strings.ReplaceAll(s, "-", " ")

	// Collapse multiple spaces to single space
	s = strings.Join(strings.Fields(s), " ")

	return s
}

// NormalizeUnicode removes diacritics and decomposes ligatures with caching.
// Results are cached per input string (5 minute TTL) to avoid repeated expensive transformations.
// For the full normalization with additional punctuation handling, use NormalizeForMatching instead.
// Examples:
//   - "Shōgun" → "Shogun"
//   - "Amélie" → "Amelie"
//   - "naïve" → "naive"
//   - "Björk" → "Bjork"
//   - "æ" → "ae"
//   - "ﬁ" → "fi"
func NormalizeUnicode(s string) string {
	return unicodeNormalizer.Normalize(s)
}

// NormalizeForMatching applies cached full normalization for cross-seed matching:
//   - Unicode normalization (removes diacritics, decomposes ligatures)
//   - Lowercase
//   - Strip apostrophes (including Unicode variants)
//   - Strip colons
//   - Convert ampersand to "and"
//   - Convert hyphens to spaces
//   - Collapse multiple spaces to single space
//
// Results are cached per input string (5 minute TTL) to avoid repeated expensive transformations.
//
// Examples:
//   - "Shōgun S01" → "shogun s01"
//   - "Bob's Burgers" → "bobs burgers"
//   - "CSI: Miami" → "csi miami"
//   - "Spider-Man" → "spider man"
//   - "His & Hers" → "his and hers"
func NormalizeForMatching(s string) string {
	return matchingNormalizer.Normalize(s)
}
