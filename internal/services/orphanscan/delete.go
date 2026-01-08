// Copyright (c) 2025, s0up and the autobrr contributors.
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

// safeDeleteTarget removes a file OR directory with safety checks.
// For directories, it deletes recursively, but first verifies that no file within
// the directory is currently referenced by TorrentFileMap.
// Symlinks are never followed.
func safeDeleteTarget(scanRoot, target string, tfm *TorrentFileMap) (deleteDisposition, error) {
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

	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, err
	}

	// Never delete symlinks by following them.
	if info.Mode()&os.ModeSymlink != 0 {
		// For symlinks, treat as a file and remove the link itself.
		if tfm.Has(normalizePath(target)) {
			return deleteDispositionSkippedInUse, nil
		}
		if err := os.Remove(target); err != nil {
			if os.IsNotExist(err) {
				return deleteDispositionSkippedMissing, nil
			}
			return 0, err
		}
		return deleteDispositionDeleted, nil
	}

	if !info.IsDir() {
		return safeDeleteFile(scanRoot, target, tfm)
	}

	// Directory: first verify no contained file is now in use by a torrent.
	err = filepath.WalkDir(target, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}

		// Do not follow symlink directories.
		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			// Symlink files should still be checked against TorrentFileMap to avoid
			// deleting a directory that contains an in-use path.
			if tfm.Has(normalizePath(p)) {
				return fmt.Errorf("%w: %s", ErrInUse, p)
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if tfm.Has(normalizePath(p)) {
			return fmt.Errorf("%w: %s", ErrInUse, p)
		}
		return nil
	})
	if err != nil {
		// Treat in-use detection as a safe skip.
		if errors.Is(err, ErrInUse) {
			return deleteDispositionSkippedInUse, nil
		}
		return 0, err
	}

	// Now remove recursively.
	if err := os.RemoveAll(target); err != nil {
		if os.IsNotExist(err) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, err
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
