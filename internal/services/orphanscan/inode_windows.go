// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package orphanscan

import "io/fs"

func inodeKeyFromInfo(info fs.FileInfo) (inodeKey, uint64, bool) {
	return inodeKey{}, 0, false
}
