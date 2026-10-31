// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
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

	// Identities of files already seeding, for skipping them.
	seenFileIDs *seedingIndex
}

// NewScanner creates a new directory scanner.
func NewScanner(backend fsops.Backend) *Scanner {
	return &Scanner{backend: backend, seenFileIDs: &seedingIndex{}}
}

// SetFileIDIndex sets the FileID index for detecting already-seeding files.
func (s *Scanner) SetFileIDIndex(index *seedingIndex) {
	s.seenFileIDs = index
}

// rootEntry is one top-level entry of a scan while the walk runs: a folder
// that collects its files, or a root-level media file.
type rootEntry struct {
	searchee *Searchee
	failed   bool // the walk could not read the folder, or a link has no file target
	symlink  bool // a root-level link, resolved after the walk
}

// ScanDirectory walks rootPath once and returns one searchee per top-level
// folder or media file, in the order of the root listing.
func (s *Scanner) ScanDirectory(ctx context.Context, rootPath string) (*ScanResult, error) {
	d := s.backend.Paths()
	rootPath = d.Clean(rootPath)

	walkCtx, cancelWalk := context.WithCancel(ctx)
	ch, err := s.backend.WalkDir(walkCtx, rootPath, fsops.WalkOptions{
		SkipHidden: true,
		WantFileID: true,
		FollowRoot: true,
	})
	if err != nil {
		cancelWalk()
		return nil, fmt.Errorf("read directory %s: %w", rootPath, err)
	}
	defer func() {
		cancelWalk()
		for range ch { //nolint:revive // drain channel to release the walk goroutine
		}
	}()

	var order []*rootEntry
	byName := map[string]*rootEntry{} // nil for a name that is not a searchee
	for entry := range ch {
		if entry.Path == rootPath {
			if err := checkScanRoot(&entry, rootPath); err != nil {
				return nil, err
			}
			continue
		}
		// A lost connection ends the walk, so the folders it had not finished
		// would read as complete.
		if errors.Is(entry.Err, fsops.ErrConnectionLost) {
			return nil, fmt.Errorf("scan directory: %w", entry.Err)
		}

		name, rest, nested := strings.Cut(entry.RelPath, d.Separator())
		top, seen := byName[name]
		if !seen {
			top = newRootEntry(d, rootPath, name, &entry, nested)
			byName[name] = top
			if top != nil {
				order = append(order, top)
			}
		}
		switch {
		case top == nil:
		case entry.Err != nil:
			// A denied subtree is skipped, any other read error drops the folder.
			if !errors.Is(entry.Err, fs.ErrPermission) {
				top.failed = true
			}
		case nested:
			top.add(&entry, rest, d)
		}
	}
	if err := s.resolveRootSymlinks(ctx, order); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("scan directory: %w", err)
	}

	result := &ScanResult{}
	for _, top := range order {
		if top.failed {
			continue
		}
		searchee := top.searchee
		// A folder holds its other files only until the walk has shown
		// whether it is a disc layout, since the marker can come after them.
		// The clone frees the slots they took.
		if !searchee.IsDisc {
			searchee.Files = slices.Clone(slices.DeleteFunc(searchee.Files, func(f *ScannedFile) bool {
				return !isMediaFile(d.Base(f.Path))
			}))
		}
		if len(searchee.Files) == 0 {
			continue
		}
		s.addSearchee(result, searchee)
	}

	return result, nil
}

// newRootEntry starts a top-level entry from the first walk entry under its
// name, or returns nil for an entry that is not a searchee.
func newRootEntry(d fsops.PathDialect, rootPath, name string, entry *fsops.WalkEntry, nested bool) *rootEntry {
	if entry.IsDir || nested || entry.Err != nil {
		// name is cut from the walk entry's path, so it is cloned to keep
		// that path from staying alive with the searchee.
		return &rootEntry{searchee: &Searchee{Name: strings.Clone(name), Path: d.Join(rootPath, name)}}
	}
	if !isMediaFile(name) {
		return nil
	}
	// A root-level link is scanned through its target, unlike a link inside
	// a folder, which the walk skips.
	if entry.IsSymlink {
		return &rootEntry{searchee: &Searchee{Path: entry.Path}, symlink: true}
	}
	if !entry.Mode.IsRegular() {
		return nil
	}
	return &rootEntry{searchee: singleFileSearchee(entry.Path, name, &entry.LstatInfo)}
}

