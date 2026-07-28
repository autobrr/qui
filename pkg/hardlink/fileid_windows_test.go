// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package hardlink

import (
	"bytes"
	"hash"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

type recordingHash struct {
	bytes.Buffer
}

var _ hash.Hash = (*recordingHash)(nil)

func (h *recordingHash) Sum(b []byte) []byte { return append(b, h.Bytes()...) }
func (h *recordingHash) Reset()              { h.Buffer.Reset() }
func (h *recordingHash) Size() int           { return h.Len() }
func (h *recordingHash) BlockSize() int      { return 1 }

func TestWindowsFileInfoLayouts(t *testing.T) {
	require.Equal(t, uintptr(24), unsafe.Sizeof(fileIDInfo{}))
	require.Equal(t, uintptr(0), unsafe.Offsetof(fileIDInfo{}.VolumeSerialNumber))
	require.Equal(t, uintptr(8), unsafe.Offsetof(fileIDInfo{}.Identifier))

	require.Equal(t, uintptr(24), unsafe.Sizeof(fileStandardInfo{}))
	require.Equal(t, uintptr(0), unsafe.Offsetof(fileStandardInfo{}.AllocationSize))
	require.Equal(t, uintptr(8), unsafe.Offsetof(fileStandardInfo{}.EndOfFile))
	require.Equal(t, uintptr(16), unsafe.Offsetof(fileStandardInfo{}.NumberOfLinks))
	require.Equal(t, uintptr(20), unsafe.Offsetof(fileStandardInfo{}.DeletePending))
	require.Equal(t, uintptr(21), unsafe.Offsetof(fileStandardInfo{}.Directory))
}

func TestWindowsFileIDComparable(t *testing.T) {
	id := FileID{
		VolumeSerialNumber: 7,
		Identifier:         [16]byte{15: 9},
	}
	ids := map[FileID]bool{id: true}
	require.True(t, ids[id])
}

func TestWindowsFileIDBytesAndHash(t *testing.T) {
	id := FileID{
		VolumeSerialNumber: 0x0102030405060708,
		Identifier: [16]byte{
			0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
			0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		},
	}
	want := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
	}

	require.Equal(t, want, id.Bytes())
	require.Len(t, id.Bytes(), 24)

	var got recordingHash
	id.WriteToHash(&got)
	require.Equal(t, want, got.Bytes())
}

func TestWindowsFileIDIsZero(t *testing.T) {
	require.True(t, (FileID{}).IsZero())
	require.False(t, (FileID{VolumeSerialNumber: 1 << 32}).IsZero())
	require.False(t, (FileID{Identifier: [16]byte{15: 1}}).IsZero())
}

func TestWindowsFileIDLess(t *testing.T) {
	lowVolume := FileID{VolumeSerialNumber: 1, Identifier: [16]byte{15: 0xff}}
	highVolume := FileID{VolumeSerialNumber: 2}
	require.True(t, lowVolume.Less(highVolume))
	require.False(t, highVolume.Less(lowVolume))

	lowIdentifier := FileID{VolumeSerialNumber: 2, Identifier: [16]byte{14: 1, 15: 0xff}}
	highIdentifier := FileID{VolumeSerialNumber: 2, Identifier: [16]byte{14: 2}}
	require.True(t, lowIdentifier.Less(highIdentifier))
	require.False(t, highIdentifier.Less(lowIdentifier))
	require.False(t, lowIdentifier.Less(lowIdentifier))
}

func TestGetFileIDWindows(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source")
	hardlinkPath := filepath.Join(dir, "hardlink")
	copyPath := filepath.Join(dir, "copy")
	require.NoError(t, os.WriteFile(sourcePath, []byte("source"), 0o600))
	require.NoError(t, os.Link(sourcePath, hardlinkPath))
	require.NoError(t, os.WriteFile(copyPath, []byte("source"), 0o600))

	sourceID, sourceLinks := getFileIDForTest(t, sourcePath)
	hardlinkID, hardlinkLinks := getFileIDForTest(t, hardlinkPath)
	copyID, copyLinks := getFileIDForTest(t, copyPath)

	require.Equal(t, sourceID, hardlinkID)
	require.NotEqual(t, sourceID, copyID)
	require.Equal(t, uint64(2), sourceLinks)
	require.Equal(t, uint64(2), hardlinkLinks)
	require.Equal(t, uint64(1), copyLinks)
	require.False(t, sourceID.IsZero())
	require.False(t, copyID.IsZero())
}

func TestGetFileIDWindowsOpenFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "removed")
	require.NoError(t, os.WriteFile(path, []byte("source"), 0o600))
	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))

	id, links, err := GetFileID(fi, path)
	require.Error(t, err)
	require.True(t, id.IsZero())
	require.Zero(t, links)
}

func getFileIDForTest(t *testing.T, path string) (FileID, uint64) {
	t.Helper()

	fi, err := os.Stat(path)
	require.NoError(t, err)
	id, links, err := GetFileID(fi, path)
	require.NoError(t, err)
	return id, links
}
