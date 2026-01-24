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

// NormalizeUnicode removes diacritics and decomposes ligatures.
// Examples:
//   - "Shōgun" → "Shogun"
//   - "Amélie" → "Amelie"
//   - "naïve" → "naive"
//   - "Björk" → "Bjork"
//   - "æ" → "ae"
//   - "ﬁ" → "fi"
func NormalizeUnicode(s string) string {
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

	// Apply NFKD normalization to decompose diacritics and compatibility ligatures (ﬁ, ﬂ, etc.)
	// Create transformer per-call since transform.Chain is not safe for concurrent use
	t := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)))
	result, _, err := transform.String(t, s)
	if err != nil {
		// Errors are extremely rare (OOM or library bugs); return original for graceful degradation
		return s
	}
	return result
}

// NormalizeForMatching applies full normalization for cross-seed matching:
//   - Unicode normalization (removes diacritics, decomposes ligatures)
//   - Lowercase
//   - Strip apostrophes (including Unicode variants)
//   - Strip colons
//   - Convert ampersand to "and"
//   - Convert hyphens to spaces
//   - Collapse multiple spaces to single space
//
// Examples:
//   - "Shōgun S01" → "shogun s01"
//   - "Bob's Burgers" → "bobs burgers"
//   - "CSI: Miami" → "csi miami"
//   - "Spider-Man" → "spider man"
//   - "His & Hers" → "his and hers"
func NormalizeForMatching(s string) string {
	// First apply unicode normalization
	s = NormalizeUnicode(s)

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
	// Some trackers use & while others use "and" in release names
	s = strings.ReplaceAll(s, "&", " and ")

	// Normalize hyphens to spaces - "Spider-Man" → "spider man"
	s = strings.ReplaceAll(s, "-", " ")

	// Collapse multiple spaces to single space
	s = strings.Join(strings.Fields(s), " ")

	return s
}
