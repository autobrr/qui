// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

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
// Keys are "qbt" for qBittorrent source, "path:/cleaned/path" for path sources,
// and "allowance:/cleaned/path" for allowance sources. The allowance is not in
// the key, so rules with one path share one measurement.
func GetFreeSpaceSourceKey(src *models.FreeSpaceSource) string {
	resolved := resolveFreeSpaceSource(src)
	switch resolved.Type {
	case models.FreeSpaceSourcePath, models.FreeSpaceSourceAllowance:
		trimmed := strings.TrimSpace(resolved.Path)
		if trimmed == "" {
			return FreeSpaceSourceKeyQBittorrent
		}

		// path, not filepath: the source is a POSIX path on the host that
		// holds the disk, which is not the qui host when access is over SSH.
		return string(resolved.Type) + ":" + path.Clean(trimmed)
	default:
		return FreeSpaceSourceKeyQBittorrent
	}
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

// usageUnder returns the bytes that the files under root hold: the sum of the
// lengths of the regular files, hidden files too. A hardlinked file counts one
// time. It follows no symlink except root itself. It fails when it cannot read
// a folder or a file, so a part of the tree never reads as empty.
func (s *Service) usageUnder(ctx context.Context, instance *models.Instance, root string) (int64, error) {
	// The pool's fresh row decides the mode, not the caller's snapshot.
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
	remote := models.FilesystemAccessMode(fsInstance) == models.FilesystemModeRemote

	walkCtx, cancel := context.WithCancel(ctx)
	entries, err := backend.WalkDir(walkCtx, root, fsops.WalkOptions{WantFileID: true, EmitStatErrors: true, FollowRoot: true})
	if err != nil {
		cancel()
		return 0, fmt.Errorf("failed to measure %s: %w", root, err)
	}
	defer func() {
		cancel()
		for range entries { //nolint:revive // drain channel to release the walk goroutine
		}
	}()

	started := time.Now()
	var usage int64
	files := 0
	counted := make(map[fsops.FileKey]struct{})
	for entry := range entries {
		switch {
		case entry.Err != nil:
			return 0, fmt.Errorf("failed to measure %s: cannot read %s: %w", root, entry.Path, entry.Err)
		case entry.StatErr != nil:
			return 0, fmt.Errorf("failed to measure %s: cannot read %s: %w", root, entry.Path, entry.StatErr)
		case !entry.Mode.IsRegular():
			continue
		// A remote walk without identity is the walk of a host that has no
		// shell, or of a tree too large for the shell walk.
		case remote && entry.FileIDErr != nil:
			return 0, fmt.Errorf("failed to measure %s: %s: %w", root, entry.Path, entry.FileIDErr)
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
	log.Trace().Int("instanceID", fsInstance.ID).Str("path", root).Int64("usedBytes", usage).Int("files", files).
		Dur("took", time.Since(started)).Msg("automations: measured the usage for a free space allowance")
	return usage, nil
}

// previewUsageReuse is how long a preview reuses the usage that a preview
// measured. The rule dialog sends a preview after each edit, and each one
// would walk the tree again.
const previewUsageReuse = time.Minute

type previewUsageKey struct {
	instanceID int
	root       string
}

type previewUsageReading struct {
	bytes int64
	at    time.Time
}

// previewUsageUnder is usageUnder for a preview. A preview that arrives while
// another one measures waits for it, and then reuses its figure.
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
