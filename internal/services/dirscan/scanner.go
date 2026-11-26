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

var mediaExtensions = map[string]struct{}{
	".mkv": {}, ".mp4": {}, ".avi": {}, ".m4v": {}, ".wmv": {}, ".mov": {},
	".ts": {}, ".m2ts": {}, ".vob": {}, ".mpg": {}, ".mpeg": {}, ".webm": {}, ".flv": {},
	".flac": {}, ".mp3": {}, ".wav": {}, ".aac": {}, ".ogg": {}, ".m4a": {},
	".wma": {}, ".ape": {}, ".alac": {}, ".dsd": {}, ".dsf": {}, ".dff": {},
	".nfo": {}, ".sfv": {}, ".srt": {}, ".sub": {}, ".idx": {}, ".ass": {}, ".ssa": {},
}

var discLayoutMarkers = map[string]struct{}{
	"bdmv":     {},
	"video_ts": {},
	"audio_ts": {},
}

type ScannedFile struct {
	Path      string
	RelPath   string
	Size      int64
	ModTime   time.Time
	FileID    hardlink.FileID
	LinkCount uint64
	HasLinks  bool
}

type Searchee struct {
	Name   string
	Path   string
	Files  []*ScannedFile
	IsDisc bool
}

type ScanResult struct {
	Searchees    []*Searchee
	TotalFiles   int
	TotalSize    int64
	SkippedFiles int
}

type Scanner struct {
	backend fsops.Backend

	seenFileIDs *seedingIndex
	ignorePaths []string
}

func NewScanner(backend fsops.Backend) *Scanner {
	return &Scanner{backend: backend, seenFileIDs: &seedingIndex{}}
}

func (s *Scanner) SetFileIDIndex(index *seedingIndex) {
	s.seenFileIDs = index
}

type rootEntry struct {
	searchee *Searchee
	failed   bool
	symlink  bool
}

func (s *Scanner) ScanDirectory(ctx context.Context, rootPath string) (*ScanResult, error) {
	d := s.backend.Paths()
	rootPath = d.Clean(rootPath)

	walkCtx, cancelWalk := context.WithCancel(ctx)
	ch, err := s.backend.WalkDir(walkCtx, rootPath, fsops.WalkOptions{
		SkipHidden:  true,
		IgnorePaths: s.ignorePaths,
		WantFileID:  true,
		FollowRoot:  true,
	})
	if err != nil {
		cancelWalk()
		return nil, fmt.Errorf("read directory %s: %w", rootPath, err)
	}
	defer func() {
		cancelWalk()
		for range ch {
		}
	}()

	var order []*rootEntry
	byName := map[string]*rootEntry{}
	for entry := range ch {
		if entry.Path == rootPath {
			if err := checkScanRoot(&entry, rootPath); err != nil {
				return nil, err
			}
			continue
		}
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

func newRootEntry(d fsops.PathDialect, rootPath, name string, entry *fsops.WalkEntry, nested bool) *rootEntry {
	if entry.IsDir || nested || entry.Err != nil {
		return &rootEntry{searchee: &Searchee{Name: strings.Clone(name), Path: d.Join(rootPath, name)}}
	}
	if !isMediaFile(name) {
		return nil
	}
	if entry.IsSymlink {
		return &rootEntry{searchee: &Searchee{Path: entry.Path}, symlink: true}
	}
	if !entry.Mode.IsRegular() {
		return nil
	}
	return &rootEntry{searchee: singleFileSearchee(entry.Path, name, &entry.LstatInfo)}
}

func (top *rootEntry) add(entry *fsops.WalkEntry, rest string, d fsops.PathDialect) {
	if entry.IsDir && !strings.Contains(rest, d.Separator()) && isDiscLayoutMarker(rest) {
		top.searchee.IsDisc = true
		return
	}
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

func checkScanRoot(entry *fsops.WalkEntry, rootPath string) error {
	if entry.Err != nil {
		return fmt.Errorf("read directory %s: %w", rootPath, entry.Err)
	}
	if !entry.IsDir {
		return fmt.Errorf("read directory %s: not a directory", rootPath)
	}
	return nil
}

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
		if lost := lostConnectionErr(infos[i], errs[i]); lost != nil {
			return fmt.Errorf("scan directory: %w", lost)
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

	return matchedCount == len(searchee.Files) && matchedCount > 0, torrentHash
}

func isMediaFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	_, ok := mediaExtensions[ext]
	return ok
}

func isDiscLayoutMarker(name string) bool {
	_, ok := discLayoutMarkers[strings.ToLower(name)]
	return ok
}
