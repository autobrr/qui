// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package hardlink

import (
	"errors"
	"hash"
	"os"
	"syscall"
)

// FileID uniquely identifies a physical file on disk.
// On Unix, this is the (device, inode) pair.
// This type is comparable and can be used as a map key without allocations.
type FileID struct {
	Dev uint64
	Ino uint64
}

// IsZero returns true if the FileID is the zero value (uninitialized).
func (f FileID) IsZero() bool {
	return f.Dev == 0 && f.Ino == 0
}

// GetFileID returns the FileID and link count for a file without allocations.
// This is more efficient than LinkInfo when you don't need the string representation.
func GetFileID(fi os.FileInfo, _ string) (FileID, uint64, error) {
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return FileID{}, 0, errors.New("failed to get syscall.Stat_t")
	}
	return FileID{Dev: uint64(sys.Dev), Ino: sys.Ino}, uint64(sys.Nlink), nil //nolint:gosec // sys.Dev is always non-negative
}

// Bytes returns a byte slice representation of the FileID for hashing.
// This is used for computing file signatures across platforms.
func (f FileID) Bytes() []byte {
	var buf [16]byte
	f.fillBytes(&buf)
	return buf[:]
}

// WriteToHash writes the FileID bytes directly to a hash.Hash.
// Uses a stack-allocated buffer to avoid heap escapes (unlike Bytes which returns a slice).
func (f FileID) WriteToHash(h hash.Hash) {
	var buf [16]byte
	f.fillBytes(&buf)
	h.Write(buf[:])
}

func (f FileID) fillBytes(buf *[16]byte) {
	buf[0] = byte(f.Dev >> 56)
	buf[1] = byte(f.Dev >> 48)
	buf[2] = byte(f.Dev >> 40)
	buf[3] = byte(f.Dev >> 32)
	buf[4] = byte(f.Dev >> 24)
	buf[5] = byte(f.Dev >> 16)
	buf[6] = byte(f.Dev >> 8)
	buf[7] = byte(f.Dev)
	buf[8] = byte(f.Ino >> 56)
	buf[9] = byte(f.Ino >> 48)
	buf[10] = byte(f.Ino >> 40)
	buf[11] = byte(f.Ino >> 32)
	buf[12] = byte(f.Ino >> 24)
	buf[13] = byte(f.Ino >> 16)
	buf[14] = byte(f.Ino >> 8)
	buf[15] = byte(f.Ino)
}

// Less returns true if this FileID is less than other.
// Used for sorting FileIDs.
func (f FileID) Less(other FileID) bool {
	if f.Dev != other.Dev {
		return f.Dev < other.Dev
	}
	return f.Ino < other.Ino
}
