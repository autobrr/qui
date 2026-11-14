// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import qbt "github.com/autobrr/go-qbittorrent"

func torrentRefs(torrents []qbt.Torrent) []*qbt.Torrent {
	refs := make([]*qbt.Torrent, len(torrents))
	for i := range torrents {
		refs[i] = &torrents[i]
	}
	return refs
}

// sortTorrentValues sorts torrents in place with sortTorrents.
func sortTorrentValues(sm *SyncManager, torrents []qbt.Torrent, column string, desc, trackerHealthSupported bool, health *TrackerHealthSnapshot) {
	refs := torrentRefs(torrents)
	sm.sortTorrents(refs, column, desc, trackerHealthSupported, health)
	copy(torrents, torrentValues(refs))
}

// torrentValues returns a copy of each torrent.
func torrentValues(refs []*qbt.Torrent) []qbt.Torrent {
	torrents := make([]qbt.Torrent, len(refs))
	for i, t := range refs {
		torrents[i] = *t
	}
	return torrents
}
