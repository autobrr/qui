// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

// TestWebSourceRelabel covers the cross-tracker relabel rule that lets the
// same web encode through when only its WEBRip/WEB-DL label differs. The real
// motivating case is "Law & Order: SVU" S05, which trackers list as both
// WEBRip (the seeded copy) and WEB-DL (the relabel) at the same content size.
func TestWebSourceRelabel(t *testing.T) {
	s := matcher{stringNormalizer: stringutils.NewDefaultNormalizer()}

	const (
		webripDotted = "Law.and.Order.Special.Victims.Unit.S05.1080p.AMZN.WEBRip.DD2.0.x264-NTb"
		webdlDotted  = "Law.and.Order.Special.Victims.Unit.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb"
		webdlSpaced  = "Law & Order: Special Victims Unit S05 1080p AMZN WEB-DL DD+ 2.0 H.264-NTb"
	)

	const size int64 = 50_000_000_000

	tests := []struct {
		name          string
		sourceName    string
		candidateName string
		noSizes       bool
		want          bool
	}{
		{
			name:          "WEBRip source vs WEB-DL relabel of same encode",
			sourceName:    webripDotted,
			candidateName: webdlDotted,
			want:          true,
		},
		{
			name:          "a relabel needs both sizes",
			sourceName:    webripDotted,
			candidateName: webdlDotted,
			noSizes:       true,
			want:          false,
		},
		{
			name:          "symmetric: WEB-DL source vs WEBRip relabel",
			sourceName:    webdlDotted,
			candidateName: webripDotted,
			want:          true,
		},
		{
			name:          "relabel detected through ampersand/colon title variant",
			sourceName:    webripDotted,
			candidateName: webdlSpaced,
			want:          true,
		},
		{
			name:          "different resolution is not a relabel",
			sourceName:    webripDotted,
			candidateName: "Law.and.Order.Special.Victims.Unit.S05.2160p.AMZN.WEB-DL.DD+2.0.x264-NTb",
			want:          false,
		},
		{
			name:          "different show is not a relabel",
			sourceName:    webripDotted,
			candidateName: "Law.and.Order.Organized.Crime.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb",
			want:          false,
		},
		{
			name:          "non-web source is not a relabel",
			sourceName:    "Law.and.Order.Special.Victims.Unit.S05.1080p.BluRay.DD2.0.x264-NTb",
			candidateName: webdlDotted,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := rls.ParseString(tt.sourceName)
			candidate := rls.ParseString(tt.candidateName)
			evidence := matchEvidence{SourceSize: size, CandidateSize: size}
			if tt.noSizes {
				evidence = matchEvidence{}
			}
			verdict := s.matchVerdict(
				namedRelease{release: &source, rawName: tt.sourceName},
				namedRelease{release: &candidate, rawName: tt.candidateName},
				evidence,
				matchPolicy{SizeTolerancePercent: 5},
			)
			require.Equal(t, tt.want, verdict.Class == searchCandidateClassWebSourceRelabel)
		})
	}
}

// TestWebSourceRelabelSizeGate covers the size gate of the relabel, including
// the regression where a single-episode WEB relabel of a season-pack source was
// dropped on the full-pack vs episode size mismatch before the episode-size
// bypass could apply (P1). The pack is ~100GB, the episode ~5GB.
func TestWebSourceRelabelSizeGate(t *testing.T) {
	s := matcher{stringNormalizer: stringutils.NewDefaultNormalizer()}

	const (
		packWebrip   = "Law.and.Order.Special.Victims.Unit.S05.1080p.AMZN.WEBRip.DD2.0.x264-NTb"
		packWebdl    = "Law.and.Order.Special.Victims.Unit.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb"
		episodeWebdl = "Law.and.Order.Special.Victims.Unit.S05E01.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb"
		otherShow    = "Law.and.Order.Organized.Crime.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb"
	)

	const (
		packSize    int64 = 100_000_000_000
		episodeSize int64 = 5_000_000_000
	)

	tests := []struct {
		name          string
		sourceName    string
		candidateName string
		findEpisodes  bool
		sourceSize    int64
		candidateSize int64
		want          bool
	}{
		{
			name:          "episode relabel accepted when episode-size bypass is active",
			sourceName:    packWebrip,
			candidateName: episodeWebdl,
			findEpisodes:  true,
			sourceSize:    packSize,
			candidateSize: episodeSize,
			want:          true,
		},
		{
			name:          "pack relabel dropped on full-size mismatch",
			sourceName:    packWebrip,
			candidateName: packWebdl,
			sourceSize:    packSize,
			candidateSize: episodeSize,
			want:          false,
		},
		{
			name:          "full-pack relabel accepted within size tolerance",
			sourceName:    packWebrip,
			candidateName: packWebdl,
			sourceSize:    packSize,
			candidateSize: packSize,
			want:          true,
		},
		{
			name:          "different show within size tolerance is not a relabel",
			sourceName:    packWebrip,
			candidateName: otherShow,
			sourceSize:    packSize,
			candidateSize: packSize,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := rls.ParseString(tt.sourceName)
			candidate := rls.ParseString(tt.candidateName)
			verdict := s.matchVerdict(
				namedRelease{release: &source, rawName: tt.sourceName},
				namedRelease{release: &candidate, rawName: tt.candidateName},
				matchEvidence{SourceSize: tt.sourceSize, CandidateSize: tt.candidateSize},
				matchPolicy{IndividualEpisodes: tt.findEpisodes, SizeTolerancePercent: 5},
			)
			require.Equal(t, tt.want, verdict.Class == searchCandidateClassWebSourceRelabel)
		})
	}
}
