// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"bytes"
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlink"
)

type trackedFilesIndex struct {
	byPath   map[string]*models.DirScanFile
	byFileID map[fsops.FileKey]*models.DirScanFile
	instance *models.Instance
	endpoint []byte
}

func (idx *trackedFilesIndex) storedFileID(id hardlink.FileID) []byte {
	if id.IsZero() {
		return nil
	}
	stored := id.Bytes()
	if idx != nil {
		stored = append(stored, idx.endpoint...)
	}
	return stored
}

func (idx *trackedFilesIndex) keyOf(id hardlink.FileID) (fsops.FileKey, bool) {
	return fsops.FileKeyOf(id, idx.instance)
}

func (idx *trackedFilesIndex) keyOfStored(stored []byte) (fsops.FileKey, bool) {
	raw, ok := bytes.CutSuffix(stored, idx.endpoint)
	if !ok {
		return fsops.FileKey{}, false
	}
	return idx.keyOf(hardlink.FileIDFromBytes(raw))
}

func (s *Service) loadTrackedFilesIndex(ctx context.Context, directoryID int, instance *models.Instance) (*trackedFilesIndex, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}

	const pageSize = 5000

	idx := &trackedFilesIndex{
		byPath:   make(map[string]*models.DirScanFile),
		byFileID: make(map[fsops.FileKey]*models.DirScanFile),
		instance: instance,
		endpoint: fsops.WalkedEndpointTag(instance),
	}

	offset := 0
	for {
		files, err := s.store.ListFiles(ctx, directoryID, nil, pageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("list tracked files: %w", err)
		}
		if len(files) == 0 {
			break
		}

		for _, f := range files {
			if f == nil {
				continue
			}
			if f.FilePath != "" {
				idx.byPath[f.FilePath] = f
			}
			if key, ok := idx.keyOfStored(f.FileID); ok {
				idx.byFileID[key] = f
			}
		}

		offset += len(files)
	}

	return idx, nil
}

func (s *Service) refreshTrackedFilesFromScan(
	ctx context.Context,
	directoryID int,
	instance *models.Instance,
	scanResult *ScanResult,
	fileIDIndex *seedingIndex,
	l *zerolog.Logger,
) (*trackedFilesIndex, error) {
	if s == nil || s.store == nil || scanResult == nil {
		return nil, nil
	}

	idx, err := s.loadTrackedFilesIndex(ctx, directoryID, instance)
	if err != nil {
		return nil, err
	}

	for _, searchee := range scanResult.Searchees {
		if searchee == nil {
			continue
		}
		for _, scanned := range searchee.Files {
			if scanned == nil {
				continue
			}

			fileModel, err := buildTrackedFileUpsert(directoryID, scanned, idx, fileIDIndex, l)
			if err != nil {
				return nil, err
			}
			if fileModel == nil {
				continue
			}
			if err := s.store.UpsertFile(ctx, fileModel); err != nil {
				return nil, fmt.Errorf("upsert tracked file: %w", err)
			}

			idx.byPath[fileModel.FilePath] = fileModel
			if key, ok := idx.keyOfStored(fileModel.FileID); ok {
				idx.byFileID[key] = fileModel
			}
		}
	}

	return idx, nil
}

func isFileAlreadySeedingByFileID(scanned *ScannedFile, index *seedingIndex) bool {
	if scanned == nil {
		return false
	}
	_, ok := index.hash(scanned.FileID)
	return ok
}

