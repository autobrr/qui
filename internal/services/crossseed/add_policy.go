// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
)

// discLayoutMarkers are directory names that indicate disc-based media (Blu-ray, DVD).
// When detected, torrents must be added paused and only auto-resumed after a full recheck.
var discLayoutMarkers = []string{"BDMV", "VIDEO_TS"}

// AddPolicy defines constraints for how a torrent should be added.
// These policies are derived from torrent content and override user settings.
type AddPolicy struct {
	// ForcePaused forces the torrent to be added in paused/stopped state
	// regardless of user's StartPaused setting.
	ForcePaused bool

	// ForceSkipAutoResume prevents any auto-resume logic from running,
	// including both immediate resume for perfect matches and queued
	// recheck-resume for alignment/extras cases.
	ForceSkipAutoResume bool

	// DiscLayout indicates this is a disc-based media torrent (Blu-ray/DVD).
	// When true, ForcePaused is set and auto-resume is only allowed after
	// a full recheck reaches 100%.
	DiscLayout bool

	// DiscMarker is the marker directory name that triggered disc layout detection
	// (e.g., "BDMV" or "VIDEO_TS"). Empty if DiscLayout is false.
	DiscMarker string
}

// PolicyForSourceFiles analyzes source files and returns an AddPolicy.
// Currently detects disc layouts (BDMV, VIDEO_TS directories).
func PolicyForSourceFiles(sourceFiles qbt.TorrentFiles) AddPolicy {
	isDisc, marker := isDiscLayoutTorrent(sourceFiles)
	if isDisc {
		return AddPolicy{
			ForcePaused:         true,
			ForceSkipAutoResume: false,
			DiscLayout:          true,
			DiscMarker:          marker,
		}
	}
	return AddPolicy{}
}

// isDiscLayoutTorrent checks if any file path contains a disc layout marker directory.
// Returns true and the matched marker if found, otherwise false and empty string.
//
// Detection is case-insensitive and matches folder names anywhere in the path,
// after normalizing path separators. Only matches exact folder names, not substrings.
// The final path segment (filename) is excluded from matching.
//
// Examples:
//   - "Movie/BDMV/index.bdmv" -> true, "BDMV"
//   - "Show/Season1/VIDEO_TS/video.vob" -> true, "VIDEO_TS"
//   - "BDMV_backup/file.txt" -> false (substring, not folder name)
//   - "movie.bdmv" -> false (file extension, not folder)
//   - "BDMV" -> false (single segment = filename only, no directory)
func isDiscLayoutTorrent(files qbt.TorrentFiles) (isDisc bool, marker string) {
	for _, f := range files {
		// Normalize Windows path separators
		path := strings.ReplaceAll(f.Name, "\\", "/")

		// Split into segments; exclude the last segment (filename)
		segments := strings.Split(path, "/")
		if len(segments) < 2 {
			// Single segment means no directory, just a filename
			continue
		}
		dirSegments := segments[:len(segments)-1]

		for _, seg := range dirSegments {
			segUpper := strings.ToUpper(seg)
			for _, m := range discLayoutMarkers {
				if segUpper == m {
					return true, m
				}
			}
		}
	}
	return false, ""
}

// ApplyToAddOptions applies policy constraints to AddTorrent options.
// If ForcePaused is set, overrides paused/stopped to "true".
func (p AddPolicy) ApplyToAddOptions(options map[string]string) {
	if p.ForcePaused {
		options["paused"] = "true"
		options["stopped"] = "true"
	}
}

// ShouldSkipAutoResume returns true if auto-resume should be skipped for this torrent.
// This includes both immediate resume (perfect match) and queued recheck-resume.
func (p AddPolicy) ShouldSkipAutoResume() bool {
	return p.ForceSkipAutoResume
}

// StatusSuffix returns a message suffix for result status when policy constraints applied.
// Returns empty string if no policy constraints are active.
func (p AddPolicy) StatusSuffix() string {
	if p.DiscLayout {
		return " - disc layout detected (" + p.DiscMarker + "), full recheck required"
	}
	return ""
}
