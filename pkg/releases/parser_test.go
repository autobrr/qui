// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"testing"
	"unicode/utf8"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"
)

func TestParser_EnrichesHDRAliases(t *testing.T) {
	t.Parallel()

	parser := NewDefaultParser()

	tests := []struct {
		name    string
		input   string
		wantHDR []string
		notHDR  []string
	}{
		{
			name:    "discussion title keeps HDR10 plus",
			input:   "End of Watch 2012 Hybrid 2160p UHD BluRay REMUX DV HDR10+ HEVC DTS-HD MA 5.1-FraMeSToR",
			wantHDR: []string{"DV", "HDR10+"},
		},
		{
			name:    "filename alias HDR10P normalizes to HDR10 plus",
			input:   "End.of.Watch.2012.UHD.BluRay.2160p.DTS-HD.MA.5.1.DV.HDR10P.HEVC.HYBRID.REMUX-FraMeSToR.mkv",
			wantHDR: []string{"DV", "HDR10+"},
			notHDR:  []string{"HDR10"},
		},
		{
			name:    "spaced HDR10 PLUS normalizes to HDR10 plus",
			input:   "Movie.2024.2160p.BluRay.x265.DV.HDR10 PLUS-GROUP",
			wantHDR: []string{"DV", "HDR10+"},
			notHDR:  []string{"HDR10"},
		},
		{
			name:    "dotted HDR10 plus drops inherited HDR10",
			input:   "Movie.2024.2160p.BluRay.x265.DV.HDR10+-GROUP",
			wantHDR: []string{"DV", "HDR10+"},
			notHDR:  []string{"HDR10"},
		},
		{
			name:    "underscored HDR10 PLUS normalizes to HDR10 plus",
			input:   "Movie.2024.2160p.BluRay.x265.DV.HDR10_PLUS-GROUP",
			wantHDR: []string{"DV", "HDR10+"},
			notHDR:  []string{"HDR10"},
		},
		{
			name:    "DV only stays DV only",
			input:   "Movie.2024.2160p.UHD.BluRay.REMUX.DV.HEVC-GROUP",
			wantHDR: []string{"DV"},
			notHDR:  []string{"HDR", "HDR10", "HDR10+", "HLG"},
		},
		{
			name:    "scene group DV does not become HDR",
			input:   "Software.Name.v1.0-DV",
			wantHDR: nil,
			notHDR:  []string{"DV", "HDR", "HDR10", "HDR10+", "HLG"},
		},
		{
			name:    "movie trailing DV group does not become HDR",
			input:   "Movie.2024.2160p.BluRay.x265-GROUP-DV",
			wantHDR: nil,
			notHDR:  []string{"DV", "HDR", "HDR10", "HDR10+", "HLG"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			release := parser.Parse(tt.input)
			if len(tt.wantHDR) == 0 {
				require.Nil(t, release.HDR)
			}
			require.ElementsMatch(t, tt.wantHDR, release.HDR)
			for _, tag := range tt.notHDR {
				require.NotContains(t, release.HDR, tag)
			}
		})
	}
}

func TestParser_HandlesInvalidUTF8WithoutPanicking(t *testing.T) {
	t.Parallel()

	// "á" encoded as Latin-1 (0xe1) rather than UTF-8 (0xc3 0xa1) is invalid UTF-8.
	//
	// Position matters: a bad byte landing in the parsed group/site token reaches
	// trailingTokenRegexes, where regexp.MustCompile rejects invalid-UTF-8 patterns and
	// panics. A bad byte in the title is only carried through. Cover both.
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "invalid byte in group",
			input: "Movie.2024.1080p.BluRay.x264-G\xe1P",
		},
		{
			name:  "invalid byte in group with extension",
			input: "Amelie.2001.1080p.BluRay.x264-G\xe1P.mkv",
		},
		{
			name:  "invalid byte in title",
			input: "Movie.\xe1.2024.2160p.BluRay.x265-GROUP.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			release := NewDefaultParser().Parse(tt.input)

			require.True(t, utf8.ValidString(release.Title), "title should be valid UTF-8")
			require.True(t, utf8.ValidString(release.Group), "group should be valid UTF-8")
		})
	}
}

