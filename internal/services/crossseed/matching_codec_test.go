package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestNormalizeVideoCodec(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// AVC/H.264 aliases
		{"x264 lowercase", "x264", "AVC"},
		{"X264 uppercase", "X264", "AVC"},
		{"H.264 with dot", "H.264", "AVC"},
		{"h.264 lowercase", "h.264", "AVC"},
		{"H264 no dot", "H264", "AVC"},
		{"AVC direct", "AVC", "AVC"},
		{"avc lowercase", "avc", "AVC"},

		// HEVC/H.265 aliases
		{"x265 lowercase", "x265", "HEVC"},
		{"X265 uppercase", "X265", "HEVC"},
		{"H.265 with dot", "H.265", "HEVC"},
		{"h.265 lowercase", "h.265", "HEVC"},
		{"H265 no dot", "H265", "HEVC"},
		{"HEVC direct", "HEVC", "HEVC"},
		{"hevc lowercase", "hevc", "HEVC"},

		// Non-aliased codecs pass through uppercased
		{"VP9 passthrough", "VP9", "VP9"},
		{"AV1 passthrough", "AV1", "AV1"},
		{"XViD passthrough", "XViD", "XVID"},
		{"DivX passthrough", "DivX", "DIVX"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeVideoCodec(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestJoinNormalizedCodecSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{"empty slice", []string{}, ""},
		{"single x264", []string{"x264"}, "AVC"},
		{"single H.264", []string{"H.264"}, "AVC"},
		{"x265 alone", []string{"x265"}, "HEVC"},
		{"H.265 alone", []string{"H.265"}, "HEVC"},
		{"multiple codecs sorted", []string{"HEVC", "AVC"}, "AVC HEVC"},
		{"passthrough codec", []string{"VP9"}, "VP9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := joinNormalizedCodecSlice(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestReleasesMatch_CodecAliasing(t *testing.T) {
	s := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}

	tests := []struct {
		name        string
		source      rls.Release
		candidate   rls.Release
		wantMatch   bool
		description string
	}{
		{
			name: "x264 matches H.264",
			source: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"x264"},
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"H.264"},
				Group:  "GROUP",
			},
			wantMatch:   true,
			description: "x264 and H.264 are the same AVC codec",
		},
		{
			name: "x264 matches H264 (no dot)",
			source: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"x264"},
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"H264"},
				Group:  "GROUP",
			},
			wantMatch:   true,
			description: "x264 and H264 are the same AVC codec",
		},
		{
			name: "x264 matches AVC",
			source: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"x264"},
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"AVC"},
				Group:  "GROUP",
			},
			wantMatch:   true,
			description: "x264 and AVC are the same codec",
		},
		{
			name: "x265 matches H.265",
			source: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"x265"},
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"H.265"},
				Group:  "GROUP",
			},
			wantMatch:   true,
			description: "x265 and H.265 are the same HEVC codec",
		},
		{
			name: "x265 matches HEVC",
			source: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"x265"},
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"HEVC"},
				Group:  "GROUP",
			},
			wantMatch:   true,
			description: "x265 and HEVC are the same codec",
		},
		{
			name: "x264 does NOT match x265",
			source: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"x264"},
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"x265"},
				Group:  "GROUP",
			},
			wantMatch:   false,
			description: "AVC and HEVC are different codecs",
		},
		{
			name: "H.264 does NOT match HEVC",
			source: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"H.264"},
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"HEVC"},
				Group:  "GROUP",
			},
			wantMatch:   false,
			description: "AVC and HEVC are different codecs",
		},
		{
			name: "user report: x264 vs H.264 season pack",
			source: rls.Release{
				Title:      "The Great British Bake Off",
				Series:     3,
				Resolution: "1080p",
				Source:     "WEB-DL",
				Collection: "NF",
				Codec:      []string{"x264"},
				Audio:      []string{"DDP"},
				Channels:   "2.0",
				Group:      "NTb",
			},
			candidate: rls.Release{
				Title:      "The Great British Bake Off",
				Series:     3,
				Resolution: "1080p",
				Source:     "WEB-DL",
				Collection: "NF",
				Codec:      []string{"H.264"},
				Audio:      []string{"DDP"},
				Channels:   "2.0",
				Group:      "NTb",
			},
			wantMatch:   true,
			description: "real-world example: x264-NTb should match H.264-NTb",
		},
		{
			name: "VP9 does not get aliased",
			source: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"VP9"},
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"VP9"},
				Group:  "GROUP",
			},
			wantMatch:   true,
			description: "non-aliased codecs should still match when identical",
		},
		{
			name: "VP9 does NOT match x264",
			source: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"VP9"},
				Group:  "GROUP",
			},
			candidate: rls.Release{
				Title:  "Test Show",
				Series: 1,
				Source: "WEB-DL",
				Codec:  []string{"x264"},
				Group:  "GROUP",
			},
			wantMatch:   false,
			description: "VP9 and AVC are different codecs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.releasesMatch(&tt.source, &tt.candidate, false)
			if tt.wantMatch {
				require.True(t, result, tt.description)
			} else {
				require.False(t, result, tt.description)
			}
		})
	}
}
