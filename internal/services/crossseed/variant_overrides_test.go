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
	if _, ok := variants["IMAX"]; !ok {
		t.Fatalf("expected IMAX variant to be detected: %#v", variants)
	}
	if _, ok := variants["HYBRID"]; !ok {
		t.Fatalf("expected HYBRID variant to be detected: %#v", variants)
	}

	multiVariant := rls.Release{
		Collection: "IMAX",
		Other:      []string{"HYBRiD"},
	}
	multiVariants := strictVariantOverrides.releaseVariants(multiVariant)
	if len(multiVariants) != 2 {
		t.Fatalf("expected both IMAX and HYBRID variants, got %#v", multiVariants)
	}
	if _, ok := multiVariants["IMAX"]; !ok {
		t.Fatalf("expected IMAX variant to be detected for multiVariant: %#v", multiVariants)
	}
	if _, ok := multiVariants["HYBRID"]; !ok {
		t.Fatalf("expected HYBRID variant to be detected for multiVariant: %#v", multiVariants)
	}

	compositeVariant := rls.Release{
		Other: []string{"IMAX.HYBRiD.REMUX"},
	}
	compositeVariants := strictVariantOverrides.releaseVariants(compositeVariant)
	if len(compositeVariants) != 1 {
		t.Fatalf("expected only HYBRID variant from composite entry, got %#v", compositeVariants)
	}
	if _, ok := compositeVariants["HYBRID"]; !ok {
		t.Fatalf("expected HYBRID token to be extracted from composite entry: %#v", compositeVariants)
	}

	tokenEdge := rls.Release{
		Other: []string{"IMAX..HYBRID", ""},
	}
	tokenEdgeVariants := strictVariantOverrides.releaseVariants(tokenEdge)
	if len(tokenEdgeVariants) != 1 {
		t.Fatalf("expected only valid HYBRID token from edge case, got %#v", tokenEdgeVariants)
	}
	if _, ok := tokenEdgeVariants["HYBRID"]; !ok {
		t.Fatalf("expected HYBRID token to survive edge tokenization: %#v", tokenEdgeVariants)
	}

	plain := rls.Release{Collection: "", Other: []string{"READNFO"}}
	plainVariants := strictVariantOverrides.releaseVariants(plain)
	if len(plainVariants) != 0 {
		t.Fatalf("expected no variants, got %#v", plainVariants)
	}
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
