// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package hardlink

import (
	"os"
	"reflect"
	"syscall"
)

func isSymlink(fi os.FileInfo) bool {
	attrs, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attrs == nil {
		return false
	}
	if attrs.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return false
	}
	v := reflect.Indirect(reflect.ValueOf(fi))
	reserved0Field := v.FieldByName("Reserved0")
	if !reserved0Field.IsValid() {
		return false
	}
	reserved0 := reserved0Field.Uint()
	return reserved0 == syscall.IO_REPARSE_TAG_SYMLINK || reserved0 == 0xA0000003
}
