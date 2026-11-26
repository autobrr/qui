// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlink

import (
	"bytes"
	"encoding/binary"
	"hash"
)

type Kind uint8

const (
	KindNone Kind = iota
	KindUnix
	KindWindows
	KindRemote
)

const fileIDRawLen = 24

type FileID struct {
	kind Kind
	raw  [fileIDRawLen]byte
}

func UnixFileID(dev, ino uint64) FileID {
	f := FileID{kind: KindUnix}
	binary.BigEndian.PutUint64(f.raw[0:8], dev)
	binary.BigEndian.PutUint64(f.raw[8:16], ino)
	return f
}

func WindowsFileID(volumeSerial uint64, identifier [16]byte) FileID {
	f := FileID{kind: KindWindows}
	binary.BigEndian.PutUint64(f.raw[0:8], volumeSerial)
	copy(f.raw[8:], identifier[:])
	return f
}

func RemoteFileID(raw []byte) FileID {
	if len(raw) == 0 || len(raw) > fileIDRawLen {
		return FileID{}
	}
	f := FileID{kind: KindRemote}
	copy(f.raw[:], raw)
	return f
}

func FileIDFromBytes(b []byte) FileID {
	if len(b) != 1+fileIDRawLen {
		return FileID{}
	}
	f := FileID{kind: Kind(b[0])}
	copy(f.raw[:], b[1:])
	return f
}

func (f FileID) Kind() Kind {
	return f.kind
}

func (f FileID) IsZero() bool {
	return f.kind == KindNone
}

func (f FileID) Bytes() []byte {
	b := make([]byte, 1+fileIDRawLen)
	b[0] = byte(f.kind)
	copy(b[1:], f.raw[:])
	return b
}

func (f FileID) WriteToHash(h hash.Hash) {
	var buf [1 + fileIDRawLen]byte
	buf[0] = byte(f.kind)
	copy(buf[1:], f.raw[:])
	h.Write(buf[:])
}

func (f FileID) Less(other FileID) bool {
	if f.kind != other.kind {
		return f.kind < other.kind
	}
	return bytes.Compare(f.raw[:], other.raw[:]) < 0
}
