// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package syncowner

import qbt "github.com/autobrr/go-qbittorrent"

// The reads in this file copy torrents out of the Sync snapshot, as the reads
// of go-qbittorrent do. They stay only while the readers move to the Sync
// snapshot (#4029), and a ratchet rule counts their callers.

func (o *Owner) GetTorrents(options qbt.TorrentFilterOptions) []qbt.Torrent {
	return o.sync.GetTorrents(options)
}

func (o *Owner) GetTorrentsUnchecked(options qbt.TorrentFilterOptions) []qbt.Torrent {
	return o.sync.GetTorrentsUnchecked(options)
}

func (o *Owner) GetTorrentsMatching(match func(*qbt.Torrent) bool) []qbt.Torrent {
	return o.sync.GetTorrentsMatching(match)
}

func (o *Owner) GetTorrentsMatchingUnchecked(match func(*qbt.Torrent) bool) []qbt.Torrent {
	return o.sync.GetTorrentsMatchingUnchecked(match)
}

func (o *Owner) GetTorrentMap(options qbt.TorrentFilterOptions) map[string]qbt.Torrent {
	return o.sync.GetTorrentMap(options)
}

func (o *Owner) GetTorrent(hash string) (qbt.Torrent, bool) {
	return o.sync.GetTorrent(hash)
}

func (o *Owner) GetTorrentUnchecked(hash string) (qbt.Torrent, bool) {
	return o.sync.GetTorrentUnchecked(hash)
}
