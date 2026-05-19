// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package local

import (
	"context"
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/autobrr/qui/internal/fsops"
)

func (b *Backend) Statfs(ctx context.Context, path string) (*fsops.StatfsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return nil, fmt.Errorf("statfs %s: %w", path, err)
	}
	//nolint:gosec,unconvert // int64 cast needed for macOS (uint32) even though Linux is already int64
	return &fsops.StatfsResult{
		BytesAvailable: int64(stat.Bavail) * int64(stat.Bsize),
		BytesTotal:     int64(stat.Blocks) * int64(stat.Bsize),
	}, nil
}
