// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"regexp"
	"strings"
	"time"

	"github.com/autobrr/qui/pkg/pathcmp"
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
	// qBittorrent cleans the path it stores, so compare cleaned paths or a move repeats every run.
	return strings.ToLower(pathcmp.NormalizePath(p))
}
