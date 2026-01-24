// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestReleasesMatch_SiteMustMatch(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	tests := []struct {
		name        string
		source      rls.Release
		candidate   rls.Release
		wantMatch   bool
		description string
	}{
		{
			name: "different sites should not match",
			source: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
			},
			candidate: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "AnoZu",
			},
			wantMatch:   false,
			description: "different fansub groups (Site field) should NOT match",
		},
		{
			name: "same sites should match",
			source: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
			},
			candidate: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
			},
			wantMatch:   true,
			description: "same fansub group should match",
		},
		{
			name: "source has site, candidate does not - should match",
			source: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
			},
			candidate: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
			},
			wantMatch:   true,
			description: "missing site metadata should not block a match",
		},
		{
			name: "candidate has site, source does not - should match",
			source: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
			},
			candidate: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
			},
			wantMatch:   true,
			description: "source has no site so site matching is not enforced",
		},
		{
			name: "site comparison is case insensitive",
			source: rls.Release{
				Title:   "Show",
				Series:  1,
				Episode: 1,
				Site:    "SUBSPLEASE",
			},
			candidate: rls.Release{
				Title:   "Show",
				Series:  1,
				Episode: 1,
				Site:    "subsplease",
			},
			wantMatch:   true,
			description: "site comparison should be case insensitive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.releasesMatch(&tt.source, &tt.candidate, false)
			if tt.wantMatch {
				require.True(t, result, tt.description)
			} else {
				require.False(t, result, tt.description)
			}
		})
	}
}

func TestReleasesMatch_SumMustMatch(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	tests := []struct {
		name        string
		source      rls.Release
		candidate   rls.Release
		wantMatch   bool
		description string
	}{
		{
			name: "different checksums should not match",
			source: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
				Sum:     "32ECE75A",
			},
			candidate: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
				Sum:     "DEADBEEF",
			},
			wantMatch:   false,
			description: "different CRC32 checksums indicate different encodes",
		},
		{
			name: "same checksums should match",
			source: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
				Sum:     "32ECE75A",
			},
			candidate: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
				Sum:     "32ECE75A",
			},
			wantMatch:   true,
			description: "same checksum should match",
		},
		{
			name: "source has sum, candidate does not - should not match",
			source: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
				Sum:     "32ECE75A",
			},
			candidate: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
			},
			wantMatch:   false,
			description: "candidate must have matching checksum when source has one",
		},
		{
			name: "candidate has sum, source does not - should match",
			source: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
			},
			candidate: rls.Release{
				Title:   "Kingdom",
				Series:  6,
				Episode: 11,
				Site:    "SubsPlease",
				Sum:     "32ECE75A",
			},
			wantMatch:   true,
			description: "source has no checksum so checksum matching is not enforced",
		},
		{
			name: "sum comparison is case insensitive",
			source: rls.Release{
				Title:   "Show",
				Series:  1,
				Episode: 1,
				Sum:     "ABCD1234",
			},
			candidate: rls.Release{
				Title:   "Show",
				Series:  1,
				Episode: 1,
				Sum:     "abcd1234",
			},
			wantMatch:   true,
			description: "checksum comparison should be case insensitive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.releasesMatch(&tt.source, &tt.candidate, false)
			if tt.wantMatch {
				require.True(t, result, tt.description)
			} else {
				require.False(t, result, tt.description)
			}
		})
	}
}

func TestReleasesMatch_AnimeRealWorld(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	// These are parsed by rls from real torrent names
	subsPlease := rls.Release{
		Title:      "Kingdom",
		Series:     6,
		Episode:    11,
		Resolution: "1080p",
		Site:       "SubsPlease",
		Sum:        "32ECE75A",
	}

	subsPleaseMatchingSum := rls.Release{
		Title:      "Kingdom",
		Series:     6,
		Episode:    11,
		Resolution: "1080p",
		Site:       "SubsPlease",
		Sum:        "32ECE75A",
	}

	anoZu := rls.Release{
		Title:      "Kingdom",
		Series:     6,
		Episode:    11,
		Resolution: "1080p",
		Source:     "WEB-DL",
		Codec:      []string{"H264"},
		Site:       "AnoZu",
	}

	// Source without checksum can match candidate with or without checksum
	subsPleaseNoSum := rls.Release{
		Title:      "Kingdom",
		Series:     6,
		Episode:    11,
		Resolution: "1080p",
		Site:       "SubsPlease",
	}

	require.False(t, s.releasesMatch(&subsPlease, &anoZu, false),
		"SubsPlease and AnoZu are different fansub groups - should NOT match")

	require.True(t, s.releasesMatch(&subsPlease, &subsPleaseMatchingSum, false),
		"same SubsPlease release with matching checksum should match")

	require.False(t, s.releasesMatch(&subsPlease, &subsPleaseNoSum, false),
		"source has checksum so candidate must have matching checksum")

	require.True(t, s.releasesMatch(&subsPleaseNoSum, &subsPlease, false),
		"source without checksum can match candidate with checksum")
}
