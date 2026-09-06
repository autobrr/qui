// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
)

// categoryPaths returns the destinations qBittorrent categories resolve to: the
// explicit save path where a category sets one, and defaultSavePath/<name>
// otherwise. defaultSavePath may be empty, in which case implicit destinations
// are unknown and only explicit ones are returned.
func (s *Service) categoryPaths(ctx context.Context, instanceID int, defaultSavePath string) ([]string, error) {
	categories, err := s.getCategories(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to read qBittorrent categories: %w", err)
	}

	seen := make(map[string]struct{}, len(categories))
	for name, category := range categories {
		savePath := filepath.Clean(strings.TrimSpace(category.SavePath))
		if savePath != "." && filepath.IsAbs(savePath) {
			addAbsoluteScanRoot(seen, savePath)
			continue
		}
		// qBittorrent leaves savePath empty for a category that inherits the
		// default; the payload still lands in defaultSavePath/<name>.
		if defaultSavePath != "" {
			addAbsoluteScanRoot(seen, filepath.Join(defaultSavePath, name))
		}
	}

	return sortedRoots(seen), nil
}

// sortDeepestFirst orders directories so a child is always judged, and removed,
// before its parent.
func sortDeepestFirst(dirs []AbandonedDir) []AbandonedDir {
	sorted := make([]AbandonedDir, len(dirs))
	copy(sorted, dirs)
	sort.Slice(sorted, func(i, j int) bool {
		if len(sorted[i].Path) != len(sorted[j].Path) {
			return len(sorted[i].Path) > len(sorted[j].Path)
		}
		return sorted[i].Path < sorted[j].Path
	})
	return sorted
}

// abandonedDirCandidates narrows the file-free directories a walk found to those
// safe to remove: inside a scan root but never a scan root itself, not ignored,
// not a category destination, and settled past the grace period.
//
// Deepest first, and a directory is kept only when every child directory is also
// kept, so removing them in order never meets a non-empty directory.
func abandonedDirCandidates(
	ctx context.Context,
	dirs []AbandonedDir,
	scanRoots, ignorePaths, categoryPaths []string,
	gracePeriod time.Duration,
	backend fsops.Backend,
) []OrphanFile {
	kept := make(map[string]struct{}, len(dirs))
	out := make([]OrphanFile, 0, len(dirs))

	for _, dir := range dirs {
		if isScanRoot(dir.Path, scanRoots) {
			continue
		}
		if isIgnoredPath(dir.Path, ignorePaths) {
			continue
		}
		if isCategoryDestination(dir.Path, categoryPaths) {
			continue
		}
		if !dir.ModTime.IsZero() && time.Since(dir.ModTime) < gracePeriod {
			continue
		}
		if !childrenAllKept(ctx, dir.Path, kept, backend) {
			continue
		}

		kept[dir.Path] = struct{}{}
		out = append(out, OrphanFile{
			Path:       dir.Path,
			IsDir:      true,
			ModifiedAt: dir.ModTime,
			Status:     FileStatusPending,
		})
	}

	return out
}

// childrenAllKept reports whether dir can be emptied by removing directories
// already kept. A directory holding anything else — a file the walk skipped, an
// ignored subtree, a symlink — is left alone rather than failing on delete.
func childrenAllKept(ctx context.Context, dir string, kept map[string]struct{}, backend fsops.Backend) bool {
	entries, err := backend.ReadDir(ctx, dir)
	if err != nil {
		log.Debug().Err(err).Str("dir", dir).Msg("orphanscan: could not read directory, not treating it as abandoned")
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir || entry.IsSymlink {
			return false
		}
		if _, ok := kept[filepath.Join(dir, entry.Name)]; !ok {
			return false
		}
	}
	return true
}

// isScanRoot reports whether path is one of the roots the run walks. A scan root
// is a configured destination, so it stays even when empty.
func isScanRoot(path string, scanRoots []string) bool {
	nPath := normalizePath(path)
	for _, root := range scanRoots {
		if nPath == normalizePath(root) {
			return true
		}
	}
	return false
}

// isCategoryDestination reports whether path is a category destination or holds
// one below it. Both stay: qBittorrent will save into them again.
func isCategoryDestination(path string, categoryPaths []string) bool {
	nPath := normalizePath(path)
	for _, categoryPath := range categoryPaths {
		nCategory := normalizePath(categoryPath)
		if nPath == nCategory || isPathUnderNormalized(nCategory, nPath) {
			return true
		}
	}
	return false
}
