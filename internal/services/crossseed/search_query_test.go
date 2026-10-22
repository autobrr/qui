// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/releases"
)

func TestBuildTorznabQueryKeepsParsedSeasonAndEpisode(t *testing.T) {
	tests := []struct {
		name            string
		inputName       string
		release         rls.Release
		expectedQuery   string
		expectedSeason  *int
		expectedEpisode *int
	}{
		{
			name:            "AnimeAbsolute",
			inputName:       "[Fansub] Example Show - 1140 (1080p) [EEC80774]",
			release:         *releases.NewDefaultParser().Parse("[Fansub] Example Show - 1140 (1080p) [EEC80774]"),
			expectedQuery:   "Example Show",
			expectedEpisode: new(1140),
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
			expectedQuery:   "Some Show",
			expectedSeason:  new(1),
			expectedEpisode: new(2),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := BuildTorznabQuery(tc.inputName, &tc.release, false)

			require.Equal(t, tc.expectedQuery, q.Query)
			require.Equal(t, tc.expectedSeason, q.Season)
			require.Equal(t, tc.expectedEpisode, q.Episode)
		})
	}
}

func TestBuildTorznabQueryOmitsYearForMovies(t *testing.T) {
	// Trackers that search a movie database instead of release names
	// (PassThePopcorn) match q against the title alone, so a trailing year
	// returns nothing. The year travels as the separate year parameter.
	release := rls.Release{Type: rls.Movie, Title: "The Matrix", Year: 1999}

	got := BuildTorznabQuery("The.Matrix.1999.1080p.BluRay.x264-GROUP", &release, false)

	require.Equal(t, "The Matrix", got.Query)
	require.Nil(t, got.Season)
	require.Nil(t, got.Episode)
}

func TestBuildTorznabQueryUsesArtistForMusic(t *testing.T) {
	release := rls.Release{Type: rls.Music, Artist: "Some Artist", Title: "Some Album", Year: 2020}

	got := BuildTorznabQuery("Some.Artist-Some.Album-2020-FLAC", &release, true)

	require.Equal(t, "Some Artist Some Album", got.Query)
}

func TestBuildTorznabQueryFallsBackToName(t *testing.T) {
	release := rls.Release{Type: rls.Unknown}

	got := BuildTorznabQuery("  Untitled Thing  ", &release, false)

	require.Equal(t, "Untitled Thing", got.Query)
}

func TestBuildTorznabQueryKeepsSeasonNumeral(t *testing.T) {
	release := rls.Release{Type: rls.Series, Title: "Kaiju Squad 100 III", Series: 3}

	got := BuildTorznabQuery("[GRP] Kaiju Squad 100 III (BD 1080p HEVC FLAC) [Dual-Audio]", &release, false)

	require.Equal(t, "Kaiju Squad 100 III", got.Query)
	require.Equal(t, new(3), got.Season)
}
