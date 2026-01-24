// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// FileID index for detecting already-seeding files.
	// Maps FileID.Bytes() to torrent hash.
	seenFileIDs map[string]string
}

// NewScanner creates a new directory scanner.
func NewScanner() *Scanner {
	return &Scanner{
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

	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", rootPath, err)
	}

	for _, entry := range entries {
		if ctx.Err() != nil {
			return result, fmt.Errorf("scan directory: %w", ctx.Err())
		}

		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		entryPath := filepath.Join(rootPath, entry.Name())
		s.processRootEntry(ctx, entry, entryPath, result)
	}

	return result, nil
}

// processRootEntry handles a single entry in the root directory.
func (s *Scanner) processRootEntry(ctx context.Context, entry fs.DirEntry, entryPath string, result *ScanResult) {
	if entry.IsDir() {
		s.processDirEntry(ctx, entry, entryPath, result)
	} else if isMediaFile(entry.Name()) {
		s.processFileEntry(entryPath, result)
	}
}

// processDirEntry scans a directory and adds it as a searchee.
func (s *Scanner) processDirEntry(ctx context.Context, entry fs.DirEntry, entryPath string, result *ScanResult) {
	searchee, err := s.scanSearcheeDir(ctx, entryPath, entry.Name())
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
func (s *Scanner) processFileEntry(entryPath string, result *ScanResult) {
	searchee, err := s.scanSingleFile(entryPath)
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

// scanSearcheeDir scans a directory as a searchee.
func (s *Scanner) scanSearcheeDir(ctx context.Context, dirPath, name string) (*Searchee, error) {
	searchee := &Searchee{
		Name:   name,
		Path:   dirPath,
		IsDisc: isDiscLayoutRoot(dirPath),
	}

	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, walkErr error) error {
		return s.walkDirEntry(ctx, path, d, walkErr, searchee)
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory %s: %w", dirPath, err)
	}

	return searchee, nil
}

// walkDirEntry handles a single entry in the WalkDir callback.
func (s *Scanner) walkDirEntry(ctx context.Context, path string, d fs.DirEntry, walkErr error, searchee *Searchee) error {
	if walkErr != nil {
		if os.IsPermission(walkErr) {
			return nil
		}
		return fmt.Errorf("walk entry %s: %w", path, walkErr)
	}

	if ctx.Err() != nil {
		return fmt.Errorf("walk canceled: %w", ctx.Err())
	}

	if shouldSkipEntry(d) {
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}

	if !shouldProcessFile(d, searchee.IsDisc) {
		return nil
	}

	return s.addFileToSearchee(path, d, searchee)
}

// shouldSkipEntry checks if an entry should be skipped entirely.
func shouldSkipEntry(d fs.DirEntry) bool {
	// Skip hidden files/directories
	if strings.HasPrefix(d.Name(), ".") {
		return true
	}
	// Skip symlinks
	if d.Type()&fs.ModeSymlink != 0 {
		return true
	}
	return false
}

// shouldProcessFile checks if a file should be processed.
func shouldProcessFile(d fs.DirEntry, isDisc bool) bool {
	// Only process regular files
	if d.IsDir() {
		return false
	}
	// For disc layouts, keep all files; otherwise only media files
	return isDisc || isMediaFile(d.Name())
}

// addFileToSearchee adds a file to the searchee's file list.
func (s *Scanner) addFileToSearchee(path string, d fs.DirEntry, searchee *Searchee) error {
	fi, err := d.Info()
	if err != nil {
		return nil //nolint:nilerr // skip files we can't stat
	}

	fileID, linkCount := getFileIDSafe(fi, path)

	relPath, err := filepath.Rel(searchee.Path, path)
	if err != nil {
		relPath = filepath.Base(path) // fallback to base name
	}

	searchee.Files = append(searchee.Files, &ScannedFile{
		Path:      path,
		RelPath:   relPath,
		Size:      fi.Size(),
		ModTime:   fi.ModTime(),
		FileID:    fileID,
		LinkCount: linkCount,
		HasLinks:  linkCount > 1,
	})

	return nil
}

// getFileIDSafe gets the FileID, returning zero value on error.
func getFileIDSafe(fi os.FileInfo, path string) (fileID hardlink.FileID, linkCount uint64) {
	fileID, linkCount, err := hardlink.GetFileID(fi, path)
	if err != nil {
		return hardlink.FileID{}, 1
	}
	return fileID, linkCount
}

// scanSingleFile creates a searchee for a single file.
func (s *Scanner) scanSingleFile(filePath string) (*Searchee, error) {
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat file %s: %w", filePath, err)
	}

	fileID, linkCount := getFileIDSafe(fi, filePath)
	base := filepath.Base(filePath)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	return &Searchee{
		Name: name,
		Path: filePath,
		Files: []*ScannedFile{{
			Path:      filePath,
			RelPath:   base,
			Size:      fi.Size(),
			ModTime:   fi.ModTime(),
			FileID:    fileID,
			LinkCount: linkCount,
			HasLinks:  linkCount > 1,
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
func isDiscLayoutRoot(dirPath string) bool {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, ok := discLayoutMarkers[strings.ToLower(entry.Name())]; ok {
			return true
		}
	}

	return false
}
