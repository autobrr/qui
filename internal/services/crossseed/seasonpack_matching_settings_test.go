// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestSeasonPackMatchingOptions(t *testing.T) {
	tests := []struct {
		name            string
		settings        *models.CrossSeedAutomationSettings
		wantSkipRepack  bool
		wantSimplifyHDR bool
		wantSimplifyWEB bool
		wantSkipYear    bool
	}{
		{
			name:           "default options",
			wantSkipRepack: true,
		},
		{
			name: "uses configured options",
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackSkipRepackCompare:  false,
				SeasonPackSimplifyHDRCompare: true,
				SeasonPackSimplifyWEBCompare: true,
				SeasonPackSkipYearCompare:    true,
			},
			wantSimplifyHDR: true,
			wantSimplifyWEB: true,
			wantSkipYear:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := seasonPackMatchOptionsFromSettings(tt.settings)

			require.Equal(t, tt.wantSkipRepack, opts.skipRepackCompare)
			require.Equal(t, tt.wantSimplifyHDR, opts.simplifyHDRCompare)
			require.Equal(t, tt.wantSimplifyWEB, opts.simplifyWEBCompare)
			require.Equal(t, tt.wantSkipYear, opts.skipYearCompare)
		})
	}
}

func TestSeasonPackMatchingReleaseCompatibility(t *testing.T) {
	tests := []struct {
		name            string
		pack            string
		episode         string
		strict          bool
		settings        *models.CrossSeedAutomationSettings
		wantSeasonPack  bool
		checkGeneric    bool
		wantGeneric     bool
		checkSeasonPack bool
	}{
		{
			name:            "ignore repack differences",
			pack:            "Show.S01E01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp",
			episode:         "Show.S01E01.1080p.WEB-DL.REPACK.DDPA5.1.H.264-RlsGrp",
			wantSeasonPack:  true,
			checkGeneric:    true,
			checkSeasonPack: true,
		},
		{
			name:    "repack differences can be ignored for season packs",
			pack:    "Show.S01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp",
			episode: "Show.S01E01.1080p.WEB-DL.REPACK.DDPA5.1.H.264-RlsGrp",
			strict:  true,
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackSkipRepackCompare: true,
			},
			wantSeasonPack:  true,
			checkSeasonPack: true,
		},
		{
			name:    "repack differences can be enforced for season packs",
			pack:    "Show.S01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp",
			episode: "Show.S01E01.1080p.WEB-DL.REPACK.DDPA5.1.H.264-RlsGrp",
			strict:  true,
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackSkipRepackCompare: false,
			},
			checkSeasonPack: true,
		},
		{
			name:    "simplify HDR matching",
			pack:    "Show.S01E01.2160p.NF.WEB-DL.DDPA5.1.DV.HDR10+.H.265-RlsGrp",
			episode: "Show.S01E01.2160p.NF.WEB-DL.DDPA5.1.DV.HDR.H.265-RlsGrp",
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackSkipRepackCompare:  true,
				SeasonPackSimplifyHDRCompare: true,
			},
			wantSeasonPack:  true,
			checkGeneric:    true,
			checkSeasonPack: true,
		},
		{
			name:    "WEB simplification disabled",
			pack:    "Show.S01E01.1080p.WEB-DL.H.264-RlsGrp",
			episode: "Show.S01E01.1080p.WEB.H.264-RlsGrp",
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackSkipRepackCompare:  true,
				SeasonPackSimplifyWEBCompare: false,
			},
			checkSeasonPack: true,
		},
		{
			name:    "simplify WEB matching",
			pack:    "Show.S01E01.1080p.WEB-DL.H.264-RlsGrp",
			episode: "Show.S01E01.1080p.WEB.H.264-RlsGrp",
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackSkipRepackCompare:  true,
				SeasonPackSimplifyWEBCompare: true,
			},
			wantSeasonPack:  true,
			checkGeneric:    true,
			wantGeneric:     true,
			checkSeasonPack: true,
		},
		{
			name:    "allows missing source metadata",
			pack:    "Show.S01.1080p.H.264-RlsGrp",
			episode: "Show.S01E01.1080p.WEB-DL.H.264-RlsGrp",
			strict:  true,
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackSkipRepackCompare: true,
			},
			wantSeasonPack:  true,
			checkSeasonPack: true,
		},
		{
			name:    "ignore year differences",
			pack:    "Show.2024.S01.1080p.WEB.H.264-RlsGrp",
			episode: "Show.2025.S01E01.1080p.WEB.H.264-RlsGrp",
			strict:  true,
			settings: &models.CrossSeedAutomationSettings{
				SeasonPackSkipRepackCompare: true,
				SeasonPackSkipYearCompare:   true,
			},
			wantSeasonPack:  true,
			checkGeneric:    true,
			checkSeasonPack: true,
		},
		{
			name:         "generic matcher remains unchanged",
			pack:         "Show.S01E01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp",
			episode:      "Show.S01E01.1080p.WEB-DL.REPACK.DDPA5.1.H.264-RlsGrp",
			checkGeneric: true,
		},
	}

	matcher := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pack := parseSeasonPackTestRelease(t, tt.pack)
			episode := parseSeasonPackTestRelease(t, tt.episode)

			if tt.checkSeasonPack {
				match, _ := matcher.seasonPackReleasesMatchWithReason(pack, episode, tt.strict, tt.settings, nil)
				require.Equal(t, tt.wantSeasonPack, match)
			}
			if tt.checkGeneric {
				require.Equal(t, tt.wantGeneric, matcher.releasesMatch(pack, episode, tt.strict))
			}
		})
	}
}

