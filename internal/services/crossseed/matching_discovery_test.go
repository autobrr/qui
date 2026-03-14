// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestReleasesMatchDiscovery_AllowsMissingGroupWithSameCoreRelease(t *testing.T) {
	t.Parallel()

	svc := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	source := &rls.Release{
		Title:      "Gladiator",
		Year:       2000,
		Group:      "UBWEB",
		Source:     "WEB-DL",
		Resolution: "2160p",
	}
	candidate := &rls.Release{
		Title:      "Gladiator",
		Year:       2000,
		Source:     "WEB-DL",
		Resolution: "2160p",
	}

	require.False(t, svc.releasesMatch(source, candidate, false))
	require.True(t, svc.releasesMatchDiscovery(source, candidate, false))
}

func TestReleasesMatchDiscovery_IgnoresBilingualAndRegionTitleNoise(t *testing.T) {
	t.Parallel()

	svc := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	t.Run("bilingual title", func(t *testing.T) {
		t.Parallel()

		source := &rls.Release{
			Title:      "角斗士 Gladiator",
			Year:       2000,
			Group:      "UBWEB",
			Source:     "WEB-DL",
			Resolution: "2160p",
		}
		candidate := &rls.Release{
			Title:      "Gladiator",
			Year:       2000,
			Source:     "WEB-DL",
			Resolution: "2160p",
		}

		require.False(t, svc.releasesMatch(source, candidate, false))
		require.True(t, svc.releasesMatchDiscovery(source, candidate, false))
	})

	t.Run("region suffix", func(t *testing.T) {
		t.Parallel()

		source := &rls.Release{
			Title:      "Doc US",
			Series:     2,
			Episode:    17,
			Group:      "Kitsune",
			Source:     "WEB-DL",
			Resolution: "1080p",
		}
		candidate := &rls.Release{
			Title:      "Doc",
			Series:     2,
			Episode:    17,
			Source:     "WEB-DL",
			Resolution: "1080p",
		}

		require.False(t, svc.releasesMatch(source, candidate, false))
		require.True(t, svc.releasesMatchDiscovery(source, candidate, false))
	})
}

func TestReleasesMatchDiscovery_KeepsInternalRegionLikeTokens(t *testing.T) {
	t.Parallel()

	svc := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	source := &rls.Release{
		Title:      "The IT Crowd",
		Series:     1,
		Episode:    1,
		Source:     "WEB-DL",
		Resolution: "1080p",
	}
	candidate := &rls.Release{
		Title:      "The Crowd",
		Series:     1,
		Episode:    1,
		Source:     "WEB-DL",
		Resolution: "1080p",
	}

	require.False(t, svc.releasesMatchDiscovery(source, candidate, false))
}

func TestReleasesMatchDiscovery_StillRejectsDistinctSpinoffs(t *testing.T) {
	t.Parallel()

	svc := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	source := &rls.Release{
		Title:      "FBI",
		Series:     1,
		Episode:    1,
		Source:     "WEB-DL",
		Resolution: "1080p",
	}
	candidate := &rls.Release{
		Title:      "FBI Most Wanted",
		Series:     1,
		Episode:    1,
		Source:     "WEB-DL",
		Resolution: "1080p",
	}

	require.False(t, svc.releasesMatchDiscovery(source, candidate, false))
}

func TestReleasesMatchDiscovery_KeepsSourceAndVersionBoundaries(t *testing.T) {
	t.Parallel()

	svc := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	t.Run("source mismatch", func(t *testing.T) {
		t.Parallel()

		source := &rls.Release{
			Title:      "Movie",
			Year:       2025,
			Source:     "WEB-DL",
			Resolution: "1080p",
		}
		candidate := &rls.Release{
			Title:      "Movie",
			Year:       2025,
			Source:     "BluRay",
			Resolution: "1080p",
		}

		require.False(t, svc.releasesMatchDiscovery(source, candidate, false))
	})

	t.Run("version mismatch", func(t *testing.T) {
		t.Parallel()

		source := &rls.Release{
			Title:      "Show",
			Series:     1,
			Episode:    4,
			Source:     "WEB-DL",
			Resolution: "1080p",
			Version:    "v2",
		}
		candidate := &rls.Release{
			Title:      "Show",
			Series:     1,
			Episode:    4,
			Source:     "WEB-DL",
			Resolution: "1080p",
		}

		require.False(t, svc.releasesMatchDiscovery(source, candidate, false))
	})
}

