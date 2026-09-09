// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"fmt"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
)

// detectSkippedFiles reports which torrents have at least one file set to
// "Do not download" (priority 0). Torrents whose file list could not be fetched
// get no entry, so HAS_SKIPPED_FILES never matches them.
func (s *Service) detectSkippedFiles(ctx context.Context, instanceID int, torrents []qbt.Torrent) (map[string]bool, error) {
	hashes := make([]string, 0, len(torrents))
	for _, t := range torrents {
		hashes = append(hashes, t.Hash)
	}
	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch torrent files: %w", err)
	}
	return buildSkippedFilesResult(filesByHash), nil
}

func buildSkippedFilesResult(filesByHash map[string]qbt.TorrentFiles) map[string]bool {
	result := make(map[string]bool, len(filesByHash))
	for hash, files := range filesByHash {
		if len(files) == 0 {
			continue // No metadata yet
		}
		result[hash] = slices.ContainsFunc(files, func(f qbt.TorrentFile) bool { return f.Priority == 0 })
	}
	return result
}
