// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"path/filepath"
	"strings"
)

var videoExtensions = map[string]struct{}{
	".mkv": {}, ".mp4": {}, ".avi": {}, ".m4v": {}, ".wmv": {}, ".mov": {},
	".ts": {}, ".m2ts": {}, ".vob": {}, ".mpg": {}, ".mpeg": {}, ".webm": {}, ".flv": {},
}

func hasAnyVideoFile(files []*ScannedFile) bool {
	for _, f := range files {
		if f == nil {
			continue
		}
		if isVideoPath(f.Path) {
			return true
		}
	}
	return false
}

func selectLargestVideoFile(files []*ScannedFile) *ScannedFile {
	var best *ScannedFile
	for _, f := range files {
		if f == nil {
			continue
		}
		if !isVideoPath(f.Path) {
			continue
		}
		if best == nil || f.Size > best.Size {
			best = f
		}
	}
	return best
}

func isVideoPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := videoExtensions[ext]
	return ok
}

func shouldPreferFileMetadata(folderMeta, fileMeta *SearcheeMetadata) bool {
	if folderMeta == nil || fileMeta == nil {
		return false
	}

	// If the folder metadata indicates an explicit season search (Season set, Episode nil),
	// keep it. This is used by TV season-pack heuristics.
	if folderMeta.IsTV && folderMeta.Season != nil && folderMeta.Episode == nil {
		return false
	}

	if folderMeta.IsMusic {
		if fileMeta.Title != "" {
			return true
		}
		if !fileMeta.IsMusic {
			return true
		}
	}
	if folderMeta.Title == "" && fileMeta.Title != "" {
		return true
	}
	if folderMeta.Year == 0 && fileMeta.Year > 0 {
		return true
	}
	if !folderMeta.IsTV && fileMeta.IsTV {
		return true
	}

	return false
}

func applyFileMetadata(dst, src *SearcheeMetadata) {
	if dst == nil || src == nil {
		return
	}

	dst.Release = src.Release
	if src.Title != "" {
		dst.Title = src.Title
	}
	if src.Year > 0 {
		dst.Year = src.Year
	}
	if src.Season != nil {
		dst.Season = src.Season
	}
	if src.Episode != nil {
		dst.Episode = src.Episode
	}

	dst.IsTV = src.IsTV
	dst.IsMovie = src.IsMovie
	dst.IsMusic = src.IsMusic
}
