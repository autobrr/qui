// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"strings"
	"time"

	"github.com/autobrr/qui/pkg/stringutils"
)

var (
	lowerTrimNormalizer      = stringutils.NewDefaultNormalizer()
	upperTrimNormalizer      = stringutils.NewNormalizer(5*time.Minute, transformToUpper)
	pathComparisonNormalizer = stringutils.NewNormalizer(5*time.Minute, normalizePathForComparisonInner)
	domainNameNormalizer     = stringutils.NewNormalizer(5*time.Minute, normalizeDomainNameInner)
)

func normalizeLowerTrim(value string) string {
	return lowerTrimNormalizer.Normalize(value)
}

func normalizeUpperTrim(value string) string {
	return upperTrimNormalizer.Normalize(value)
}

func normalizePathForComparison(value string) string {
	return pathComparisonNormalizer.Normalize(value)
}

func normalizePathForComparisonInner(value string) string {
	return strings.ToLower(normalizePath(value))
}

func normalizeDomainNameValue(value string) string {
	return domainNameNormalizer.Normalize(value)
}

func normalizeDomainNameInner(value string) string {
	normalized := strings.ReplaceAll(value, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, ".", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return normalized
}
