// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestReleasesMatch_NonTVRequiresExactTitle(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	base := rls.Release{
		Title: "Test Movie",
		Year:  2025,
	}

	same := rls.Release{
		Title: "Test Movie",
		Year:  2025,
	}

	variantTitle := rls.Release{
		Title: "Test Movie Extended",
		Year:  2025,
	}

	require.True(t, s.releasesMatch(&base, &same, false), "identical non-TV titles should match")
	require.False(t, s.releasesMatch(&base, &variantTitle, false), "non-TV titles must match exactly after normalization")
}

func TestReleasesMatch_NonTVRequiresCompatibleType(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	movie := rls.Release{
		Type:  rls.Movie,
		Title: "Shared Title",
		Year:  2025,
	}

	music := rls.Release{
		Type:  rls.Music,
		Title: "Shared Title",
		Year:  2025,
	}

	unknown := rls.Release{
		Type:  rls.Unknown,
		Title: "Shared Title",
		Year:  2025,
	}

	require.False(t, s.releasesMatch(&movie, &music, false), "movie and music with same title/year should not match")
	require.True(t, s.releasesMatch(&movie, &unknown, false), "unknown type should not block matching when other metadata agrees")
	require.True(t, s.releasesMatch(&unknown, &music, false), "unknown type should not block matching when other metadata agrees")
}

func TestReleasesMatch_ArtistMustMatch(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	// Different artists with same title should NOT match (regression test for 0day scene)
	adamBeyer := rls.Release{
		Type:   rls.Music,
		Artist: "Adam Beyer",
		Title:  "Dance Department",
		Year:   2025,
		Month:  10,
		Day:    4,
		Source: "CABLE",
		Group:  "TALiON",
	}

	arminVanBuuren := rls.Release{
		Type:   rls.Music,
		Artist: "Armin van Buuren",
		Title:  "Dance Department",
		Year:   2025,
		Month:  11,
		Day:    29,
		Source: "CABLE",
		Group:  "TALiON",
	}

	sameArtist := rls.Release{
		Type:   rls.Music,
		Artist: "Adam Beyer",
		Title:  "Dance Department",
		Year:   2025,
		Month:  10,
		Day:    4,
		Source: "CABLE",
		Group:  "TALiON",
	}

	require.False(t, s.releasesMatch(&adamBeyer, &arminVanBuuren, false),
		"different artists with same title should NOT match")
	require.True(t, s.releasesMatch(&adamBeyer, &sameArtist, false),
		"same artist with same title should match")
}

func TestReleasesMatch_DateBasedReleasesRequireExactDate(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	// Same year but different month/day should NOT match (0day scene releases)
	oct4 := rls.Release{
		Type:   rls.Music,
		Artist: "Artist",
		Title:  "Show",
		Year:   2025,
		Month:  10,
		Day:    4,
		Group:  "GROUP",
	}

	nov29 := rls.Release{
		Type:   rls.Music,
		Artist: "Artist",
		Title:  "Show",
		Year:   2025,
		Month:  11,
		Day:    29,
		Group:  "GROUP",
	}

	sameDate := rls.Release{
		Type:   rls.Music,
		Artist: "Artist",
		Title:  "Show",
		Year:   2025,
		Month:  10,
		Day:    4,
		Group:  "GROUP",
	}

	// Release with only year (no month/day) - e.g. albums
	yearOnly := rls.Release{
		Type:   rls.Music,
		Artist: "Artist",
		Title:  "Album",
		Year:   2025,
		Group:  "GROUP",
	}

	require.False(t, s.releasesMatch(&oct4, &nov29, false),
		"same year but different month/day should NOT match")
	require.True(t, s.releasesMatch(&oct4, &sameDate, false),
		"same year/month/day should match")

	// Year-only releases should still be allowed to match each other
	anotherYearOnly := rls.Release{
		Type:   rls.Music,
		Artist: "Artist",
		Title:  "Album",
		Year:   2025,
		Group:  "GROUP",
	}
	require.True(t, s.releasesMatch(&yearOnly, &anotherYearOnly, false),
		"year-only releases should match when year is same")
}