func TestTrimTrailingGroupOrSite_RemovesExposedTrailingTokens(t *testing.T) {
	t.Parallel()

	release := &rls.Release{
		Group: "DV",
		Site:  "SITE",
	}

	trimmed := trimTrailingGroupOrSite("Movie.2024.2160p.BluRay.x265-DV [SITE]", release)
	require.Equal(t, "Movie.2024.2160p.BluRay.x265", trimmed)
}

func TestTrimTrailingGroupOrSite_RemovesTrailingTokenBeforeExtension(t *testing.T) {
	t.Parallel()

	release := &rls.Release{
		Group: "DV",
	}

	trimmed := trimTrailingGroupOrSite("Movie.2024.2160p.BluRay.x265-DV.mkv", release)
	require.Equal(t, "Movie.2024.2160p.BluRay.x265", trimmed)
}

func TestTrimTrailingGroupOrSite_RemovesTrailingTokenWithoutExtension(t *testing.T) {
	t.Parallel()

	release := &rls.Release{
		Group: "DV",
	}

	trimmed := trimTrailingGroupOrSite("Movie.2024.2160p.BluRay.x265-DV", release)
	require.Equal(t, "Movie.2024.2160p.BluRay.x265", trimmed)
}

// rls reports "S00E02-E05" as episode 2 and drops the range.
func TestParser_EpisodeRangeBecomesPack(t *testing.T) {
	t.Parallel()

	parser := NewDefaultParser()

	tests := []struct {
		name        string
		input       string
		wantEpisode int
	}{
		{
			name:        "season zero range",
			input:       "Darker than Black AKA DARKER THAN BLACK: Kuro no Keiyakusha S00E02-E05 720p BluRay Dual-Audio Opus 2.0 x264-Headpatter",
			wantEpisode: 0,
		},
		{
			name:        "concatenated range",
			input:       "The.X-Files.S01E01E03.DKsubs.1080p.BluRay.HEVC.x265",
			wantEpisode: 0,
		},
		{
			name:        "a path repeating one episode is not a range",
			input:       "Show.Name.S11E11.1080p.WEB-GRP/Show.Name.S11E11.1080p.WEB-GRP.mkv",
			wantEpisode: 11,
		},
		{
			name:        "trailing resolution is not a range",
			input:       "Show.Name.S01E01-1080p.WEB-DL-GRP",
			wantEpisode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			release := parser.Parse(tt.input)
			require.Equal(t, tt.wantEpisode, release.Episode)
			if tt.wantEpisode == 0 {
				require.Equal(t, rls.Series, release.Type, "a range must read as a pack, not an episode")
			}
		})
	}
}

func TestIsEpisodeRange(t *testing.T) {
	t.Parallel()

	parser := NewDefaultParser()
	require.True(t, IsEpisodeRange(parser.Parse("Show.Name.S01E05E06.1080p.WEB-GRP")))
	require.True(t, IsEpisodeRange(parser.Parse("Darker than Black S00E02-E05 720p BluRay x264-Headpatter")))
	require.False(t, IsEpisodeRange(parser.Parse("Show.Name.S01E05.1080p.WEB-GRP")))
	require.False(t, IsEpisodeRange(parser.Parse("Show.Name.S01.1080p.WEB-GRP")))
	// A path naming the same episode twice is one episode, not a range.
	require.False(t, IsEpisodeRange(parser.Parse("Show.Name.S11E11.1080p.WEB-GRP/Show.Name.S11E11.1080p.WEB-GRP.mkv")))
	require.False(t, IsEpisodeRange(nil))
}
