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

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
)

// categoryPaths returns the on-disk destination of every qBittorrent category,
// resolved the way qBittorrent resolves it.
func (s *Service) categoryPaths(ctx context.Context, instanceID int, defaultSavePath string, useSubcategories bool) ([]string, error) {
	categories, err := s.getCategories(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to read qBittorrent categories: %w", err)
	}

	seen := make(map[string]struct{}, len(categories))
	for name := range categories {
		addAbsoluteScanRoot(seen, resolveCategoryPath(name, categories, defaultSavePath, useSubcategories, 0))
	}

	return sortedRoots(seen), nil
}

// maxCategoryDepth bounds the parent walk. Category names nest by "/" so a
// parent is always shorter than its child and this cannot loop, but a malformed
// name should not be able to recurse without end either.
const maxCategoryDepth = 16

// resolveCategoryPath mirrors qBittorrent's own resolution:
//
//   - an absolute save path is used as-is;
//   - a relative save path is taken against the default save path;
//   - a category with no save path of its own inherits, which with subcategories
//     enabled means its parent's destination plus the last name segment, and
//     otherwise the default save path plus the whole name.
//
// Returns "" when the destination cannot be determined, which callers treat as
// "no such destination" rather than as a path.
func resolveCategoryPath(name string, categories map[string]qbt.Category, defaultSavePath string, useSubcategories bool, depth int) string {
	if depth > maxCategoryDepth {
		return ""
	}

	savePath := filepath.Clean(strings.TrimSpace(categories[name].SavePath))
	if savePath != "." && savePath != "" {
		if filepath.IsAbs(savePath) {
			return savePath
		}
		if defaultSavePath == "" {
			return ""
		}
		return filepath.Join(defaultSavePath, savePath)
	}

	// Category names are slash-delimited whatever the host separator is.
	if useSubcategories {
		if i := strings.LastIndex(name, "/"); i > 0 {
			parent := resolveCategoryPath(name[:i], categories, defaultSavePath, useSubcategories, depth+1)
			if parent == "" {
				return ""
			}
			return filepath.Join(parent, filepath.FromSlash(name[i+1:]))
		}
	}

	if defaultSavePath == "" {
		return ""
	}
	return filepath.Join(defaultSavePath, filepath.FromSlash(name))
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
