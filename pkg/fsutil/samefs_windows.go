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

var (
	evalSymlinksFn    = filepath.EvalSymlinks
	getVolumeSerialFn = getVolumeSerial
)

func sameFilesystem(path1, path2 string) (bool, error) {
	resolvedPath1, err := evalSymlinksFn(path1)
	if err != nil {
		return false, fmt.Errorf("resolve path %s: %w", path1, err)
	}

	resolvedPath2, err := evalSymlinksFn(path2)
	if err != nil {
		return false, fmt.Errorf("resolve path %s: %w", path2, err)
	}

	vol1, err := getVolumeSerialFn(resolvedPath1)
	if err != nil {
		return false, fmt.Errorf("get volume for %s: %w", resolvedPath1, err)
	}

	vol2, err := getVolumeSerialFn(resolvedPath2)
	if err != nil {
		return false, fmt.Errorf("get volume for %s: %w", resolvedPath2, err)
	}

	return vol1 == vol2, nil
}

func getVolumeSerial(path string) (uint32, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return 0, fmt.Errorf("abs path: %w", err)
	}

	volumePath := make([]uint16, windows.MAX_PATH+1)
	pathPtr, err := windows.UTF16PtrFromString(absPath)
	if err != nil {
		return 0, fmt.Errorf("convert path: %w", err)
	}

	err = windows.GetVolumePathName(pathPtr, &volumePath[0], uint32(len(volumePath)))
	if err != nil {
		return 0, fmt.Errorf("get volume path name: %w", err)
	}

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
		nil, 0,
		&volumeSerial,
		nil,
		nil,
		nil, 0,
	)
	if err != nil {
		return 0, fmt.Errorf("get volume information: %w", err)
	}

	return volumeSerial, nil
}
