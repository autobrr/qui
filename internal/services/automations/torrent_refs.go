// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import qbt "github.com/autobrr/go-qbittorrent"

func torrentRefs(torrents []qbt.Torrent) []*qbt.Torrent {
	refs := make([]*qbt.Torrent, len(torrents))
	for i := range torrents {
		refs[i] = &torrents[i]
	}
	return refs
}
