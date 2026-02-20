// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package fsutil

import (
	"fmt"
	"os"
	"syscall"
)

// sameFilesystem checks if two paths are on the same filesystem on Unix.
// Uses stat(2) to compare device IDs.
func sameFilesystem(path1, path2 string) (bool, error) {
	stat1, err := os.Stat(path1)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", path1, err)
	}

	stat2, err := os.Stat(path2)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", path2, err)
	}

	sys1, ok := stat1.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("failed to get syscall.Stat_t for %s", path1)
	}

	sys2, ok := stat2.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("failed to get syscall.Stat_t for %s", path2)
	}

	return sys1.Dev == sys2.Dev, nil
}
