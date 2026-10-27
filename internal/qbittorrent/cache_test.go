// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"

	qbt "github.com/autobrr/go-qbittorrent"
)

// Helper function to create test torrents
func createTestTorrents(count int) []qbt.Torrent {
	torrents := make([]qbt.Torrent, count)
	for i := range count {
		torrents[i] = qbt.Torrent{
			Hash:              fmt.Sprintf("hash%d", i),
			Name:              fmt.Sprintf("test-torrent-%d", i),
			Size:              int64(1000000 + i*100000), // Varying sizes
			Progress:          float64(i) / float64(count),
			DlSpeed:           int64(i * 1000),
			UpSpeed:           int64(i * 500),
			DownloadedSession: int64(i * 2000),
			UploadedSession:   int64(i * 1500),
			State:             qbt.TorrentStateDownloading,
			Category:          fmt.Sprintf("category%d", i%3),
			Tags:              fmt.Sprintf("tag%d", i%2),
			AddedOn:           int64(1600000000 + i*3600), // Different timestamps
			Ratio:             float64(i) * 0.1,
			ETA:               int64(3600 * (count - i)),
			Tracker:           fmt.Sprintf("http://tracker%d.example.com/announce", i%2),
		}
	}
	return torrents
}
