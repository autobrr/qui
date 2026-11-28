// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package local

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/pkg/hardlink"
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

	require.Equal(t, uintptr(8), unsafe.Sizeof(fileAttributeTagInfo{}))
	require.Equal(t, uintptr(0), unsafe.Offsetof(fileAttributeTagInfo{}.FileAttributes))
	require.Equal(t, uintptr(4), unsafe.Offsetof(fileAttributeTagInfo{}.ReparseTag))
}

func TestPathIdentityWindows(t *testing.T) {
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

func TestPathIdentityWindowsOpenFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "removed")
	require.NoError(t, os.WriteFile(path, []byte("source"), 0o600))
	require.NoError(t, os.Remove(path))

	id, links, err := pathIdentity(path, nil)
	require.Error(t, err)
	require.True(t, id.IsZero())
	require.Zero(t, links)
}

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

	require.Equal(t, hardlink.KindWindows, sourceID.Kind())
	require.Equal(t, make([]byte, 8), sourceID.Bytes()[17:25])
}

func TestLegacyFileIDWindowsPropagatesOriginalError(t *testing.T) {
	id, links, err := legacyFileID(windows.InvalidHandle, windows.ERROR_INVALID_PARAMETER)
	require.ErrorIs(t, err, windows.ERROR_INVALID_PARAMETER)
	require.True(t, id.IsZero())
	require.Zero(t, links)
}

func legacyFileIDForTest(t *testing.T, path string) (hardlink.FileID, uint64) {
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

func getFileIDForTest(t *testing.T, path string) (hardlink.FileID, uint64) {
	t.Helper()

	fi, err := os.Stat(path)
	require.NoError(t, err)
	id, links, err := pathIdentity(path, fi)
	require.NoError(t, err)
	return id, links
}

type stubFileInfo struct {
	mode fs.FileMode
}

func (s stubFileInfo) Name() string       { return "stub" }
func (s stubFileInfo) Size() int64        { return 0 }
func (s stubFileInfo) Mode() fs.FileMode  { return s.mode }
func (s stubFileInfo) ModTime() time.Time { return time.Time{} }
func (s stubFileInfo) IsDir() bool        { return s.mode.IsDir() }
func (s stubFileInfo) Sys() any           { return nil }

func TestIsSymlinkWindows(t *testing.T) {
	require.False(t, isSymlink(nil))
	require.True(t, isSymlink(stubFileInfo{mode: fs.ModeSymlink}))
	require.False(t, isSymlink(stubFileInfo{mode: 0}))
	require.False(t, isSymlink(stubFileInfo{mode: fs.ModeIrregular}))

	dir := t.TempDir()
	regularPath := filepath.Join(dir, "regular")
	hardlinkPath := filepath.Join(dir, "hardlink")
	require.NoError(t, os.WriteFile(regularPath, []byte("data"), 0o600))
	require.NoError(t, os.Link(regularPath, hardlinkPath))

	regLstat, err := os.Lstat(regularPath)
	require.NoError(t, err)
	require.False(t, isSymlink(regLstat))

	hlLstat, err := os.Lstat(hardlinkPath)
	require.NoError(t, err)
	require.False(t, isSymlink(hlLstat))

	junctionTarget := filepath.Join(dir, "target-dir")
	junctionPath := filepath.Join(dir, "junction")
	require.NoError(t, os.Mkdir(junctionTarget, 0o755))
	createJunctionForTest(t, junctionTarget, junctionPath)

	jLstat, err := os.Lstat(junctionPath)
	require.NoError(t, err)
	require.False(t, isSymlink(jLstat))
	require.NotZero(t, jLstat.Mode()&os.ModeIrregular)

	jStat, err := os.Stat(junctionPath)
	require.NoError(t, err)
	require.False(t, isSymlink(jStat))

	t.Run("native symlink", func(t *testing.T) {
		symlinkPath := filepath.Join(dir, "symlink")
		createSymlinkOrSkip(t, regularPath, symlinkPath)

		lstatFi, err := os.Lstat(symlinkPath)
		require.NoError(t, err)
		require.True(t, isSymlink(lstatFi))

		statFi, err := os.Stat(symlinkPath)
		require.NoError(t, err)
		require.False(t, isSymlink(statFi))
	})
}

func TestPathIdentityWindowsSymlink(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target")
	hardlinkPath := filepath.Join(dir, "hardlink")
	symlinkPath := filepath.Join(dir, "symlink")

	require.NoError(t, os.WriteFile(targetPath, []byte("target"), 0o600))
	require.NoError(t, os.Link(targetPath, hardlinkPath))
	createSymlinkOrSkip(t, targetPath, symlinkPath)

	targetID, targetLinks := getFileIDForTest(t, targetPath)
	require.Equal(t, uint64(2), targetLinks)

	lstatFi, err := os.Lstat(symlinkPath)
	require.NoError(t, err)
	linkID, linkCount, err := pathIdentity(symlinkPath, lstatFi)
	require.NoError(t, err)
	require.NotEqual(t, targetID, linkID)
	require.Equal(t, uint64(1), linkCount)
	require.False(t, linkID.IsZero())

	followedID, followedLinks := getFileIDForTest(t, symlinkPath)
	require.Equal(t, targetID, followedID)
	require.Equal(t, targetLinks, followedLinks)
}

func TestPathIdentityWindowsJunction(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "target&dir")
	junctionPath := filepath.Join(dir, "junction&link")
	require.NoError(t, os.Mkdir(targetDir, 0o755))
	createJunctionForTest(t, targetDir, junctionPath)

	targetFi, err := os.Lstat(targetDir)
	require.NoError(t, err)
	targetID, _, err := pathIdentity(targetDir, targetFi)
	require.NoError(t, err)

	jLstat, err := os.Lstat(junctionPath)
	require.NoError(t, err)
	junctionID, junctionLinks, err := pathIdentity(junctionPath, jLstat)
	require.NoError(t, err)
	require.NotEqual(t, targetID, junctionID)
	require.Equal(t, uint64(1), junctionLinks)

	jStat, err := os.Stat(junctionPath)
	require.NoError(t, err)
	followedID, _, err := pathIdentity(junctionPath, jStat)
	require.NoError(t, err)
	require.Equal(t, targetID, followedID)

	irregularDirStat := stubFileInfo{mode: jStat.Mode() | fs.ModeIrregular}
	irregularFollowedID, _, err := pathIdentity(junctionPath, irregularDirStat)
	require.NoError(t, err)
	require.Equal(t, targetID, irregularFollowedID)
}

func createSymlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()

	if err := os.Symlink(target, link); err != nil {
		if errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
			t.Skipf("symlink creation requires Developer Mode or elevated privileges: %v", err)
		}
		require.NoError(t, err)
	}
}

func createJunctionForTest(t *testing.T, target, junction string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: fmt.Sprintf(`cmd /c mklink /J "%s" "%s"`, junction, target),
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "mklink /J failed: %s", string(out))
}
