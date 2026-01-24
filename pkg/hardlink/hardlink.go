// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package hardlink provides filesystem hardlink detection utilities.
package hardlink

import (
	"os"
	"path/filepath"
)

// IsAnyHardlinked returns true if at least one REGULAR file has link count > 1.
// Directories, symlinks, and non-regular files are skipped.
// Inaccessible files are skipped.
// Handles both relative paths (joined with basePath) and absolute paths.
func IsAnyHardlinked(basePath string, filePaths []string) bool {
	for _, file := range filePaths {
		// Normalize slashes for cross-platform compatibility
		cleaned := filepath.FromSlash(file)
		cleaned = filepath.Clean(cleaned)

		// Handle absolute vs relative paths
		var fullPath string
		if filepath.IsAbs(cleaned) {
			fullPath = cleaned
		} else {
			fullPath = filepath.Join(basePath, cleaned)
		}

		info, err := os.Lstat(fullPath)
		if err != nil {
			continue
		}
		// Skip directories and non-regular files
		if !info.Mode().IsRegular() {
			continue
		}
		nlink, err := getLinkCount(info, fullPath)
		if err != nil {
			continue
		}
		if nlink > 1 {
			return true
		}
	}
	return false
}
