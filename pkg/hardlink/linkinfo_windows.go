// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package hardlink

import (
	"fmt"
	"os"
	"syscall"
)

// LinkInfo returns the unique file identifier and link count for a file.
// On Windows, fileID is composed of volume serial number and file index.
// This enables hardlink accounting to distinguish between:
// - hardlinks within the torrent set (cross-seeds)
// - hardlinks outside the torrent set (arr imports)
func LinkInfo(fi os.FileInfo, path string) (fileID string, nlink uint64, err error) {
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", 0, err
	}
	attrs := uint32(syscall.FILE_FLAG_BACKUP_SEMANTICS)
	if isSymlink(fi) {
		// FILE_FLAG_OPEN_REPARSE_POINT to not follow symlinks
		attrs |= syscall.FILE_FLAG_OPEN_REPARSE_POINT
	}
	// Use full sharing mode to avoid failures when file is open by another process (e.g., qBittorrent)
	shareMode := uint32(syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE)
	h, err := syscall.CreateFile(pathp, 0, shareMode, nil, syscall.OPEN_EXISTING, attrs, 0)
	if err != nil {
		return "", 0, err
	}
	defer syscall.CloseHandle(h)

	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return "", 0, err
	}

	// Format: volume|indexHigh|indexLow - stable unique identifier for the physical file
	fileID = fmt.Sprintf("%d|%d|%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow)
	nlink = uint64(info.NumberOfLinks)
	return fileID, nlink, nil
}
