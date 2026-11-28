// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pathutil

import (
	"path"
	"runtime"
	"strings"
)

// NormalizeGitBashDrivePath converts Git Bash / MSYS2 drive-rooted paths such as
// "/c/backups" or "/d" into Windows drive paths ("C:\\backups", "D:\\").
// Paths without a single-letter leading segment are returned unchanged.
func NormalizeGitBashDrivePath(raw string) string {
	if len(raw) < 2 || raw[0] != '/' {
		return raw
	}
	drive := raw[1]
	isUpper := drive >= 'A' && drive <= 'Z'
	isLower := drive >= 'a' && drive <= 'z'
	if !isUpper && !isLower {
		return raw
	}
	if len(raw) > 2 && raw[2] != '/' {
		return raw
	}
	if isLower {
		drive -= 'a' - 'A'
	}
	rest := "/"
	if len(raw) > 2 {
		rest = path.Clean(raw[2:])
	}
	if rest == "/" || rest == "." {
		return string(drive) + `:\`
	}
	return string(drive) + `:` + strings.ReplaceAll(rest, "/", `\`)
}

// NormalizeWindowsDrivePath converts Git Bash / MSYS2 drive-rooted paths into
// Windows drive paths when running on Windows, and returns raw unchanged on other OSes.
func NormalizeWindowsDrivePath(raw string) string {
	if runtime.GOOS != "windows" {
		return raw
	}
	return NormalizeGitBashDrivePath(raw)
}
