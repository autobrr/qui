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

// NormalizePath cleans a path and case-folds it for consistent comparison. Keys
// keep d's separators: a slash key would merge a UNC path with a rooted one.
//
// Case-folding is unconditional because runtime.GOOS cannot answer whether the
// scanned filesystem is case-insensitive: macOS APFS and Windows volumes are
// case-insensitive by default, and Docker bind-mounts them into a Linux
// container, where qBittorrent can report two save paths that differ only by
// case for one physical directory.
//
// Keep the fold stateless. Orphan scan fills its file map before the walk and
// runs one goroutine per instance, so a per-scan or global fold flag that
// differs between Add and Has turns every file into an orphan.
//
// Invalid UTF-8 is folded too. strings.ToLower and encoding/json replace a bad
// byte the same way, so the fold is what lets a name read from disk match the
// same name after a round trip through the qBittorrent API. Do not add a
// utf8.ValidString guard here: an owned file whose name carries a non-UTF-8
// byte is then reported as an orphan and deleted.
//
// ToLower can turn an NFC string into a non-NFC one (H + U+0331 becomes U+1E96),
// so the folded result is cleaned again.
//
// ponytail: names that differ only by case, or only in which invalid bytes they
// carry, collapse to one key. That direction is safe (a missed orphan, never a
// deleted owned file). Upgrade path if it is ever reported: keep a folded
// secondary index and confirm hits with os.SameFile.
//
// The result is for comparison only. It must never reach an os.* call or the
// database, because on a case-sensitive filesystem it can name a different file.
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
