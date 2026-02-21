// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestVariantOverridesReleaseVariants(t *testing.T) {
	release := rls.Release{
		Collection: "IMAX",
		Other:      []string{"HYBRiD REMUX"},
	}

	variants := strictVariantOverrides.releaseVariants(&release)
	_, hasIMAX := variants["IMAX"]
	require.True(t, hasIMAX, "expected IMAX variant to be detected: %#v", variants)
	_, hasHYBRID := variants["HYBRID"]
	require.True(t, hasHYBRID, "expected HYBRID variant to be detected: %#v", variants)

	multiVariant := rls.Release{
		Collection: "IMAX",
		Other:      []string{"HYBRiD"},
	}
	multiVariants := strictVariantOverrides.releaseVariants(&multiVariant)
	require.Len(t, multiVariants, 2, "expected both IMAX and HYBRID variants")
	_, hasIMAX = multiVariants["IMAX"]
	require.True(t, hasIMAX, "expected IMAX variant to be detected for multiVariant: %#v", multiVariants)
	_, hasHYBRID = multiVariants["HYBRID"]
	require.True(t, hasHYBRID, "expected HYBRID variant to be detected for multiVariant: %#v", multiVariants)

	compositeVariant := rls.Release{
		Other: []string{"IMAX.HYBRiD.REMUX"},
	}
	compositeVariants := strictVariantOverrides.releaseVariants(&compositeVariant)
	require.Len(t, compositeVariants, 1, "expected only HYBRID variant from composite entry")
	_, hasHYBRID = compositeVariants["HYBRID"]
	require.True(t, hasHYBRID, "expected HYBRID token to be extracted from composite entry: %#v", compositeVariants)

	tokenEdge := rls.Release{
		Other: []string{"IMAX..HYBRID", ""},
	}
	tokenEdgeVariants := strictVariantOverrides.releaseVariants(&tokenEdge)
	require.Len(t, tokenEdgeVariants, 1, "expected only valid HYBRID token from edge case")
	_, hasHYBRID = tokenEdgeVariants["HYBRID"]
	require.True(t, hasHYBRID, "expected HYBRID token to survive edge tokenization: %#v", tokenEdgeVariants)

	plain := rls.Release{Collection: "", Other: []string{"READNFO"}}
	plainVariants := strictVariantOverrides.releaseVariants(&plain)
	require.Empty(t, plainVariants, "expected no variants")
}

func TestReleasesMatch_StrictVariantsMustMatch(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	base := rls.Release{
		Title:      "The Conjuring Last Rites",
		Year:       2025,
		Source:     "BLURAY",
		Resolution: "1080P",
		Collection: "IMAX",
	}

	nonVariant := rls.Release{
		Title:      base.Title,
		Year:       base.Year,
		Source:     base.Source,
		Resolution: base.Resolution,
	}

	require.False(t, s.releasesMatch(&base, &nonVariant, false), "IMAX should not match vanilla release")

	imaxCandidate := nonVariant
	imaxCandidate.Collection = "IMAX"
	require.True(t, s.releasesMatch(&base, &imaxCandidate, false), "matching IMAX releases should be compatible")

	hybridCandidate := nonVariant
	hybridCandidate.Other = []string{"HYBRiD"}
	require.False(t, s.releasesMatch(&nonVariant, &hybridCandidate, false), "HYBRID variant should not match vanilla release")
	require.False(t, s.releasesMatch(&hybridCandidate, &nonVariant, false), "HYBRID mismatch must be symmetric")
}

func TestReleasesMatch_IMAXVsHybridMismatch(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	imaxRelease := rls.Release{
		Title:      "The Conjuring Last Rites",
		Year:       2025,
		Source:     "BLURAY",
		Resolution: "1080P",
		Collection: "IMAX",
	}
	hybridRelease := rls.Release{
		Title:      imaxRelease.Title,
		Year:       imaxRelease.Year,
		Source:     imaxRelease.Source,
		Resolution: imaxRelease.Resolution,
		Other:      []string{"HYBRiD"},
	}

	require.False(t, s.releasesMatch(&imaxRelease, &hybridRelease, false), "IMAX should not match HYBRID")
	require.False(t, s.releasesMatch(&hybridRelease, &imaxRelease, false), "HYBRID vs IMAX mismatch must be symmetric")
}

