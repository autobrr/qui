// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"fmt"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlink"
)

// seedingIndex maps the identity of every file the instance already seeds to
// its torrent hash. Identities are scoped to the instance they were read from.
type seedingIndex struct {
	instance *models.Instance
	byKey    map[fsops.FileKey]string
}

func (idx *seedingIndex) len() int {
	return len(idx.byKey)
}

// hash returns the torrent seeding the file with this identity.
func (idx *seedingIndex) hash(id hardlink.FileID) (string, bool) {
	if id.IsZero() {
		return "", false
	}
	hash, ok := idx.byKey[fsops.FileKeyOf(id, idx.instance)]
	return hash, ok
}

func (s *Service) buildFileIDIndex(ctx context.Context, instance *models.Instance, backend fsops.Backend, l *zerolog.Logger) (*seedingIndex, error) {
	if s == nil || s.syncManager == nil {
		return nil, nil
	}

	instanceID := instance.ID
	start := time.Now()
	torrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get cached torrents: %w", err)
	}

	hashes, savePaths := collectCompletedTorrentSavePaths(torrents)

	index := &seedingIndex{instance: instance, byKey: map[fsops.FileKey]string{}}
	if len(hashes) == 0 {
		return index, nil
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		return nil, fmt.Errorf("get torrent files batch: %w", err)
	}

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
			Int("fileIDs", index.len()).
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

func addTorrentFilesToFileIDIndex(ctx context.Context, index *seedingIndex, hash, savePath string, files qbt.TorrentFiles, backend fsops.Backend) (statErrors int) {
	d := backend.Paths()
	for _, file := range files {
		absPath := d.Join(savePath, d.FromSlash(file.Name))
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
		index.byKey[fsops.FileKeyOf(info.FileID, index.instance)] = hash
	}

	return statErrors
}
