// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package hardlink

import (
	"os"
	"reflect"
	"syscall"
)

// isSymlink detects symlinks on Windows using reparse point attributes.
// This approach is based on tqm's implementation and uses reflection to access
// the Reserved0 field which contains the reparse tag. This may be brittle across
// Go versions if the internal FileInfo structure changes.
func isSymlink(fi os.FileInfo) bool {
	// Guard type assert to avoid panic
	attrs, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok || attrs == nil {
		return false
	}
	// Check for reparse point flag
	if attrs.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return false
	}
	// Check for symlink reparse tag via reflection
	v := reflect.Indirect(reflect.ValueOf(fi))
	reserved0Field := v.FieldByName("Reserved0")
	if !reserved0Field.IsValid() {
		return false
	}
	reserved0 := reserved0Field.Uint()
	return reserved0 == syscall.IO_REPARSE_TAG_SYMLINK || reserved0 == 0xA0000003
}

func getLinkCount(fi os.FileInfo, path string) (uint64, error) {
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	attrs := uint32(syscall.FILE_FLAG_BACKUP_SEMANTICS)
	if isSymlink(fi) {
		// FILE_FLAG_OPEN_REPARSE_POINT to not follow symlinks
		attrs |= syscall.FILE_FLAG_OPEN_REPARSE_POINT
	}
	h, err := syscall.CreateFile(pathp, 0, 0, nil, syscall.OPEN_EXISTING, attrs, 0)
	if err != nil {
		return 0, err
	}
	defer syscall.CloseHandle(h)

	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return 0, err
	}
	return uint64(info.NumberOfLinks), nil
}
