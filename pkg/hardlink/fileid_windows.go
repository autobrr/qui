// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package hardlink

import (
	"encoding/binary"
	"hash"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// FILE_READ_ATTRIBUTES is the Windows access right for reading file attributes.
// This is the minimum access needed to query file identity and link counts.
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

// FileID uniquely identifies a physical file on disk.
// On Windows, this is the (VolumeSerialNumber, 128-bit file identifier) tuple.
// This type is comparable and can be used as a map key without allocations.
type FileID struct {
	VolumeSerialNumber uint64
	Identifier         [16]byte
}

// IsZero returns true if the FileID is the zero value (uninitialized).
func (f FileID) IsZero() bool {
	return f.VolumeSerialNumber == 0 && f.Identifier == [16]byte{}
}

// GetFileID returns the FileID and link count for a file with low-allocation overhead.
// This is more efficient than LinkInfo when you don't need the string representation.
func GetFileID(fi os.FileInfo, path string) (FileID, uint64, error) {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return FileID{}, 0, err
	}
	attrs := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if isSymlink(fi) {
		attrs |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}
	// Use full sharing mode to avoid failures when file is open by another process.
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

	return FileID{
		VolumeSerialNumber: idInfo.VolumeSerialNumber,
		Identifier:         idInfo.Identifier,
	}, uint64(standardInfo.NumberOfLinks), nil
}

// legacyFileID synthesizes a FileID from the pre-Win8 GetFileInformationByHandle
// call. FileIdInfo is an optional per-driver query: FAT/exFAT reject it with
// ERROR_INVALID_PARAMETER and some SMB redirectors (notably Azure Files) with
// ERROR_INVALID_LEVEL, where the legacy 64-bit file index still works. Falling
// back keeps file identity working there instead of silently disabling
// already-seeding detection, rename tracking and hardlink scopes.
// Mirrors microsoft/STL's _Get_file_id_by_handle. idErr is returned when the
// legacy call fails too, since FileIdInfo is the path that matters on NTFS/ReFS.
func legacyFileID(h windows.Handle, idErr error) (FileID, uint64, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return FileID{}, 0, idErr
	}

	id := FileID{VolumeSerialNumber: uint64(info.VolumeSerialNumber)}
	binary.LittleEndian.PutUint32(id.Identifier[0:4], info.FileIndexHigh)
	binary.LittleEndian.PutUint32(id.Identifier[4:8], info.FileIndexLow)
	return id, uint64(info.NumberOfLinks), nil
}

// Bytes returns a byte slice representation of the FileID for hashing.
// This is used for computing file signatures across platforms.
func (f FileID) Bytes() []byte {
	var buf [24]byte
	f.fillBytes(&buf)
	return buf[:]
}

// WriteToHash writes the FileID bytes directly to a hash.Hash.
// Uses a stack-allocated buffer to avoid heap escapes (unlike Bytes which returns a slice).
func (f FileID) WriteToHash(h hash.Hash) {
	var buf [24]byte
	f.fillBytes(&buf)
	h.Write(buf[:])
}

func (f FileID) fillBytes(buf *[24]byte) {
	buf[0] = byte(f.VolumeSerialNumber >> 56)
	buf[1] = byte(f.VolumeSerialNumber >> 48)
	buf[2] = byte(f.VolumeSerialNumber >> 40)
	buf[3] = byte(f.VolumeSerialNumber >> 32)
	buf[4] = byte(f.VolumeSerialNumber >> 24)
	buf[5] = byte(f.VolumeSerialNumber >> 16)
	buf[6] = byte(f.VolumeSerialNumber >> 8)
	buf[7] = byte(f.VolumeSerialNumber)
	copy(buf[8:], f.Identifier[:])
}

// Less returns true if this FileID is less than other.
// Used for sorting FileIDs.
func (f FileID) Less(other FileID) bool {
	if f.VolumeSerialNumber != other.VolumeSerialNumber {
		return f.VolumeSerialNumber < other.VolumeSerialNumber
	}
	for i := range f.Identifier {
		if f.Identifier[i] != other.Identifier[i] {
			return f.Identifier[i] < other.Identifier[i]
		}
	}
	return false
}