func TestReleasesMatch_REPACKAllowedForSeasonPacks(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	// Season pack without REPACK
	seasonPack := rls.Release{
		Title:      "The Show",
		Series:     1,
		Episode:    0, // Season pack
		Source:     "BLURAY",
		Resolution: "1080P",
		Group:      "GROUP",
	}

	// Season pack with REPACK (might have REPACK of just one episode)
	seasonPackRepack := rls.Release{
		Title:      "The Show",
		Series:     1,
		Episode:    0, // Season pack
		Source:     "BLURAY",
		Resolution: "1080P",
		Group:      "GROUP",
		Other:      []string{"REPACK"},
	}

	// Season packs should be allowed to cross-seed even with REPACK mismatch
	require.True(t, s.releasesMatch(&seasonPack, &seasonPackRepack, false),
		"season pack should match REPACK season pack")
	require.True(t, s.releasesMatch(&seasonPackRepack, &seasonPack, false),
		"REPACK season pack should match vanilla season pack")
}

func TestReleasesMatch_REPACKBlockedForEpisodes(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	// Individual episode without REPACK
	episode := rls.Release{
		Title:      "The Show",
		Series:     1,
		Episode:    5,
		Source:     "BLURAY",
		Resolution: "1080P",
		Group:      "GROUP",
	}

	// Individual episode with REPACK
	episodeRepack := rls.Release{
		Title:      "The Show",
		Series:     1,
		Episode:    5,
		Source:     "BLURAY",
		Resolution: "1080P",
		Group:      "GROUP",
		Other:      []string{"REPACK"},
	}

	// Episodes must match REPACK status exactly
	require.False(t, s.releasesMatch(&episode, &episodeRepack, false),
		"vanilla episode should NOT match REPACK episode")
	require.False(t, s.releasesMatch(&episodeRepack, &episode, false),
		"REPACK episode should NOT match vanilla episode")

	// Same REPACK status should match
	require.True(t, s.releasesMatch(&episodeRepack, &episodeRepack, false),
		"REPACK episodes should match each other")
}

func TestReleasesMatch_PROPERBlockedForMovies(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	movie := rls.Release{
		Title:      "The Movie",
		Year:       2025,
		Source:     "BLURAY",
		Resolution: "1080P",
		Group:      "GROUP",
	}

	movieProper := rls.Release{
		Title:      "The Movie",
		Year:       2025,
		Source:     "BLURAY",
		Resolution: "1080P",
		Group:      "GROUP",
		Other:      []string{"PROPER"},
	}

	// Movies must match PROPER status exactly
	require.False(t, s.releasesMatch(&movie, &movieProper, false),
		"vanilla movie should NOT match PROPER movie")
	require.False(t, s.releasesMatch(&movieProper, &movie, false),
		"PROPER movie should NOT match vanilla movie")
}

func TestReleasesMatch_IMAXBlockedEvenForSeasonPacks(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	// Season pack without IMAX
	seasonPack := rls.Release{
		Title:      "The Show",
		Series:     1,
		Episode:    0,
		Source:     "BLURAY",
		Resolution: "1080P",
		Group:      "GROUP",
	}

	// Season pack with IMAX
	seasonPackIMAX := rls.Release{
		Title:      "The Show",
		Series:     1,
		Episode:    0,
		Source:     "BLURAY",
		Resolution: "1080P",
		Group:      "GROUP",
		Collection: "IMAX",
	}

	// IMAX must ALWAYS match, even for season packs (different video master)
	require.False(t, s.releasesMatch(&seasonPack, &seasonPackIMAX, false),
		"vanilla season pack should NOT match IMAX season pack")
	require.False(t, s.releasesMatch(&seasonPackIMAX, &seasonPack, false),
		"IMAX season pack should NOT match vanilla season pack")
}
