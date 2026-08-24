// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import "testing"

func TestCalculateSizeRange_ZeroTolerance_IsExact(t *testing.T) {
	minSize, maxSize := CalculateSizeRange(1234, 0)
	if minSize != 1234 || maxSize != 1234 {
		t.Fatalf("expected exact range (1234,1234), got (%d,%d)", minSize, maxSize)
	}
}

func TestCalculateSizeRange_NonPositiveSize_IsZero(t *testing.T) {
	minSize, maxSize := CalculateSizeRange(0, 5)
	if minSize != 0 || maxSize != 0 {
		t.Fatalf("expected (0,0), got (%d,%d)", minSize, maxSize)
	}
}

// TestBuildSearchQuery pins the q dir scan sends to indexers. Dir scan used to
// append the year for movies, which returned zero results on trackers that
// search a movie database rather than release names. The year travels as the
// separate year parameter instead.
func TestBuildSearchQuery(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "The.Matrix.1999.1080p.BluRay.x264-GROUP", want: "The Matrix"},
		{name: "Some Movie (2021) {imdb-tt1234567} [1080p]", want: "Some Movie"},
		{name: "Some.Show.S01E02.1080p.WEB-DL.DDP5.1.H.264-GROUP", want: "Some Show"},
		{name: "[Fansub] Example Show - 1140 (1080p) [EEC80774]", want: "Example Show"},
		{name: "Some.Artist-Some.Album-2020-FLAC", want: "Some Artist Some Album"},
	}

	parser := NewParser(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildSearchQuery(parser.Parse(tt.name)); got != tt.want {
				t.Fatalf("buildSearchQuery(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
