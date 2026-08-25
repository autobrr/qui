// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package stringutils

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The fold helpers exist only because they avoid allocating, so every one of
// them has to return exactly what the strings.ToLower spelling returns.

var foldSamples = []string{
	"",
	"a",
	"Some.Release.Title.S01E02.2160p.WEB-DL-GRPA",
	"some release title s01e02",
	"UPPER.CASE.ONLY",
	"mixed_Case-With[Brackets](And){Braces}",
	"  leading and trailing  ",
	"Bjørnen.Sover.2019.1080p",
	"Æbler.Og.Pærer",
	"Ünïcödé Ñämé",
	"cross-seed, unregistered, permaseed",
	"k",
	"ß",
	"ss",
	"İ",
}

// The Kelvin sign and capital sharp s lower-case to FEWER bytes ("k", "ß"),
// and the dotted capital I to MORE ("i" plus a combining dot), so they catch
// any byte-length arithmetic that runs before the non-ASCII fallback.
var foldNeedles = []string{"", "s01", "S01", "release", "ø", "æbler", "  ", "grpa", "nomatch", "a", "K", "ẞ", "İ"}

func TestContainsFoldMatchesLowerContains(t *testing.T) {
	for _, haystack := range foldSamples {
		for _, needle := range foldNeedles {
			want := strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
			assert.Equal(t, want, ContainsFold(haystack, needle), "haystack %q needle %q", haystack, needle)
			assert.Equal(t, want, ContainsFold(haystack, strings.ToUpper(needle)), "haystack %q upper needle %q", haystack, needle)
		}
	}
}

func TestCompareFoldMatchesLowerCompare(t *testing.T) {
	for _, a := range foldSamples {
		for _, b := range foldSamples {
			want := strings.Compare(strings.ToLower(a), strings.ToLower(b))
			assert.Equal(t, want, CompareFold(a, b), "a %q b %q", a, b)
		}
	}

	// Prefix and case-boundary cases the sample list does not cover.
	extra := [][2]string{
		{"abc", "ABCD"}, {"ABCD", "abc"}, {"abc", "abc"}, {"", "a"}, {"a", ""},
		{"Zebra", "apple"}, {"apple", "Zebra"}, {"a", "Æ"}, {"Æ", "a"}, {"æ", "Æ"},
		{"tracker", "Tracker.example"}, {"[b]", "[B]"},
	}
	for _, pair := range extra {
		want := strings.Compare(strings.ToLower(pair[0]), strings.ToLower(pair[1]))
		assert.Equal(t, want, CompareFold(pair[0], pair[1]), "a %q b %q", pair[0], pair[1])
	}
}

func TestHasPrefixSuffixFoldMatchLowerForms(t *testing.T) {
	for _, s := range foldSamples {
		for _, needle := range foldNeedles {
			lower := strings.ToLower(needle)
			assert.Equal(t, strings.HasPrefix(strings.ToLower(s), lower), HasPrefixFold(s, needle), "prefix %q in %q", needle, s)
			assert.Equal(t, strings.HasSuffix(strings.ToLower(s), lower), HasSuffixFold(s, needle), "suffix %q in %q", needle, s)
			assert.Equal(t, strings.HasPrefix(strings.ToLower(s), lower), HasPrefixFold(s, strings.ToUpper(needle)), "upper prefix %q in %q", needle, s)
			assert.Equal(t, strings.HasSuffix(strings.ToLower(s), lower), HasSuffixFold(s, strings.ToUpper(needle)), "upper suffix %q in %q", needle, s)
		}
	}
}
