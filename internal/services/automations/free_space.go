// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const FreeSpaceSourceKeyQBittorrent = "qbt"

func resolveFreeSpaceSource(src *models.FreeSpaceSource) models.FreeSpaceSource {
	if src == nil || src.Type == "" {
		return models.FreeSpaceSource{Type: models.FreeSpaceSourceQBittorrent}
	}
	return *src
}

func GetFreeSpaceSourceKey(src *models.FreeSpaceSource) string {
	resolved := resolveFreeSpaceSource(src)
	trimmed := strings.TrimSpace(resolved.Path)
	if trimmed == "" {
		return FreeSpaceSourceKeyQBittorrent
	}

	switch resolved.Type {
	case models.FreeSpaceSourcePath, models.FreeSpaceSourceAllowance:
		return string(resolved.Type) + ":" + path.Clean(trimmed)
	case models.FreeSpaceSourceQbitPath:
		return "qbitPath:" + trimmed
	default:
		return FreeSpaceSourceKeyQBittorrent
	}
}

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

func qbtFreeSpaceAtPath(ctx context.Context, syncManager *qbittorrent.SyncManager, instance *models.Instance, p string) (int64, error) {
	if syncManager == nil {
		return 0, errors.New("syncManager is nil")
	}
	if instance == nil {
		return 0, errors.New("instance required for qBittorrent path free space source")
	}

	freeSpace, err := syncManager.GetFreeSpaceAtPath(ctx, instance.ID, p)
	if err != nil {
		return 0, fmt.Errorf("failed to get free space at %s from qBittorrent: %w", p, err)
	}
	return freeSpace, nil
}

func (s *Service) freeSpaceBytes(ctx context.Context, instance *models.Instance, src *models.FreeSpaceSource) (int64, error) {
	resolved := resolveFreeSpaceSource(src)

	switch resolved.Type {
	case models.FreeSpaceSourceQBittorrent, "":
		return qbtFreeSpace(ctx, s.syncManager, instance)

	case models.FreeSpaceSourcePath:
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

	case models.FreeSpaceSourceQbitPath:
		p := strings.TrimSpace(resolved.Path)
		if p == "" {
			return 0, errors.New("free space source path is empty")
		}
		return qbtFreeSpaceAtPath(ctx, s.syncManager, instance, p)

	default:
		return 0, fmt.Errorf("unsupported free space source type: %s", resolved.Type)
	}
}

func (s *Service) usageUnder(ctx context.Context, instance *models.Instance, root string) (int64, error) {
	backend, fsInstance, err := s.backendPool.Require(ctx, instance.ID, models.CapabilityIdentity)
	if err != nil {
		return 0, fmt.Errorf("no filesystem backend with file identity for free space allowance: %w", err)
	}
	if windowsLocalPathSource(fsInstance) {
		return 0, errors.New("allowance free space source is not supported on Windows with local filesystem access")
	}
	root = backend.Paths().Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return 0, errors.New("free space source path is empty")
	}

	walkCtx, cancel := context.WithCancel(ctx)
	entries, err := backend.WalkDir(walkCtx, root, fsops.WalkOptions{WantFileID: true, EmitStatErrors: true, FollowRoot: true})
	if err != nil {
		cancel()
		return 0, fmt.Errorf("failed to measure %s: %w", root, err)
	}
	defer func() {
		cancel()
		for range entries {
		}
	}()

	started := time.Now()
	var usage int64
	files := 0
	counted := make(map[fsops.FileKey]struct{})
	var identityErr error
	for entry := range entries {
		switch {
		case entry.Err != nil:
			return 0, fmt.Errorf("failed to measure %s: cannot read %s: %w", root, entry.Path, entry.Err)
		case errors.Is(entry.StatErr, fs.ErrNotExist):
			continue
		case entry.StatErr != nil:
			return 0, fmt.Errorf("failed to measure %s: cannot read %s: %w", root, entry.Path, entry.StatErr)
		case !entry.Mode.IsRegular():
			continue
		case entry.FileIDErr != nil:
			if identityErr == nil {
				identityErr = fmt.Errorf("failed to measure %s: %s: %w", root, entry.Path, entry.FileIDErr)
			}
			continue
		}
		if entry.Nlinks > 1 {
			if key, ok := fsops.FileKeyOf(entry.FileID, fsInstance); ok {
				if _, seen := counted[key]; seen {
					continue
				}
				counted[key] = struct{}{}
			}
		}
		usage += entry.Size
		files++
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("failed to measure %s: %w", root, err)
	}
	if identityErr != nil {
		return 0, identityErr
	}
	log.Trace().Int("instanceID", fsInstance.ID).Str("path", root).Int64("usedBytes", usage).Int("files", files).
		Dur("took", time.Since(started)).Msg("automations: measured the usage for a free space allowance")
	return usage, nil
}

const previewUsageReuse = time.Minute

type previewUsageKey struct {
	instanceID int
	root       string
}

type previewUsageReading struct {
	bytes int64
	at    time.Time
}

func (s *Service) previewUsageUnder(ctx context.Context, instance *models.Instance, root string) (int64, error) {
	key := previewUsageKey{instanceID: instance.ID, root: root}
	s.previewUsageMu.Lock()
	defer s.previewUsageMu.Unlock()

	if reading, ok := s.previewUsage[key]; ok && time.Since(reading.at) < previewUsageReuse {
		return reading.bytes, nil
	}
	usage, err := s.usageUnder(ctx, instance, root)
	if err != nil {
		return 0, err
	}
	if s.previewUsage == nil {
		s.previewUsage = make(map[previewUsageKey]previewUsageReading)
	}
	maps.DeleteFunc(s.previewUsage, func(_ previewUsageKey, reading previewUsageReading) bool {
		return time.Since(reading.at) >= previewUsageReuse
	})
	s.previewUsage[key] = previewUsageReading{bytes: usage, at: time.Now()}
	return usage, nil
}
