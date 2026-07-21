// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

// detectFileCounts fetches the number of files qBittorrent reports for each torrent.
// Returns a map of torrent hash to file count.
func (s *Service) detectFileCounts(ctx context.Context, instanceID int, torrents []qbt.Torrent) map[string]int {
	result := make(map[string]int)

	hashes := make([]string, 0, len(torrents))
	for _, t := range torrents {
		hashes = append(hashes, t.Hash)
	}

	if len(hashes) == 0 {
		return result
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).
			Msg("automations: failed to fetch files for file count detection")
		return result
	}

	for hash, files := range filesByHash {
		result[hash] = len(files)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("torrents", len(hashes)).
		Int("counted", len(result)).
		Msg("automations: file count detection completed")

	return result
}
