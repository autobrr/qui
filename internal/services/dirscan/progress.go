// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/autobrr/qui/internal/models"
)

type trackedFilesIndex struct {
	byPath   map[string]*models.DirScanFile
	byFileID map[string]*models.DirScanFile
}

func (s *Service) loadTrackedFilesIndex(ctx context.Context, directoryID int) (*trackedFilesIndex, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}

	const pageSize = 5000

	idx := &trackedFilesIndex{
		byPath:   make(map[string]*models.DirScanFile),
		byFileID: make(map[string]*models.DirScanFile),
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
			if len(f.FileID) > 0 {
				idx.byFileID[string(f.FileID)] = f
			}
		}

		offset += len(files)
	}

	return idx, nil
}

func (s *Service) refreshTrackedFilesFromScan(
	ctx context.Context,
	directoryID int,
	scanResult *ScanResult,
	fileIDIndex map[string]string,
	l *zerolog.Logger,
) (*trackedFilesIndex, error) {
	if s == nil || s.store == nil || scanResult == nil {
		return nil, nil
	}

	idx, err := s.loadTrackedFilesIndex(ctx, directoryID)
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

			alreadySeeding := isFileAlreadySeedingByFileID(scanned, fileIDIndex)
			fileModel, err := buildTrackedFileUpsert(directoryID, scanned, idx, alreadySeeding, l)
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
			if len(fileModel.FileID) > 0 {
				idx.byFileID[string(fileModel.FileID)] = fileModel
			}
		}
	}

	return idx, nil
}

func isFileAlreadySeedingByFileID(scanned *ScannedFile, index map[string]string) bool {
	if scanned == nil || scanned.FileID.IsZero() || len(index) == 0 {
		return false
	}
	_, ok := index[string(scanned.FileID.Bytes())]
	return ok
}

func buildTrackedFileUpsert(
	directoryID int,
	scanned *ScannedFile,
	idx *trackedFilesIndex,
	alreadySeeding bool,
	l *zerolog.Logger,
) (*models.DirScanFile, error) {
	if directoryID <= 0 || scanned == nil {
		return nil, nil
	}

	var fileID []byte
	if !scanned.FileID.IsZero() {
		fileID = scanned.FileID.Bytes()
	}

	existing, matchedBy := lookupTrackedFile(scanned, idx)

	status := models.DirScanFileStatusPending
	var matchedTorrentHash string
	var matchedIndexerID *int
	unchanged := false

	if existing != nil {
		unchanged = existing.FileSize == scanned.Size && existing.FileModTime.Equal(scanned.ModTime)
		if unchanged {
			status = existing.Status
			matchedTorrentHash = existing.MatchedTorrentHash
			matchedIndexerID = existing.MatchedIndexerID
		}
	}

	// If the file is already seeding, treat it as final (unless it was already finalized
	// to a different status like matched).
	if alreadySeeding && (existing == nil || !isFinalFileStatus(existing.Status)) {
		status = models.DirScanFileStatusAlreadySeeding
		matchedTorrentHash = ""
		matchedIndexerID = nil
	}

	// If the torrent disappeared since the last scan, clear already_seeding so the file becomes eligible again.
	if existing != nil && existing.Status == models.DirScanFileStatusAlreadySeeding && !alreadySeeding {
		status = models.DirScanFileStatusPending
		matchedTorrentHash = ""
		matchedIndexerID = nil
	}

	// If the file changed on disk, clear any prior match and reprocess.
	if existing != nil && status != existing.Status {
		// status already pending; ensure match info isn't carried forward.
		matchedTorrentHash = ""
		matchedIndexerID = nil
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
	}

	logTrackedFileDecision(l, scanned, existing, fileModel, matchedBy, unchanged, alreadySeeding)

	return fileModel, nil
}

func lookupTrackedFile(scanned *ScannedFile, idx *trackedFilesIndex) (*models.DirScanFile, string) {
	if scanned == nil || idx == nil {
		return nil, ""
	}

	if !scanned.FileID.IsZero() {
		if existing := idx.byFileID[string(scanned.FileID.Bytes())]; existing != nil {
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

func searcheeIsEligible(searchee *Searchee, idx *trackedFilesIndex) bool {
	if searchee == nil || len(searchee.Files) == 0 {
		return false
	}

	for _, f := range searchee.Files {
		if f == nil {
			continue
		}

		var tracked *models.DirScanFile
		if idx != nil {
			tracked = idx.byPath[f.Path]
			if tracked == nil && !f.FileID.IsZero() {
				tracked = idx.byFileID[string(f.FileID.Bytes())]
			}
		}

		if tracked == nil {
			return true
		}
		if !isFinalFileStatus(tracked.Status) {
			return true
		}
	}

	return false
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
