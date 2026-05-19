// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package hardlink

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// LinkInfo returns the unique file identifier and link count for a file.
// On Unix systems, fileID is composed of device ID and inode number.
// This enables hardlink accounting to distinguish between:
// - hardlinks within the torrent set (cross-seeds)
// - hardlinks outside the torrent set (arr imports)
func LinkInfo(fi os.FileInfo, _ string) (fileID string, nlink uint64, err error) {
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", 0, errors.New("failed to get syscall.Stat_t")
	}
	// Format: dev|ino - stable unique identifier for the physical file
	fileID = fmt.Sprintf("%d|%d", sys.Dev, sys.Ino)
	nlink = uint64(sys.Nlink)
	return fileID, nlink, nil
}
