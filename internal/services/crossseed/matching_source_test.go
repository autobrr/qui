// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestNormalizeSource(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// WEB-DL variants normalize to WEBDL
		{"WEB-DL uppercase", "WEB-DL", "WEBDL"},
		{"web-dl lowercase", "web-dl", "WEBDL"},
		{"WEBDL no hyphen", "WEBDL", "WEBDL"},

		// WEBRip variants normalize to WEBRIP
		{"WEBRIP uppercase", "WEBRIP", "WEBRIP"},
		{"WEBRip mixed", "WEBRip", "WEBRIP"},
		{"webrip lowercase", "webrip", "WEBRIP"},

		// Plain WEB stays as WEB (ambiguous)
		{"WEB uppercase", "WEB", "WEB"},
		{"web lowercase", "web", "WEB"},

		// Non-web sources pass through uppercased
		{"BluRay passthrough", "BluRay", "BLURAY"},
		{"HDTV passthrough", "HDTV", "HDTV"},
		{"DVDRip passthrough", "DVDRip", "DVDRIP"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeSource(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSourcesCompatible(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		candidate  string
		compatible bool
	}{
		// Empty sources are always compatible
		{"both empty", "", "", true},
		{"source empty", "", "WEBDL", true},
		{"candidate empty", "WEBDL", "", true},

		// Identical sources are compatible
		{"both WEBDL", "WEBDL", "WEBDL", true},
		{"both WEBRIP", "WEBRIP", "WEBRIP", true},
		{"both WEB", "WEB", "WEB", true},
		{"both BLURAY", "BLURAY", "BLURAY", true},

		// WEB is ambiguous - matches both WEBDL and WEBRIP
		{"WEB matches WEBDL", "WEB", "WEBDL", true},
		{"WEBDL matches WEB", "WEBDL", "WEB", true},
		{"WEB matches WEBRIP", "WEB", "WEBRIP", true},
		{"WEBRIP matches WEB", "WEBRIP", "WEB", true},

		// WEBDL and WEBRIP are explicitly different
		{"WEBDL vs WEBRIP", "WEBDL", "WEBRIP", false},
		{"WEBRIP vs WEBDL", "WEBRIP", "WEBDL", false},

		// Other sources must match exactly
		{"BLURAY vs HDTV", "BLURAY", "HDTV", false},
		{"WEBDL vs BLURAY", "WEBDL", "BLURAY", false},
		{"WEB does not match BLURAY", "WEB", "BLURAY", false},
		{"BLURAY does not match WEB", "BLURAY", "WEB", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sourcesCompatible(tt.source, tt.candidate)
			require.Equal(t, tt.compatible, result)
		})
	}
}

func TestReleasesMatch_SourceCompatibility(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	tests := []struct {
		name        string
		source      rls.Release
		candidate   rls.Release
		shouldMatch bool
		description string
	}{
		{
			name: "WEB matches WEB-DL",
			source: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEB",
				Resolution: "2160p",
				Group:      "GROUP",
			},
			candidate: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEB-DL",
				Resolution: "2160p",
				Group:      "GROUP",
			},
			shouldMatch: true,
			description: "WEB is ambiguous and should match WEB-DL for precheck",
		},
		{
			name: "WEB-DL matches WEB",
			source: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEB-DL",
				Resolution: "2160p",
				Group:      "GROUP",
			},
			candidate: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEB",
				Resolution: "2160p",
				Group:      "GROUP",
			},
			shouldMatch: true,
			description: "WEB-DL should match ambiguous WEB for precheck",
		},
		{
			name: "WEB matches WEBRip",
			source: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEB",
				Resolution: "1080p",
				Group:      "GROUP",
			},
			candidate: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEBRip",
				Resolution: "1080p",
				Group:      "GROUP",
			},
			shouldMatch: true,
			description: "WEB is ambiguous and should match WEBRip for precheck",
		},
		{
			name: "WEB-DL does not match WEBRip",
			source: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Group:      "GROUP",
			},
			candidate: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEBRip",
				Resolution: "1080p",
				Group:      "GROUP",
			},
			shouldMatch: false,
			description: "WEB-DL and WEBRip are explicitly different sources",
		},
		{
			name: "WEBRip does not match WEB-DL",
			source: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEBRip",
				Resolution: "1080p",
				Group:      "GROUP",
			},
			candidate: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Group:      "GROUP",
			},
			shouldMatch: false,
			description: "WEBRip and WEB-DL are explicitly different sources",
		},
		{
			name: "BluRay does not match WEB-DL",
			source: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "BluRay",
				Resolution: "1080p",
				Group:      "GROUP",
			},
			candidate: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Group:      "GROUP",
			},
			shouldMatch: false,
			description: "Different source types should not match",
		},
		{
			name: "WEB does not match BluRay",
			source: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "WEB",
				Resolution: "1080p",
				Group:      "GROUP",
			},
			candidate: rls.Release{
				Title:      "Movie",
				Year:       2025,
				Source:     "BluRay",
				Resolution: "1080p",
				Group:      "GROUP",
			},
			shouldMatch: false,
			description: "Ambiguous WEB should not match non-web sources",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.releasesMatch(&tt.source, &tt.candidate, false)
			require.Equalf(t, tt.shouldMatch, result, tt.description)
		})
	}
}
