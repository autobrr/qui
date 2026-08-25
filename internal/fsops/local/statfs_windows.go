// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/internal/fsops"
)

func (b *Backend) Statfs(ctx context.Context, path string) (*fsops.StatfsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// GetDiskFreeSpaceEx requires a directory, while the unix reference
	// (unix.Statfs) accepts files. Stat first so a missing path keeps its
	// portable fs.ErrNotExist, then query the containing directory for
	// non-directories.
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		path = filepath.Dir(path)
	}

	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path %s: %w", path, err)
	}

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(
		pathPtr, &freeBytesAvailable, &totalBytes, &totalFreeBytes,
	); err != nil {
		return nil, fmt.Errorf("GetDiskFreeSpaceEx %s: %w", path, err)
	}

	//nolint:gosec // uint64 to int64: disk sizes won't exceed int64 max
	return &fsops.StatfsResult{
		BytesAvailable: int64(freeBytesAvailable),
		BytesTotal:     int64(totalBytes),
	}, nil
}
