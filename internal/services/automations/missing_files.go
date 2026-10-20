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
	// build, which can run for minutes, so the backend comes from a new read
	// of the row, which must still grant Read.
	backend, _, err := s.backendPool.Require(ctx, instanceID, models.CapabilityRead)
	if err != nil {
		return result, fmt.Errorf("failed to get backend for missing files detection: %w", err)
	}

	filesByHash, err := s.filesReader.GetTorrentFilesBatch(ctx, instanceID, completedHashes)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).
			Msg("automations: failed to fetch files for missing files detection")
		return result, fmt.Errorf("failed to fetch torrent files: %w", err)
	}

	result, connectionLost := buildMissingFilesResult(ctx, backend, torrentByHash, filesByHash)
	if connectionLost {
		log.Warn().Int("instanceID", instanceID).
			Msg("automations: connection to the filesystem lost during missing files detection, torrents with unchecked files stay unknown until the next run")
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("completedTorrents", len(completedHashes)).
		Int("checked", len(result)).
		Msg("automations: missing files detection completed")

	return result, nil
}

// buildMissingFilesResult stats every file in one fsops.StatAll. A torrent with
// an unchecked file gets no result, unless another file is confirmed missing.
//
// ponytail: no cache, so every evaluation stats every file again: 50,000
// files take 3 to 4 minutes on the sftp tier, and a preview waits as long. A
// cache is the upgrade if a field test or a user report shows a problem.
func buildMissingFilesResult(ctx context.Context, backend fsops.Backend, torrentByHash map[string]qbt.Torrent, filesByHash map[string]qbt.TorrentFiles) (result map[string]bool, connectionLost bool) {
	d := backend.Paths()

	// span is the run of paths that one torrent's files take in paths.
	type span struct {
		start, end int
		rejected   bool
	}
	spans := make(map[string]span, len(filesByHash))
	var paths []string
	for hash, files := range filesByHash {
		torrent := torrentByHash[hash]
		s := span{start: len(paths)}
		for _, f := range files {
			fullPath, ok := buildFullPath(d, torrent.SavePath, f.Name)
			if !ok {
				s.rejected = true
				continue
			}
			paths = append(paths, fullPath)
		}
		s.end = len(paths)
		spans[hash] = s
	}

	_, errs := fsops.StatAll(ctx, backend, paths)

	result = make(map[string]bool, len(spans))
	for hash, s := range spans {
		hasMissing, unchecked, filesChecked := false, s.rejected, 0
		for i := s.start; i < s.end; i++ {
			switch err := errs[i]; {
			case err == nil:
				filesChecked++
			// OpenSSH's sftp-server answers a symlink loop as no such file,
			// so a loop counts as missing on a remote instance. A local
			// os.Stat returns ELOOP, and the default case skips the file.
			case errors.Is(err, fs.ErrNotExist):
				hasMissing = true
			case errors.Is(err, fsops.ErrConnectionLost):
				unchecked, connectionLost = true, true
			default:
				log.Trace().Err(err).Str("path", paths[i]).Str("torrent", torrentByHash[hash].Name).
					Msg("automations: error checking file existence")
			}
		}

		if hasMissing || (!unchecked && filesChecked > 0) {
			result[hash] = hasMissing
		}
	}

	return result, connectionLost
}
