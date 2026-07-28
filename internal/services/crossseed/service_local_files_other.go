// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package crossseed

import "os"

func isLinkedLocalFile(fi os.FileInfo) bool {
	return fi.Mode()&os.ModeSymlink != 0
}
