// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package local

import (
	"errors"
	"os"
	"syscall"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlink"
)

func setStatIdentity(info *fsops.LstatInfo, fi os.FileInfo) {
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		info.FileIDErr = errors.New("failed to get syscall.Stat_t")
		return
	}
	info.FileID = hardlink.UnixFileID(uint64(sys.Dev), sys.Ino)
	info.Nlinks = uint64(sys.Nlink)
}