func TestNormalizeForMatching(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"lowercase", "MIKE'S TACOS", "mikes tacos"},
		{"apostrophe", "Mike's Tacos", "mikes tacos"},
		{"curly apostrophe right", "Don't Stop", "dont stop"},
		{"curly apostrophe left", "It's Fine", "its fine"},
		{"backtick", "Rock`n Roll", "rockn roll"},
		{"colon", "City: Downtown", "city downtown"},
		{"comma", "Signal, Bloom", "signal bloom"},
		{"hyphen to space", "Laser-Cat", "laser cat"},
		{"multiple hyphens", "Up-And-Away", "up and away"},
		{"anime star separator", "Classic★Stars", "classic stars"},
		{"anime middle dot separator", "Kaguya・Sama", "kaguya sama"},
		{"mixed punctuation", "Jake's Place: Season 1", "jakes place season 1"},
		{"extra spaces collapsed", "The   Show", "the show"},
		{"trim whitespace", "  Trimmed  ", "trimmed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringutils.NormalizeForMatching(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestReleasesMatch_PunctuationVariations(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	tests := []struct {
		name        string
		source      rls.Release
		candidate   rls.Release
		wantMatch   bool
		description string
	}{
		{
			name: "apostrophe vs no apostrophe TV",
			source: rls.Release{
				Title:  "Mike's Tacos",
				Series: 1,
				Source: "WEB-DL",
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Mikes Tacos",
				Series: 1,
				Source: "WEB-DL",
				Group:  "GROUP",
			},
			wantMatch:   true,
			description: "apostrophe should be stripped - 'Mike's' matches 'Mikes'",
		},
		{
			name: "colon vs no colon TV",
			source: rls.Release{
				Title:  "City: Downtown",
				Series: 1,
				Source: "WEB-DL",
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "City Downtown",
				Series: 1,
				Source: "WEB-DL",
				Group:  "GROUP",
			},
			wantMatch:   true,
			description: "colon should be stripped - 'City: Downtown' matches 'City Downtown'",
		},
		{
			name: "hyphen vs space TV",
			source: rls.Release{
				Title:  "Laser-Cat",
				Series: 1,
				Source: "WEB-DL",
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Laser Cat",
				Series: 1,
				Source: "WEB-DL",
				Group:  "GROUP",
			},
			wantMatch:   true,
			description: "hyphen should become space - 'Laser-Cat' matches 'Laser Cat'",
		},
		{
			name: "comma vs no comma TV",
			source: rls.Release{
				Title:  "Signal Bloom",
				Series: 1,
				Source: "WEB-DL",
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Signal, Bloom",
				Series: 1,
				Source: "WEB-DL",
				Group:  "GROUP",
			},
			wantMatch:   true,
			description: "comma should be stripped - 'Signal, Bloom' matches 'Signal Bloom'",
		},
		{
			name: "unicode curly apostrophe TV",
			source: rls.Release{
				Title:  "Can't Stop Now",
				Series: 1,
				Source: "WEB-DL",
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Cant Stop Now",
				Series: 1,
				Source: "WEB-DL",
				Group:  "GROUP",
			},
			wantMatch:   true,
			description: "curly apostrophe should be stripped",
		},
		{
			name: "anime symbol separator TV",
			source: rls.Release{
				Title:      "Classic Stars",
				Series:     1,
				Resolution: "1080p",
				Source:     "WEB-DL",
			},
			candidate: rls.Release{
				Title:      "Classic★Stars",
				Series:     1,
				Resolution: "1080p",
				Source:     "WEB-DL",
			},
			wantMatch:   true,
			description: "decorative anime symbols should separate title words",
		},
		{
			name: "apostrophe vs no apostrophe non-TV movie",
			source: rls.Release{
				Title: "Jake's Adventure",
				Year:  2024,
				Type:  rls.Movie,
				Group: "GROUP",
			},
			candidate: rls.Release{
				Title: "Jakes Adventure",
				Year:  2024,
				Type:  rls.Movie,
				Group: "GROUP",
			},
			wantMatch:   true,
			description: "non-TV should also match with punctuation normalization",
		},
		{
			name: "completely different titles should not match",
			source: rls.Release{
				Title:  "Space Adventures",
				Series: 1,
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Ocean Mysteries",
				Series: 1,
				Group:  "GROUP",
			},
			wantMatch:   false,
			description: "different titles should still not match",
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

func TestReleasesMatch_TVTitleMustNotUseSubstringMatching(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	// Regression: different shows that share a common prefix should not match.
	// Example: "FBI" vs "FBI Most Wanted".
	fbi := rls.ParseString("FBI.S03.720p.AMZN.WEB-DL.DDP5.1.H.264-NTb")
	fbiMostWanted := rls.ParseString("FBI.Most.Wanted.S03.720p.AMZN.WEB-DL.DDP5.1.H.264-NTb")
	require.False(t, s.releasesMatch(&fbi, &fbiMostWanted, false))
	require.False(t, s.releasesMatch(&fbiMostWanted, &fbi, false), "match should be symmetric")

	// Another common case: main series vs spinoff where the spinoff embeds the parent title.
	sao := rls.ParseString("Sword.Art.Online.S01.1080p.BluRay.REMUX.AVC.Dual-Audio.FLAC.2.0-NAN0")
	ggo := rls.ParseString("Sword.Art.Online.Alternative.Gun.Gale.Online.S01.1080p.BluRay.REMUX.AVC.Dual-Audio.FLAC.2.0-NAN0")
	require.False(t, s.releasesMatch(&sao, &ggo, false))
	require.False(t, s.releasesMatch(&ggo, &sao, false), "match should be symmetric")
}

func TestReleasesMatch_AKAVariants(t *testing.T) {
	tests := []struct {
		name           string
		source         rls.Release
		candidate      rls.Release
		candidateName  string
		expectedMatch  bool
		expectedReason string
	}{
		{
			name: "parsed title alternatives match exactly",
			source: rls.Release{
				Type:   rls.Series,
				Title:  "Kuro Gear no Meiro Tansaku",
				Series: 2,
				Source: "WEB-DL",
			},
			candidate: rls.Release{
				Type:   rls.Series,
				Title:  "Black Gear, Maze Patrol",
				Alt:    "Kuro Gear no Meiro Tansaku",
				Series: 2,
				Source: "WEB-DL",
			},
			expectedMatch: true,
		},
		{
			name: "raw AKA fallback matches exact normalized title",
			source: rls.Release{
				Type:   rls.Series,
				Title:  "Aoi Clockwork no Vault Run",
				Series: 2,
				Source: "WEB-DL",
			},
			candidate: rls.Release{
				Type:   rls.Series,
				Title:  "Blue Clockwork Vault Run",
				Series: 2,
				Source: "WEB-DL",
			},
			candidateName: "Blue Clockwork Vault Run AKA Aoi Clockwork no Vault Run S02 720p WEB-DL-GRP",
			expectedMatch: true,
		},
		{
			name: "AKA matching does not use substrings",
			source: rls.Release{
				Type:   rls.Series,
				Title:  "Kuro Gear",
				Series: 1,
			},
			candidate: rls.Release{
				Type:   rls.Series,
				Title:  "Kuro Gear Outer Ring",
				Alt:    "Kuro Gear Outer Ring",
				Series: 1,
			},
			expectedMatch:  false,
			expectedReason: "title mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

			var match bool
			var reason string
			if tt.candidateName == "" {
				match, reason = s.releasesMatchWithReason(&tt.source, &tt.candidate, false)
			} else {
				match, reason = s.releasesMatchWithReasonAndNames(&tt.source, &tt.candidate, "", tt.candidateName, false)
			}

			if tt.expectedMatch {
				require.True(t, match, "got reason %q", reason)
			} else {
				require.False(t, match, "got reason %q", reason)
			}
			require.Equal(t, tt.expectedReason, reason)
		})
	}
}

// rls ends a title at a slash, so "Fate/strange Fake" parsed as only "Fate" and
// an exact-size cross-seed of the same release was rejected as a title mismatch.
func TestReleasesMatch_SlashInTitleReadsAsSeparator(t *testing.T) {
	tests := []struct {
		name       string
		sourceName string
		candidate  string
		wantMatch  bool
	}{
		{
			name:       "forward slash in candidate title",
			sourceName: "Fate.strange.Fake.S01.1080p.BluRay.Dual-Audio.Opus2.0.x265-Headpatter",
			candidate:  "Fate/strange Fake S01 1080p BluRay Dual-Audio Opus 2.0 x265-Headpatter",
			wantMatch:  true,
		},
		{
			name:       "back slash in candidate title",
			sourceName: "Fate.strange.Fake.S01.1080p.BluRay.Dual-Audio.Opus2.0.x265-Headpatter",
			candidate:  `Fate\strange Fake S01 1080p BluRay Dual-Audio Opus 2.0 x265-Headpatter`,
			wantMatch:  true,
		},
		{
			name:       "slash does not move the artist into the title",
			sourceName: "Whitesnake-Back.In.Black-1980-FLAC",
			candidate:  "AC/DC - Back In Black 1980 FLAC",
			wantMatch:  false,
		},
		{
			name:       "slash does not join unrelated shows",
			sourceName: "Fate.strange.Fake.S01.1080p.BluRay.Dual-Audio.Opus2.0.x265-Headpatter",
			candidate:  "Fate/Zero S01 1080p BluRay Dual-Audio Opus 2.0 x265-Headpatter",
			wantMatch:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer(), releaseCache: NewReleaseCache()}
			source := rls.ParseString(tt.sourceName)
			candidate := rls.ParseString(tt.candidate)

			match, reason := s.releasesMatchWithReasonAndNames(&source, &candidate, tt.sourceName, tt.candidate, false)

			if tt.wantMatch {
				require.True(t, match, "got reason %q", reason)
				return
			}
			require.False(t, match)
			require.Equal(t, "title mismatch", reason)
		})
	}
}

