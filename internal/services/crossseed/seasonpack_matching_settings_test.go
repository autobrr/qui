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
				require.Equal(t, tt.wantSeasonPack, matcher.seasonPackReleasesMatch(pack, episode, tt.strict, tt.settings))
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
