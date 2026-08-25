// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package automations

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// FreeSpaceSourceKeyQBittorrent is the source key for qBittorrent free space.
const FreeSpaceSourceKeyQBittorrent = "qbt"

// resolveFreeSpaceSource converts a models.FreeSpaceSource to the internal type.
// Returns a default qBittorrent source if the input is nil.
func resolveFreeSpaceSource(src *models.FreeSpaceSource) models.FreeSpaceSource {
	if src == nil || src.Type == "" {
		return models.FreeSpaceSource{Type: models.FreeSpaceSourceQBittorrent}
	}
	return *src
}

// GetFreeSpaceSourceKey returns a unique key for the given source.
// Keys are "qbt" for qBittorrent source or "path:/cleaned/path" for path sources.
func GetFreeSpaceSourceKey(src *models.FreeSpaceSource) string {
	resolved := resolveFreeSpaceSource(src)
	switch resolved.Type {
	case models.FreeSpaceSourcePath:
		trimmed := strings.TrimSpace(resolved.Path)
		if trimmed == "" {
			return FreeSpaceSourceKeyQBittorrent
		}

		// Clean path for consistent keys
		cleanPath := filepath.Clean(trimmed)
		return "path:" + cleanPath
	default:
		return FreeSpaceSourceKeyQBittorrent
	}
}

// GetFreeSpaceRuleKey returns a unique key for the given rule's free space state.
// The key includes both the source key and rule ID to ensure each rule has its own
// projection state, even when multiple rules share the same disk/source.
func GetFreeSpaceRuleKey(rule *models.Automation) string {
	if rule == nil {
		return FreeSpaceSourceKeyQBittorrent + "|rule:0"
	}
	return GetFreeSpaceSourceKey(rule.FreeSpaceSource) + fmt.Sprintf("|rule:%d", rule.ID)
}

// GetFreeSpaceBytesForSource returns the free space in bytes for the given source.
// On Windows, only qBittorrent source is supported.
func GetFreeSpaceBytesForSource(
	ctx context.Context,
	syncManager *qbittorrent.SyncManager,
	instance *models.Instance,
	src *models.FreeSpaceSource,
) (int64, error) {
	resolved := resolveFreeSpaceSource(src)

	switch resolved.Type {
	case models.FreeSpaceSourceQBittorrent, "":
		// Default: use qBittorrent's reported free space
		if syncManager == nil {
			return 0, errors.New("syncManager is nil")
		}
		if instance == nil {
			return 0, errors.New("instance required for qBittorrent free space source")
		}
		freeSpace, err := syncManager.GetFreeSpace(ctx, instance.ID)
		if err != nil {
			return 0, fmt.Errorf("failed to get free space from qBittorrent: %w", err)
		}
		return freeSpace, nil

	case models.FreeSpaceSourcePath:
		// Path-based free space is not supported on Windows
		return 0, errors.New("path-based free space source is not supported on Windows")

	default:
		return 0, fmt.Errorf("unsupported free space source type: %s", resolved.Type)
	}
}
