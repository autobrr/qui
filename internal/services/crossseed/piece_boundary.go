// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"

	"github.com/autobrr/qui/pkg/pathutil"
)

type PieceBoundarySafetyResult struct {
	Safe bool

	Reason string

	UnsafeBoundaries []PieceBoundaryViolation
}

type PieceBoundaryViolation struct {
	Offset int64

	PieceIndex int

	PieceStart int64

	PieceEnd int64

	ContentFile string

	IgnoredFile string
}

type TorrentFileForBoundaryCheck struct {
	Path string

	Size int64

	IsContent bool
}

func CheckPieceBoundarySafety(files []TorrentFileForBoundaryCheck, pieceLength int64) PieceBoundarySafetyResult {
	if pieceLength <= 0 {
		return PieceBoundarySafetyResult{
			Safe:   false,
			Reason: "invalid piece length",
		}
	}

	if len(files) == 0 {
		return PieceBoundarySafetyResult{
			Safe:   true,
			Reason: "no files to check",
		}
	}

	var violations []PieceBoundaryViolation
	var offset int64

	for i := range len(files) - 1 {
		currentFile := files[i]
		nextFile := files[i+1]

		offset += currentFile.Size

		if currentFile.IsContent != nextFile.IsContent {
			if offset%pieceLength != 0 {
				pieceIndex := int(offset / pieceLength)
				pieceStart := int64(pieceIndex) * pieceLength
				pieceEnd := pieceStart + pieceLength

				var contentFile, ignoredFile string
				if currentFile.IsContent {
					contentFile = currentFile.Path
					ignoredFile = nextFile.Path
				} else {
					contentFile = nextFile.Path
					ignoredFile = currentFile.Path
				}

				violations = append(violations, PieceBoundaryViolation{
					Offset:      offset,
					PieceIndex:  pieceIndex,
					PieceStart:  pieceStart,
					PieceEnd:    pieceEnd,
					ContentFile: contentFile,
					IgnoredFile: ignoredFile,
				})
			}
		}
	}

	if len(violations) > 0 {
		return PieceBoundarySafetyResult{
			Safe:             false,
			Reason:           fmt.Sprintf("found %d piece boundary violation(s) between content and ignored files", len(violations)),
			UnsafeBoundaries: violations,
		}
	}

	return PieceBoundarySafetyResult{
		Safe:   true,
		Reason: "all content/ignored transitions are piece-aligned",
	}
}

func BuildFilesForBoundaryCheck(
	info *metainfo.Info,
	isContentFile func(path string) bool,
) []TorrentFileForBoundaryCheck {
	if info == nil {
		return nil
	}

	if len(info.Files) == 0 {
		name := pathutil.TorrentPathName(info.Name)
		return []TorrentFileForBoundaryCheck{
			{
				Path:      name,
				Size:      info.Length,
				IsContent: isContentFile(name),
			},
		}
	}

	files := make([]TorrentFileForBoundaryCheck, 0, len(info.Files))
	rootName := pathutil.TorrentPathName(info.Name)
	for i := range info.Files {
		path, _ := torrentFilePath(info, &info.Files[i], rootName)
		files = append(files, TorrentFileForBoundaryCheck{
			Path:      path,
			Size:      info.Files[i].Length,
			IsContent: isContentFile(path),
		})
	}
	return files
}

func HasUnsafeIgnoredExtras(
	info *metainfo.Info,
	isIgnored func(path string) bool,
) (unsafe bool, result PieceBoundarySafetyResult) {
	if info == nil {
		return false, PieceBoundarySafetyResult{Safe: true, Reason: "nil torrent info"}
	}

	hasIgnored := false
	files := BuildFilesForBoundaryCheck(info, func(path string) bool {
		if isIgnored(path) {
			hasIgnored = true
			return false
		}
		return true
	})

	if !hasIgnored {
		return false, PieceBoundarySafetyResult{Safe: true, Reason: "no ignored files"}
	}

	result = CheckPieceBoundarySafety(files, info.PieceLength)
	return !result.Safe, result
}

func unmaterializedSourceFilePaths(sourceFiles, candidateFiles qbt.TorrentFiles) map[string]bool {
	matches, _ := matchMaterializedSourceFilesToCandidates(sourceFiles, candidateFiles)
	materialized := make(map[string]bool, len(matches))
	for _, match := range matches {
		materialized[match.sourcePath] = true
	}

	unmaterialized := make(map[string]bool)
	for _, file := range sourceFiles {
		if !materialized[file.Name] {
			unmaterialized[file.Name] = true
		}
	}
	return unmaterialized
}

func HasUnsafeUnmaterializedSourcePieces(
	info *metainfo.Info,
	sourceFiles,
	candidateFiles qbt.TorrentFiles,
) (unsafe bool, result PieceBoundarySafetyResult) {
	unmaterialized := unmaterializedSourceFilePaths(sourceFiles, candidateFiles)
	if len(unmaterialized) == 0 {
		return false, PieceBoundarySafetyResult{Safe: true, Reason: "no unmaterialized files"}
	}

	return HasUnsafeIgnoredExtras(info, func(path string) bool {
		return unmaterialized[path]
	})
}
