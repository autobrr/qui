// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package local

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/internal/fsops"
)

func (b *Backend) Statfs(ctx context.Context, path string) (*fsops.StatfsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path %s: %w", path, err)
	}

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(
		pathPtr,
		(*uint64)(unsafe.Pointer(&freeBytesAvailable)),
		(*uint64)(unsafe.Pointer(&totalBytes)),
		(*uint64)(unsafe.Pointer(&totalFreeBytes)),
	); err != nil {
		return nil, fmt.Errorf("GetDiskFreeSpaceEx %s: %w", path, err)
	}

	//nolint:gosec // uint64 to int64: disk sizes won't exceed int64 max
	return &fsops.StatfsResult{
		BytesAvailable: int64(freeBytesAvailable),
		BytesTotal:     int64(totalBytes),
	}, nil
}
