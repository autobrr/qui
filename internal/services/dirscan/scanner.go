// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlink"
)

// mediaExtensions defines common video/audio file extensions to scan.
var mediaExtensions = map[string]struct{}{
	// Video
	".mkv": {}, ".mp4": {}, ".avi": {}, ".m4v": {}, ".wmv": {}, ".mov": {},
	".ts": {}, ".m2ts": {}, ".vob": {}, ".mpg": {}, ".mpeg": {}, ".webm": {}, ".flv": {},
	// Audio
	".flac": {}, ".mp3": {}, ".wav": {}, ".aac": {}, ".ogg": {}, ".m4a": {},
	".wma": {}, ".ape": {}, ".alac": {}, ".dsd": {}, ".dsf": {}, ".dff": {},
	// Common torrent extras (often included in releases)
	".nfo": {}, ".sfv": {}, ".srt": {}, ".sub": {}, ".idx": {}, ".ass": {}, ".ssa": {},
}

// discLayoutMarkers identifies disc-layout directories.
var discLayoutMarkers = map[string]struct{}{
	"bdmv":     {}, // Blu-ray
	"video_ts": {}, // DVD
	"audio_ts": {}, // DVD Audio
}

// ScannedFile represents a file found during directory scanning.
type ScannedFile struct {
	Path      string          // Absolute path to the file
	RelPath   string          // Relative path from searchee root
	Size      int64           // File size in bytes
	ModTime   time.Time       // Modification time
	FileID    hardlink.FileID // Platform-specific file identifier
	LinkCount uint64          // Hardlink count
	HasLinks  bool            // True if file has multiple hardlinks (count > 1)
}

// Searchee represents a unit to search for on indexers (folder or single file).
type Searchee struct {
	Name   string         // Release name (folder or file base name)
	Path   string         // Absolute path to the searchee root
	Files  []*ScannedFile // Files in this searchee
	IsDisc bool           // True if this is a disc-layout folder
}

// ScanResult holds the results of a directory scan.
type ScanResult struct {
	Searchees    []*Searchee // Searchees found
	TotalFiles   int         // Total media files found
	TotalSize    int64       // Total size in bytes
	SkippedFiles int         // Files skipped (already seeding, etc.)
}

// Scanner walks directories and collects media files into searchees.
type Scanner struct {
	backend fsops.Backend

	// FileID index for detecting already-seeding files.
	// Maps FileID.Bytes() to torrent hash.
	seenFileIDs map[string]string
}

// NewScanner creates a new directory scanner.
func NewScanner(backend fsops.Backend) *Scanner {
	return &Scanner{
		backend:     backend,
		seenFileIDs: make(map[string]string),
	}
}

// SetFileIDIndex sets the FileID index for detecting already-seeding files.
func (s *Scanner) SetFileIDIndex(index map[string]string) {
	s.seenFileIDs = index
}

// ScanDirectory walks a directory and returns searchees.
func (s *Scanner) ScanDirectory(ctx context.Context, rootPath string) (*ScanResult, error) {
	result := &ScanResult{}
	rootPath = filepath.Clean(rootPath)

	dirEntries, err := s.backend.ReadDir(ctx, rootPath)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", rootPath, err)
	}

	for _, entry := range dirEntries {
		if ctx.Err() != nil {
			return result, fmt.Errorf("scan directory: %w", ctx.Err())
		}

		if strings.HasPrefix(entry.Name, ".") {
			continue
		}

		entryPath := filepath.Join(rootPath, entry.Name)
		s.processRootEntry(ctx, entry, entryPath, result)
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("scan directory: %w", err)
		}
	}

	return result, nil
}

// processRootEntry handles a single entry in the root directory.
func (s *Scanner) processRootEntry(ctx context.Context, entry fsops.DirEntry, entryPath string, result *ScanResult) {
	if entry.IsDir {
		s.processDirEntry(ctx, entryPath, entry.Name, result)
	} else if isMediaFile(entry.Name) {
		s.processFileEntry(ctx, entryPath, result)
	}
}

// processDirEntry scans a directory and adds it as a searchee.
func (s *Scanner) processDirEntry(ctx context.Context, entryPath, name string, result *ScanResult) {
	searchee, err := s.scanSearcheeDir(ctx, entryPath, name)
	if err != nil || len(searchee.Files) == 0 {
		return
	}

	alreadySeeding, _ := s.CheckAlreadySeeding(searchee)
	if alreadySeeding {
		result.SkippedFiles += len(searchee.Files)
	}

	result.Searchees = append(result.Searchees, searchee)
	if !alreadySeeding {
		for _, f := range searchee.Files {
			result.TotalFiles++
			result.TotalSize += f.Size
		}
	}
}

