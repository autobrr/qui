// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import "testing"

func TestTrackerMessageMatchesDown(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		// Actual tracker down messages should match
		{
			name:    "tracker is down",
			message: "tracker is down",
			want:    true,
		},
		{
			name:    "forbidden error",
			message: "forbidden",
			want:    true,
		},
		{
			name:    "service unavailable",
			message: "service unavailable",
			want:    true,
		},
		{
			name:    "bad gateway",
			message: "bad gateway",
			want:    true,
		},
		{
			name:    "timeout",
			message: "Connection timed out",
			want:    true,
		},

		// URLs containing down-pattern words should NOT match
		{
			name:    "forbidden in URL path",
			message: "Trumped: Better Source: https://tracker.example.com/torrents/forbidden-planet-1956-1080p-x264.12345",
			want:    false,
		},
		{
			name:    "down in URL path (showdown)",
			message: "Trumped: no bloated audio: https://tracker.example.com/torrents/showdown-in-tokyo-2020-720p-x264.67890",
			want:    false,
		},
		{
			name:    "multiple URLs with down-pattern words",
			message: "See: https://site.com/forbidden-path and https://site.com/breakdown-2023",
			want:    false,
		},

		// Mix of actual error and URL should match (error is outside URL)
		{
			name:    "actual forbidden error with URL",
			message: "forbidden https://site.com/some-path",
			want:    true,
		},
		{
			name:    "down error with URL",
			message: "tracker is down - see https://site.com/status",
			want:    true,
		},

		// Edge cases
		{
			name:    "empty message",
			message: "",
			want:    false,
		},
		{
			name:    "only URL",
			message: "https://example.com/forbidden-content",
			want:    false,
		},
		{
			name:    "http URL with down in path",
			message: "http://site.com/showdown-movie.123",
			want:    false,
		},
		{
			name:    "uppercase HTTPS URL with forbidden in path",
			message: "Trumped: HTTPS://site.com/forbidden-world.123",
			want:    false,
		},
		{
			name:    "mixed case Http URL with down in path",
			message: "Http://site.com/showdown-2023.456",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrackerMessageMatchesDown(tt.message); got != tt.want {
				t.Errorf("TrackerMessageMatchesDown(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}

func TestTrackerMessageMatchesUnregistered(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		// Unregistered patterns should match
		{
			name:    "trumped with URL",
			message: "Trumped: Better Source: https://tracker.example.com/torrents/forbidden-planet-1956.12345",
			want:    true,
		},
		{
			name:    "torrent not found",
			message: "torrent not found",
			want:    true,
		},
		{
			name:    "unregistered",
			message: "Unregistered torrent",
			want:    true,
		},
		{
			name:    "nuked",
			message: "This torrent has been nuked",
			want:    true,
		},
		{
			name:    "dead",
			message: "Torrent is dead",
			want:    true,
		},

		// Non-matching messages
		{
			name:    "working tracker",
			message: "Peers: 5 seeders, 2 leechers",
			want:    false,
		},
		{
			name:    "empty message",
			message: "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrackerMessageMatchesUnregistered(tt.message); got != tt.want {
				t.Errorf("TrackerMessageMatchesUnregistered(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}
