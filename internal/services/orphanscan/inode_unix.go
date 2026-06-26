// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package orphanscan

import (
	"io/fs"
	"syscall"
)

func inodeKeyFromInfo(info fs.FileInfo) (inodeKey, uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return inodeKey{}, 0, false
	}
	return inodeKey{dev: uint64(stat.Dev), ino: uint64(stat.Ino)}, uint64(stat.Nlink), true
}
