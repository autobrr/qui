// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package fsutil

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// sameFilesystem checks if two paths are on the same volume on Windows.
// Hardlinks on Windows require the same volume.
// Compares volume serial numbers using GetVolumeInformation.
func sameFilesystem(path1, path2 string) (bool, error) {
	vol1, err := getVolumeSerial(path1)
	if err != nil {
		return false, fmt.Errorf("get volume for %s: %w", path1, err)
	}

	vol2, err := getVolumeSerial(path2)
	if err != nil {
		return false, fmt.Errorf("get volume for %s: %w", path2, err)
	}

	return vol1 == vol2, nil
}

// getVolumeSerial returns the volume serial number for the volume containing the given path.
func getVolumeSerial(path string) (uint32, error) {
	// Get absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return 0, fmt.Errorf("abs path: %w", err)
	}

	// Get volume path name (e.g., "C:\" or "\\?\Volume{GUID}\")
	volumePath := make([]uint16, windows.MAX_PATH+1)
	pathPtr, err := windows.UTF16PtrFromString(absPath)
	if err != nil {
		return 0, fmt.Errorf("convert path: %w", err)
	}

	err = windows.GetVolumePathName(pathPtr, &volumePath[0], uint32(len(volumePath)))
	if err != nil {
		return 0, fmt.Errorf("get volume path name: %w", err)
	}

	// Get volume information
	volumePathStr := windows.UTF16ToString(volumePath)
	if !strings.HasSuffix(volumePathStr, `\`) {
		volumePathStr += `\`
	}

	volumePathPtr, err := windows.UTF16PtrFromString(volumePathStr)
	if err != nil {
		return 0, fmt.Errorf("convert volume path: %w", err)
	}

	var volumeSerial uint32
	err = windows.GetVolumeInformation(
		volumePathPtr,
		nil, 0, // volume name buffer
		&volumeSerial,
		nil,    // max component length
		nil,    // file system flags
		nil, 0, // file system name buffer
	)
	if err != nil {
		return 0, fmt.Errorf("get volume information: %w", err)
	}

	return volumeSerial, nil
}
