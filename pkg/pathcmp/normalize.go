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

// IsAbsolute reports whether p is absolute on any client OS; filepath.IsAbs would judge qui's host.
func IsAbsolute(p string) bool {
	p = strings.ReplaceAll(p, `\`, "/")
	return strings.HasPrefix(p, "/") || IsWindowsDriveAbs(p)
}

func NormalizePath(p string) string {
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	// Keep the UNC server/share outside path.Clean, which collapses // and
	// would otherwise let .. move above the share root.
	if strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///") {
		server, rest, found := strings.Cut(p[2:], "/")
		share, _, _ := strings.Cut(rest, "/")
		if found && server != "" && share != "" && server != ".." && share != ".." {
			root := p[:2+len(server)+1+len(share)]
			cleaned := path.Clean(p[len(root):])
			if cleaned == "." || cleaned == "/" {
				return root
			}
			return root + cleaned
		}
	}

	if hasDriveLetter(p) {
		drive := p[:2]
		rest := p[2:]

		// Bare drive letter (C:) is drive-relative.
		if rest == "" {
			return drive
		}

		rest = path.Clean(rest)
		if rest == "." {
			return drive
		}
		return drive + rest
	}

	p = path.Clean(p)
	if hasDriveLetter(p) {
		return "./" + p
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

func hasDriveLetter(p string) bool {
	return len(p) >= 2 && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) && p[1] == ':'
}
