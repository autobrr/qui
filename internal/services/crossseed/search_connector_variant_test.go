// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
)

func TestAlternateConnectorQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantAlt string
		wantOK  bool
	}{
		{
			name:    "spelled-out and becomes ampersand",
			query:   "Law and Order Special Victims Unit 1080p",
			wantAlt: "Law & Order Special Victims Unit 1080p",
			wantOK:  true,
		},
		{
			// rls preserves the source release's connector casing, so a release
			// named "Law.And.Order..." yields the title "Law And Order ...". This
			// title-case spelling is common in scene/p2p naming and must still swap.
			name:    "title-case And connector is swapped",
			query:   "Law And Order Special Victims Unit 1080p",
			wantAlt: "Law & Order Special Victims Unit 1080p",
			wantOK:  true,
		},
		{
			name:    "uppercase AND connector is swapped",
			query:   "Will AND Grace 1080p",
			wantAlt: "Will & Grace 1080p",
			wantOK:  true,
		},
		{
			name:    "ampersand becomes spelled-out and",
			query:   "Will & Grace 1080p",
			wantAlt: "Will and Grace 1080p",
			wantOK:  true,
		},
		{
			name:    "no connector yields no variant",
			query:   "Breaking Bad S01 1080p",
			wantAlt: "",
			wantOK:  false,
		},
		{
			name:    "embedded and is not a standalone connector",
			query:   "Andor S01 1080p",
			wantAlt: "",
			wantOK:  false,
		},
		{
			name:    "standalone ampersand connector becomes and",
			query:   "Tom & Jerry",
			wantAlt: "Tom and Jerry",
			wantOK:  true,
		},
		{
			name:    "intra-token ampersand is not a connector",
			query:   "AT&T S01 1080p",
			wantAlt: "",
			wantOK:  false,
		},
		{
			name:    "intra-token ampersand R&B is left intact",
			query:   "Best of R&B 2024",
			wantAlt: "",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alt, ok := alternateConnectorQuery(tt.query)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantAlt, alt)
		})
	}
}

// TestIndexersWithoutResults locks in the alternate-connector pass's indexer
// scoping: it re-queries only the indexers that returned nothing in the primary
// pass, in request order, so the extra round-trip stays minimal.
func TestIndexersWithoutResults(t *testing.T) {
	result := func(indexerID int) jackett.SearchResult {
		return jackett.SearchResult{IndexerID: indexerID}
	}

	tests := []struct {
		name        string
		requestedID []int
		results     []jackett.SearchResult
		want        []int
	}{
		{
			name:        "indexer that returned nothing is re-queried",
			requestedID: []int{1, 2, 3},
			results:     []jackett.SearchResult{result(1), result(3)},
			want:        []int{2},
		},
		{
			name:        "request order is preserved",
			requestedID: []int{5, 4, 3, 2, 1},
			results:     []jackett.SearchResult{result(3)},
			want:        []int{5, 4, 2, 1},
		},
		{
			name:        "an indexer with multiple results is still omitted",
			requestedID: []int{1, 2},
			results:     []jackett.SearchResult{result(1), result(1), result(1)},
			want:        []int{2},
		},
		{
			name:        "all indexers responded yields nothing to retry",
			requestedID: []int{1, 2},
			results:     []jackett.SearchResult{result(1), result(2)},
			want:        nil,
		},
		{
			name:        "zero primary results re-queries every requested indexer",
			requestedID: []int{1, 2, 3},
			results:     nil,
			want:        []int{1, 2, 3},
		},
		{
			name:        "empty requested set yields nil (all-indexers search is skipped)",
			requestedID: nil,
			results:     []jackett.SearchResult{result(1)},
			want:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, indexersWithoutResults(tt.requestedID, tt.results))
		})
	}
}
