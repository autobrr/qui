// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package httphelpers

import "strings"

// NormalizeBasePath converts a configured base URL into a path prefix that
// always begins with a leading slash and omits any trailing slash. An empty
// string indicates the root.
func NormalizeBasePath(baseURL string) string {
	base := strings.TrimSpace(baseURL)
	if base == "" || base == "/" {
		return ""
	}

	base = strings.TrimRight(base, "/")
	if base == "" || base == "/" {
		return ""
	}

	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}

	return base
}

// JoinBasePath ensures the provided suffix is properly joined to the base
// prefix. The suffix may be relative or absolute and is treated as relative
// when the base is non-empty.
func JoinBasePath(basePath, suffix string) string {
	if basePath == "" {
		if suffix == "" {
			return "/"
		}
		if strings.HasPrefix(suffix, "/") {
			return suffix
		}
		return "/" + suffix
	}

	if suffix == "" {
		return basePath
	}

	if strings.HasPrefix(suffix, "/") {
		return basePath + suffix
	}

	return basePath + "/" + suffix
}
