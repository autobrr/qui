// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

// detectMissingFiles checks which completed torrents have missing files on disk.
// Returns a map of torrent hash to missing files boolean, and an error if
// the backend cannot be resolved.
func (s *Service) detectMissingFiles(ctx context.Context, instanceID int, torrents []qbt.Torrent) (map[string]bool, error) {
	result := make(map[string]bool)

	// Fast path: skip backend resolution when there are no completed torrents.
	var completedHashes []string
	torrentByHash := make(map[string]qbt.Torrent)
	for _, t := range torrents {
		if t.Progress >= 1.0 {
			completedHashes = append(completedHashes, t.Hash)
			torrentByHash[t.Hash] = t
		}
	}

	if len(completedHashes) == 0 {
		return result, nil
	}

	backend, err := s.backendPool.GetBackend(ctx, instanceID)
	if err != nil {
		return result, fmt.Errorf("get backend for missing files detection: %w", err)
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, completedHashes)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).
			Msg("automations: failed to fetch files for missing files detection")
		return result, nil
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
			if _, err := backend.Stat(ctx, fullPath); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					hasMissing = true
					break
				}
				log.Trace().Err(err).Str("path", fullPath).Str("torrent", torrent.Name).
					Msg("automations: error checking file existence")
				continue
			}
			filesChecked++
		}

		if filesChecked > 0 || hasMissing {
			result[hash] = hasMissing
		}
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("completedTorrents", len(completedHashes)).
		Int("checked", len(result)).
		Msg("automations: missing files detection completed")

	return result, nil
}
