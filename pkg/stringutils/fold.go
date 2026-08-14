// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package stringutils

import (
	"cmp"
	"strings"
	"unicode/utf8"
)

// Case-insensitive helpers that do not allocate for ASCII input, which torrent
// names, categories, tags and tracker URLs effectively always are. The obvious
// spelling, strings.Contains(strings.ToLower(a), strings.ToLower(b)), builds a
// lower-cased copy of both strings. That is fine once, but these run once per
// torrent, or once per comparison inside a sort, where the copies dominate the
// work. Both arguments are folded, so callers never have to pre-lower anything.
// Non-ASCII input falls back to the ToLower form so results stay identical.

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// isASCII reports whether s contains only single-byte runes.
func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// ContainsFold reports whether s contains sub, case-insensitively.
func ContainsFold(s, sub string) bool {
	if sub == "" {
		return true
	}
	if !isASCII(s) || !isASCII(sub) {
		return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
	}
	// Only sound for ASCII: ToLower can change a non-ASCII string's byte length.
	if len(sub) > len(s) {
		return false
	}

	first := lowerASCII(sub[0])
	for i := 0; i+len(sub) <= len(s); i++ {
		if lowerASCII(s[i]) != first {
			continue
		}
		match := true
		for j := 1; j < len(sub); j++ {
			if lowerASCII(s[i+j]) != lowerASCII(sub[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// HasPrefixFold reports whether s starts with prefix, case-insensitively.
func HasPrefixFold(s, prefix string) bool {
	if !isASCII(s) || !isASCII(prefix) {
		return strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix))
	}
	// Only sound for ASCII: ToLower can change a non-ASCII string's byte length.
	if len(prefix) > len(s) {
		return false
	}
	for i := range len(prefix) {
		if lowerASCII(s[i]) != lowerASCII(prefix[i]) {
			return false
		}
	}
	return true
}

// HasSuffixFold reports whether s ends with suffix, case-insensitively.
func HasSuffixFold(s, suffix string) bool {
	if !isASCII(s) || !isASCII(suffix) {
		return strings.HasSuffix(strings.ToLower(s), strings.ToLower(suffix))
	}
	// Only sound for ASCII: ToLower can change a non-ASCII string's byte length.
	if len(suffix) > len(s) {
		return false
	}
	offset := len(s) - len(suffix)
	for i := range len(suffix) {
		if lowerASCII(s[offset+i]) != lowerASCII(suffix[i]) {
			return false
		}
	}
	return true
}

// CompareFold compares a and b case-insensitively, matching
// strings.Compare(strings.ToLower(a), strings.ToLower(b)).
func CompareFold(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		ca, cb := a[i], b[i]
		if ca >= utf8.RuneSelf || cb >= utf8.RuneSelf {
			return strings.Compare(strings.ToLower(a), strings.ToLower(b))
		}
		ca, cb = lowerASCII(ca), lowerASCII(cb)
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
	}

	// One string is a folded prefix of the other, so only the lengths decide.
	// Lower-casing cannot change that, even if the longer tail is not ASCII.
	return cmp.Compare(len(a), len(b))
}
