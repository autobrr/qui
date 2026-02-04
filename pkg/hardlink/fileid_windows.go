// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package hardlink

import (
	"hash"
	"os"
	"syscall"
)

// FILE_READ_ATTRIBUTES is the Windows access right for reading file attributes.
// Required for GetFileInformationByHandle to reliably work on all filesystem types.
const fileReadAttributes = 0x0080

// FileID uniquely identifies a physical file on disk.
// On Windows, this is the (VolumeSerialNumber, FileIndexHigh, FileIndexLow) tuple.
// This type is comparable and can be used as a map key without allocations.
type FileID struct {
	VolumeSerialNumber uint32
	FileIndexHigh      uint32
	FileIndexLow       uint32
}

// IsZero returns true if the FileID is the zero value (uninitialized).
func (f FileID) IsZero() bool {
	return f.VolumeSerialNumber == 0 && f.FileIndexHigh == 0 && f.FileIndexLow == 0
}

// GetFileID returns the FileID and link count for a file with low-allocation overhead.
// This is more efficient than LinkInfo when you don't need the string representation.
func GetFileID(fi os.FileInfo, path string) (FileID, uint64, error) {
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return FileID{}, 0, err
	}
	attrs := uint32(syscall.FILE_FLAG_BACKUP_SEMANTICS)
	if isSymlink(fi) {
		attrs |= syscall.FILE_FLAG_OPEN_REPARSE_POINT
	}
	// Use full sharing mode to avoid failures when file is open by another process.
	// FILE_READ_ATTRIBUTES is required for GetFileInformationByHandle to work
	// reliably across different Windows filesystem types.
	shareMode := uint32(syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE)
	h, err := syscall.CreateFile(pathp, fileReadAttributes, shareMode, nil, syscall.OPEN_EXISTING, attrs, 0)
	if err != nil {
		return FileID{}, 0, err
	}
	defer syscall.CloseHandle(h)

	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return FileID{}, 0, err
	}

	return FileID{
		VolumeSerialNumber: info.VolumeSerialNumber,
		FileIndexHigh:      info.FileIndexHigh,
		FileIndexLow:       info.FileIndexLow,
	}, uint64(info.NumberOfLinks), nil
}

// Bytes returns a byte slice representation of the FileID for hashing.
// This is used for computing file signatures across platforms.
func (f FileID) Bytes() []byte {
	var buf [12]byte
	f.fillBytes(&buf)
	return buf[:]
}

// WriteToHash writes the FileID bytes directly to a hash.Hash.
// Uses a stack-allocated buffer to avoid heap escapes (unlike Bytes which returns a slice).
func (f FileID) WriteToHash(h hash.Hash) {
	var buf [12]byte
	f.fillBytes(&buf)
	h.Write(buf[:])
}

func (f FileID) fillBytes(buf *[12]byte) {
	buf[0] = byte(f.VolumeSerialNumber >> 24)
	buf[1] = byte(f.VolumeSerialNumber >> 16)
	buf[2] = byte(f.VolumeSerialNumber >> 8)
	buf[3] = byte(f.VolumeSerialNumber)
	buf[4] = byte(f.FileIndexHigh >> 24)
	buf[5] = byte(f.FileIndexHigh >> 16)
	buf[6] = byte(f.FileIndexHigh >> 8)
	buf[7] = byte(f.FileIndexHigh)
	buf[8] = byte(f.FileIndexLow >> 24)
	buf[9] = byte(f.FileIndexLow >> 16)
	buf[10] = byte(f.FileIndexLow >> 8)
	buf[11] = byte(f.FileIndexLow)
}

// Less returns true if this FileID is less than other.
// Used for sorting FileIDs.
func (f FileID) Less(other FileID) bool {
	if f.VolumeSerialNumber != other.VolumeSerialNumber {
		return f.VolumeSerialNumber < other.VolumeSerialNumber
	}
	if f.FileIndexHigh != other.FileIndexHigh {
		return f.FileIndexHigh < other.FileIndexHigh
	}
	return f.FileIndexLow < other.FileIndexLow
}