func TestReleasesMatchDiscovery_KeepsSharedCompatibilityChecks(t *testing.T) {
	t.Parallel()

	svc := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	t.Run("collection mismatch", func(t *testing.T) {
		t.Parallel()

		source := &rls.Release{
			Title:      "Gladiator",
			Year:       2000,
			Group:      "UBWEB",
			Source:     "WEB-DL",
			Resolution: "2160p",
			Collection: "NF",
		}
		candidate := &rls.Release{
			Title:      "Gladiator",
			Year:       2000,
			Source:     "WEB-DL",
			Resolution: "2160p",
		}

		require.False(t, svc.releasesMatchDiscovery(source, candidate, false))
	})

	t.Run("variant mismatch", func(t *testing.T) {
		t.Parallel()

		source := &rls.Release{
			Title:      "Gladiator",
			Year:       2000,
			Group:      "UBWEB",
			Source:     "WEB-DL",
			Resolution: "2160p",
			Other:      []string{"PROPER"},
		}
		candidate := &rls.Release{
			Title:      "Gladiator",
			Year:       2000,
			Source:     "WEB-DL",
			Resolution: "2160p",
		}

		require.False(t, svc.releasesMatchDiscovery(source, candidate, false))
	})
}

func TestReleasesMatchDiscovery_UsesDefaultNormalizerFallback(t *testing.T) {
	t.Parallel()

	svc := &Service{}

	source := &rls.Release{
		Title:      "Gladiator",
		Year:       2000,
		Group:      "UBWEB",
		Source:     "WEB-DL",
		Resolution: "2160p",
	}
	candidate := &rls.Release{
		Title:      "Gladiator",
		Year:       2000,
		Source:     "WEB-DL",
		Resolution: "2160p",
	}

	require.NotPanics(t, func() {
		require.True(t, svc.releasesMatchDiscovery(source, candidate, false))
	})
}

func TestReleasesMatchDiscovery_AllowsImplicitUHDToMatchExplicit2160p(t *testing.T) {
	t.Parallel()

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	source := svc.releaseCache.Parse("Salt.2010.COMPLETE.UHD.BLURAY-COASTER")
	candidate := svc.releaseCache.Parse("Salt 2010 Theatrical Cut 2160p UHD Blu-ray HEVC TrueHD 7.1-COASTER")

	require.True(t, svc.releasesMatchDiscoveryWithContext(
		source,
		candidate,
		false,
		resolutionMatchContext{discLayout: true, discMarker: "BDMV", rawName: "Salt.2010.COMPLETE.UHD.BLURAY-COASTER"},
		resolutionMatchContext{},
	))
}

func TestReleasesMatchDiscovery_DoesNotInfer2160pFromPlainBluray(t *testing.T) {
	t.Parallel()

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	source := svc.releaseCache.Parse("Salt.2010.COMPLETE.BLURAY-COASTER")
	candidate := svc.releaseCache.Parse("Salt 2010 2160p Blu-ray HEVC TrueHD 7.1-COASTER")

	require.False(t, svc.releasesMatchDiscoveryWithContext(
		source,
		candidate,
		false,
		resolutionMatchContext{discLayout: true, discMarker: "BDMV", rawName: "Salt.2010.COMPLETE.BLURAY-COASTER"},
		resolutionMatchContext{},
	))
}

func TestReleasesMatchDiscovery_Infers1080pFromPlainBlurayDiscLayout(t *testing.T) {
	t.Parallel()

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	source := svc.releaseCache.Parse("Salt.2010.COMPLETE.BLURAY-COASTER")
	candidate := svc.releaseCache.Parse("Salt 2010 1080p Blu-ray AVC TrueHD 5.1-COASTER")

	require.True(t, svc.releasesMatchDiscoveryWithContext(
		source,
		candidate,
		false,
		resolutionMatchContext{discLayout: true, discMarker: "BDMV", rawName: "Salt.2010.COMPLETE.BLURAY-COASTER"},
		resolutionMatchContext{},
	))
}
