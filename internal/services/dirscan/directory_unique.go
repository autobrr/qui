// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"fmt"

	"github.com/autobrr/qui/internal/models"
)

func (s *Service) ensureUniqueDirectoryPath(ctx context.Context, candidate *models.DirScanDirectory) error {
	dirs, err := s.store.ListDirectories(ctx)
	if err != nil {
		return fmt.Errorf("list directories: %w", err)
	}

	d := s.DirectoryPaths(ctx, candidate)
	cleanPath := d.Clean(candidate.Path)
	for _, dir := range dirs {
		if dir.ID == candidate.ID || dir.TargetInstanceID != candidate.TargetInstanceID {
			continue
		}
		if rel, err := d.Rel(d.Clean(dir.Path), cleanPath); err == nil && rel == "." {
			return fmt.Errorf("%w: %s", models.ErrDuplicateDirScanDirectoryPath, cleanPath)
		}
	}
	return nil
}
