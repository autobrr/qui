// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestReleasesMatchWebhook_FillsMissingCollection(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	tests := []struct {
		name      string
		indexer   string
		source    rls.Release
		candidate rls.Release
		wantMatch bool
	}{
		{
			name:    "hdb tv missing collection matches when group anchors release",
			indexer: "hdb",
			source: rls.Release{
				Title:      "Sample Show",
				Series:     8,
				Episode:    11,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Group:      "NTb",
			},
			candidate: rls.Release{
				Title:      "Sample Show",
				Series:     8,
				Episode:    11,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Collection: "DSNP",
				Group:      "NTb",
			},
			wantMatch: true,
		},
		{
			name:    "hdb web movie missing collection matches when group anchors release",
			indexer: "hdb",
			source: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Group:      "NTb",
			},
			candidate: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Collection: "DSNP",
				Group:      "NTb",
			},
			wantMatch: true,
		},
		{
			name:    "non-hdb missing collection stays strict",
			indexer: "btn",
			source: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Group:      "NTb",
			},
			candidate: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Collection: "DSNP",
				Group:      "NTb",
			},
			wantMatch: false,
		},
		{
			name: "missing indexer stays strict",
			source: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Group:      "NTb",
			},
			candidate: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Collection: "DSNP",
				Group:      "NTb",
			},
			wantMatch: false,
		},
		{
			name:    "hdb missing collection still needs group or site anchor",
			indexer: "hdb",
			source: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "WEB-DL",
				Resolution: "1080p",
			},
			candidate: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Collection: "DSNP",
				Group:      "NTb",
			},
			wantMatch: false,
		},
		{
			name:    "hdb non-web movie missing collection stays strict",
			indexer: "hdb",
			source: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "BluRay",
				Resolution: "1080p",
				Group:      "FraMeSToR",
			},
			candidate: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "BluRay",
				Resolution: "1080p",
				Collection: "Criterion",
				Group:      "FraMeSToR",
			},
			wantMatch: false,
		},
		{
			name:    "hdb explicit source collection mismatch stays strict",
			indexer: "hdb",
			source: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Collection: "AMZN",
				Group:      "NTb",
			},
			candidate: rls.Release{
				Title:      "Sample Movie",
				Year:       2024,
				Source:     "WEB-DL",
				Resolution: "1080p",
				Collection: "DSNP",
				Group:      "NTb",
			},
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantMatch, s.releasesMatchWebhook(&tt.source, &tt.candidate, false, tt.indexer))
		})
	}
}
