// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// CleanPath cleans a path without changing its casing.
// Uses the backend's path dialect and NFC unicode normalization to
// avoid mismatches between canonically-equivalent strings (e.g. composed vs
// decomposed forms on some platforms).
// Tradeoff: canonically-equivalent names collapse to one key, so byte-distinct
// NFC/NFD twins on normalization-sensitive filesystems are treated as one path.
// Use this when the result is shown to the user or stored; use NormalizePath
// when the result is only compared against another path.
func CleanPath(d PathDialect, path string) string {
	p := d.Clean(path)
	// On Unix, paths can contain arbitrary bytes (not always valid UTF-8).
	// Avoid normalizing invalid UTF-8 to prevent replacing bytes with U+FFFD.
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
		// Already NFC, and lowering ASCII cannot unclean it.
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
