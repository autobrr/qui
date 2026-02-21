// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package pathcmp provides shared path normalization helpers used for
// cross-platform path comparisons. qBittorrent paths are generally forward-slashed,
// so we normalize using path semantics (not filepath).
package pathcmp

import (
	"path"
	"strings"
)

// IsWindowsDriveAbs returns true if p is a Windows absolute path (e.g., C:/...).
// It requires a drive letter, colon, and forward slash. Backslashes should be
// normalized before calling.
func IsWindowsDriveAbs(p string) bool {
	if len(p) < 3 {
		return false
	}
	c := p[0]
	return ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) && p[1] == ':' && p[2] == '/'
}

// NormalizePath normalizes a file path for comparison by:
// - Converting backslashes to forward slashes
// - Removing trailing slashes (preserving Windows drive roots like C:/)
// - Cleaning the path (removing . and .. where possible)
func NormalizePath(p string) string {
	if p == "" {
		return ""
	}
	// Convert backslashes to forward slashes for cross-platform comparison.
	p = strings.ReplaceAll(p, "\\", "/")

	// Handle Windows drive paths specially to preserve C:/ (path.Clean turns it into C:).
	if len(p) >= 2 && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) && p[1] == ':' {
		drive := p[:2] // "C:"
		rest := p[2:]  // "/foo/bar" or "/" or "" (drive-relative)

		// Bare drive letter (C:) is drive-relative.
		if rest == "" {
			return drive
		}

		rest = path.Clean(rest)
		// Ensure drive root stays as C:/ not C:
		if rest == "/" || rest == "." {
			return drive + "/"
		}
		return drive + rest
	}

	// Non-Windows path: standard cleaning.
	p = path.Clean(p)
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// NormalizePathFold is a case-folded version of NormalizePath for case-insensitive comparisons.
func NormalizePathFold(p string) string {
	return strings.ToLower(NormalizePath(p))
}
