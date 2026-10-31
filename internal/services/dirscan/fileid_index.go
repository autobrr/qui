// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlink"
)

// seedingIndex maps the identity of every file the instance already seeds to
// its torrent hash. Dirscan walks the qui host only, so the keys are unscoped.
type seedingIndex struct {
	byKey map[fsops.FileKey]string
	// complete is set when qBittorrent answered with the file list of every
	// completed torrent and qui read every one of those files that is on disk,
	// so a file missing from byKey is known not to seed.
	complete bool
}

func (idx *seedingIndex) len() int {
	return len(idx.byKey)
}

// hash returns the torrent seeding the file with this identity.
func (idx *seedingIndex) hash(id hardlink.FileID) (string, bool) {
	key, ok := fsops.FileKeyOf(id, nil)
	if !ok {
		return "", false
	}
	hash, ok := idx.byKey[key]
	return hash, ok
}

// knowsNotSeeding reports whether a file with this identity that is missing
// from the index is known not to seed. An index that did not build in full,
// or a file whose identity was not read, says nothing either way.
func (idx *seedingIndex) knowsNotSeeding(id hardlink.FileID) bool {
	if !idx.complete {
		return false
	}
	_, ok := fsops.FileKeyOf(id, nil)
	return ok
}

// torrentFilesReader is what the seeding index reads from qBittorrent.
type torrentFilesReader interface {
	GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error)
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	// HasSynced reports whether the instance's cache synced at least once.
	HasSynced(ctx context.Context, instanceID int) bool
}

func (s *Service) buildFileIDIndex(ctx context.Context, instanceID int, backend fsops.Backend, l *zerolog.Logger) (*seedingIndex, error) {
	start := time.Now()
	// Before the first sync the cache holds no torrents, which reads the same
	// as an instance that seeds nothing. Check before the read, so a sync that
	// lands in between cannot pass an empty read as complete.
	synced := s.torrentFiles.HasSynced(ctx, instanceID)
	torrents, err := s.torrentFiles.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get cached torrents: %w", err)
	}

	hashes, savePaths := collectCompletedTorrentSavePaths(torrents)

	index := &seedingIndex{byKey: map[fsops.FileKey]string{}, complete: synced}
	if len(hashes) == 0 {
		return index, nil
	}

	filesByHash, err := s.torrentFiles.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		return nil, fmt.Errorf("get torrent files batch: %w", err)
	}
	// The sync manager leaves out a torrent whose file list it could not
	// fetch, and keys the map by the lowercased hash.
	for _, hash := range hashes {
		if _, ok := filesByHash[strings.ToLower(strings.TrimSpace(hash))]; !ok {
			index.complete = false
			break
		}
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
			Bool("complete", index.complete).
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
			// A missing file shows the torrent no longer has it there. A file
			// qui could not read shows nothing, so the index cannot rule out
			// that it is seeded.
			if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
				index.complete = false
			}
			continue
		}

		key, ok := fsops.FileKeyOf(info.FileID, nil)
		if !ok {
			index.complete = false
			continue
		}
		index.byKey[key] = hash
	}

	return statErrors
}
