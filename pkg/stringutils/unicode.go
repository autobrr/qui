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
	unicodeNormalizer = NewNormalizer(defaultNormalizerTTL, normalizeUnicodeInner)

	matchingNormalizer = NewNormalizer(defaultNormalizerTTL, normalized)

	animeTitleSymbolReplacer = strings.NewReplacer(
		"★", " ",
		"☆", " ",
		"✩", " ",
		"✭", " ",
		"✯", " ",
		"✰", " ",
		"✦", " ",
		"✧", " ",
		"◆", " ",
		"◇", " ",
		"■", " ",
		"□", " ",
		"●", " ",
		"○", " ",
		"◎", " ",
		"♪", " ",
		"♫", " ",
		"♬", " ",
		"♩", " ",
		"♡", " ",
		"♥", " ",
		"・", " ",
		"･", " ",
		"·", " ",
		"•", " ",
	)
)

// SanitizeUTF8 replaces invalid UTF-8 byte sequences in s with U+FFFD, returning valid UTF-8.
//
// The BitTorrent spec requires all string fields in a torrent file (torrent name,
// file paths, comment, etc.) to be UTF-8, but torrents with raw legacy-encoded bytes
// (e.g. "á" as Latin-1 0xe1) exist in the wild and break code that assumes valid UTF-8,
// for example regexp.Compile rejects invalid-UTF-8 patterns and panics via MustCompile.
// Invalid bytes are replaced with U+FFFD rather than dropped because the other lossy
// decoders in the pipeline (encoding/json, which delivers all qBittorrent API strings,
// and the NFKD transform in NormalizeForMatching) coerce invalid bytes to U+FFFD too,
// so sanitized strings still compare equal to those forms. This is a no-op on
// well-formed input, so legitimate values are unaffected.
func SanitizeUTF8(s string) string {
	return strings.ToValidUTF8(s, "\uFFFD")
}

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

func normalized(s string) string {
	s = unicodeNormalizer.Normalize(s)

	s = strings.ToLower(strings.TrimSpace(s))

	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, "\u2019", "")
	s = strings.ReplaceAll(s, "\u2018", "")
	s = strings.ReplaceAll(s, "\u02bc", "")
	s = strings.ReplaceAll(s, "`", "")

	// Colons become spaces - "csi: miami" → "csi miami", "re:zero" → "re zero".
	// A file name cannot hold a colon, so on-disk names space it while an
	// arr-built announce name keeps it. Trade: "re:zero" no longer folds to "rezero".
	s = strings.ReplaceAll(s, ":", " ")

	// Remove exclamation and question marks - scene naming drops them,
	// so "Overtake!" announces as "Overtake.S01..."
	s = strings.ReplaceAll(s, "!", "")
	s = strings.ReplaceAll(s, "?", "")

	s = strings.ReplaceAll(s, ",", " ")

	// Periods become spaces - "Madame de..." → "madame de". A dotted name
	// loses its periods when it is parsed; an announce keeps them.
	s = strings.ReplaceAll(s, ".", " ")

	s = strings.ReplaceAll(s, "&", " and ")

	s = strings.ReplaceAll(s, "-", " ")

	s = animeTitleSymbolReplacer.Replace(s)

	s = strings.Join(strings.Fields(s), " ")

	return s
}

func NormalizeUnicode(s string) string {
	return unicodeNormalizer.Normalize(s)
}

func NormalizeForMatching(s string) string {
	return matchingNormalizer.Normalize(s)
}
