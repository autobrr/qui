// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrInUse indicates a deletion target contains files currently in use by torrents.
var ErrInUse = errors.New("contains in-use torrent file")

type deleteDisposition int

const (
	deleteDispositionDeleted deleteDisposition = iota
	deleteDispositionSkippedInUse
	deleteDispositionSkippedMissing
	deleteDispositionSkippedIgnored
)

// safeDeleteFile removes a single file with safety checks.
// Re-checks TorrentFileMap before deletion to handle torrents added since scan.
// Never removes directories.
func safeDeleteFile(scanRoot, target string, tfm *TorrentFileMap) (deleteDisposition, error) {
	// Must be absolute
	if !filepath.IsAbs(target) {
		return 0, fmt.Errorf("refusing non-absolute path: %s", target)
	}

	// Must not be the scan root itself
	if filepath.Clean(target) == filepath.Clean(scanRoot) {
		return 0, fmt.Errorf("refusing to delete scan root: %s", scanRoot)
	}

	// Must be within scan root (no path traversal)
	rel, err := filepath.Rel(scanRoot, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return 0, fmt.Errorf("path escapes scan root: %s", target)
	}

	// Re-check: torrent may have been added since scan (skip)
	if tfm.Has(normalizePath(target)) {
		return deleteDispositionSkippedInUse, nil
	}

	// Verify it's actually a file (not a directory)
	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("refusing to delete directory as file: %s", target)
	}

	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, err
	}
	return deleteDispositionDeleted, nil
}

// validateDeleteTarget checks that target is a valid deletion candidate.
func validateDeleteTarget(scanRoot, target string) error {
	if !filepath.IsAbs(target) {
		return fmt.Errorf("refusing non-absolute path: %s", target)
	}
	if filepath.Clean(target) == filepath.Clean(scanRoot) {
		return fmt.Errorf("refusing to delete scan root: %s", scanRoot)
	}
	rel, err := filepath.Rel(scanRoot, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path escapes scan root: %s", target)
	}
	return nil
}

// checkDirContainsInUseFile walks a directory and returns ErrInUse if any file is in the TorrentFileMap.
func checkDirContainsInUseFile(target string, tfm *TorrentFileMap) error {
	err := filepath.WalkDir(target, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}

		if d.IsDir() {
			return nil
		}

		return checkFileInUse(p, tfm)
	})
	if err != nil {
		return fmt.Errorf("walk directory: %w", err)
	}
	return nil
}

func checkFileInUse(path string, tfm *TorrentFileMap) error {
	if tfm.Has(normalizePath(path)) {
		return fmt.Errorf("%w: %s", ErrInUse, path)
	}
	return nil
}

// safeDeleteTarget removes a file OR directory with safety checks.
// For directories, it deletes recursively, but first verifies that no file within
// the directory is currently referenced by TorrentFileMap or protected by ignorePaths.
// Symlinks are never followed.
func safeDeleteTarget(scanRoot, target string, tfm *TorrentFileMap, ignorePaths []string) (deleteDisposition, error) {
	if err := validateDeleteTarget(scanRoot, target); err != nil {
		return 0, err
	}
	if len(ignorePaths) > 0 && isPathProtectedByIgnorePaths(target, ignorePaths) {
		return deleteDispositionSkippedIgnored, nil
	}

	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, fmt.Errorf("stat target: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return safeDeleteSymlink(target, tfm)
	}
	if !info.IsDir() {
		return safeDeleteFile(scanRoot, target, tfm)
	}
	return safeDeleteDirectory(target, tfm)
}

func safeDeleteSymlink(target string, tfm *TorrentFileMap) (deleteDisposition, error) {
	if tfm.Has(normalizePath(target)) {
		return deleteDispositionSkippedInUse, nil
	}
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, fmt.Errorf("remove symlink: %w", err)
	}
	return deleteDispositionDeleted, nil
}

func safeDeleteDirectory(target string, tfm *TorrentFileMap) (deleteDisposition, error) {
	if err := checkDirContainsInUseFile(target, tfm); err != nil {
		if errors.Is(err, ErrInUse) {
			return deleteDispositionSkippedInUse, nil
		}
		return 0, fmt.Errorf("check directory contents: %w", err)
	}

	if err := os.RemoveAll(target); err != nil {
		if os.IsNotExist(err) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, fmt.Errorf("remove directory: %w", err)
	}
	return deleteDispositionDeleted, nil
}

// safeDeleteEmptyDir removes a directory only if empty. Never recursive.
func safeDeleteEmptyDir(scanRoot, target string) error {
	// Must be absolute
	if !filepath.IsAbs(target) {
		return fmt.Errorf("refusing non-absolute path: %s", target)
	}

	// Must not be the scan root itself
	if filepath.Clean(target) == filepath.Clean(scanRoot) {
		return fmt.Errorf("refusing to delete scan root: %s", scanRoot)
	}

	// Must be within scan root (no path traversal)
	rel, err := filepath.Rel(scanRoot, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path escapes scan root: %s", target)
	}

	// os.Remove on a directory only succeeds if it's empty
	err = os.Remove(target)
	if os.IsNotExist(err) {
		return nil // Already gone
	}
	return err
}

func collectCandidateDirsForCleanup(files []string, scanRoots []string, ignorePaths []string) []string {
	candidates := make(map[string]struct{})
	for _, filePath := range files {
		scanRoot := findScanRoot(filePath, scanRoots)
		if scanRoot == "" {
			continue
		}
		scanRoot = filepath.Clean(scanRoot)

		dir := filepath.Clean(filepath.Dir(filePath))
		for dir != scanRoot {
			if dir == "." || dir == string(filepath.Separator) {
				break
			}
			if isIgnoredPath(dir, ignorePaths) {
				break
			}
			candidates[dir] = struct{}{}

			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	ordered := make([]string, 0, len(candidates))
	for dir := range candidates {
		ordered = append(ordered, dir)
	}

	sort.Slice(ordered, func(i, j int) bool {
		return len(ordered[i]) > len(ordered[j])
	})

	return ordered
}
