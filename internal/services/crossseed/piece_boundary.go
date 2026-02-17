// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"

	"github.com/anacrolix/torrent/metainfo"
)

// PieceBoundarySafetyResult contains the outcome of a piece-boundary safety check.
type PieceBoundarySafetyResult struct {
	// Safe indicates whether missing/ignored files are safe to download.
	// True means no piece spans both content and ignored/missing bytes.
	Safe bool

	// Reason provides a human-readable explanation of the result.
	Reason string

	// UnsafeBoundaries lists byte offsets where piece boundaries are violated.
	// Only populated when Safe is false.
	UnsafeBoundaries []PieceBoundaryViolation
}

// PieceBoundaryViolation describes a specific boundary where a piece spans
// both content and ignored/missing file bytes.
type PieceBoundaryViolation struct {
	// Offset is the byte offset in the torrent where the transition occurs.
	Offset int64

	// PieceIndex is the piece that spans this boundary.
	PieceIndex int

	// PieceStart is the byte offset where the spanning piece begins.
	PieceStart int64

	// PieceEnd is the byte offset where the spanning piece ends.
	PieceEnd int64

	// ContentFile is the content file adjacent to this boundary.
	ContentFile string

	// IgnoredFile is the ignored/missing file adjacent to this boundary.
	IgnoredFile string
}

// TorrentFileForBoundaryCheck represents a file in the torrent for boundary analysis.
type TorrentFileForBoundaryCheck struct {
	// Path is the file path within the torrent.
	Path string

	// Size is the file size in bytes.
	Size int64

	// IsContent indicates whether this file is required content (true)
	// or an ignored/missing file that may be downloaded (false).
	IsContent bool
}

// CheckPieceBoundarySafety determines whether missing/ignored files can safely
// be downloaded by qBittorrent without risking data corruption to content files.
//
// The check passes (Safe=true) when every byte offset where a "content" file
// transitions to an "ignored/missing" file (or vice versa) falls exactly on
// a piece boundary. This ensures qBittorrent can download the missing pieces
// without affecting the hash of pieces containing content data.
//
// Parameters:
//   - files: ordered list of torrent files with their content/ignored status
//   - pieceLength: the piece size from torrent metadata (info.PieceLength)
//
// Returns a PieceBoundarySafetyResult indicating whether the operation is safe.
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

	// Track content vs ignored transitions
	var violations []PieceBoundaryViolation
	var offset int64

	for i := range len(files) - 1 {
		currentFile := files[i]
		nextFile := files[i+1]

		// Advance offset past current file
		offset += currentFile.Size

		// Check if there's a content/ignored transition at this boundary
		if currentFile.IsContent != nextFile.IsContent {
			// This is a transition point - check piece alignment
			if offset%pieceLength != 0 {
				// Not piece-aligned: this transition is unsafe
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

// CheckPieceBoundarySafetyFromMetainfo is a convenience wrapper that builds the
// file list from metainfo.Info and a predicate function for content detection.
//
// Parameters:
//   - info: parsed torrent info from metainfo
//   - isContentFile: predicate returning true if the file path is required content
func CheckPieceBoundarySafetyFromMetainfo(
	info *metainfo.Info,
	isContentFile func(path string) bool,
) PieceBoundarySafetyResult {
	if info == nil {
		return PieceBoundarySafetyResult{
			Safe:   false,
			Reason: "nil torrent info",
		}
	}

	files := BuildFilesForBoundaryCheck(info, isContentFile)
	return CheckPieceBoundarySafety(files, info.PieceLength)
}

// BuildFilesForBoundaryCheck constructs the file list from torrent metadata.
func BuildFilesForBoundaryCheck(
	info *metainfo.Info,
	isContentFile func(path string) bool,
) []TorrentFileForBoundaryCheck {
	if info == nil {
		return nil
	}

	// Single-file torrent
	if len(info.Files) == 0 {
		return []TorrentFileForBoundaryCheck{
			{
				Path:      info.Name,
				Size:      info.Length,
				IsContent: isContentFile(info.Name),
			},
		}
	}

	// Multi-file torrent
	// Path format must match qbt.TorrentFiles.Name (see BuildTorrentFilesFromInfo):
	// - When info.IsDir(): rootName + "/" + displayPath
	// - Otherwise: displayPath
	files := make([]TorrentFileForBoundaryCheck, 0, len(info.Files))
	for i := range info.Files {
		displayPath := info.Files[i].DisplayPath(info)
		var path string
		switch {
		case info.IsDir() && displayPath != "":
			path = info.Name + "/" + displayPath
		case displayPath != "":
			path = displayPath
		default:
			path = info.Name
		}
		files = append(files, TorrentFileForBoundaryCheck{
			Path:      path,
			Size:      info.Files[i].Length,
			IsContent: isContentFile(path),
		})
	}
	return files
}

// HasUnsafeIgnoredExtras checks whether the incoming torrent has ignored/missing
// files that share pieces with content files. This is the main entry point for
// cross-seed safety validation.
//
// Parameters:
//   - info: parsed incoming torrent info
//   - isIgnored: predicate that returns true for torrent-internal paths that
//     should be treated as "ignored/missing/non-content" for the piece-boundary
//     safety check
//
// Returns unsafe=true only when there is at least one ignored file AND at least
// one piece-boundary violation between content and ignored bytes. Otherwise returns
// unsafe=false with result.Safe=true (reason: "no ignored files" or "all content/
// ignored transitions are piece-aligned").
func HasUnsafeIgnoredExtras(
	info *metainfo.Info,
	isIgnored func(path string) bool,
) (unsafe bool, result PieceBoundarySafetyResult) {
	if info == nil {
		return false, PieceBoundarySafetyResult{Safe: true, Reason: "nil torrent info"}
	}

	// Build file list and check if there are any ignored files
	hasIgnored := false
	files := BuildFilesForBoundaryCheck(info, func(path string) bool {
		if isIgnored(path) {
			hasIgnored = true
			return false // ignored = not content
		}
		return true // not ignored = content
	})

	if !hasIgnored {
		// No ignored files, so nothing to check
		return false, PieceBoundarySafetyResult{Safe: true, Reason: "no ignored files"}
	}

	result = CheckPieceBoundarySafety(files, info.PieceLength)
	return !result.Safe, result
}
