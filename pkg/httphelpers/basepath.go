// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package httphelpers

import "strings"

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
