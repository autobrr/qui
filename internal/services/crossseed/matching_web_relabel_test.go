// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

// TestIsWebSourceRelabel covers the cross-tracker relabel detector that lets the
// same web encode through when only its WEBRip/WEB-DL label differs. The real
// motivating case is "Law & Order: SVU" S05, which trackers list as both
// WEBRip (the seeded copy) and WEB-DL (the relabel) at the same content size.
func TestIsWebSourceRelabel(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	const (
		webripDotted = "Law.and.Order.Special.Victims.Unit.S05.1080p.AMZN.WEBRip.DD2.0.x264-NTb"
		webdlDotted  = "Law.and.Order.Special.Victims.Unit.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb"
		webdlSpaced  = "Law & Order: Special Victims Unit S05 1080p AMZN WEB-DL DD+ 2.0 H.264-NTb"
	)

	tests := []struct {
		name          string
		sourceName    string
		candidateName string
		want          bool
	}{
		{
			name:          "WEBRip source vs WEB-DL relabel of same encode",
			sourceName:    webripDotted,
			candidateName: webdlDotted,
			want:          true,
		},
		{
			name:          "symmetric: WEB-DL source vs WEBRip relabel",
			sourceName:    webdlDotted,
			candidateName: webripDotted,
			want:          true,
		},
		{
			name:          "relabel detected through ampersand/colon title variant",
			sourceName:    webripDotted,
			candidateName: webdlSpaced,
			want:          true,
		},
		{
			name:          "different resolution is not a relabel",
			sourceName:    webripDotted,
			candidateName: "Law.and.Order.Special.Victims.Unit.S05.2160p.AMZN.WEB-DL.DD+2.0.x264-NTb",
			want:          false,
		},
		{
			name:          "different show is not a relabel",
			sourceName:    webripDotted,
			candidateName: "Law.and.Order.Organized.Crime.S05.1080p.AMZN.WEB-DL.DD+2.0.x264-NTb",
			want:          false,
		},
		{
			name:          "non-web source is not a relabel",
			sourceName:    "Law.and.Order.Special.Victims.Unit.S05.1080p.BluRay.DD2.0.x264-NTb",
			candidateName: webdlDotted,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := rls.ParseString(tt.sourceName)
			candidate := rls.ParseString(tt.candidateName)
			got := s.isWebSourceRelabel(&source, &candidate, tt.sourceName, tt.candidateName, nil, nil, false)
			require.Equal(t, tt.want, got)
		})
	}
}
