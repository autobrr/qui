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

func SanitizeUTF8(s string) string {
	return strings.ToValidUTF8(s, "\uFFFD")
}

func normalizeUnicodeInner(s string) string {
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

	s = strings.ReplaceAll(s, ":", " ")

	s = strings.ReplaceAll(s, "!", "")
	s = strings.ReplaceAll(s, "?", "")

	s = strings.ReplaceAll(s, ",", " ")

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
