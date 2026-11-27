// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"testing"
	"unicode/utf8"

	"github.com/autobrr/rls"
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

func TestParser_AudioDescriptionIsOtherTag(t *testing.T) {
	t.Parallel()

	release := NewDefaultParser().Parse("Show.Name.S01E03.with.Audio.Description.1080p.WEB-DL.DDP5.1.H.264-GRP")
	require.Equal(t, []string{"Audio.Description"}, release.Other)
	require.Empty(t, release.Subtitle)
	require.Equal(t, "Show Name", release.Title)
}

func TestIsEpisodeRange(t *testing.T) {
	t.Parallel()

	parser := NewDefaultParser()
	require.True(t, IsEpisodeRange(parser.Parse("Show.Name.S01E05E06.1080p.WEB-GRP")))
	require.True(t, IsEpisodeRange(parser.Parse("Darker than Black S00E02-E05 720p BluRay x264-Headpatter")))
	require.False(t, IsEpisodeRange(parser.Parse("Show.Name.S01E05.1080p.WEB-GRP")))
	require.False(t, IsEpisodeRange(parser.Parse("Show.Name.S01.1080p.WEB-GRP")))
	require.False(t, IsEpisodeRange(parser.Parse("Show.Name.S11E11.1080p.WEB-GRP/Show.Name.S11E11.1080p.WEB-GRP.mkv")))
	require.False(t, IsEpisodeRange(nil))
	release := parser.Parse("Sea.Voyage.S23E19.Episode.1174.1080p.CR.WEB-DL.DDP2.0.H.264-Grp.mkv")
	require.False(t, IsEpisodeRange(release))
	require.Equal(t, 19, release.Episode)
	require.Equal(t, rls.Episode, release.Type)
	require.True(t, IsEpisodeRange(parser.Parse("Show.Name.S01E05.E06.1080p.WEB-GRP")))
	require.True(t, IsEpisodeRange(parser.Parse("Show.Name.1x05.1x06.1080p.WEB-GRP")))
	require.True(t, IsEpisodeRange(parser.Parse("Show.Name.S01E01E02-GRP/Show.Name.Episode.1-2.mkv")))
	require.True(t, IsEpisodeRange(parser.Parse("[Grp] Show Name - 01 - 12 [1080p]")))
}

func TestParser_ReadsWebSourceAfterUHDAndAnotherTag(t *testing.T) {
	t.Parallel()

	parser := NewDefaultParser()
	for name, want := range map[string]string{
		"Harbor.Lights.2022.2160p.UHD.AMZN.WEB-DL.DDP5.1.x265-GRP": "UHD.WEB-DL",
		"Harbor.Lights.2022.2160p.UHD.NF.WEBRip.DDP5.1.x265-GRP":   "UHD.WEBRiP",
		"Harbor.Lights.2022.2160p.UHD.DDP5.1.x265-GRP":             "UHD.BluRay",
		"Harbor.Lights.2022.2160p.UHD.BluRay.DDP5.1.x265-GRP":      "UHD.BluRay",
	} {
		require.Equal(t, want, parser.Parse(name).Source, name)
	}
}

func TestParser_GivesTheReleaseOfParseString(t *testing.T) {
	t.Parallel()

	parser := NewDefaultParser()
	for _, name := range []string{
		"Harbor.Watch.S01.UHD.HDTV.x265-GRP",
		"Quiet Tide - Harbor Sounds [FLAC 16bit 44.1kHz]",
	} {
		want := rls.ParseString(name)
		got := parser.Parse(name)
		require.Equal(t, want.Title, got.Title, name)
		require.Equal(t, want.Resolution, got.Resolution, name)
		require.Equal(t, want.Audio, got.Audio, name)
		require.Equal(t, want.Language, got.Language, name)
	}
}

func TestParser_KeepsATagListOfTheExactSize(t *testing.T) {
	t.Parallel()

	release := NewDefaultParser().Parse("Harbor.Watch.S01E02.The.Quiet.Tide.2160p.WEB-DL.DDP5.1.DV.HDR.H.265-GRP")
	tags := release.Tags()
	require.NotEmpty(t, tags)
	require.LessOrEqual(t, cap(tags), len(tags)+len(tags)/8+1)
}

func TestParser_ReadsAKGListingAsItsDownloadName(t *testing.T) {
	t.Parallel()

	parser := NewDefaultParser()
	for _, tc := range []struct {
		name      string
		wantTitle string
		wantYear  int
		wantGroup string
	}{
		{name: "Quiet Harbour AKA Stiller Hafen 1987 France [Quiet.Harbour.1987.DVDRip.x264-GRP.mkv]", wantTitle: "Quiet Harbour", wantYear: 1987, wantGroup: "GRP"},
		{name: "Still Waters 1957 Czech Republic [Still.Waters.1957]", wantTitle: "Still Waters", wantYear: 1957},
		{name: "Kein - Ein Wienerfilm 1997 Austria [Kein.Ein.Wienerfilm]", wantTitle: "Kein Ein Wienerfilm", wantYear: 1997},
		{name: "Still Waters 1957  [Still.Waters]", wantTitle: "Still Waters", wantYear: 1957},
		{name: "(1000) Nights of Rain 2009 United States [1000.Nights.of.Rain.2009.1080p.BluRay.x264-GRP.mkv]", wantTitle: "1000 Nights of Rain", wantYear: 2009, wantGroup: "GRP"},
	} {
		got := parser.Parse(tc.name)
		require.Equal(t, tc.wantTitle, got.Title, tc.name)
		require.Equal(t, tc.wantYear, got.Year, tc.name)
		require.Equal(t, tc.wantGroup, got.Group, tc.name)
	}
}

func TestParser_KeepsABracketTagThatIsNoKGDownloadName(t *testing.T) {
	t.Parallel()

	parser := NewDefaultParser()
	for _, name := range []string{
		"Quiet Tide - Harbor Sounds 2011 [Remaster Vol.2]",
		"Quiet Tide - Harbor Sounds 2011 [Deluxe]",
		"Harbor Lights 2019 [H.264]",
		"Harbor Lights 2019 WEB-DL [DD5.1]",
		"Harbor Lights 2019 [Harbor.Subs]",
		"Harbor Lights 2019 [Quiet.Harbor]",
		"Harbor Lights 2019 (Dual Audio) [Dual.Audio]",
		"[GRP] Kaiju Squad - 05 [1080p][Multi-Subs]",
		"Harbor Lights [Harbor.Lights.2019.1080p.BluRay.x264-GRP]",
		"Harbor Lights 2019 [.]",
		"[GRP] Kaiju Squad - 05 (1080p) [Batch.Fix]",
		"Harbor.Toolkit-GRP (2019) [Windows / Multi-Language / Full ISO]",
	} {
		want := rls.ParseString(name)
		got := parser.Parse(name)
		require.Equal(t, want.Title, got.Title, name)
		require.Equal(t, want.Year, got.Year, name)
		require.Equal(t, want.Group, got.Group, name)
	}
}
