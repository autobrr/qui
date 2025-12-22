// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// walkScanRoot walks a directory tree and returns orphan files not in the TorrentFileMap.
// Only files are returned as orphans - directories are cleaned up separately after file deletion.
func walkScanRoot(ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, maxFiles int) ([]OrphanFile, bool, error) {

	var orphans []OrphanFile
	truncated := false

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			if os.IsPermission(err) {
				return nil // Skip inaccessible, continue walk
			}
			return err
		}

		// Don't follow symlink directories
		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil // Skip symlink files too
		}

		// Skip directories entirely - they're not orphans, only files are
		if d.IsDir() {
			// But check ignore paths to skip entire subtrees
			if isIgnoredPath(path, ignorePaths) {
				return fs.SkipDir
			}
			return nil
		}

		// Check ignore paths for files (boundary-safe prefix match)
		if isIgnoredPath(path, ignorePaths) {
			return nil
		}

		// Skip if in torrent file map
		if tfm.Has(normalizePath(path)) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil // Skip files we can't stat
		}

		// Grace period check
		if time.Since(info.ModTime()) < gracePeriod {
			return nil
		}

		// Cap check
		if len(orphans) >= maxFiles {
			truncated = true
			return fs.SkipAll
		}

		orphans = append(orphans, OrphanFile{
			Path:       path,
			Size:       info.Size(),
			ModifiedAt: info.ModTime(),
			Status:     FileStatusPending,
		})
		return nil
	})

	return orphans, truncated, err
}

// isIgnoredPath checks if path matches any ignore prefix with boundary safety.
// Ensures /data/foo doesn't match /data/foobar (requires separator after prefix).
func isIgnoredPath(path string, ignorePaths []string) bool {
	for _, prefix := range ignorePaths {
		if path == prefix {
			return true
		}
		if strings.HasPrefix(path, prefix) {
			// Ensure match is at path boundary
			if len(path) > len(prefix) && path[len(prefix)] == filepath.Separator {
				return true
			}
		}
	}
	return false
}

// NormalizeIgnorePaths validates and normalizes ignore paths.
// All paths must be absolute.
func NormalizeIgnorePaths(paths []string) ([]string, error) {
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		cleaned := filepath.Clean(p)
		if !filepath.IsAbs(cleaned) {
			return nil, fmt.Errorf("ignore path must be absolute: %s", p)
		}
		result = append(result, cleaned)
	}
	return result, nil
}

// collectEmptyDirs walks the scan roots and returns empty directories (deepest first).
// This should be called after orphan files have been deleted.
func collectEmptyDirs(ctx context.Context, roots []string, ignorePaths []string) ([]string, error) {
	var emptyDirs []string

	for _, root := range roots {
		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if err != nil {
				return nil // Skip errors
			}

			if !d.IsDir() {
				return nil
			}

			if isIgnoredPath(path, ignorePaths) {
				return fs.SkipDir
			}

			// Check if directory is empty
			empty, err := isDirEmpty(path)
			if err != nil || !empty {
				return nil
			}

			emptyDirs = append(emptyDirs, path)
			return nil
		}); err != nil {
			return nil, err
		}
	}

	// Sort by path length descending (deepest first)
	sortByLengthDesc(emptyDirs)

	return emptyDirs, nil
}

// isDirEmpty checks if a directory is empty.
func isDirEmpty(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if err == nil {
		return false, nil // Has at least one entry
	}
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}

func sortByLengthDesc(paths []string) {
	sort.Slice(paths, func(i, j int) bool {
		return len(paths[i]) > len(paths[j])
	})
}
