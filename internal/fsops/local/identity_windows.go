// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlink"
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

type fileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

func lstatPath(path string) (*fsops.LstatInfo, error) {
	fi, err := os.Lstat(path)
	return identify(fi, err, path, false)
}

func statPath(path string) (*fsops.LstatInfo, error) {
	fi, err := os.Stat(path)
	return identify(fi, err, path, true)
}

func lstatEntry(path string, d fs.DirEntry, wantID bool) (*fsops.LstatInfo, error) {
	fi, err := d.Info()
	if err != nil {
		return nil, err
	}
	info := statFields(fi, path, false)
	if wantID && info.Mode.IsRegular() {
		info.FileID, info.Nlinks, info.FileIDErr = pathIdentity(path, fi)
	}
	return info, nil
}

func lstatIn(r *os.Root, name string) (*fsops.LstatInfo, error) {
	native := filepath.FromSlash(name)
	fi, err := r.Lstat(native)
	if err != nil {
		return nil, err
	}
	info := statFields(fi, name, false)
	if hasIdentity(info) {
		info.FileID, info.Nlinks, info.FileIDErr = rootIdentity(r, native)
	}
	return info, nil
}

func identify(fi os.FileInfo, err error, path string, follow bool) (*fsops.LstatInfo, error) {
	if err != nil {
		return nil, err
	}
	info := statFields(fi, path, follow)
	if hasIdentity(info) {
		info.FileID, info.Nlinks, info.FileIDErr = pathIdentity(path, fi)
	}
	return info, nil
}

func rootIdentity(r *os.Root, name string) (hardlink.FileID, uint64, error) {
	f, err := r.Open(name)
	if err != nil {
		return hardlink.FileID{}, 0, err
	}
	defer func() { _ = f.Close() }()
	return handleIdentity(windows.Handle(f.Fd()))
}

func openIdentityHandle(fi os.FileInfo, pathp *uint16) (windows.Handle, error) {
	shareMode := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	baseFlags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if isSymlink(fi) {
		return windows.CreateFile(pathp, fileReadAttributes, shareMode, nil, windows.OPEN_EXISTING, baseFlags|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	}
	if fi != nil && !fi.IsDir() && fi.Mode()&os.ModeIrregular != 0 {
		h, err := windows.CreateFile(pathp, fileReadAttributes, shareMode, nil, windows.OPEN_EXISTING, baseFlags|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return 0, err
		}
		var tagInfo fileAttributeTagInfo
		tagErr := windows.GetFileInformationByHandleEx(
			h,
			windows.FileAttributeTagInfo,
			(*byte)(unsafe.Pointer(&tagInfo)),
			uint32(unsafe.Sizeof(tagInfo)),
		)
		if tagErr == nil &&
			tagInfo.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 &&
			tagInfo.ReparseTag == windows.IO_REPARSE_TAG_MOUNT_POINT {
			return h, nil
		}
		_ = windows.CloseHandle(h)
	}
	return windows.CreateFile(pathp, fileReadAttributes, shareMode, nil, windows.OPEN_EXISTING, baseFlags, 0)
}

func pathIdentity(path string, fi os.FileInfo) (hardlink.FileID, uint64, error) {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return hardlink.FileID{}, 0, err
	}
	h, err := openIdentityHandle(fi, pathp)
	if err != nil {
		return hardlink.FileID{}, 0, err
	}
	defer func() {
		_ = windows.CloseHandle(h)
	}()
	return handleIdentity(h)
}

func handleIdentity(h windows.Handle) (hardlink.FileID, uint64, error) {
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
		return hardlink.FileID{}, 0, err
	}

	return hardlink.WindowsFileID(idInfo.VolumeSerialNumber, idInfo.Identifier), uint64(standardInfo.NumberOfLinks), nil
}

func legacyFileID(h windows.Handle, idErr error) (hardlink.FileID, uint64, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return hardlink.FileID{}, 0, idErr
	}

	var identifier [16]byte
	binary.LittleEndian.PutUint32(identifier[0:4], info.FileIndexHigh)
	binary.LittleEndian.PutUint32(identifier[4:8], info.FileIndexLow)
	return hardlink.WindowsFileID(uint64(info.VolumeSerialNumber), identifier), uint64(info.NumberOfLinks), nil
}

func isSymlink(fi os.FileInfo) bool {
	return fi != nil && fi.Mode()&os.ModeSymlink != 0
}
