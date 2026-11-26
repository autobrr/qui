// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"syscall"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlink"
)

type seedingIndex struct {
	byKey            map[fsops.FileKey]string
	instance         *models.Instance
	complete         bool
	incompleteReason string
}

func (idx *seedingIndex) markIncomplete(reason string) {
	if idx.complete {
		idx.complete = false
		idx.incompleteReason = reason
	}
}

func (idx *seedingIndex) len() int {
	return len(idx.byKey)
}

func (idx *seedingIndex) hash(id hardlink.FileID) (string, bool) {
	key, ok := fsops.FileKeyOf(id, idx.instance)
	if !ok {
		return "", false
	}
	hash, ok := idx.byKey[key]
	return hash, ok
}

func (idx *seedingIndex) knowsNotSeeding(id hardlink.FileID) bool {
	if !idx.complete {
		return false
	}
	_, ok := fsops.FileKeyOf(id, idx.instance)
	return ok
}

type torrentFilesReader interface {
	GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error)
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	SeedingReady(ctx context.Context, instanceID int) error
}

func (s *Service) buildFileIDIndex(ctx context.Context, instance *models.Instance, backend fsops.Backend, l *zerolog.Logger) (*seedingIndex, error) {
	instanceID := instance.ID
	start := time.Now()
	if err := s.torrentFiles.SeedingReady(ctx, instanceID); err != nil {
		return nil, err
	}
	torrents, err := s.torrentFiles.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get cached torrents: %w", err)
	}

	hashes, savePaths := collectCompletedTorrentSavePaths(torrents)

	index := &seedingIndex{byKey: map[fsops.FileKey]string{}, instance: instance, complete: true}
	if len(hashes) == 0 {
		return index, nil
	}

	filesByHash, err := s.torrentFiles.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		return nil, fmt.Errorf("get torrent files batch: %w", err)
	}
	for _, hash := range hashes {
		files, ok := filesByHash[canonicalizeHash(hash)]
		if !ok {
			index.markIncomplete("no file list for torrent " + hash)
			break
		}
		if len(files) == 0 {
			index.markIncomplete("empty file list for torrent " + hash)
			break
		}
	}

	statErrors, err := addTorrentsToFileIDIndex(ctx, index, filesByHash, savePaths, backend)
	if err != nil {
		return nil, fmt.Errorf("read torrent files: %w", err)
	}

	if l != nil && !index.complete {
		l.Info().
			Str("reason", index.incompleteReason).
			Msg("dirscan: seeding index incomplete, already seeding files keep their status")
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

func canonicalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
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

func missingSavePaths(ctx context.Context, filesByHash map[string]qbt.TorrentFiles, savePaths map[string]string, backend fsops.Backend) (map[string]error, error) {
	distinct := make(map[string]struct{})
	for hash, files := range filesByHash {
		if savePath := savePaths[hash]; savePath != "" && len(files) > 0 {
			distinct[savePath] = struct{}{}
		}
	}
	if len(distinct) == 0 {
		return nil, nil
	}

	paths := slices.Collect(maps.Keys(distinct))
	cleaned := make([]string, len(paths))
	for i, savePath := range paths {
		cleaned[i] = backend.Paths().Clean(savePath)
	}
	infos, errs := fsops.StatAll(ctx, backend, cleaned)
	missing := make(map[string]error)
	for i, err := range errs {
		if lost := lostConnectionErr(infos[i], err); lost != nil {
			return nil, lost
		}
		if isMissingPath(err) {
			missing[paths[i]] = err
		}
	}
	return missing, nil
}

func lostConnectionErr(info *fsops.LstatInfo, err error) error {
	if errors.Is(err, fsops.ErrConnectionLost) {
		return err
	}
	if info != nil && errors.Is(info.FileIDErr, fsops.ErrConnectionLost) {
		return info.FileIDErr
	}
	return nil
}

func isMissingPath(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

type indexedFile struct {
	hash string
	file *qbt.TorrentFile
}

func addTorrentsToFileIDIndex(ctx context.Context, index *seedingIndex, filesByHash map[string]qbt.TorrentFiles, savePaths map[string]string, backend fsops.Backend) (statErrors int, err error) {
	missing, err := missingSavePaths(ctx, filesByHash, savePaths, backend)
	if err != nil {
		return 0, err
	}
	paths, owners, statErrors := indexMissingAndListRest(index, filesByHash, savePaths, missing, backend.Paths())
	if len(paths) == 0 {
		return statErrors, nil
	}

	infos, errs := fsops.StatAll(ctx, backend, paths)
	for i, absPath := range paths {
		if lost := lostConnectionErr(infos[i], errs[i]); lost != nil {
			return statErrors, lost
		}
		if errs[i] != nil {
			statErrors++
		}
		index.addStat(owners[i], absPath, infos[i], errs[i])
	}

	return statErrors, nil
}

func indexMissingAndListRest(index *seedingIndex, filesByHash map[string]qbt.TorrentFiles, savePaths map[string]string, missing map[string]error, d fsops.PathDialect) (paths []string, owners []indexedFile, statErrors int) {
	total := 0
	for _, files := range filesByHash {
		total += len(files)
	}
	paths = make([]string, 0, total)
	owners = make([]indexedFile, 0, total)
	for hash, files := range filesByHash {
		savePath := savePaths[hash]
		if savePath == "" {
			continue
		}
		savePathErr, savePathMissing := missing[savePath]
		prefix := d.Clean(savePath) + d.Separator()
		for i := range files {
			owner := indexedFile{hash: hash, file: &files[i]}
			absPath := d.Join(savePath, d.FromSlash(files[i].Name))
			if savePathMissing && strings.HasPrefix(absPath, prefix) {
				statErrors++
				index.addStat(owner, absPath, nil, savePathErr)
				continue
			}
			paths = append(paths, absPath)
			owners = append(owners, owner)
		}
	}
	return paths, owners, statErrors
}

func (idx *seedingIndex) addStat(owner indexedFile, absPath string, info *fsops.LstatInfo, err error) {
	hash, file := owner.hash, owner.file
	if err != nil {
		if idx.complete && (!isMissingPath(err) || file.Priority > 0) {
			idx.markIncomplete(fmt.Sprintf("torrent %s file %s: %v", hash, file.Name, err))
		}
		return
	}

	key, ok := fsops.FileKeyOf(info.FileID, idx.instance)
	if !ok {
		if idx.complete {
			idx.markIncomplete(fmt.Sprintf("torrent %s file %s: no file identity at %s", hash, file.Name, absPath))
		}
		return
	}
	idx.byKey[key] = hash
}
