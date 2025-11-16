// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"
)

func TestVariantOverridesReleaseVariants(t *testing.T) {
	release := rls.Release{
		Collection: "IMAX",
		Other:      []string{"HYBRiD REMUX"},
	}

	variants := strictVariantOverrides.releaseVariants(release)
	_, hasIMAX := variants["IMAX"]
	require.True(t, hasIMAX, "expected IMAX variant to be detected: %#v", variants)
	_, hasHYBRID := variants["HYBRID"]
	require.True(t, hasHYBRID, "expected HYBRID variant to be detected: %#v", variants)

	multiVariant := rls.Release{
		Collection: "IMAX",
		Other:      []string{"HYBRiD"},
	}
	multiVariants := strictVariantOverrides.releaseVariants(multiVariant)
	require.Len(t, multiVariants, 2, "expected both IMAX and HYBRID variants")
	_, hasIMAX = multiVariants["IMAX"]
	require.True(t, hasIMAX, "expected IMAX variant to be detected for multiVariant: %#v", multiVariants)
	_, hasHYBRID = multiVariants["HYBRID"]
	require.True(t, hasHYBRID, "expected HYBRID variant to be detected for multiVariant: %#v", multiVariants)

	compositeVariant := rls.Release{
		Other: []string{"IMAX.HYBRiD.REMUX"},
	}
	compositeVariants := strictVariantOverrides.releaseVariants(compositeVariant)
	require.Len(t, compositeVariants, 1, "expected only HYBRID variant from composite entry")
	_, hasHYBRID = compositeVariants["HYBRID"]
	require.True(t, hasHYBRID, "expected HYBRID token to be extracted from composite entry: %#v", compositeVariants)

	tokenEdge := rls.Release{
		Other: []string{"IMAX..HYBRID", ""},
	}
	tokenEdgeVariants := strictVariantOverrides.releaseVariants(tokenEdge)
	require.Len(t, tokenEdgeVariants, 1, "expected only valid HYBRID token from edge case")
	_, hasHYBRID = tokenEdgeVariants["HYBRID"]
	require.True(t, hasHYBRID, "expected HYBRID token to survive edge tokenization: %#v", tokenEdgeVariants)

	plain := rls.Release{Collection: "", Other: []string{"READNFO"}}
	plainVariants := strictVariantOverrides.releaseVariants(plain)
	require.Empty(t, plainVariants, "expected no variants")
}

func TestReleasesMatch_StrictVariantsMustMatch(t *testing.T) {
	s := &Service{}

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

	require.False(t, s.releasesMatch(base, nonVariant, false), "IMAX should not match vanilla release")

	imaxCandidate := nonVariant
	imaxCandidate.Collection = "IMAX"
	require.True(t, s.releasesMatch(base, imaxCandidate, false), "matching IMAX releases should be compatible")

	hybridCandidate := nonVariant
	hybridCandidate.Other = []string{"HYBRiD"}
	require.False(t, s.releasesMatch(nonVariant, hybridCandidate, false), "HYBRID variant should not match vanilla release")
	require.False(t, s.releasesMatch(hybridCandidate, nonVariant, false), "HYBRID mismatch must be symmetric")
}

func TestReleasesMatch_IMAXVsHybridMismatch(t *testing.T) {
	s := &Service{}

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

	require.False(t, s.releasesMatch(imaxRelease, hybridRelease, false), "IMAX should not match HYBRID")
	require.False(t, s.releasesMatch(hybridRelease, imaxRelease, false), "HYBRID vs IMAX mismatch must be symmetric")
}
