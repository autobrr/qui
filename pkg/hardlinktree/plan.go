// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package hardlinktree provides utilities for creating hardlink trees
// that mirror torrent file layouts for cross-seeding.
package hardlinktree

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autobrr/qui/pkg/pathutil"
)

// TorrentFile represents a file in a torrent.
type TorrentFile struct {
	Path string // Relative path within the torrent (e.g., "Folder/file.mkv")
	Size int64  // File size in bytes
}

// ExistingFile represents a file that already exists on disk.
type ExistingFile struct {
	AbsPath string // Absolute path on disk
	RelPath string // Relative path within the torrent structure
	Size    int64  // File size in bytes
}

// FilePlan describes a single hardlink to create.
type FilePlan struct {
	SourcePath string // Absolute path to existing file on disk
	TargetPath string // Absolute path where hardlink should be created
}

// TreePlan describes the complete hardlink tree to create.
type TreePlan struct {
	RootDir string     // Root directory for the hardlink tree
	Files   []FilePlan // Files to hardlink
}

// ContentLayout describes how qBittorrent organizes torrent content.
type ContentLayout string

const (
	LayoutOriginal    ContentLayout = "Original"    // Files placed as-is from torrent
	LayoutSubfolder   ContentLayout = "Subfolder"   // All files wrapped in a folder named after torrent
	LayoutNoSubfolder ContentLayout = "NoSubfolder" // Top-level folder stripped from multi-file torrent
)

// BuildPlan creates a hardlink tree plan that maps existing files to the incoming torrent's layout.
//
// Parameters:
//   - candidateFiles: Files as they appear in the INCOMING torrent (target layout)
//   - existingFiles: Files from the MATCHED torrent that already exist on disk
//   - layout: How qBittorrent will organize the torrent (from instance preferences)
//   - torrentName: Name of the incoming torrent (used for Subfolder layout)
//   - destDir: Base directory for the hardlink tree
//
// The resulting tree will match what qBittorrent expects when adding the torrent.
func BuildPlan(
	candidateFiles []TorrentFile,
	existingFiles []ExistingFile,
	layout ContentLayout,
	torrentName string,
	destDir string,
) (*TreePlan, error) {
	if len(candidateFiles) == 0 {
		return nil, errors.New("no candidate files provided")
	}
	if len(existingFiles) == 0 {
		return nil, errors.New("no existing files provided")
	}
	if destDir == "" {
		return nil, errors.New("destination directory is required")
	}

	// Validate candidate file paths for path traversal attacks
	for _, cf := range candidateFiles {
		if err := validateCandidatePath(cf.Path); err != nil {
			return nil, err
		}
	}

	// Build index of existing files by size
	existingBySize := make(map[int64][]*existingEntry)
	for i := range existingFiles {
		ef := &existingFiles[i]
		entry := &existingEntry{
			file:       ef,
			base:       strings.ToLower(fileBaseName(ef.RelPath)),
			normalized: normalizeFileKey(ef.RelPath),
		}
		existingBySize[ef.Size] = append(existingBySize[ef.Size], entry)
	}

	plan := &TreePlan{
		RootDir: destDir,
		Files:   make([]FilePlan, 0, len(candidateFiles)),
	}

	// Match each candidate file to an existing file
	for _, cf := range candidateFiles {
		bucket := existingBySize[cf.Size]
		if len(bucket) == 0 {
			return nil, errors.New("no matching file for: " + cf.Path)
		}

		candidateBase := strings.ToLower(fileBaseName(cf.Path))
		candidateNorm := normalizeFileKey(cf.Path)

		var available []*existingEntry
		for _, entry := range bucket {
			if !entry.used {
				available = append(available, entry)
			}
		}

		if len(available) == 0 {
			return nil, errors.New("no available match for: " + cf.Path)
		}

		var match *existingEntry

		// Strategy 1: Exact relative path match
		for _, entry := range available {
			if entry.file.RelPath == cf.Path {
				match = entry
				break
			}
		}

		// Strategy 2: Identical base name
		if match == nil {
			var candidates []*existingEntry
			for _, entry := range available {
				if entry.base == candidateBase {
					candidates = append(candidates, entry)
				}
			}
			if len(candidates) == 1 {
				match = candidates[0]
			}
		}

		// Strategy 3: Normalized key comparison (ignores punctuation)
		if match == nil {
			var candidates []*existingEntry
			for _, entry := range available {
				if entry.normalized == candidateNorm {
					candidates = append(candidates, entry)
				}
			}
			if len(candidates) == 1 {
				match = candidates[0]
			}
		}

		// Strategy 4: Single remaining candidate for this size
		if match == nil && len(available) == 1 {
			match = available[0]
		}

		if match == nil {
			return nil, errors.New("could not match file: " + cf.Path)
		}

		match.used = true

		// Compute target path based on layout
		targetPath := computeTargetPath(cf.Path, layout, torrentName, destDir)

		// Validate that target path is inside destDir (defense in depth)
		if err := validateTargetInsideBase(targetPath, destDir); err != nil {
			return nil, err
		}

		plan.Files = append(plan.Files, FilePlan{
			SourcePath: match.file.AbsPath,
			TargetPath: targetPath,
		})
	}

	// Sort for deterministic ordering
	sort.Slice(plan.Files, func(i, j int) bool {
		return plan.Files[i].TargetPath < plan.Files[j].TargetPath
	})

	return plan, nil
}

type existingEntry struct {
	file       *ExistingFile
	base       string
	normalized string
	used       bool
}

