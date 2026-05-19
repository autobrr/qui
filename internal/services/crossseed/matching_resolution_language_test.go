// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestReleasesMatch_ResolutionAndLanguage(t *testing.T) {
	t.Parallel()

	svc := &Service{
		releaseCache:     releases.NewDefaultParser(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	tests := []struct {
		name          string
		sourceName    string
		candidateName string
		wantMatch     bool
	}{
		{
			name:          "missing resolution and explicit language must not match different resolution/language",
			sourceName:    "NCIS.Sydney.S03E07.FRENCH.WEB.H264-GROUP",
			candidateName: "NCIS.Sydney.S03E07.MULTi.1080p.WEB.H264-GROUP",
			wantMatch:     false,
		},
		{
			name:          "missing resolution must not match explicit resolution",
			sourceName:    "NCIS.Sydney.S03E07.MULTI.WEB.H264-GROUP",
			candidateName: "NCIS.Sydney.S03E07.MULTi.1080p.WEB.H264-GROUP",
			wantMatch:     false,
		},
		{
			name:          "explicit language must not match different language",
			sourceName:    "NCIS.Sydney.S03E07.FRENCH.1080p.WEB.H264-GROUP",
			candidateName: "NCIS.Sydney.S03E07.MULTi.1080p.WEB.H264-GROUP",
			wantMatch:     false,
		},
		{
			name:          "identical releases should match",
			sourceName:    "NCIS.Sydney.S03E07.MULTi.1080p.WEB.H264-GROUP",
			candidateName: "NCIS.Sydney.S03E07.MULTi.1080p.WEB.H264-GROUP",
			wantMatch:     true,
		},
		// SD resolution exception: empty resolution should match known SD resolutions.
		{
			name:          "empty resolution matches 480p (SD exception)",
			sourceName:    "Show.S01E01.WEB-DL-GROUP",
			candidateName: "Show.S01E01.480p.WEB-DL-GROUP",
			wantMatch:     true,
		},
		{
			name:          "empty resolution matches 576p (SD exception)",
			sourceName:    "Show.S01E01.WEB-DL-GROUP",
			candidateName: "Show.S01E01.576p.WEB-DL-GROUP",
			wantMatch:     true,
		},
		{
			name:          "empty resolution matches SD (SD exception)",
			sourceName:    "Show.S01E01.WEB-DL-GROUP",
			candidateName: "Show.S01E01.SD.WEB-DL-GROUP",
			wantMatch:     true,
		},
		{
			name:          "576p matches empty resolution (SD exception bidirectional)",
			sourceName:    "Show.S01E01.576p.WEB-DL-GROUP",
			candidateName: "Show.S01E01.WEB-DL-GROUP",
			wantMatch:     true,
		},
		{
			name:          "empty resolution does NOT match 720p",
			sourceName:    "Show.S01E01.WEB-DL-GROUP",
			candidateName: "Show.S01E01.720p.WEB-DL-GROUP",
			wantMatch:     false,
		},
		// Language exception: empty language treated as equivalent to ENGLISH.
		{
			name:          "empty language matches ENGLISH",
			sourceName:    "Show.S01E01.1080p.WEB-DL-GROUP",
			candidateName: "Show.S01E01.ENGLISH.1080p.WEB-DL-GROUP",
			wantMatch:     true,
		},
		{
			name:          "ENGLISH matches empty language",
			sourceName:    "Show.S01E01.ENGLISH.1080p.WEB-DL-GROUP",
			candidateName: "Show.S01E01.1080p.WEB-DL-GROUP",
			wantMatch:     true,
		},
		{
			name:          "empty language does NOT match FRENCH",
			sourceName:    "Show.S01E01.1080p.WEB-DL-GROUP",
			candidateName: "Show.S01E01.FRENCH.1080p.WEB-DL-GROUP",
			wantMatch:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sourceRelease := svc.releaseCache.Parse(tt.sourceName)
			candidateRelease := svc.releaseCache.Parse(tt.candidateName)

			result := svc.releasesMatch(sourceRelease, candidateRelease, false)
			require.Equal(t, tt.wantMatch, result)
		})
	}
}
