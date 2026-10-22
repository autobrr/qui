// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package hardlink

import (
	"errors"
	"os"
	"syscall"
)

// GetFileID returns the FileID and link count for a file without allocations.
// This is more efficient than LinkInfo when you don't need the string representation.
func GetFileID(fi os.FileInfo, _ string) (FileID, uint64, error) {
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return FileID{}, 0, errors.New("failed to get syscall.Stat_t")
	}
	//nolint:gosec,unconvert // sys.Dev is always non-negative, and it is uint64 on linux but int32 on darwin, so the conversion only reads as redundant on one of them.
	return UnixFileID(uint64(sys.Dev), sys.Ino), uint64(sys.Nlink), nil
}
