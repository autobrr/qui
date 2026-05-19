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
		options         SearchQueryOptions
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
			name:      "KeepsParsedTitle",
			inputName: "Some.Show.S01E02.mkv",
			release: rls.Release{
				Type:       rls.Episode,
				Title:      "Some Show",
				Series:     1,
				Episode:    2,
				Resolution: "720p",
			},
			parsedTitle:     "Some Show",
			options:         SearchQueryOptions{IncludeResolution: true},
			expectedQuery:   "Some Show 720",
			expectedSeason:  intPtr(1),
			expectedEpisode: intPtr(2),
		},
		{
			name:      "DoesNotDuplicateResolution",
			inputName: "Some.Show.S01.720p.WEB-DL.mkv",
			release: rls.Release{
				Type:       rls.Series,
				Title:      "Some Show",
				Series:     1,
				Resolution: "720p",
			},
			parsedTitle:    "Some Show 720p",
			options:        SearchQueryOptions{IncludeResolution: true},
			expectedQuery:  "Some Show 720p",
			expectedSeason: intPtr(1),
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := buildSafeSearchQuery(tc.inputName, &tc.release, tc.parsedTitle, tc.options)

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
