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

// TestEffectiveSearchYear locks in that the alternate connector pass reuses the
// year actually searched: once the yearless retry has run, the alternate pass
// must drop the year too rather than re-applying the proven-ineffective original.
func TestEffectiveSearchYear(t *testing.T) {
	tests := []struct {
		name             string
		requestedYear    int
		yearlessRetryRan bool
		want             int
	}{
		{
			name:             "no retry keeps the requested year",
			requestedYear:    2005,
			yearlessRetryRan: false,
			want:             2005,
		},
		{
			name:             "after yearless retry the year is dropped",
			requestedYear:    2005,
			yearlessRetryRan: true,
			want:             0,
		},
		{
			name:             "no year requested stays zero",
			requestedYear:    0,
			yearlessRetryRan: false,
			want:             0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, effectiveSearchYear(tt.requestedYear, tt.yearlessRetryRan))
		})
	}
}

// TestMergeAltConnectorResults verifies the alternate-pass merge appends results
// and ORs the partial flag, so an incomplete alternate pass can never be reported
// as a complete search.
func TestMergeAltConnectorResults(t *testing.T) {
	primary := []jackett.SearchResult{{Title: "primary-1"}}

	tests := []struct {
		name           string
		primaryPartial bool
		altPartial     bool
		wantPartial    bool
	}{
		{name: "complete primary + complete alt stays complete", primaryPartial: false, altPartial: false, wantPartial: false},
		{name: "partial alt makes the merged result partial", primaryPartial: false, altPartial: true, wantPartial: true},
		{name: "partial primary stays partial", primaryPartial: true, altPartial: false, wantPartial: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alt := &jackett.SearchResponse{
				Results: []jackett.SearchResult{{Title: "alt-1"}, {Title: "alt-2"}},
				Partial: tt.altPartial,
			}

			merged, partial := mergeAltConnectorResults(tt.primaryPartial, primary, alt)

			require.Equal(t, tt.wantPartial, partial)
			require.Len(t, merged, len(primary)+len(alt.Results))
			require.Equal(t, "primary-1", merged[0].Title)
			require.Equal(t, "alt-2", merged[len(merged)-1].Title)
		})
	}
}