func buildTrackedFileUpsert(
	directoryID int,
	scanned *ScannedFile,
	idx *trackedFilesIndex,
	seeding *seedingIndex,
	l *zerolog.Logger,
) (*models.DirScanFile, error) {
	if directoryID <= 0 || scanned == nil {
		return nil, nil
	}
	alreadySeeding := isFileAlreadySeedingByFileID(scanned, seeding)

	fileID := idx.storedFileID(scanned.FileID)

	existing, matchedBy := lookupTrackedFile(scanned, idx)

	status := models.DirScanFileStatusPending
	var matchedTorrentHash string
	var matchedIndexerID *int
	var searchedIndexerIDs []int
	unchanged := false

	if existing != nil {
		unchanged = existing.FileSize == scanned.Size && existing.FileModTime.Unix() == scanned.ModTime.Unix()
		if unchanged {
			status = existing.Status
			matchedTorrentHash = existing.MatchedTorrentHash
			matchedIndexerID = existing.MatchedIndexerID
			searchedIndexerIDs = existing.SearchedIndexerIDs
		}
	}

	if alreadySeeding && (existing == nil || !isFinalFileStatus(existing.Status)) {
		status = models.DirScanFileStatusAlreadySeeding
		matchedTorrentHash = ""
		matchedIndexerID = nil
		searchedIndexerIDs = nil
	}

	if existing != nil && existing.Status == models.DirScanFileStatusAlreadySeeding {
		if alreadySeeding || !seeding.knowsNotSeeding(scanned.FileID) {
			status = models.DirScanFileStatusAlreadySeeding
		} else {
			status = models.DirScanFileStatusPending
		}
	}

	if existing != nil && status != existing.Status {
		matchedTorrentHash = ""
		matchedIndexerID = nil
		searchedIndexerIDs = nil
	}

	fileModel := &models.DirScanFile{
		DirectoryID:        directoryID,
		FilePath:           scanned.Path,
		FileSize:           scanned.Size,
		FileModTime:        scanned.ModTime,
		FileID:             fileID,
		Status:             status,
		MatchedTorrentHash: matchedTorrentHash,
		MatchedIndexerID:   matchedIndexerID,
		SearchedIndexerIDs: searchedIndexerIDs,
	}

	logTrackedFileDecision(l, scanned, existing, fileModel, matchedBy, unchanged, alreadySeeding)

	return fileModel, nil
}

func lookupTrackedFile(scanned *ScannedFile, idx *trackedFilesIndex) (*models.DirScanFile, string) {
	if scanned == nil || idx == nil {
		return nil, ""
	}

	if key, ok := idx.keyOf(scanned.FileID); ok {
		if existing := idx.byFileID[key]; existing != nil {
			return existing, "file_id"
		}
	}
	if existing := idx.byPath[scanned.Path]; existing != nil {
		return existing, "path"
	}

	return nil, ""
}

func logTrackedFileDecision(
	l *zerolog.Logger,
	scanned *ScannedFile,
	existing *models.DirScanFile,
	fileModel *models.DirScanFile,
	matchedBy string,
	unchanged bool,
	alreadySeeding bool,
) {
	if l == nil || scanned == nil || fileModel == nil {
		return
	}
	if !shouldLogTrackedFileDecision(existing, fileModel, matchedBy, alreadySeeding) {
		return
	}

	existingStatus := ""
	existingPath := ""
	if existing != nil {
		existingStatus = string(existing.Status)
		existingPath = existing.FilePath
	}

	l.Debug().
		Str("path", scanned.Path).
		Str("existingPath", existingPath).
		Str("matchedBy", matchedBy).
		Bool("fileIDPresent", !scanned.FileID.IsZero()).
		Bool("alreadySeeding", alreadySeeding).
		Bool("unchanged", unchanged).
		Str("existingStatus", existingStatus).
		Str("newStatus", string(fileModel.Status)).
		Int64("size", scanned.Size).
		Time("modTime", scanned.ModTime).
		Msg("dirscan: tracked file decision")
}

func shouldLogTrackedFileDecision(
	existing *models.DirScanFile,
	fileModel *models.DirScanFile,
	matchedBy string,
	alreadySeeding bool,
) bool {
	if fileModel == nil {
		return false
	}
	if alreadySeeding || matchedBy == "file_id" {
		return true
	}
	if fileModel.Status != models.DirScanFileStatusPending {
		return true
	}
	if existing == nil {
		return false
	}

	return existing.Status != fileModel.Status || existing.FilePath != fileModel.FilePath
}

func isFinalFileStatus(status models.DirScanFileStatus) bool {
	switch status {
	case models.DirScanFileStatusMatched,
		models.DirScanFileStatusNoMatch,
		models.DirScanFileStatusAlreadySeeding,
		models.DirScanFileStatusInQBittorrent:
		return true
	case models.DirScanFileStatusPending, models.DirScanFileStatusError:
		return false
	}
	return false
}
