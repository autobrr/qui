// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package hardlink

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

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

// TestLegacyFileIDWindows covers the fallback taken on volumes that reject the
// FileIdInfo query (FAT/exFAT, some SMB redirectors). It cannot be reached
// through GetFileID on NTFS, so it is exercised directly.
func TestLegacyFileIDWindows(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source")
	hardlinkPath := filepath.Join(dir, "hardlink")
	copyPath := filepath.Join(dir, "copy")
	require.NoError(t, os.WriteFile(sourcePath, []byte("source"), 0o600))
	require.NoError(t, os.Link(sourcePath, hardlinkPath))
	require.NoError(t, os.WriteFile(copyPath, []byte("source"), 0o600))

	sourceID, sourceLinks := legacyFileIDForTest(t, sourcePath)
	hardlinkID, hardlinkLinks := legacyFileIDForTest(t, hardlinkPath)
	copyID, copyLinks := legacyFileIDForTest(t, copyPath)

	require.Equal(t, sourceID, hardlinkID)
	require.NotEqual(t, sourceID, copyID)
	require.Equal(t, uint64(2), sourceLinks)
	require.Equal(t, uint64(2), hardlinkLinks)
	require.Equal(t, uint64(1), copyLinks)
	require.False(t, sourceID.IsZero())

	// The legacy index occupies the low 8 bytes of the identifier; the rest stays zeroed.
	require.Equal(t, KindWindows, sourceID.Kind())
	require.Equal(t, make([]byte, 8), sourceID.Bytes()[17:25])
}

func TestLegacyFileIDWindowsPropagatesOriginalError(t *testing.T) {
	id, links, err := legacyFileID(windows.InvalidHandle, windows.ERROR_INVALID_PARAMETER)
	require.ErrorIs(t, err, windows.ERROR_INVALID_PARAMETER)
	require.True(t, id.IsZero())
	require.Zero(t, links)
}

func legacyFileIDForTest(t *testing.T, path string) (FileID, uint64) {
	t.Helper()

	pathp, err := windows.UTF16PtrFromString(path)
	require.NoError(t, err)
	h, err := windows.CreateFile(
		pathp,
		fileReadAttributes,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	require.NoError(t, err)
	defer func() {
		_ = windows.CloseHandle(h)
	}()

	id, links, err := legacyFileID(h, windows.ERROR_INVALID_PARAMETER)
	require.NoError(t, err)
	return id, links
}

func getFileIDForTest(t *testing.T, path string) (FileID, uint64) {
	t.Helper()

	fi, err := os.Stat(path)
	require.NoError(t, err)
	id, links, err := GetFileID(fi, path)
	require.NoError(t, err)
	return id, links
}