func parseSeasonPackTestRelease(t *testing.T, name string) *rls.Release {
	t.Helper()

	release := rls.ParseString(name)
	require.NotEmpty(t, release.Title, "expected parser to extract a title from %q", name)

	return &release
}

// TestSeasonPackReleasesMatchWithReason_AliasTitles proves the matcher returns a
// field-level reject reason (observability, #1) and that a pack and an episode using
// different title languages/forms only match when the show's alternate titles are
// supplied on the pack side (alias bridge, #2). The no-alias case is the current 0%
// behaviour and keeps the fix load-bearing.
func TestSeasonPackReleasesMatchWithReason_AliasTitles(t *testing.T) {
	matcher := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	cases := []struct {
		name    string
		pack    string
		episode string
		aliases []string
	}{
		{
			name:    "romaji pack vs english episode",
			pack:    "Jidou Hanbaiki ni Umarekawatta Ore wa Meikyuu wo Samayou S03 1080p CR WEB-DL AAC2.0 H.264-SubsPlease",
			episode: "Reborn as a Vending Machine I Now Wander the Dungeon S03E01 1080p CR WEB-DL AAC2.0 H.264-SubsPlease",
			aliases: []string{
				"Reborn as a Vending Machine, I Now Wander the Dungeon",
				"Jidou Hanbaiki ni Umarekawatta Ore wa Meikyuu wo Samayou",
			},
		},
		{
			name:    "abbreviated pack title vs full episode title",
			pack:    "NIPPON SANGOKU S01 1080p AMZN WEB-DL DDP2.0 H.264-VARYG",
			episode: "NIPPON SANGOKU The Three Nations of the Crimson Sun S01E01 1080p AMZN WEB-DL DDP2.0 H.264-VARYG",
			aliases: []string{
				"NIPPON SANGOKU",
				"NIPPON SANGOKU: The Three Nations of the Crimson Sun",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pack := parseSeasonPackTestRelease(t, tc.pack)
			episode := parseSeasonPackTestRelease(t, tc.episode)

			ok, reason := matcher.seasonPackReleasesMatchWithReason(pack, episode, true, nil, nil)
			require.False(t, ok, "expected no match without aliases")
			require.Equal(t, "title mismatch", reason)

			ok, reason = matcher.seasonPackReleasesMatchWithReason(pack, episode, true, nil, tc.aliases)
			require.True(t, ok, "expected match with aliases, got reason %q", reason)
			require.Empty(t, reason)
		})
	}
}

// TestSeasonPackReleasesMatchWithReason_OriginalLanguageTag proves a pack that labels the
// original audio language still matches episodes published without a language tag. Trackers
// like Aither tag JAPANESE on anime that every other tracker leaves untagged, which used to
// reject the whole season as a language mismatch.
func TestSeasonPackReleasesMatchWithReason_OriginalLanguageTag(t *testing.T) {
	matcher := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	pack := parseSeasonPackTestRelease(t, "Reborn as a Vending Machine, I Now Wander the Dungeon S03 JAPANESE 1080p CR WEB-DL AAC 2.0 H.264-SubsPlease")
	episode := parseSeasonPackTestRelease(t, "[SubsPlease] Jidou Hanbaiki ni Umarekawatta Ore wa Meikyuu wo Samayou S3 - 02 (1080p) [7ECCC53C]")
	aliases := []string{
		"Reborn as a Vending Machine, I Now Wander the Dungeon",
		"Jidou Hanbaiki ni Umarekawatta Ore wa Meikyuu wo Samayou",
	}

	ok, reason := matcher.seasonPackReleasesMatchWithReason(pack, episode, true, nil, aliases)
	require.True(t, ok, "expected match, got reason %q", reason)
	require.Empty(t, reason)
}