// processFileEntry scans a single file and adds it as a searchee.
func (s *Scanner) processFileEntry(ctx context.Context, entryPath string, result *ScanResult) {
	searchee, err := s.scanSingleFile(ctx, entryPath)
	if err != nil || searchee == nil {
		return
	}

	alreadySeeding, _ := s.CheckAlreadySeeding(searchee)
	if alreadySeeding {
		result.SkippedFiles++
	}

	result.Searchees = append(result.Searchees, searchee)
	if !alreadySeeding {
		result.TotalFiles++
		result.TotalSize += searchee.Files[0].Size
	}
}

// scanSearcheeDir scans a directory as a searchee using the backend's WalkDir.
func (s *Scanner) scanSearcheeDir(ctx context.Context, dirPath, name string) (*Searchee, error) {
	searchee := &Searchee{
		Name:   name,
		Path:   dirPath,
		IsDisc: s.isDiscLayoutRoot(ctx, dirPath),
	}

	walkCtx, cancelWalk := context.WithCancel(ctx)
	ch, err := s.backend.WalkDir(walkCtx, dirPath, fsops.WalkOptions{
		SkipHidden: true,
		WantFileID: true,
	})
	if err != nil {
		cancelWalk()
		return nil, fmt.Errorf("walk directory %s: %w", dirPath, err)
	}
	defer func() {
		cancelWalk()
		for range ch { //nolint:revive // drain channel to release the walk goroutine
		}
	}()

	for entry := range ch {
		if entry.Err != nil {
			if errors.Is(entry.Err, fs.ErrPermission) {
				continue
			}
			return nil, fmt.Errorf("walk entry %s: %w", entry.Path, entry.Err)
		}

		// Skip symlinks
		if entry.IsSymlink {
			continue
		}

		// Skip directories (WalkDir yields them for traversal, we only want files)
		if entry.IsDir {
			continue
		}

		// For non-disc layouts, only process media files
		if !searchee.IsDisc && !isMediaFile(filepath.Base(entry.Path)) {
			continue
		}

		relPath := entry.RelPath
		if relPath == "" {
			relPath = filepath.Base(entry.Path)
		}

		searchee.Files = append(searchee.Files, &ScannedFile{
			Path:      entry.Path,
			RelPath:   relPath,
			Size:      entry.Size,
			ModTime:   entry.ModTime,
			FileID:    entry.FileID,
			LinkCount: entry.Nlinks,
			HasLinks:  entry.Nlinks > 1,
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("walk directory %s: %w", dirPath, err)
	}

	return searchee, nil
}

// scanSingleFile creates a searchee for a single file. Stat, not Lstat:
// root-level symlinked media files are scanned via their target, unlike the
// directory walk which skips links inside a searchee.
func (s *Scanner) scanSingleFile(ctx context.Context, filePath string) (*Searchee, error) {
	info, err := s.backend.Stat(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("stat file %s: %w", filePath, err)
	}

	base := filepath.Base(filePath)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	return &Searchee{
		Name: name,
		Path: filePath,
		Files: []*ScannedFile{{
			Path:      filePath,
			RelPath:   base,
			Size:      info.Size,
			ModTime:   info.ModTime,
			FileID:    info.FileID,
			LinkCount: info.Nlinks,
			HasLinks:  info.Nlinks > 1,
		}},
	}, nil
}

// CheckAlreadySeeding checks if a searchee's files are already being seeded.
func (s *Scanner) CheckAlreadySeeding(searchee *Searchee) (allSeeding bool, torrentHash string) {
	if len(s.seenFileIDs) == 0 || len(searchee.Files) == 0 {
		return false, ""
	}

	matchedCount := 0
	for _, f := range searchee.Files {
		if f.FileID.IsZero() {
			continue
		}

		if hash, ok := s.seenFileIDs[string(f.FileID.Bytes())]; ok {
			matchedCount++
			if torrentHash == "" {
				torrentHash = hash
			}
		}
	}

	// All files must be seeding to consider the searchee as already seeding
	return matchedCount == len(searchee.Files) && matchedCount > 0, torrentHash
}

// isMediaFile checks if a filename has a media extension.
func isMediaFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	_, ok := mediaExtensions[ext]
	return ok
}

// isDiscLayoutRoot checks if a directory is a disc layout root.
func (s *Scanner) isDiscLayoutRoot(ctx context.Context, dirPath string) bool {
	entries, err := s.backend.ReadDir(ctx, dirPath)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		if _, ok := discLayoutMarkers[strings.ToLower(entry.Name)]; ok {
			return true
		}
	}

	return false
}
