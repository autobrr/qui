// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func CleanPath(d PathDialect, path string) string {
	p := d.Clean(path)
	if !utf8.ValidString(p) {
		return p
	}
	if !norm.NFC.IsNormalString(p) {
		p = norm.NFC.String(p)
	}
	return p
}

func NormalizePath(d PathDialect, path string) string {
	p := d.Clean(path)
	if isASCII(p) {
		return strings.ToLower(p)
	}
	return CleanPath(d, strings.ToLower(CleanPath(d, p)))
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
