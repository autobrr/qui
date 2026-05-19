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

func TestSeasonPackMatching_DefaultOptions(t *testing.T) {
	opts := seasonPackMatchOptionsFromSettings(nil)

	require.True(t, opts.skipRepackCompare)
	require.False(t, opts.simplifyHDRCompare)
	require.False(t, opts.simplifyWEBCompare)
	require.False(t, opts.skipYearCompare)
}

func TestSeasonPackMatching_UsesConfiguredOptions(t *testing.T) {
	opts := seasonPackMatchOptionsFromSettings(&models.CrossSeedAutomationSettings{
		SeasonPackSkipRepackCompare:  false,
		SeasonPackSimplifyHDRCompare: true,
		SeasonPackSimplifyWEBCompare: true,
		SeasonPackSkipYearCompare:    true,
	})

	require.False(t, opts.skipRepackCompare)
	require.True(t, opts.simplifyHDRCompare)
	require.True(t, opts.simplifyWEBCompare)
	require.True(t, opts.skipYearCompare)
}

func TestSeasonPackMatching_IgnoreRepackDifferences(t *testing.T) {
	matcher := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	pack := parseSeasonPackTestRelease(t, "Show.S01E01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp")
	episode := parseSeasonPackTestRelease(t, "Show.S01E01.1080p.WEB-DL.REPACK.DDPA5.1.H.264-RlsGrp")

	require.True(t, matcher.seasonPackReleasesMatch(pack, episode, false, nil))
	require.False(t, matcher.releasesMatch(pack, episode, false))
}

func TestSeasonPackMatching_RepackDifferencesCanBeDisabledForSeasonPacks(t *testing.T) {
	matcher := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	pack := parseSeasonPackTestRelease(t, "Show.S01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp")
	episode := parseSeasonPackTestRelease(t, "Show.S01E01.1080p.WEB-DL.REPACK.DDPA5.1.H.264-RlsGrp")

	require.True(t, matcher.seasonPackReleasesMatch(pack, episode, true, &models.CrossSeedAutomationSettings{
		SeasonPackSkipRepackCompare: true,
	}))
	require.False(t, matcher.seasonPackReleasesMatch(pack, episode, true, &models.CrossSeedAutomationSettings{
		SeasonPackSkipRepackCompare: false,
	}))
}

func TestSeasonPackMatching_SimplifyHDRMatching(t *testing.T) {
	matcher := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	pack := parseSeasonPackTestRelease(t, "Show.S01E01.2160p.NF.WEB-DL.DDPA5.1.DV.HDR10+.H.265-RlsGrp")
	episode := parseSeasonPackTestRelease(t, "Show.S01E01.2160p.NF.WEB-DL.DDPA5.1.DV.HDR.H.265-RlsGrp")

	require.True(t, matcher.seasonPackReleasesMatch(pack, episode, false, &models.CrossSeedAutomationSettings{
		SeasonPackSkipRepackCompare:  true,
		SeasonPackSimplifyHDRCompare: true,
	}))
	require.False(t, matcher.releasesMatch(pack, episode, false))
}

func TestSeasonPackMatching_SimplifyWEBMatching(t *testing.T) {
	matcher := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	pack := parseSeasonPackTestRelease(t, "Show.S01E01.1080p.WEB-DL.H.264-RlsGrp")
	episode := parseSeasonPackTestRelease(t, "Show.S01E01.1080p.WEB.H.264-RlsGrp")

	require.False(t, matcher.seasonPackReleasesMatch(pack, episode, false, &models.CrossSeedAutomationSettings{
		SeasonPackSkipRepackCompare:  true,
		SeasonPackSimplifyWEBCompare: false,
	}))
	require.True(t, matcher.seasonPackReleasesMatch(pack, episode, false, &models.CrossSeedAutomationSettings{
		SeasonPackSkipRepackCompare:  true,
		SeasonPackSimplifyWEBCompare: true,
	}))
	require.True(t, matcher.releasesMatch(pack, episode, false))
}

func TestSeasonPackMatching_AllowsMissingSourceMetadata(t *testing.T) {
	matcher := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	pack := parseSeasonPackTestRelease(t, "Show.S01.1080p.H.264-RlsGrp")
	episode := parseSeasonPackTestRelease(t, "Show.S01E01.1080p.WEB-DL.H.264-RlsGrp")

	require.True(t, matcher.seasonPackReleasesMatch(pack, episode, true, &models.CrossSeedAutomationSettings{
		SeasonPackSkipRepackCompare: true,
	}))
}

func TestSeasonPackMatching_IgnoreYearDifferences(t *testing.T) {
	matcher := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	pack := parseSeasonPackTestRelease(t, "Show.2024.S01.1080p.WEB.H.264-RlsGrp")
	episode := parseSeasonPackTestRelease(t, "Show.2025.S01E01.1080p.WEB.H.264-RlsGrp")

	require.True(t, matcher.seasonPackReleasesMatch(pack, episode, true, &models.CrossSeedAutomationSettings{
		SeasonPackSkipRepackCompare: true,
		SeasonPackSkipYearCompare:   true,
	}))
	require.False(t, matcher.releasesMatch(pack, episode, true))
}

func TestSeasonPackMatching_GenericMatcherRemainsUnchanged(t *testing.T) {
	matcher := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	left := parseSeasonPackTestRelease(t, "Show.S01E01.1080p.WEB-DL.DDPA5.1.H.264-RlsGrp")
	right := parseSeasonPackTestRelease(t, "Show.S01E01.1080p.WEB-DL.REPACK.DDPA5.1.H.264-RlsGrp")

	require.False(t, matcher.releasesMatch(left, right, false))
}

func parseSeasonPackTestRelease(t *testing.T, name string) *rls.Release {
	t.Helper()

	release := rls.ParseString(name)
	require.NotEmpty(t, release.Title, "expected parser to extract a title from %q", name)

	return &release
}
