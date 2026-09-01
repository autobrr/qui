// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package automations

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// GetFreeSpaceBytesForSource returns the free space in bytes for the given source.
// This is the preferred function as it doesn't require a full rule.
func GetFreeSpaceBytesForSource(
	ctx context.Context,
	syncManager *qbittorrent.SyncManager,
	instance *models.Instance,
	src *models.FreeSpaceSource,
	backend fsops.Backend,
) (int64, error) {
	resolved := resolveFreeSpaceSource(src)

	switch resolved.Type {
	case models.FreeSpaceSourceQBittorrent, "":
		return qbtFreeSpace(ctx, syncManager, instance)

	case models.FreeSpaceSourcePath:
		// Read free space via the backend (local or remote)
		if backend == nil {
			return 0, errors.New("backend is required for path-based free space source")
		}
		p := filepath.Clean(strings.TrimSpace(resolved.Path))
		if p == "" || p == "." {
			return 0, errors.New("free space source path is empty")
		}
		result, err := backend.Statfs(ctx, p)
		if err != nil {
			return 0, fmt.Errorf("failed to get filesystem stats for %s: %w", p, err)
		}
		return result.BytesAvailable, nil

	default:
		return 0, fmt.Errorf("unsupported free space source type: %s", resolved.Type)
	}
}
