// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

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
		{
			name:    "repack available or grab internal",
			message: "Other: Repack available, or grab internal:",
			want:    true,
		},
		{
			name:    "torrent has been rejected",
			message: "Torrent has been rejected.",
			want:    true,
		},
		{
			name:    "unknown outside the libtorrent HTTP error",
			message: "Unknown torrent",
			want:    true,
		},

		// Non-matching messages
		{
			name:    "libtorrent unknown HTTP error",
			message: "504 (unknown HTTP error)",
			want:    false,
		},
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

func TestTrackerHealthOf(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	addedAgo := func(d time.Duration) int64 { return now.Add(-d).Unix() }
	tracker := func(status qbt.TrackerStatus, message string) qbt.TorrentTracker {
		return qbt.TorrentTracker{Status: status, Message: message}
	}
	old := addedAgo(2 * time.Hour)

	tests := []struct {
		name     string
		addedOn  int64
		trackers []qbt.TorrentTracker
		want     TrackerHealth
	}{
		{name: "no tracker data", addedOn: old},
		{name: "only DHT and PeX entries", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusDisabled, "")}},
		{name: "one working tracker", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusOK, "")}},

		// Unregistered and the grace hour.
		{name: "unregistered after the grace hour", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusTrackerError, "Torrent not registered on tracker")}, want: TrackerHealthUnregistered},
		{name: "unregistered added 5 minutes ago", addedOn: addedAgo(5 * time.Minute), trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusTrackerError, "Torrent not registered on tracker")}},
		{name: "unregistered added 59 minutes ago", addedOn: addedAgo(59 * time.Minute), trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Unregistered torrent")}},
		{name: "unregistered added 61 minutes ago", addedOn: addedAgo(61 * time.Minute), trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Unregistered torrent")}, want: TrackerHealthUnregistered},
		{name: "unregistered added exactly one hour ago", addedOn: addedAgo(time.Hour), trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Unregistered torrent")}, want: TrackerHealthUnregistered},
		{name: "unregistered with no added time", trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Unregistered torrent")}, want: TrackerHealthUnregistered},
		// regression: #3616
		{name: "unregistered tracker error added 10 minutes ago", addedOn: addedAgo(10 * time.Minute), trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusTrackerError, "Unregistered torrent")}},
		{name: "unregistered not working", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Torrent not registered on tracker")}, want: TrackerHealthUnregistered},
		{name: "unregistered while updating", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusUpdating, "Torrent not registered on tracker")}, want: TrackerHealthUnregistered},
		{name: "unregistered while updating beside a working tracker", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusUpdating, "Torrent not registered on tracker"), tracker(qbt.TrackerStatusOK, "")}},
		{name: "down keyword inside a URL", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Trumped: https://tracker.example.com/torrents/example-show-2025-s02e07-the-final-countdown-1080p")}, want: TrackerHealthUnregistered},
		{name: "unregistered inside the grace hour beside an unreachable tracker", addedOn: addedAgo(10 * time.Minute), trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Unregistered torrent"), tracker(qbt.TrackerStatusUnreachable, "")}, want: TrackerHealthDown},
		{name: "unregistered and down in one message inside the grace hour", addedOn: addedAgo(10 * time.Minute), trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Unregistered torrent, tracker is down")}, want: TrackerHealthDown},

		// Tracker down.
		{name: "unreachable", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusUnreachable, "Connection timed out")}, want: TrackerHealthDown},
		{name: "down message", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Tracker is down for maintenance")}, want: TrackerHealthDown},
		{name: "HTTP status without a reason phrase", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "429 (unknown HTTP error)")}, want: TrackerHealthDown},
		{name: "down message while updating", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusUpdating, "Tracker is down for maintenance")}},
		{name: "down beside a working tracker", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Tracker is down for maintenance"), tracker(qbt.TrackerStatusOK, "")}},

		// Tracker error.
		{name: "every tracker errored", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Invalid tracker response payload"), tracker(qbt.TrackerStatusTrackerError, "Tracker reply failed checksum validation")}, want: TrackerHealthError},
		{name: "error beside a working tracker", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Invalid tracker response payload"), tracker(qbt.TrackerStatusOK, "")}},
		{name: "error beside an updating tracker", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotWorking, "Invalid tracker response payload"), tracker(qbt.TrackerStatusUpdating, "")}},

		// Odd verdicts of today, kept on purpose. A fix changes these rows.
		{name: "not contacted beside a timed out tracker", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotContacted, ""), tracker(qbt.TrackerStatusNotWorking, "timed out")}, want: TrackerHealthDown},
		{name: "not contacted beside an errored tracker", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusNotContacted, ""), tracker(qbt.TrackerStatusNotWorking, "ratio too low")}},
		{name: "updating with an unregistered message", addedOn: old, trackers: []qbt.TorrentTracker{tracker(qbt.TrackerStatusUpdating, "Unregistered torrent")}, want: TrackerHealthUnregistered},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrent := &qbt.Torrent{AddedOn: tt.addedOn, Trackers: tt.trackers}
			if got := trackerHealthOf(torrent, now); got != tt.want {
				t.Errorf("trackerHealthOf() = %q, want %q", got, tt.want)
			}
		})
	}

	if got := trackerHealthOf(nil, now); got != "" {
		t.Errorf("trackerHealthOf(nil) = %q, want no health", got)
	}
}
