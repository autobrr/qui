// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/qbittorrent"
)

func (s *Service) buildFileIDIndex(ctx context.Context, instanceID int, l *zerolog.Logger) (map[string]string, error) {
	if s == nil || s.syncManager == nil {
		return nil, nil
	}

	start := time.Now()
	torrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get cached torrents: %w", err)
	}

	hashes, savePaths := collectCompletedTorrentSavePaths(torrents)

	if len(hashes) == 0 {
		return map[string]string{}, nil
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		return nil, fmt.Errorf("get torrent files batch: %w", err)
	}

	backend, err := s.backendPool.GetBackend(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get backend: %w", err)
	}

	index := make(map[string]string, len(filesByHash))
	statErrors := 0
	for hash, files := range filesByHash {
		savePath := savePaths[hash]
		if savePath == "" {
			continue
		}
		statErrors += addTorrentFilesToFileIDIndex(ctx, index, hash, savePath, files, backend)
	}

	if l != nil {
		l.Debug().
			Int("torrents", len(hashes)).
			Int("fileIDs", len(index)).
			Int("statErrors", statErrors).
			Dur("took", time.Since(start)).
			Msg("dirscan: built FileID index")
	}

	return index, nil
}

func collectCompletedTorrentSavePaths(torrents []qbittorrent.CrossInstanceTorrentView) (hashes []string, savePaths map[string]string) {
	hashes = make([]string, 0, len(torrents))
	savePaths = make(map[string]string, len(torrents))

	for i := range torrents {
		t := torrents[i].Torrent
		if t.Hash == "" || t.Progress < 1.0 || t.SavePath == "" {
			continue
		}
		hashes = append(hashes, t.Hash)
		savePaths[t.Hash] = t.SavePath
	}

	return hashes, savePaths
}

func addTorrentFilesToFileIDIndex(ctx context.Context, index map[string]string, hash, savePath string, files qbt.TorrentFiles, backend fsops.Backend) (statErrors int) {
	for _, file := range files {
		absPath := filepath.Join(savePath, filepath.FromSlash(file.Name))
		// Stat, not Lstat: symlinked torrent data must index the target's
		// identity or symlink-farm setups lose already-seeding detection.
		info, err := backend.Stat(ctx, absPath)
		if err != nil {
			statErrors++
			continue
		}

		if info.FileID.IsZero() {
			continue
		}
		index[string(info.FileID.Bytes())] = hash
	}

	return statErrors
}
