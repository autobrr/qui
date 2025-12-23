// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pathutil

import (
	"testing"
)

func TestSanitizePathSegment(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name",
			input:    "MyTracker",
			expected: "MyTracker",
		},
		{
			name:     "name with spaces",
			input:    "My Tracker",
			expected: "My Tracker",
		},
		{
			name:     "strips illegal chars",
			input:    "Tracker<>:\"/\\|?*Name",
			expected: "TrackerName",
		},
		{
			name:     "removes trailing dots",
			input:    "Tracker...",
			expected: "Tracker",
		},
		{
			name:     "removes trailing spaces",
			input:    "Tracker   ",
			expected: "Tracker",
		},
		{
			name:     "Windows reserved name CON",
			input:    "CON",
			expected: "_CON",
		},
		{
			name:     "Windows reserved name PRN",
			input:    "PRN",
			expected: "_PRN",
		},
		{
			name:     "Windows reserved name AUX",
			input:    "AUX",
			expected: "_AUX",
		},
		{
			name:     "Windows reserved name NUL",
			input:    "NUL",
			expected: "_NUL",
		},
		{
			name:     "Windows reserved name COM1",
			input:    "COM1",
			expected: "_COM1",
		},
		{
			name:     "Windows reserved name LPT1",
			input:    "LPT1",
			expected: "_LPT1",
		},
		{
			name:     "case insensitive reserved name",
			input:    "con",
			expected: "_con",
		},
		{
			name:     "reserved name not at start",
			input:    "MyCON",
			expected: "MyCON",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "_",
		},
		{
			name:     "all illegal chars",
			input:    "<>:\"/\\|?*",
			expected: "_",
		},
		{
			name:     "mixed content",
			input:    "Tracker [Private]!@#$%^&()",
			expected: "Tracker [Private]!@#$%^&()",
		},
		{
			name:     "unicode characters preserved",
			input:    "トラッカー",
			expected: "トラッカー",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizePathSegment(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizePathSegment(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTorrentKey(t *testing.T) {
	tests := []struct {
		name         string
		infohash     string
		torrentName  string
		expectedLen  int  // Expected minimum length (hash prefix + separator + sanitized name)
		expectPrefix bool // Should start with infohash prefix
	}{
		{
			name:         "simple torrent",
			infohash:     "abcdef1234567890abcdef1234567890abcdef12",
			torrentName:  "My.Movie.2024.1080p.BluRay.x264",
			expectedLen:  10, // at least hash prefix + separator
			expectPrefix: true,
		},
		{
			name:         "torrent with special chars",
			infohash:     "0123456789abcdef0123456789abcdef01234567",
			torrentName:  "Movie <with> special:chars",
			expectedLen:  10,
			expectPrefix: true,
		},
		{
			name:         "short infohash",
			infohash:     "abc",
			torrentName:  "ShortHash",
			expectedLen:  3, // uses full short hash when < 8 chars
			expectPrefix: false,
		},
		{
			name:         "empty torrent name",
			infohash:     "abcdef1234567890abcdef1234567890abcdef12",
			torrentName:  "",
			expectedLen:  8, // hash prefix only
			expectPrefix: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TorrentKey(tt.infohash, tt.torrentName)
			if len(result) < tt.expectedLen {
				t.Errorf("TorrentKey(%q, %q) = %q (len %d), want len >= %d",
					tt.infohash, tt.torrentName, result, len(result), tt.expectedLen)
			}

			// Verify no illegal path characters
			for _, c := range result {
				if c == '<' || c == '>' || c == ':' || c == '"' || c == '/' || c == '\\' || c == '|' || c == '?' || c == '*' {
					t.Errorf("TorrentKey result %q contains illegal path character %q", result, string(c))
				}
			}
		})
	}
}

func TestTorrentKeyDeterministic(t *testing.T) {
	// Same inputs should produce same output
	infohash := "abcdef1234567890abcdef1234567890abcdef12"
	name := "My.Movie.2024.1080p.BluRay.x264"

	result1 := TorrentKey(infohash, name)
	result2 := TorrentKey(infohash, name)

	if result1 != result2 {
		t.Errorf("TorrentKey should be deterministic, got %q and %q", result1, result2)
	}
}

func TestTorrentKeyUniqueness(t *testing.T) {
	// Different infohashes should produce different keys
	name := "Same.Movie.Name"

	key1 := TorrentKey("abcdef1234567890abcdef1234567890abcdef12", name)
	key2 := TorrentKey("1234567890abcdef1234567890abcdef12345678", name)

	if key1 == key2 {
		t.Errorf("TorrentKey should be unique for different infohashes, both got %q", key1)
	}
}
