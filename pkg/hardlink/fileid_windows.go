// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package hardlink

import (
	"encoding/binary"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const fileReadAttributes = 0x0080

type fileIDInfo struct {
	VolumeSerialNumber uint64
	Identifier         [16]byte
}

type fileStandardInfo struct {
	AllocationSize int64
	EndOfFile      int64
	NumberOfLinks  uint32
	DeletePending  byte
	Directory      byte
	_              [2]byte
}

func GetFileID(fi os.FileInfo, path string) (FileID, uint64, error) {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return FileID{}, 0, err
	}
	attrs := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if isSymlink(fi) {
		attrs |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}
	shareMode := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	h, err := windows.CreateFile(pathp, fileReadAttributes, shareMode, nil, windows.OPEN_EXISTING, attrs, 0)
	if err != nil {
		return FileID{}, 0, err
	}
	defer func() {
		_ = windows.CloseHandle(h)
	}()

	var idInfo fileIDInfo
	if err := windows.GetFileInformationByHandleEx(
		h,
		windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&idInfo)),
		uint32(unsafe.Sizeof(idInfo)),
	); err != nil {
		return legacyFileID(h, err)
	}

	var standardInfo fileStandardInfo
	if err := windows.GetFileInformationByHandleEx(
		h,
		windows.FileStandardInfo,
		(*byte)(unsafe.Pointer(&standardInfo)),
		uint32(unsafe.Sizeof(standardInfo)),
	); err != nil {
		return FileID{}, 0, err
	}

	return WindowsFileID(idInfo.VolumeSerialNumber, idInfo.Identifier), uint64(standardInfo.NumberOfLinks), nil
}

func legacyFileID(h windows.Handle, idErr error) (FileID, uint64, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return FileID{}, 0, idErr
	}

	var identifier [16]byte
	binary.LittleEndian.PutUint32(identifier[0:4], info.FileIndexHigh)
	binary.LittleEndian.PutUint32(identifier[4:8], info.FileIndexLow)
	return WindowsFileID(uint64(info.VolumeSerialNumber), identifier), uint64(info.NumberOfLinks), nil
}
