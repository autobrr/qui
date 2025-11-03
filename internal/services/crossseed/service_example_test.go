// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
)

// TestExtractBasename tests the basename extraction logic using rls parser
func TestExtractBasename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple episode",
			input:    "Show.Name.S01E05.1080p.mkv",
			expected: "S01E05",
		},
		{
			name:     "with directory",
			input:    "dir/Show.S01E05.mkv",
			expected: "S01E05",
		},
		{
			name:     "season pack",
			input:    "Show.Name.S01.1080p.mkv",
			expected: "S01",
		},
		{
			name:     "lowercase",
			input:    "show.name.s01e05.mkv",
			expected: "S01E05",
		},
		{
			name:     "multi-episode",
			input:    "Show.S01E05E06.mkv",
			expected: "S01E05", // rls parses first episode
		},
		{
			name:     "no season info",
			input:    "Movie.2020.1080p.mkv",
			expected: "",
		},
		{
			name:     "episode with group",
			input:    "Show.Name.S02E10.1080p.WEB-DL.x264-GROUP.mkv",
			expected: "S02E10",
		},
		{
			name:     "single digit season/episode",
			input:    "Show.S1E2.mkv",
			expected: "S01E02",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBasename(tt.input)
			if result != tt.expected {
				t.Errorf("extractBasename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestEnrichReleaseFromTorrent tests metadata enrichment from torrent name
func TestEnrichReleaseFromTorrent(t *testing.T) {
	tests := []struct {
		name            string
		fileName        string
		torrentName     string
		checkField      string
		expectedPresent bool
		expectedValue   string
	}{
		{
			name:            "enrich group from season pack",
			fileName:        "Show.Name.S01E05.mkv",
			torrentName:     "Show.Name.S01.1080p.WEB-DL.x264-GROUP",
			checkField:      "Group",
			expectedPresent: true,
			expectedValue:   "GROUP",
		},
		{
			name:            "enrich resolution from season pack",
			fileName:        "Show.Name.S01E05.mkv",
			torrentName:     "Show.Name.S01.1080p.BluRay.x264-GROUP",
			checkField:      "Resolution",
			expectedPresent: true,
			expectedValue:   "1080p",
		},
		{
			name:            "enrich source from season pack",
			fileName:        "Show.Name.S01E05.mkv",
			torrentName:     "Show.Name.S01.1080p.WEB-DL.x264-GROUP",
			checkField:      "Source",
			expectedPresent: true,
			expectedValue:   "WEB-DL",
		},
		{
			name:            "preserve existing group",
			fileName:        "Show.Name.S01E05.x264-ORIGINAL.mkv",
			torrentName:     "Show.Name.S01.1080p.WEB-DL.x264-DIFFERENT",
			checkField:      "Group",
			expectedPresent: true,
			expectedValue:   "ORIGINAL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileRelease := rls.ParseString(tt.fileName)
			torrentRelease := rls.ParseString(tt.torrentName)
			enriched := enrichReleaseFromTorrent(fileRelease, torrentRelease)

			switch tt.checkField {
			case "Group":
				if tt.expectedPresent && enriched.Group != tt.expectedValue {
					t.Errorf("enriched.Group = %q, want %q", enriched.Group, tt.expectedValue)
				}
			case "Resolution":
				if tt.expectedPresent && enriched.Resolution != tt.expectedValue {
					t.Errorf("enriched.Resolution = %q, want %q", enriched.Resolution, tt.expectedValue)
				}
			case "Source":
				if tt.expectedPresent && enriched.Source != tt.expectedValue {
					t.Errorf("enriched.Source = %q, want %q", enriched.Source, tt.expectedValue)
				}
			}
		})
	}
}

// TestCheckPartialMatch tests the partial matching logic
func TestCheckPartialMatch(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name     string
		subset   map[string]int64
		superset map[string]int64
		expected bool
	}{
		{
			name: "single episode in pack",
			subset: map[string]int64{
				"S01E05": 1000000000,
			},
			superset: map[string]int64{
				"S01E01": 1000000000,
				"S01E02": 1000000000,
				"S01E03": 1000000000,
				"S01E04": 1000000000,
				"S01E05": 1000000000,
				"S01E06": 1000000000,
				"S01E07": 1000000000,
			},
			expected: true,
		},
		{
			name: "multiple episodes in pack",
			subset: map[string]int64{
				"S01E05": 1000000000,
				"S01E06": 1000000000,
			},
			superset: map[string]int64{
				"S01E01": 1000000000,
				"S01E02": 1000000000,
				"S01E03": 1000000000,
				"S01E04": 1000000000,
				"S01E05": 1000000000,
				"S01E06": 1000000000,
				"S01E07": 1000000000,
			},
			expected: true,
		},
		{
			name: "no match",
			subset: map[string]int64{
				"S01E08": 1000000000,
			},
			superset: map[string]int64{
				"S01E01": 1000000000,
				"S01E02": 1000000000,
			},
			expected: false,
		},
		{
			name: "size mismatch",
			subset: map[string]int64{
				"S01E05": 1000000000,
			},
			superset: map[string]int64{
				"S01E05": 2000000000, // different size
			},
			expected: false,
		},
		{
			name: "partial match above threshold",
			subset: map[string]int64{
				"S01E01": 1000000000,
				"S01E02": 1000000000,
				"S01E03": 1000000000,
				"S01E04": 1000000000,
				"S01E05": 1000000000,
			},
			superset: map[string]int64{
				"S01E01": 1000000000,
				"S01E02": 1000000000,
				"S01E03": 1000000000,
				"S01E04": 1000000000,
				// S01E05 missing, but 4/5 = 80% matches threshold
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.checkPartialMatch(tt.subset, tt.superset)
			if result != tt.expected {
				t.Errorf("checkPartialMatch() = %v, want %v", result, tt.expected)
			}
		})
	}
}
