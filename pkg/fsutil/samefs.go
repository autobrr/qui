// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package fsutil provides filesystem utilities for hardlink operations.
package fsutil

// SameFilesystem checks if two paths are on the same filesystem.
// This is required for hardlinks, which cannot span filesystems.
// Returns true if both paths are on the same filesystem, false otherwise.
// Returns an error if either path doesn't exist or cannot be accessed.
//
// Implementation is platform-specific:
//   - Unix: compares device IDs from stat(2)
//   - Windows: compares volume serial numbers
func SameFilesystem(path1, path2 string) (bool, error) {
	return sameFilesystem(path1, path2)
}
