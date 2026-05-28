// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package fsutil

import (
	"path/filepath"
	"testing"
)

func TestSameFilesystem_ResolvesReparsePointBeforeVolumeCheck(t *testing.T) {
	linkPath := `D:\cross_linked\source.mkv`
	linkDir := `D:\cross_linked\dest`
	realSource := `L:\Movies\source.mkv`
	realDest := `D:\cross_linked\dest`

	restoreWindowsSameFSHelpers(t)
	evalSymlinksFn = func(path string) (string, error) {
		switch path {
		case linkPath:
			return realSource, nil
		case linkDir:
			return realDest, nil
		default:
			t.Fatalf("unexpected path passed to evalSymlinksFn: %s", path)
			return "", nil
		}
	}
	getVolumeSerialFn = func(path string) (uint32, error) {
		switch filepath.VolumeName(path) {
		case `L:`:
			return 1, nil
		case `D:`:
			return 2, nil
		default:
			t.Fatalf("unexpected path passed to getVolumeSerialFn: %s", path)
			return 0, nil
		}
	}

	same, err := sameFilesystem(linkPath, linkDir)
	if err != nil {
		t.Fatalf("sameFilesystem failed: %v", err)
	}
	if same {
		t.Fatal("expected resolved source and destination to be on different filesystems")
	}
}

func restoreWindowsSameFSHelpers(t *testing.T) {
	t.Helper()

	originalEvalSymlinks := evalSymlinksFn
	originalGetVolumeSerial := getVolumeSerialFn

	t.Cleanup(func() {
		evalSymlinksFn = originalEvalSymlinks
		getVolumeSerialFn = originalGetVolumeSerial
	})
}
