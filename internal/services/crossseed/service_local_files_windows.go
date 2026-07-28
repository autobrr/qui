// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package crossseed

import (
	"os"
	"syscall"
)

func isLinkedLocalFile(fi os.FileInfo) bool {
	attributes, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	return ok && attributes != nil &&
		attributes.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
