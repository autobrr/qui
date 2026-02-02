// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package fsutil provides filesystem utilities for hardlink operations.
package fsutil

import (
	"errors"
	"fmt"
	"os"
)

// SameFilesystem checks if two paths are on the same filesystem.
// This is required for hardlinks, which cannot span filesystems.
// Returns true if both paths are on the same filesystem, false otherwise.
// Returns an error if either path doesn't exist or cannot be accessed.
//
// Implementation is platform-specific:
//   - Unix: compares device IDs from stat(2)
//   - Windows: compares volume serial numbers
func SameFilesystem(path1, path2 string) (bool, error) {
	if path1 == "" || path2 == "" {
		return false, errors.New("path must not be empty")
	}
	if _, err := os.Stat(path1); err != nil {
		return false, fmt.Errorf("path does not exist: %s: %w", path1, err)
	}
	if _, err := os.Stat(path2); err != nil {
		return false, fmt.Errorf("path does not exist: %s: %w", path2, err)
	}
	return sameFilesystem(path1, path2)
}
