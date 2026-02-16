// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"sort"
	"strings"
)

// videoCodecAliases maps equivalent video codec names to a canonical form.
// x264, H.264, H264, and AVC all refer to the same underlying codec (AVC/H.264).
// x265, H.265, H265, and HEVC all refer to the same underlying codec (HEVC/H.265).
var videoCodecAliases = map[string]string{
	"X264":  "AVC",
	"H.264": "AVC",
	"H264":  "AVC",
	"AVC":   "AVC",
	"X265":  "HEVC",
	"H.265": "HEVC",
	"H265":  "HEVC",
	"HEVC":  "HEVC",
}

// NormalizeVideoCodec converts a video codec string to its canonical form.
// Returns the original (uppercased) string if no alias mapping exists.
func NormalizeVideoCodec(codec string) string {
	upper := strings.ToUpper(strings.TrimSpace(codec))
	if canonical, ok := videoCodecAliases[upper]; ok {
		return canonical
	}
	return upper
}

// JoinNormalizedCodecSlice converts a codec slice to a normalized string for comparison.
// Applies codec aliasing so that x264, H.264, H264, and AVC are treated as equivalent.
func JoinNormalizedCodecSlice(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(slice))
	normalized := make([]string, 0, len(slice))
	for _, codec := range slice {
		n := NormalizeVideoCodec(codec)
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		normalized = append(normalized, n)
	}
	sort.Strings(normalized)
	return strings.Join(normalized, " ")
}

// sourceAliases maps source names to a canonical form for comparison.
// WEB-DL variants normalize to WEBDL, WEBRip variants to WEBRIP.
// Plain "WEB" stays as "WEB" and is treated as ambiguous (matches both).
var sourceAliases = map[string]string{
	"WEB-DL": "WEBDL",
	"WEBDL":  "WEBDL",
	"WEBRIP": "WEBRIP",
	"WEB":    "WEB",
}

// NormalizeSource converts a source string to its canonical form.
// Returns the original (uppercased) string if no alias mapping exists.
func NormalizeSource(source string) string {
	upper := strings.ToUpper(strings.TrimSpace(source))
	if canonical, ok := sourceAliases[upper]; ok {
		return canonical
	}
	return upper
}