func TestReleasesMatch_ARRTitleAliasesOnlyWidenTitleCheck(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	source := rls.Release{
		Type:       rls.Episode,
		Title:      "Haibara kun no Tsuyokute Seishun New Game",
		Series:     1,
		Episode:    1,
		Site:       "SubsPlease",
		Sum:        "1D28F62C",
		Resolution: "720p",
	}
	candidate := rls.Release{
		Type:       rls.Episode,
		Title:      "Haibara's Teenage New Game+",
		Series:     1,
		Episode:    1,
		Site:       "SubsPlease",
		Sum:        "1D28F62C",
		Resolution: "720p",
	}

	match, reason := s.releasesMatchWithReasonAndNames(&source, &candidate, "", "", false)
	require.False(t, match)
	require.Equal(t, "title mismatch", reason)

	aliases := []string{"Haibara's Teenage New Game+"}
	match, reason = s.releasesMatchWithReasonAndNamesAndTitles(&source, &candidate, "", "", aliases, nil, false)
	require.True(t, match, "got reason %q", reason)

	checksumMismatch := candidate
	checksumMismatch.Sum = "52D759EF"
	match, reason = s.releasesMatchWithReasonAndNamesAndTitles(&source, &checksumMismatch, "", "", aliases, nil, false)
	require.False(t, match)
	require.Equal(t, "checksum mismatch", reason)

	episodeMismatch := candidate
	episodeMismatch.Episode = 2
	match, reason = s.releasesMatchWithReasonAndNamesAndTitles(&source, &episodeMismatch, "", "", aliases, nil, false)
	require.False(t, match)
	require.Equal(t, "episode mismatch", reason)
}

func TestRawAKATitleParts(t *testing.T) {
	tests := []struct {
		name     string
		rawName  string
		expected []string
	}{
		{
			name:     "valid AKA titles",
			rawName:  "Blue Clockwork Vault Run AKA Aoi Clockwork no Vault Run S02 720p WEB-DL-GRP",
			expected: []string{"Blue Clockwork Vault Run", "Aoi Clockwork no Vault Run S02 720p WEB-DL-GRP"},
		},
		{
			name:     "filters short noisy fragments",
			rawName:  "Blue Clockwork Vault Run AKA S01 AKA Aoi Clockwork no Vault Run",
			expected: []string{"Blue Clockwork Vault Run", "Aoi Clockwork no Vault Run"},
		},
		{
			name:    "requires at least two valid fragments",
			rawName: "AKA Aoi",
		},
		{
			name:    "ignores names without AKA delimiter",
			rawName: "Aoi Clockwork no Vault Run",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, rawAKATitleParts(tt.rawName))
		})
	}
}
