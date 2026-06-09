// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"
)

func TestBuildSafeSearchQuery(t *testing.T) {
	tests := []struct {
		name            string
		inputName       string
		release         rls.Release
		parsedTitle     string
		expectedQuery   string
		expectedSeason  *int
		expectedEpisode *int
	}{
		{
			name:            "AnimeAbsolute",
			inputName:       "[Fansub] Example Show - 1140 (1080p) [EEC80774]",
			release:         rls.Release{Type: rls.Unknown},
			expectedQuery:   "example show 1140",
			expectedEpisode: intPtr(1140),
		},
		{
			// Resolution must never be appended to the query: indexers whose free-text
			// search only matches the series name (e.g. BTN, IPT) return zero results
			// when a bare resolution token is present. Resolution is enforced post-search
			// in releasesMatch instead.
			name:      "KeepsParsedTitleWithoutResolution",
			inputName: "Some.Show.S01E02.mkv",
			release: rls.Release{
				Type:       rls.Episode,
				Title:      "Some Show",
				Series:     1,
				Episode:    2,
				Resolution: "720p",
			},
			parsedTitle:     "Some Show",
			expectedQuery:   "Some Show",
			expectedSeason:  intPtr(1),
			expectedEpisode: intPtr(2),
		},
		{
			name:      "MovieFallback",
			inputName: "Some.Movie.2024.1080p.WEBRip.x264",
			release: rls.Release{
				Type:       rls.Movie,
				Resolution: "1080p",
			},
			expectedQuery: "some movie 2024",
		},
		{
			// Non-movie path with no parsed title where cleanAnimeTitle strips
			// everything: fall back to the original name so the query is never empty.
			name:          "FallsBackToNameWhenCleanedEmpty",
			inputName:     "[Group][1080p]",
			release:       rls.Release{Type: rls.Unknown},
			expectedQuery: "[Group][1080p]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := buildSafeSearchQuery(tc.inputName, &tc.release, tc.parsedTitle)

			require.Equal(t, tc.expectedQuery, q.Query)
			require.Equal(t, tc.expectedSeason, q.Season)
			require.Equal(t, tc.expectedEpisode, q.Episode)
		})
	}
}

func TestParseEpisodeNumber(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{input: "1080", want: 0},
		{input: "2025", want: 0},
		{input: "999", want: 999},
		{input: "5000", want: 5000},
		{input: "5001", want: 0},
		{input: "720", want: 0},
		{input: "2160", want: 0},
		{input: "4320", want: 0},
		{input: "1899", want: 1899},
		{input: "1900", want: 0},
		{input: "2100", want: 0},
		{input: "2101", want: 2101},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			require.Equal(t, tc.want, parseEpisodeNumber(tc.input))
		})
	}
}

func intPtr(v int) *int {
	value := v
	return &value
}
