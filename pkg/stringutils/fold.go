// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package stringutils

import (
	"cmp"
	"strings"
	"unicode/utf8"
)

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func ContainsFold(s, sub string) bool {
	if sub == "" {
		return true
	}
	if !isASCII(s) || !isASCII(sub) {
		return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
	}
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

func HasPrefixFold(s, prefix string) bool {
	if !isASCII(s) || !isASCII(prefix) {
		return strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix))
	}
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

func HasSuffixFold(s, suffix string) bool {
	if !isASCII(s) || !isASCII(suffix) {
		return strings.HasSuffix(strings.ToLower(s), strings.ToLower(suffix))
	}
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

	return cmp.Compare(len(a), len(b))
}
