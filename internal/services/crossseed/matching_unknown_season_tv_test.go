// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestReleasesMatch_UnknownSeasonTV(t *testing.T) {
	t.Parallel()

	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	tests := []struct {
		name                   string
		source                 rls.Release
		candidate              rls.Release
		findIndividualEpisodes bool
		wantMatch              bool
		wantReason             string
	}{
		{
			name: "seasonless anime pack matches known season pack",
			source: rls.Release{
				Type:       rls.Series,
				Title:      "Classic Stars",
				Resolution: "1080p",
				Site:       "SubsPlease",
			},
			candidate: rls.Release{
				Type:       rls.Series,
				Title:      "Classic Stars",
				Series:     1,
				Resolution: "1080p",
				Collection: "CR",
				Group:      "SubsPlease",
			},
			wantMatch: true,
		},
		{
			name: "seasonless anime pack rejects different release group",
			source: rls.Release{
				Type:       rls.Series,
				Title:      "Classic Stars",
				Resolution: "1080p",
				Site:       "SubsPlease",
			},
			candidate: rls.Release{
				Type:       rls.Series,
				Title:      "Classic★Stars",
				Series:     1,
				Resolution: "1080p",
				Collection: "CR",
				Group:      "BiOMA",
			},
			wantMatch:  false,
			wantReason: "site mismatch",
		},
		{
			name: "seasonless anime episode matches same known-season episode",
			source: rls.Release{
				Type:       rls.Episode,
				Title:      "Classic Stars",
				Episode:    11,
				Resolution: "1080p",
			},
			candidate: rls.Release{
				Type:       rls.Episode,
				Title:      "Classic Stars",
				Series:     1,
				Episode:    11,
				Resolution: "1080p",
			},
			wantMatch: true,
		},
		{
			name: "seasonless anime episode rejects different known-season episode",
			source: rls.Release{
				Type:       rls.Episode,
				Title:      "Classic Stars",
				Episode:    11,
				Resolution: "1080p",
			},
			candidate: rls.Release{
				Type:       rls.Episode,
				Title:      "Classic Stars",
				Series:     1,
				Episode:    12,
				Resolution: "1080p",
			},
			wantMatch:  false,
			wantReason: "episode mismatch",
		},
		{
			name: "seasonless pack does not match episode when individual episodes are disabled",
			source: rls.Release{
				Type:       rls.Series,
				Title:      "Classic Stars",
				Resolution: "1080p",
			},
			candidate: rls.Release{
				Type:       rls.Episode,
				Title:      "Classic Stars",
				Series:     1,
				Episode:    11,
				Resolution: "1080p",
			},
			wantMatch:  false,
			wantReason: "season pack versus episode mismatch",
		},
		{
			name: "known-season bracket anime episode matches collection-tagged pack",
			source: rls.Release{
				Type:       rls.Series,
				Title:      "Reborn as a Vending Machine I Now Wander the Dungeon",
				Series:     3,
				Resolution: "1080p",
				Collection: "CR",
				Group:      "SubsPlease",
			},
			candidate: rls.Release{
				Type:       rls.Episode,
				Title:      "Reborn as a Vending Machine I Now Wander the Dungeon",
				Series:     3,
				Episode:    2,
				Resolution: "1080p",
				Site:       "SubsPlease",
			},
			findIndividualEpisodes: true,
			wantMatch:              true,
		},
		{
			name: "known-season scene candidate without collection matches",
			source: rls.Release{
				Type:       rls.Series,
				Title:      "Classic Stars",
				Series:     3,
				Resolution: "1080p",
				Collection: "NF",
				Group:      "GROUP",
			},
			candidate: rls.Release{
				Type:       rls.Series,
				Title:      "Classic Stars",
				Series:     3,
				Resolution: "1080p",
				Group:      "GROUP",
			},
			wantMatch: true,
		},
		{
			name: "known-season site-tagged scene candidate without collection matches",
			source: rls.Release{
				Type:       rls.Series,
				Title:      "Classic Stars",
				Series:     3,
				Resolution: "1080p",
				Collection: "NF",
				Group:      "GROUP",
			},
			candidate: rls.Release{
				Type:       rls.Series,
				Title:      "Classic Stars",
				Series:     3,
				Resolution: "1080p",
				Site:       "TGx",
				Group:      "GROUP",
			},
			wantMatch: true,
		},
		{
			name: "bracket anime candidate with conflicting collection still rejected",
			source: rls.Release{
				Type:       rls.Series,
				Title:      "Classic Stars",
				Series:     3,
				Resolution: "1080p",
				Collection: "CR",
				Group:      "SubsPlease",
			},
			candidate: rls.Release{
				Type:       rls.Series,
				Title:      "Classic Stars",
				Series:     3,
				Resolution: "1080p",
				Collection: "NF",
				Site:       "SubsPlease",
			},
			wantMatch:  false,
			wantReason: "collection mismatch",
		},
		{
			name: "movie source still rejects tv candidate",
			source: rls.Release{
				Type:       rls.Movie,
				Title:      "Classic Stars",
				Resolution: "1080p",
			},
			candidate: rls.Release{
				Type:       rls.Episode,
				Title:      "Classic Stars",
				Series:     1,
				Episode:    11,
				Resolution: "1080p",
			},
			wantMatch:  false,
			wantReason: "source not recognized as TV",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			match, reason := s.releasesMatchWithReason(&tt.source, &tt.candidate, tt.findIndividualEpisodes)
			require.Equal(t, tt.wantMatch, match)
			require.Equal(t, tt.wantReason, reason)
		})
	}
}
