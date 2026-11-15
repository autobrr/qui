// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"io/fs"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/pkg/pathutil"
)

// Data-file tags from WinNT.h. Unknown tags can represent special files.
const (
	reparseCloudMask       = 0x0000f000
	reparseCloud           = 0x9000001a
	reparseFilePlaceholder = 0x80000015
	reparseWOF             = 0x80000017
	reparseOneDrive        = 0x80000021
)

// localFileMode recognizes cloud/WOF data files without admitting junctions,
// WSL devices/FIFOs or unknown irregular entries.
func localFileMode(path string, mode fs.FileMode, follow bool) fs.FileMode {
	if mode&fs.ModeIrregular == 0 {
		return mode
	}
	p, err := pathutil.Win32PathPointer(path)
	if err != nil {
		return mode
	}
	var data windows.Win32finddata
	h, err := windows.FindFirstFile(p, &data)
	if err != nil {
		return mode // Keep unverified entries excluded from scans and deletion.
	}
	defer func() { _ = windows.FindClose(h) }()
	if data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return mode
	}
	// Stat already followed the final symlink; classify the same target.
	if follow && data.Reserved0 == windows.IO_REPARSE_TAG_SYMLINK {
		tag, err := followedReparseTag(p)
		if err != nil {
			return mode
		}
		data.Reserved0 = tag
	}
	switch data.Reserved0 {
	case reparseFilePlaceholder, reparseWOF, reparseOneDrive:
		return mode &^ fs.ModeIrregular
	}
	if data.Reserved0&^reparseCloudMask == reparseCloud {
		return mode &^ fs.ModeIrregular
	}
	return mode
}

// followedReparseTag queries the opened target, including through junction parents.
func followedReparseTag(path *uint16) (uint32, error) {
	h, err := windows.CreateFile(path, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var info struct {
		FileAttributes uint32
		ReparseTag     uint32
	}
	err = windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	return info.ReparseTag, err
}
