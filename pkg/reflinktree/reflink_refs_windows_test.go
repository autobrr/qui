// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package reflinktree

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/pkg/sharedextents"
)

// regression: #4074
func TestCloneFile_ReFSIntegrity(t *testing.T) {
	root := os.Getenv("QUI_REFS_TEST_DIR")
	if root == "" {
		t.Skip("QUI_REFS_TEST_DIR is not set")
	}
	volume, err := getVolumeRoot(root)
	require.NoError(t, err)
	filesystem, err := getFilesystemName(volume)
	require.NoError(t, err)
	if !strings.EqualFold(filesystem, refsFilesystemName) {
		t.Skipf("QUI_REFS_TEST_DIR is on %s, not ReFS", filesystem)
	}

	for _, tt := range []struct {
		name        string
		source      uint16
		destination uint16
	}{
		{name: "on_off", source: 2, destination: 0},
		{name: "off_on", source: 0, destination: 2},
		{name: "on_on", source: 2, destination: 2},
		{name: "off_off", source: 0, destination: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir, err := os.MkdirTemp(root, "qui-refs-integrity-")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
			dstDir := filepath.Join(dir, "destination")
			require.NoError(t, os.Mkdir(dstDir, 0o700))
			destinationIntegrity := setRefsTestIntegrity(t, dstDir, tt.destination)

			src := filepath.Join(dir, "source.bin")
			dst := filepath.Join(dstDir, "clone.bin")
			require.NoError(t, os.WriteFile(src, nil, 0o600))
			sourceIntegrity := setRefsTestIntegrity(t, src, tt.source)
			data := make([]byte, 3*64*1024)
			_, err = rand.Read(data)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(src, data, 0o600))
			before := readRefsTestIntegrity(t, src)
			require.Equal(t, sourceIntegrity.ChecksumAlgorithm, before.ChecksumAlgorithm)
			require.Equal(t, sourceIntegrity.Flags, before.Flags)

			inherited := filepath.Join(dstDir, "inherited.bin")
			require.NoError(t, os.WriteFile(inherited, nil, 0o600))
			inheritedIntegrity := readRefsTestIntegrity(t, inherited)
			require.Equal(t, destinationIntegrity.ChecksumAlgorithm, inheritedIntegrity.ChecksumAlgorithm)
			require.Equal(t, destinationIntegrity.Flags, inheritedIntegrity.Flags)
			require.NoError(t, os.Remove(inherited))

			t.Logf("requested source=%#x destination=%#x; actual source=%#x destination-parent=%#x inherited=%#x cluster=%d", tt.source, tt.destination, before.ChecksumAlgorithm, destinationIntegrity.ChecksumAlgorithm, inheritedIntegrity.ChecksumAlgorithm, before.ClusterSizeInBytes)
			cloneErr := cloneFile(src, dst)
			require.Equal(t, before, readRefsTestIntegrity(t, src), "source integrity changed")
			sourceContent, err := os.ReadFile(src)
			require.NoError(t, err)
			require.Equal(t, data, sourceContent, "source content changed")
			if cloneErr != nil {
				_, statErr := os.Stat(dst)
				require.ErrorIs(t, statErr, os.ErrNotExist, "failed clone left its destination")
			}
			require.NoError(t, cloneErr)
			after := readRefsTestIntegrity(t, dst)
			require.Equal(t, before.ChecksumAlgorithm, after.ChecksumAlgorithm)
			require.Equal(t, before.Flags, after.Flags)
			content, err := os.ReadFile(dst)
			require.NoError(t, err)
			require.Equal(t, data, content)
			shared, err := sharedextents.FilesShareAllocation(src, dst)
			require.NoError(t, err)
			require.True(t, shared, "clone must share allocated extents")

			originalDuplicateExtent := duplicateExtentFn
			t.Cleanup(func() { duplicateExtentFn = originalDuplicateExtent })
			duplicateExtentFn = func(windows.Handle, windows.Handle, int64, int64, int64) error {
				return windows.ERROR_INVALID_DATA
			}
			failedDst := filepath.Join(dstDir, "failed.bin")
			require.ErrorIs(t, cloneFile(src, failedDst), windows.ERROR_INVALID_DATA)
			_, err = os.Stat(failedDst)
			require.ErrorIs(t, err, os.ErrNotExist)
			require.Equal(t, before, readRefsTestIntegrity(t, src), "failed clone changed source integrity")
			sourceContent, err = os.ReadFile(src)
			require.NoError(t, err)
			require.Equal(t, data, sourceContent, "failed clone changed source content")
			t.Logf("clone=%#x; source state and content unchanged; shared extents verified; failure cleanup verified", after.ChecksumAlgorithm)
		})
	}
}

func readRefsTestIntegrity(t testing.TB, path string) fileIntegrityInformation {
	t.Helper()
	handle := openRefsTestHandle(t, path, windows.GENERIC_READ)
	defer func() { require.NoError(t, windows.CloseHandle(handle)) }()
	var info fileIntegrityInformation
	var returned uint32
	require.NoError(t, windows.DeviceIoControl(handle, windows.FSCTL_GET_INTEGRITY_INFORMATION,
		nil, 0, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), &returned, nil))
	return info
}

func setRefsTestIntegrity(t testing.TB, path string, algorithm uint16) fileIntegrityInformation {
	t.Helper()
	handle := openRefsTestHandle(t, path, windows.GENERIC_READ|windows.GENERIC_WRITE)
	defer func() { require.NoError(t, windows.CloseHandle(handle)) }()
	info := struct {
		ChecksumAlgorithm uint16
		Reserved          uint16
		Flags             uint32
	}{ChecksumAlgorithm: algorithm}
	var returned uint32
	require.NoError(t, windows.DeviceIoControl(handle, windows.FSCTL_SET_INTEGRITY_INFORMATION,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil, 0, &returned, nil))
	actual := readRefsTestIntegrity(t, path)
	if algorithm == 0 {
		require.Zero(t, actual.ChecksumAlgorithm, "fixture integrity must be disabled")
	} else {
		// ReFS v2 selects CRC32 or CRC64, regardless of which enabled algorithm was requested.
		// https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-fscc/a4517cd5-3f5a-4058-a457-bcff2baac011
		require.Contains(t, []uint16{1, 2}, actual.ChecksumAlgorithm, "fixture integrity must be enabled")
	}
	return actual
}

func openRefsTestHandle(t testing.TB, path string, access uint32) windows.Handle {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	require.NoError(t, err)
	handle, err := windows.CreateFile(name, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	require.NoError(t, err)
	return handle
}
