// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParser_EnrichesHDRAliases(t *testing.T) {
	t.Parallel()

	parser := NewDefaultParser()

	tests := []struct {
		name    string
		input   string
		wantHDR []string
		notHDR  []string
	}{
		{
			name:    "discussion title keeps HDR10 plus",
			input:   "End of Watch 2012 Hybrid 2160p UHD BluRay REMUX DV HDR10+ HEVC DTS-HD MA 5.1-FraMeSToR",
			wantHDR: []string{"DV", "HDR10+"},
		},
		{
			name:    "filename alias HDR10P normalizes to HDR10 plus",
			input:   "End.of.Watch.2012.UHD.BluRay.2160p.DTS-HD.MA.5.1.DV.HDR10P.HEVC.HYBRID.REMUX-FraMeSToR.mkv",
			wantHDR: []string{"DV", "HDR10+"},
			notHDR:  []string{"HDR10"},
		},
		{
			name:    "spaced HDR10 PLUS normalizes to HDR10 plus",
			input:   "Movie.2024.2160p.BluRay.x265.DV.HDR10 PLUS-GROUP",
			wantHDR: []string{"DV", "HDR10+"},
			notHDR:  []string{"HDR10"},
		},
		{
			name:    "dotted HDR10 plus drops inherited HDR10",
			input:   "Movie.2024.2160p.BluRay.x265.DV.HDR10+-GROUP",
			wantHDR: []string{"DV", "HDR10+"},
			notHDR:  []string{"HDR10"},
		},
		{
			name:    "underscored HDR10 PLUS normalizes to HDR10 plus",
			input:   "Movie.2024.2160p.BluRay.x265.DV.HDR10_PLUS-GROUP",
			wantHDR: []string{"DV", "HDR10+"},
			notHDR:  []string{"HDR10"},
		},
		{
			name:    "DV only stays DV only",
			input:   "Movie.2024.2160p.UHD.BluRay.REMUX.DV.HEVC-GROUP",
			wantHDR: []string{"DV"},
			notHDR:  []string{"HDR", "HDR10", "HDR10+", "HLG"},
		},
		{
			name:    "scene group DV does not become HDR",
			input:   "Software.Name.v1.0-DV",
			wantHDR: nil,
			notHDR:  []string{"DV", "HDR", "HDR10", "HDR10+", "HLG"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			release := parser.Parse(tt.input)
			require.ElementsMatch(t, tt.wantHDR, release.HDR)
			for _, tag := range tt.notHDR {
				require.NotContains(t, release.HDR, tag)
			}
		})
	}
}
