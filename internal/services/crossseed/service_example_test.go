// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
)

// TestMakeReleaseKey tests the release key extraction logic using rls parser
func TestMakeReleaseKey(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name     string
		input    string
		expected releaseKey
	}{
		{
			name:  "simple episode",
			input: "Show.Name.S01E05.1080p.mkv",
			expected: releaseKey{
				series:  1,
				episode: 5,
			},
		},
		{
			name:  "with directory",
			input: "dir/Show.S01E05.mkv",
			expected: releaseKey{
				series:  1,
				episode: 5,
			},
		},
		{
			name:  "season pack",
			input: "Show.Name.S01.1080p.mkv",
			expected: releaseKey{
				series: 1,
			},
		},
		{
			name:  "lowercase",
			input: "show.name.s01e05.mkv",
			expected: releaseKey{
				series:  1,
				episode: 5,
			},
		},
		{
			name:  "multi-episode",
			input: "Show.S01E05E06.mkv",
			expected: releaseKey{
				series:  1,
				episode: 5, // rls parses first episode
			},
		},
		{
			name:  "movie with year",
			input: "Movie.2020.1080p.mkv",
			expected: releaseKey{
				year: 2020,
			},
		},
		{
			name:  "episode with group",
			input: "Show.Name.S02E10.1080p.WEB-DL.x264-GROUP.mkv",
			expected: releaseKey{
				series:  2,
				episode: 10,
			},
		},
		{
			name:  "single digit season/episode",
			input: "Show.S1E2.mkv",
			expected: releaseKey{
				series:  1,
				episode: 2,
			},
		},
		{
			name:  "date-based release",
			input: "Show.2024.01.15.1080p.mkv",
			expected: releaseKey{
				year:  2024,
				month: 1,
				day:   15,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.input)
			result := makeReleaseKey(release)
			if result != tt.expected {
				t.Errorf("makeReleaseKey(%q) = %+v, want %+v", tt.input, result, tt.expected)
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
			enriched := enrichReleaseFromTorrent(&fileRelease, &torrentRelease)

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
		subset   map[releaseKey]int64
		superset map[releaseKey]int64
		expected bool
	}{
		{
			name: "single episode in pack",
			subset: map[releaseKey]int64{
				{series: 1, episode: 5}: 1000000000,
			},
			superset: map[releaseKey]int64{
				{series: 1, episode: 1}: 1000000000,
				{series: 1, episode: 2}: 1000000000,
				{series: 1, episode: 3}: 1000000000,
				{series: 1, episode: 4}: 1000000000,
				{series: 1, episode: 5}: 1000000000,
				{series: 1, episode: 6}: 1000000000,
				{series: 1, episode: 7}: 1000000000,
			},
			expected: true,
		},
		{
			name: "multiple episodes in pack",
			subset: map[releaseKey]int64{
				{series: 1, episode: 5}: 1000000000,
				{series: 1, episode: 6}: 1000000000,
			},
			superset: map[releaseKey]int64{
				{series: 1, episode: 1}: 1000000000,
				{series: 1, episode: 2}: 1000000000,
				{series: 1, episode: 3}: 1000000000,
				{series: 1, episode: 4}: 1000000000,
				{series: 1, episode: 5}: 1000000000,
				{series: 1, episode: 6}: 1000000000,
				{series: 1, episode: 7}: 1000000000,
			},
			expected: true,
		},
		{
			name: "no match",
			subset: map[releaseKey]int64{
				{series: 1, episode: 8}: 1000000000,
			},
			superset: map[releaseKey]int64{
				{series: 1, episode: 1}: 1000000000,
				{series: 1, episode: 2}: 1000000000,
			},
			expected: false,
		},
		{
			name: "size mismatch",
			subset: map[releaseKey]int64{
				{series: 1, episode: 5}: 1000000000,
			},
			superset: map[releaseKey]int64{
				{series: 1, episode: 5}: 2000000000, // different size
			},
			expected: false,
		},
		{
			name: "partial match above threshold",
			subset: map[releaseKey]int64{
				{series: 1, episode: 1}: 1000000000,
				{series: 1, episode: 2}: 1000000000,
				{series: 1, episode: 3}: 1000000000,
				{series: 1, episode: 4}: 1000000000,
				{series: 1, episode: 5}: 1000000000,
			},
			superset: map[releaseKey]int64{
				{series: 1, episode: 1}: 1000000000,
				{series: 1, episode: 2}: 1000000000,
				{series: 1, episode: 3}: 1000000000,
				{series: 1, episode: 4}: 1000000000,
				// episode 5 missing, but 4/5 = 80% matches threshold
			},
			expected: true,
		},
		{
			name: "date-based releases match",
			subset: map[releaseKey]int64{
				{year: 2024, month: 1, day: 15}: 1000000000,
			},
			superset: map[releaseKey]int64{
				{year: 2024, month: 1, day: 15}: 1000000000,
				{year: 2024, month: 1, day: 16}: 1000000000,
			},
			expected: true,
		},
		{
			name: "year-based releases match",
			subset: map[releaseKey]int64{
				{year: 2020}: 1000000000,
			},
			superset: map[releaseKey]int64{
				{year: 2020}: 1000000000,
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
