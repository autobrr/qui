// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlink

import (
	"bytes"
	"encoding/binary"
	"hash"
)

// Kind tags which identity domain a FileID's bytes came from.
type Kind uint8

const (
	KindNone    Kind = iota
	KindUnix         // device and inode, big endian
	KindWindows      // volume serial and the 16-byte file identifier
	KindRemote       // reported by a remote host; peer-asserted, never kernel-attested
)

const fileIDRawLen = 24

// FileID is an opaque, comparable file identity: one kind byte and 24 raw
// bytes with the unused tail zeroed, so == and map keys stay well-defined.
// An ID only means something on the filesystem that produced it; compare
// IDs through fsops.SameFile or key maps by fsops.FileKey.
type FileID struct {
	kind Kind
	raw  [fileIDRawLen]byte
}

// UnixFileID builds the identity of a (device, inode) pair.
func UnixFileID(dev, ino uint64) FileID {
	f := FileID{kind: KindUnix}
	binary.BigEndian.PutUint64(f.raw[0:8], dev)
	binary.BigEndian.PutUint64(f.raw[8:16], ino)
	return f
}

// WindowsFileID builds the identity of a volume serial and file identifier.
func WindowsFileID(volumeSerial uint64, identifier [16]byte) FileID {
	f := FileID{kind: KindWindows}
	binary.BigEndian.PutUint64(f.raw[0:8], volumeSerial)
	copy(f.raw[8:], identifier[:])
	return f
}

// RemoteFileID wraps up to 24 bytes of identity a remote host reported.
// Empty input is no identity.
func RemoteFileID(raw []byte) FileID {
	if len(raw) == 0 {
		return FileID{}
	}
	f := FileID{kind: KindRemote}
	copy(f.raw[:], raw)
	return f
}

// FileIDFromBytes is the inverse of Bytes. Any other length is no identity,
// which is what a row written before the tagged form decodes to.
func FileIDFromBytes(b []byte) FileID {
	if len(b) != 1+fileIDRawLen {
		return FileID{}
	}
	f := FileID{kind: Kind(b[0])}
	copy(f.raw[:], b[1:])
	return f
}

// Kind reports which domain the ID belongs to.
func (f FileID) Kind() Kind {
	return f.kind
}

// IsZero reports no identity.
func (f FileID) IsZero() bool {
	return f.kind == KindNone
}

// Bytes returns the tagged form, kind byte first. This is what dirscan stores.
func (f FileID) Bytes() []byte {
	b := make([]byte, 1+fileIDRawLen)
	b[0] = byte(f.kind)
	copy(b[1:], f.raw[:])
	return b
}

// WriteToHash writes the tagged form without allocating a slice.
func (f FileID) WriteToHash(h hash.Hash) {
	var buf [1 + fileIDRawLen]byte
	buf[0] = byte(f.kind)
	copy(buf[1:], f.raw[:])
	h.Write(buf[:])
}

// Less orders IDs by kind, then raw bytes, for stable signatures.
func (f FileID) Less(other FileID) bool {
	if f.kind != other.kind {
		return f.kind < other.kind
	}
	return bytes.Compare(f.raw[:], other.raw[:]) < 0
}