// computeTargetPath determines the full target path based on content layout.
func computeTargetPath(candidatePath string, layout ContentLayout, torrentName, destDir string) string {
	// Normalize path separators
	candidatePath = filepath.FromSlash(candidatePath)

	switch layout {
	case LayoutSubfolder:
		// Wrap in a folder named after the torrent
		// For single-file torrents, qBittorrent strips the extension from folder name
		folderName := torrentName
		if !strings.Contains(candidatePath, string(filepath.Separator)) {
			// Single file - strip extension for folder name
			if ext := filepath.Ext(torrentName); ext != "" {
				folderName = strings.TrimSuffix(torrentName, ext)
			}
		}
		// Sanitize folder name for Windows compatibility (removes illegal characters)
		// but keep candidatePath unchanged to match incoming torrent layout
		folderName = pathutil.SanitizePathSegment(folderName)
		return filepath.Join(destDir, folderName, candidatePath)

	case LayoutNoSubfolder:
		// Strip the first directory component if present
		parts := strings.SplitN(candidatePath, string(filepath.Separator), 2)
		if len(parts) == 2 {
			return filepath.Join(destDir, parts[1])
		}
		return filepath.Join(destDir, candidatePath)

	default: // LayoutOriginal
		return filepath.Join(destDir, candidatePath)
	}
}

// validateCandidatePath checks if a torrent file path is safe (no path traversal).
func validateCandidatePath(path string) error {
	if path == "" {
		return errors.New("empty file path in torrent")
	}

	// Reject Unix-style absolute paths on all platforms.
	// (On Windows, filepath.IsAbs("/etc/passwd") is false.)
	if strings.HasPrefix(path, "/") {
		return errors.New("absolute path not allowed in torrent: " + path)
	}

	// Normalize path
	p := filepath.Clean(filepath.FromSlash(path))

	// Reject absolute paths
	if filepath.IsAbs(p) {
		return errors.New("absolute path not allowed in torrent: " + path)
	}

	// Reject paths with Windows volume prefix
	if filepath.VolumeName(p) != "" {
		return errors.New("volume prefix not allowed in torrent path: " + path)
	}

	// Reject current directory or parent directory references
	if p == "." {
		return errors.New("current directory path not allowed: " + path)
	}
	if p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		return errors.New("parent directory traversal not allowed: " + path)
	}

	// Check each component for ".."
	for _, part := range strings.Split(p, string(filepath.Separator)) {
		if part == ".." {
			return errors.New("parent directory traversal not allowed: " + path)
		}
	}

	return nil
}

// validateTargetInsideBase ensures the target path is inside the base directory.
func validateTargetInsideBase(targetPath, baseDir string) error {
	// Get absolute paths for comparison
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return errors.New("failed to resolve target path: " + err.Error())
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return errors.New("failed to resolve base path: " + err.Error())
	}

	// Check if target is inside base
	rel, err := filepath.Rel(absBase, absTarget)
	if err != nil {
		return errors.New("failed to compute relative path: " + err.Error())
	}

	// If the relative path starts with "..", target is outside base
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return errors.New("target path escapes base directory: " + targetPath)
	}

	return nil
}

// fileBaseName extracts the base name from a path (last component).
func fileBaseName(path string) string {
	path = filepath.FromSlash(path)
	return filepath.Base(path)
}

// normalizeFileKey creates a normalized key for fuzzy file matching.
// Strips punctuation and handles double extensions (e.g., .mkv.nfo).
func normalizeFileKey(path string) string {
	base := fileBaseName(path)
	if base == "" {
		return ""
	}

	ext := ""
	if dot := strings.LastIndex(base, "."); dot >= 0 && dot < len(base)-1 {
		ext = strings.ToLower(base[dot+1:])
		base = base[:dot]
	}

	// For sidecar files, ignore intermediate video extension
	// e.g., "Name.mkv.nfo" and "Name.nfo" should match
	sidecarExts := map[string]bool{
		"nfo": true, "srt": true, "sub": true, "idx": true, "sfv": true, "txt": true,
	}
	if sidecarExts[ext] {
		if dot := strings.LastIndex(base, "."); dot >= 0 && dot < len(base)-1 {
			videoExt := strings.ToLower(base[dot+1:])
			videoExts := map[string]bool{
				"mkv": true, "mp4": true, "avi": true, "ts": true,
				"m2ts": true, "mov": true, "mpg": true, "mpeg": true,
			}
			if videoExts[videoExt] {
				base = base[:dot]
			}
		}
	}

	// Normalize: lowercase, strip non-alphanumeric
	var sb strings.Builder
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}

	if ext != "" {
		sb.WriteRune('.')
		sb.WriteString(ext)
	}

	return sb.String()
}

// HasCommonRootFolder checks if all files in the torrent share a common
// top-level directory. Returns true if a single root folder exists.
//
// Examples:
//   - ["Movie/video.mkv", "Movie/subs.srt"] → true (common root "Movie")
//   - ["video.mkv", "subs.srt"] → false (files at root, no folder)
//   - ["Movie/video.mkv", "Other/subs.srt"] → false (different root folders)
//   - ["video.mkv"] → false (single file at root)
func HasCommonRootFolder(files []TorrentFile) bool {
	if len(files) == 0 {
		return false
	}

	var commonRoot string
	for i, f := range files {
		path := filepath.FromSlash(f.Path)
		parts := strings.SplitN(path, string(filepath.Separator), 2)

		// Check if file has a directory component
		if len(parts) < 2 {
			// File at root level, no folder structure
			return false
		}

		root := parts[0]
		if i == 0 {
			commonRoot = root
		} else if root != commonRoot {
			// Different root folders
			return false
		}
	}

	return commonRoot != ""
}
