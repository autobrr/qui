// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	// endpoint ends every stored remote identity, see endpointTag.
	endpoint []byte
}

// endpointTag names the endpoint a stored identity was read through, since the
// same device and inode on another server is another file. It lives in the row
// so a run that overlaps an SSH edit, which still writes the old identity, needs
// no cleanup. A new tag costs a row its rename detection until its next walk. On a
// host with no identity, an already_seeding row stays so until Reset Scan Progress.
func endpointTag(instance *models.Instance) []byte {
	if instance == nil {
		return nil
	}
	endpoint := fsops.WalkedEndpoint(instance)
	if endpoint == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(endpoint))
	return sum[:8]
}

// storedFileID is the dir_scan_files.file_id value for id.
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

// keyOfStored decodes a dir_scan_files.file_id value. A row from before the
// tagged form or from another endpoint decodes to no identity and is never indexed.
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
		endpoint: endpointTag(instance),
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

			// Keep the in-memory index in sync for eligibility filtering.
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
		// Whole seconds, because the database column and the filesystem keep different precision.
		unchanged = existing.FileSize == scanned.Size && existing.FileModTime.Unix() == scanned.ModTime.Unix()
		if unchanged {
			status = existing.Status
			matchedTorrentHash = existing.MatchedTorrentHash
			matchedIndexerID = existing.MatchedIndexerID
			searchedIndexerIDs = existing.SearchedIndexerIDs
		}
	}

	// If the file is already seeding, treat it as final (unless it was already finalized
	// to a different status like matched).
	if alreadySeeding && (existing == nil || !isFinalFileStatus(existing.Status)) {
		status = models.DirScanFileStatusAlreadySeeding
		matchedTorrentHash = ""
		matchedIndexerID = nil
		searchedIndexerIDs = nil
	}

	// A file already seeding stays so, even when it changed on disk, until the
	// index shows the torrent is gone. Only an index that built in full,
	// checked against the file's identity, shows that. Then the file becomes
	// eligible again. The match fields are empty here: a changed file never
	// copied them, and every already_seeding row is stored without them.
	if existing != nil && existing.Status == models.DirScanFileStatusAlreadySeeding {
		if alreadySeeding || !seeding.knowsNotSeeding(scanned.FileID) {
			status = models.DirScanFileStatusAlreadySeeding
		} else {
			status = models.DirScanFileStatusPending
		}
	}

	// If the file changed on disk, clear any prior match and reprocess.
	if existing != nil && status != existing.Status {
		// status already pending; ensure match info isn't carried forward.
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
