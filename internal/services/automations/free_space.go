// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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

// qbtFreeSpace returns qBittorrent's reported free space for the instance.
func qbtFreeSpace(ctx context.Context, syncManager *qbittorrent.SyncManager, instance *models.Instance) (int64, error) {
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
}

// freeSpaceBytes reads the free space of src. Only a path source resolves a
// backend, and it fails rather than fall back to qBittorrent's value, which
// can measure a different disk.
func (s *Service) freeSpaceBytes(ctx context.Context, instance *models.Instance, src *models.FreeSpaceSource) (int64, error) {
	resolved := resolveFreeSpaceSource(src)

	switch resolved.Type {
	case models.FreeSpaceSourceQBittorrent, "":
		return qbtFreeSpace(ctx, s.syncManager, instance)

	case models.FreeSpaceSourcePath:
		// The pool's fresh row decides the mode, not the caller's snapshot.
		backend, fsInstance, err := s.backendPool.Require(ctx, instance.ID, models.CapabilityRead)
		if err != nil {
			return 0, fmt.Errorf("no filesystem backend for free space path: %w", err)
		}
		if windowsLocalPathSource(fsInstance) {
			return 0, errors.New("path-based free space source is not supported on Windows with local filesystem access")
		}
		p := backend.Paths().Clean(strings.TrimSpace(resolved.Path))
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
