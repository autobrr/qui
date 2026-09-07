// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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
// Keys are "qbt" for the qBittorrent default directory, "path:/cleaned/path" for local
// filesystem sources, and "qbitPath:<path>" for paths read on the qBittorrent host.
func GetFreeSpaceSourceKey(src *models.FreeSpaceSource) string {
	resolved := resolveFreeSpaceSource(src)
	trimmed := strings.TrimSpace(resolved.Path)
	if trimmed == "" {
		return FreeSpaceSourceKeyQBittorrent
	}

	switch resolved.Type {
	case models.FreeSpaceSourcePath:
		// Clean path for consistent keys
		return "path:" + filepath.Clean(trimmed)
	case models.FreeSpaceSourceQbitPath:
		// Remote path: keep the qBittorrent host's separators, so a Windows host
		// and a POSIX host never collapse onto the same key.
		return "qbitPath:" + trimmed
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

// qbtFreeSpaceAtPath returns the free space qBittorrent reports at path on its own host.
func qbtFreeSpaceAtPath(ctx context.Context, syncManager *qbittorrent.SyncManager, instance *models.Instance, path string) (int64, error) {
	if syncManager == nil {
		return 0, errors.New("syncManager is nil")
	}
	if instance == nil {
		return 0, errors.New("instance required for qBittorrent path free space source")
	}

	// An unmeasurable path arrives as an error, so conditions never read it as zero free space.
	freeSpace, err := syncManager.GetFreeSpaceAtPath(ctx, instance.ID, path)
	if err != nil {
		return 0, fmt.Errorf("failed to get free space at %s from qBittorrent: %w", path, err)
	}

	return freeSpace, nil
}

// GetFreeSpaceBytesForSource returns the free space in bytes for the given source.
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
		p := filepath.Clean(strings.TrimSpace(resolved.Path))
		if p == "" || p == "." {
			return 0, errors.New("free space source path is empty")
		}
		return backendFreeSpace(ctx, backend, p)

	case models.FreeSpaceSourceQbitPath:
		// The path belongs to the qBittorrent host, so qui passes it through unchanged.
		p := strings.TrimSpace(resolved.Path)
		if p == "" {
			return 0, errors.New("free space source path is empty")
		}
		return qbtFreeSpaceAtPath(ctx, syncManager, instance, p)

	default:
		return 0, fmt.Errorf("unsupported free space source type: %s", resolved.Type)
	}
}
