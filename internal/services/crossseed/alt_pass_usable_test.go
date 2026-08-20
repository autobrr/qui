// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

// TestSearchResultUsable covers the per-result usability decision that drives the
// alternate connector pass's indexer scoping: a result is usable only when it
// would survive the match loop's release/size filtering. Junk hits (different
// show, or a relabel outside tolerance, or a size mismatch) are NOT usable, so
// their indexer stays eligible for the alternate-spelling re-query.
func TestSearchResultUsable(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

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
			name:       "title rescue does not stop safer re-query passes",
			sourceName: webdlDotted, candidateName: "Different.Title.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb",
			sourceSize: size, candidateSize: size, tolerance: 5,
			want: false,
		},
		{
			// findIndividualEpisodes makes a season-pack source match an individual
			// episode candidate, and ignoreSizeCheck then bypasses the size gate.
			// The candidate is 2x the source size (well outside tolerance) yet still
			// usable, which is only possible if the size bypass is honored.
			name:       "episode within season pack is usable despite size mismatch (size bypass)",
			sourceName: webdlDotted, candidateName: episodeDotted,
			sourceSize: size, candidateSize: size * 2, tolerance: 3,
			findIndividualEpisodes: true,
			want:                   true,
		},
		{
			// Reverse pairing: a season-pack candidate against a single-episode source
			// is a forbidden cross-seed even under findIndividualEpisodes. Sizes are
			// equal (within tolerance) and the show/season match, so the ONLY thing
			// that makes this not usable is the season-pack-from-episode rejection.
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
			candidate := rls.ParseString(tt.candidateName)
			got := s.searchResultUsable(
				namedRelease{release: &source, rawName: tt.sourceName},
				namedRelease{release: &candidate, rawName: tt.candidateName},
				tt.sourceSize, tt.candidateSize, nil, tt.tolerance, tt.findIndividualEpisodes,
			)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestIndexersWithoutUsableResults is the P1 regression: an indexer that returned
// only junk in the primary pass must still be re-queried with the alternate
// connector spelling. The old indexersWithoutResults treated any raw hit as
// "responded" and skipped such an indexer, suppressing connector-variant matches.
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
		{IndexerID: 1, Title: junkTitle, Size: size},  // junk only -> indexer 1 NOT usable
		{IndexerID: 2, Title: sourceName, Size: size}, // usable match -> indexer 2 responded
	}

	// Indexer 1 returned a raw hit (so the old logic omitted it), but it is junk;
	// indexer 3 returned nothing. Both must be re-queried; only indexer 2 is done.
	got := s.indexersWithoutUsableResults(
		[]int{1, 2, 3}, results,
		namedRelease{release: &source, rawName: sourceName},
		size, nil, 5, false,
	)
	require.Equal(t, []int{1, 3}, got)

	// Sanity: the raw helper would have skipped indexer 1 (the P1 bug).
	require.Equal(t, []int{3}, indexersWithoutResults([]int{1, 2, 3}, results))
}

func TestShouldRunTitleFallbackForRescueOnly(t *testing.T) {
	service := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	const (
		sourceName = "Original.Show.S01E01.1080p.WEB-DL.H.264-GROUP"
		rescueName = "Renamed.Show.S01E01.1080p.WEB-DL.H.264-GROUP"
		size       = int64(4_000_000_000)
	)
	source := rls.ParseString(sourceName)
	rescue := jackett.SearchResult{Title: rescueName, Size: size}
	normal := jackett.SearchResult{Title: sourceName, Size: size}
	junk := jackett.SearchResult{Title: "Other.Show.S02E02.720p.WEB-DL.H.264-OTHER", Size: size}

	sourceView := namedRelease{release: &source, rawName: sourceName}
	require.True(t, service.shouldRunTitleFallback(nil, sourceView, size, nil, 5, false, false))
	require.False(t, service.shouldRunTitleFallback([]jackett.SearchResult{rescue}, sourceView, size, nil, 5, false, false))
	require.True(t, service.shouldRunTitleFallback([]jackett.SearchResult{rescue}, sourceView, size, nil, 5, false, true))
	require.False(t, service.shouldRunTitleFallback([]jackett.SearchResult{junk}, sourceView, size, nil, 5, false, true))
	require.False(t, service.shouldRunTitleFallback([]jackett.SearchResult{rescue, normal}, sourceView, size, nil, 5, false, true))
}
