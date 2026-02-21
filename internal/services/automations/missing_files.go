// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"os"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

// detectMissingFiles checks which completed torrents have missing files on disk.
// Returns a map of torrent hash to missing files boolean.
func (s *Service) detectMissingFiles(ctx context.Context, instanceID int, torrents []qbt.Torrent) map[string]bool {
	result := make(map[string]bool)

	// Only completed torrents
	var completedHashes []string
	torrentByHash := make(map[string]qbt.Torrent)
	for _, t := range torrents {
		if t.Progress >= 1.0 {
			completedHashes = append(completedHashes, t.Hash)
			torrentByHash[t.Hash] = t
		}
	}

	if len(completedHashes) == 0 {
		return result
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, completedHashes)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).
			Msg("automations: failed to fetch files for missing files detection")
		return result
	}

	for hash, files := range filesByHash {
		torrent := torrentByHash[hash]
		hasMissing := false
		filesChecked := 0

		for _, f := range files {
			if f.Name == "" {
				continue
			}
			fullPath := buildFullPath(torrent.SavePath, f.Name)
			if _, err := os.Stat(fullPath); err != nil {
				if os.IsNotExist(err) {
					hasMissing = true
					break
				}
				// Log warning for other errors, continue checking
				log.Trace().Err(err).Str("path", fullPath).Str("torrent", torrent.Name).
					Msg("automations: error checking file existence")
				continue
			}
			filesChecked++
		}

		// Only set result if we checked at least one file or found missing
		if filesChecked > 0 || hasMissing {
			result[hash] = hasMissing
		}
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("completedTorrents", len(completedHashes)).
		Int("checked", len(result)).
		Msg("automations: missing files detection completed")

	return result
}
