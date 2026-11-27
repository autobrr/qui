// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/secrets"
)

func TrackerOrigin(announceURL string) string {
	return secrets.Origin(announceURL)
}

func HideTrackerURLs(urls []string) {
	for i, announceURL := range urls {
		urls[i] = TrackerOrigin(announceURL)
	}
}

func TrackersWithoutPasskeys(trackers []qbt.TorrentTracker) []qbt.TorrentTracker {
	hidden := slices.Clone(trackers)
	for i := range hidden {
		hidden[i].Url = TrackerOrigin(hidden[i].Url)
	}
	return hidden
}

func torrentWithoutPasskeys(t *qbt.Torrent) qbt.Torrent {
	hidden := *t
	hidden.Tracker = TrackerOrigin(t.Tracker)
	hidden.Trackers = TrackersWithoutPasskeys(t.Trackers)
	return hidden
}

func (r *TorrentResponse) withoutTrackerPasskeys() *TorrentResponse {
	hidden := *r
	hidden.Torrents = slices.Clone(r.Torrents)
	for i := range hidden.Torrents {
		hidden.Torrents[i].Torrent = new(torrentWithoutPasskeys(hidden.Torrents[i].Torrent))
	}
	hidden.CrossInstanceTorrents = slices.Clone(r.CrossInstanceTorrents)
	for i := range hidden.CrossInstanceTorrents {
		row := &hidden.CrossInstanceTorrents[i]
		view := *row.TorrentView
		view.Torrent = new(torrentWithoutPasskeys(view.Torrent))
		row.TorrentView = &view
	}
	return &hidden
}
