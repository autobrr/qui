// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pathutil

import (
	"strings"
	"testing"
)

// regression: #4073
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
			name:     "Windows console input device",
			input:    "CONIN$",
			expected: "_CONIN$",
		},
		{
			name:     "Windows console output device case insensitive",
			input:    "conout$",
			expected: "_conout$",
		},
		{
			name:     "Windows COM superscript device",
			input:    "COM¹",
			expected: "_COM¹",
		},
		{
			name:     "Windows LPT superscript device",
			input:    "LPT³",
			expected: "_LPT³",
		},
		{
			name:     "reserved name with space before extension",
			input:    "CON .txt",
			expected: "_CON .txt",
		},
		{
			name:     "reserved name with spaces before extension case insensitive",
			input:    "nul  .mkv",
			expected: "_nul  .mkv",
		},
		{
			name:     "non-device CON prefix",
			input:    "CONSOLE",
			expected: "CONSOLE",
		},
		{
			name:     "non-device COM number",
			input:    "COM10",
			expected: "COM10",
		},
		{
			name:     "non-device LPT prefix",
			input:    "LPT",
			expected: "LPT",
		},
		{
			name:     "COM zero is not reserved",
			input:    "COM0",
			expected: "COM0",
		},
		{
			name:     "LPT zero is not reserved",
			input:    "LPT0",
			expected: "LPT0",
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

func TestIsolationFolder(t *testing.T) {
	tests := []struct {
		name    string
		torrent string
		want    string
	}{
		{name: "normal name", torrent: "My.Movie.2024.1080p.BluRay", want: "My.Movie.2024.1080p.BluRay"},
		{name: "illegal characters", torrent: "Movie: The <Sequel>", want: "Movie The Sequel"},
		{name: "longer than 80 bytes is kept", torrent: strings.Repeat("a", 200), want: strings.Repeat("a", 200)},
		{name: "cut to 245 bytes", torrent: strings.Repeat("a", 300), want: strings.Repeat("a", 245)},
		{name: "cut never splits a character", torrent: strings.Repeat("a", 244) + "é" + "bbb", want: strings.Repeat("a", 244)},
		{name: "name over 255 bytes of 2-byte characters", torrent: strings.Repeat("é", 200), want: strings.Repeat("é", 122)},
		{name: "cut never splits a 4-byte character", torrent: strings.Repeat("a", 243) + "😀" + "bbb", want: strings.Repeat("a", 243)},
		{name: "trailing dots and spaces after the cut", torrent: strings.Repeat("a", 241) + ". .." + "tail", want: strings.Repeat("a", 241)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsolationFolder(tt.torrent); got != tt.want {
				t.Errorf("IsolationFolder(%q) = %q, want %q", tt.torrent, got, tt.want)
			}
		})
	}
}

func TestHashedIsolationFolder(t *testing.T) {
	if got := HashedIsolationFolder("ABCDEF1234567890", "Movie"); got != "Movie--abcdef12" {
		t.Errorf("HashedIsolationFolder = %q, want %q", got, "Movie--abcdef12")
	}

	// A cut name keeps room for the hash inside the 255-byte limit.
	got := HashedIsolationFolder("abcdef1234567890", strings.Repeat("é", 200))
	if want := strings.Repeat("é", 122) + "--abcdef12"; got != want {
		t.Errorf("HashedIsolationFolder = %q (%d bytes), want %q", got, len(got), want)
	}
}
