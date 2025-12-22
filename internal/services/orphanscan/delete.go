// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
