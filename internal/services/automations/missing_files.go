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

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

// detectMissingFiles checks which completed torrents have missing files on disk.
// Returns a map of torrent hash to missing files boolean, and an error if
// the backend cannot be resolved (callers must not treat errors as "no missing files").
func (s *Service) detectMissingFiles(ctx context.Context, instanceID int, torrents []qbt.Torrent) (map[string]bool, error) {
	result := make(map[string]bool)

	// Only completed torrents — fast path before backend resolution.
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

	// The callers gate on an instance snapshot taken before the hardlink index
	// build, which can run for minutes. If local access was turned off since,
	// the backend now points at the SSH host, where qBittorrent's paths need
	// not exist, and every torrent would read as missing.
	backend, mode, err := s.backendPool.Resolve(ctx, instanceID)
	if err != nil {
		return result, fmt.Errorf("failed to get backend for missing files detection: %w", err)
	}
	if mode != models.FilesystemModeLocal {
		log.Debug().Int("instanceID", instanceID).Str("mode", string(mode)).
			Msg("automations: instance no longer has local filesystem access, skipping missing files detection")
		return result, nil
	}

	filesByHash, err := s.filesReader.GetTorrentFilesBatch(ctx, instanceID, completedHashes)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).
			Msg("automations: failed to fetch files for missing files detection")
		return result, fmt.Errorf("failed to fetch torrent files: %w", err)
	}

	result = buildMissingFilesResult(ctx, backend, torrentByHash, filesByHash)

	log.Debug().
		Int("instanceID", instanceID).
		Int("completedTorrents", len(completedHashes)).
		Int("checked", len(result)).
		Msg("automations: missing files detection completed")

	return result, nil
}

func buildMissingFilesResult(ctx context.Context, backend fsops.Backend, torrentByHash map[string]qbt.Torrent, filesByHash map[string]qbt.TorrentFiles) map[string]bool {
	result := make(map[string]bool)

	for hash, files := range filesByHash {
		torrent := torrentByHash[hash]
		hasMissing := false
		filesChecked := 0
		allPathsValid := true

		for _, f := range files {
			if f.Name == "" {
				allPathsValid = false
				continue
			}
			fullPath, ok := buildFullPath(torrent.SavePath, f.Name)
			if !ok {
				allPathsValid = false
				continue
			}
			if _, err := backend.Stat(ctx, fullPath); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
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

		// A confirmed missing file is known even if another path was rejected.
		// Only publish false when no path was rejected and at least one file was checked.
		if hasMissing || (allPathsValid && filesChecked > 0) {
			result[hash] = hasMissing
		}
	}

	return result
}
