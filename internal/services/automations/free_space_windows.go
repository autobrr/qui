// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package automations

import (
	"context"
	"errors"
	"fmt"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// GetFreeSpaceBytesForSource returns the free space in bytes for the given source.
// On Windows, only the qBittorrent source is supported — matching pre-fsops
// behavior. The backend can serve path-based free space here too; enabling it
// is a deliberate follow-up, not a refactor side effect.
func GetFreeSpaceBytesForSource(
	ctx context.Context,
	syncManager *qbittorrent.SyncManager,
	instance *models.Instance,
	src *models.FreeSpaceSource,
	_ fsops.Backend,
) (int64, error) {
	resolved := resolveFreeSpaceSource(src)

	switch resolved.Type {
	case models.FreeSpaceSourceQBittorrent, "":
		return qbtFreeSpace(ctx, syncManager, instance)

	case models.FreeSpaceSourcePath:
		// Path-based free space is not supported on Windows
		return 0, errors.New("path-based free space source is not supported on Windows")

	default:
		return 0, fmt.Errorf("unsupported free space source type: %s", resolved.Type)
	}
}
