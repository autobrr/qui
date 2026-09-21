// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"regexp"
	"strings"
	"time"

	"github.com/autobrr/qui/pkg/stringutils"
)

var (
	lowerTrimNormalizer       = stringutils.NewDefaultNormalizer()
	lowercaseNormalizer       = stringutils.NewNormalizer(5*time.Minute, strings.ToLower)
	pathComparisonNormalizer  = stringutils.NewNormalizer(5*time.Minute, normalizePathInner)
	trackerHostSanitizeRegexp = regexp.MustCompile(`[^a-zA-Z0-9\.-]`)
)

func normalizeLowerTrim(value string) string {
	return lowerTrimNormalizer.Normalize(value)
}

func normalizeLower(value string) string {
	return lowercaseNormalizer.Normalize(value)
}

func normalizePathInner(p string) string {
	if p == "" {
		return ""
	}
	// Lowercase for case-insensitive comparison
	p = strings.ToLower(p)
	// Normalize path separators (Windows backslashes to forward slashes)
	p = strings.ReplaceAll(p, "\\", "/")
	// qBittorrent reports /a//b as /a/b, so collapse repeated separators. Exactly
	// two leading slashes mark a UNC path and stay; three or more collapse.
	unc := strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if unc {
		p = "/" + p
	}
	// Remove trailing slash
	p = strings.TrimSuffix(p, "/")
	return p
}
