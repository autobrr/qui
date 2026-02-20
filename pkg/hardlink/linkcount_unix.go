// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package hardlink

import (
	"errors"
	"os"
	"syscall"
)

func getLinkCount(fi os.FileInfo, _ string) (uint64, error) {
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("failed to get syscall.Stat_t")
	}
	return uint64(sys.Nlink), nil
}
