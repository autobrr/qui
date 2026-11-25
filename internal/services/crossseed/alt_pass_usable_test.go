// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestSearchUsablePredicate(t *testing.T) {
	s := &Service{releaseCache: NewReleaseCache(), stringNormalizer: stringutils.NewDefaultNormalizer()}

	const (
		webripDotted  = "Law.and.Order.Special.Victims.Unit.S05.1080p.AMZN.WEBRip.DD2.0.x264-NTb"
		webdlDotted   = "Law.and.Order.Special.Victims.Unit.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb"
		otherShow     = "Law.and.Order.Organized.Crime.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb"
		episodeDotted = "Law.and.Order.Special.Victims.Unit.S05E03.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb"
		size          = int64(115_682_424_111)
	)

	tests := []struct {
		name                   string
		sourceName             string
		candidateName          string
		sourceSize             int64
		candidateSize          int64
		tolerance              float64
		findIndividualEpisodes bool
		rescueTitleMismatches  bool
		want                   bool
	}{
		{
			name:       "direct match within tolerance is usable",
			sourceName: webdlDotted, candidateName: webdlDotted,
			sourceSize: size, candidateSize: size, tolerance: 5,
			want: true,
		},
		{
			name:       "relabel WEBRip<->WEB-DL within tolerance is usable",
			sourceName: webripDotted, candidateName: webdlDotted,
			sourceSize: size, candidateSize: size, tolerance: 5,
			want: true,
		},
		{
			name:       "direct match outside size tolerance is not usable",
			sourceName: webdlDotted, candidateName: webdlDotted,
			sourceSize: size, candidateSize: size * 2, tolerance: 3,
			want: false,
		},
		{
			name:       "relabel outside size tolerance is not usable",
			sourceName: webripDotted, candidateName: webdlDotted,
			sourceSize: size, candidateSize: size * 2, tolerance: 3,
			want: false,
		},
		{
			name:       "different show is not usable (indexer stays eligible for re-query)",
			sourceName: webripDotted, candidateName: otherShow,
			sourceSize: size, candidateSize: size, tolerance: 5,
			want: false,
		},
		{
			name:       "title mismatch is not usable with title rescue off",
			sourceName: webdlDotted, candidateName: "Different.Title.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb",
			sourceSize: size, candidateSize: size, tolerance: 5,
			want: false,
		},
		{
			name:       "title rescue at an exact size is usable with title rescue on",
			sourceName: webdlDotted, candidateName: "Different.Title.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb",
			sourceSize: size, candidateSize: size, tolerance: 5,
			rescueTitleMismatches: true,
			want:                  true,
		},
		{
			name:       "episode within season pack is usable despite size mismatch (size bypass)",
			sourceName: webdlDotted, candidateName: episodeDotted,
			sourceSize: size, candidateSize: size * 2, tolerance: 3,
			findIndividualEpisodes: true,
			want:                   true,
		},
		{
			name:       "season pack candidate against episode source is rejected (pairing rule)",
			sourceName: episodeDotted, candidateName: webdlDotted,
			sourceSize: size, candidateSize: size, tolerance: 5,
			findIndividualEpisodes: true,
			want:                   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := rls.ParseString(tt.sourceName)
			usable := s.searchUsablePredicate(searchCandidateInput{
				Source:                 namedRelease{release: &source, rawName: tt.sourceName},
				SourceSize:             tt.sourceSize,
				TolerancePercent:       tt.tolerance,
				FindIndividualEpisodes: tt.findIndividualEpisodes,
				RescueTitleMismatches:  tt.rescueTitleMismatches,
			})
			got := usable(jackett.SearchResult{Title: tt.candidateName, Size: tt.candidateSize})
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIndexersWithoutUsableResults(t *testing.T) {
	s := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	const (
		sourceName = "Law.and.Order.Special.Victims.Unit.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb"
		junkTitle  = "Completely.Different.Show.S01.1080p.AMZN.WEB-DL.DD+2.0.x264-XYZ"
		size       = int64(115_682_424_111)
	)
	source := rls.ParseString(sourceName)

	results := []jackett.SearchResult{
		{IndexerID: 1, Title: junkTitle, Size: size},
		{IndexerID: 2, Title: sourceName, Size: size},
	}

	usable := s.searchUsablePredicate(searchCandidateInput{
		Source:           namedRelease{release: &source, rawName: sourceName},
		SourceSize:       size,
		TolerancePercent: 5,
	})
	require.Equal(t, []int{1, 3}, indexersWithoutUsableResults([]int{1, 2, 3}, results, usable))

	require.Equal(t, []int{3}, indexersWithoutResults([]int{1, 2, 3}, results))
}