// add takes one entry beneath a top-level folder. rest is its path relative
// to that folder.
func (top *rootEntry) add(entry *fsops.WalkEntry, rest string, d fsops.PathDialect) {
	if entry.IsDir && !strings.Contains(rest, d.Separator()) && isDiscLayoutMarker(rest) {
		top.searchee.IsDisc = true
		return
	}
	// Links, junctions and other special entries are not media files.
	if !entry.Mode.IsRegular() {
		return
	}
	top.searchee.Files = append(top.searchee.Files, &ScannedFile{
		Path:      entry.Path,
		RelPath:   rest,
		Size:      entry.Size,
		ModTime:   entry.ModTime,
		FileID:    entry.FileID,
		LinkCount: entry.Nlinks,
		HasLinks:  entry.Nlinks > 1,
	})
}

// checkScanRoot fails the scan when the walk could not read the root or the
// root is not a directory.
func checkScanRoot(entry *fsops.WalkEntry, rootPath string) error {
	if entry.Err != nil {
		return fmt.Errorf("read directory %s: %w", rootPath, entry.Err)
	}
	if !entry.IsDir {
		return fmt.Errorf("read directory %s: not a directory", rootPath)
	}
	return nil
}

// resolveRootSymlinks stats every root-level link in one batch and keeps
// the ones whose target is a regular file. A lost connection fails the scan,
// since the links it left unread would be dropped as if they led nowhere.
func (s *Scanner) resolveRootSymlinks(ctx context.Context, order []*rootEntry) error {
	var links []*rootEntry
	var paths []string
	for _, top := range order {
		if top.symlink {
			links = append(links, top)
			paths = append(paths, top.searchee.Path)
		}
	}
	if len(paths) == 0 {
		return nil
	}

	infos, errs := fsops.StatAll(ctx, s.backend, paths)
	for i, top := range links {
		if errors.Is(errs[i], fsops.ErrConnectionLost) {
			return fmt.Errorf("scan directory: %w", errs[i])
		}
		if errs[i] != nil || !infos[i].Mode.IsRegular() {
			top.failed = true
			continue
		}
		base := s.backend.Paths().Base(paths[i])
		top.searchee = singleFileSearchee(paths[i], base, infos[i])
	}
	return nil
}

// singleFileSearchee is the searchee of one root-level media file.
func singleFileSearchee(filePath, base string, info *fsops.LstatInfo) *Searchee {
	return &Searchee{
		Name: strings.TrimSuffix(base, filepath.Ext(base)),
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
	}
}

// addSearchee adds a searchee to the result. The files of an already seeding
// searchee count as skipped.
func (s *Scanner) addSearchee(result *ScanResult, searchee *Searchee) {
	result.Searchees = append(result.Searchees, searchee)
	if alreadySeeding, _ := s.CheckAlreadySeeding(searchee); alreadySeeding {
		result.SkippedFiles += len(searchee.Files)
		return
	}
	for _, f := range searchee.Files {
		result.TotalFiles++
		result.TotalSize += f.Size
	}
}

// CheckAlreadySeeding checks if a searchee's files are already being seeded.
func (s *Scanner) CheckAlreadySeeding(searchee *Searchee) (allSeeding bool, torrentHash string) {
	if s.seenFileIDs.len() == 0 || len(searchee.Files) == 0 {
		return false, ""
	}

	matchedCount := 0
	for _, f := range searchee.Files {
		if hash, ok := s.seenFileIDs.hash(f.FileID); ok {
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

// isDiscLayoutMarker reports whether a folder with this name makes its parent
// a disc layout.
func isDiscLayoutMarker(name string) bool {
	_, ok := discLayoutMarkers[strings.ToLower(name)]
	return ok
}
