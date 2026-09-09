// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
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
		addAbsoluteScanRoot(seen, resolveCategoryPath(name, categories, defaultSavePath, useSubcategories))
	}

	return sortedRoots(seen), nil
}

// qbtInvalidPathChars is the regex from Utils::Fs::toValidPath; slashes are
// absent there because they separate path segments.
var qbtInvalidPathChars = regexp.MustCompile(`[:?"*<>|]+`)

// toValidPath converts a category name into the relative path qBittorrent
// creates for it, so a category called "movies:hd" is protected at "movies hd".
func toValidPath(name string) string {
	return qbtInvalidPathChars.ReplaceAllString(name, " ")
}

// resolveCategoryPath mirrors SessionImpl::categorySavePath. Returns "" when the
// destination cannot be determined; a depth cap here would silently drop
// protection for a deeply nested category.
func resolveCategoryPath(name string, categories map[string]qbt.Category, defaultSavePath string, useSubcategories bool) string {
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
			parent := resolveCategoryPath(name[:i], categories, defaultSavePath, useSubcategories)
			if parent == "" {
				return ""
			}
			// qBittorrent converts only the last segment and resolves the rest
			// through the parent category.
			return filepath.Join(parent, filepath.FromSlash(toValidPath(name[i+1:])))
		}
	}

	if defaultSavePath == "" {
		return ""
	}
	return filepath.Join(defaultSavePath, filepath.FromSlash(toValidPath(name)))
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

// abandonedDirCandidates narrows file-free directories to those safe to remove.
// Deepest first, so removing them in order never meets a non-empty directory.
func abandonedDirCandidates(
	ctx context.Context,
	dirs []AbandonedDir,
	scanRoots, ignorePaths, categoryPaths []string,
	gracePeriod time.Duration,
	backend fsops.Backend,
) []OrphanFile {
	// The protected sets are the same for every candidate, so normalize them
	// once rather than once per directory.
	normRoots := normalizePaths(scanRoots)
	normCategories := normalizePaths(categoryPaths)

	kept := make(map[string]struct{}, len(dirs))
	out := make([]OrphanFile, 0, len(dirs))

	for _, dir := range dirs {
		normDir := normalizePath(dir.Path)
		if slices.Contains(normRoots, normDir) {
			continue
		}
		if isIgnoredPath(dir.Path, ignorePaths) {
			continue
		}
		if isCategoryDestinationNormalized(normDir, normCategories) {
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
			Path:           dir.Path,
			IsAbandonedDir: true,
			ModifiedAt:     dir.ModTime,
			Status:         FileStatusPending,
		})
	}

	return out
}

// childrenAllKept reports whether dir can be emptied by removing directories
// already kept, so an ignored subtree or a symlink leaves it alone.
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

// normalizePaths normalizes a whole list once, for repeated membership tests.
func normalizePaths(paths []string) []string {
	normalized := make([]string, len(paths))
	for i, p := range paths {
		normalized[i] = normalizePath(p)
	}
	return normalized
}

// isCategoryDestinationNormalized reports whether normPath is a category
// destination or holds one below it. Both stay: qBittorrent will save into them
// again. Callers normalize once and test many paths against the same set.
func isCategoryDestinationNormalized(normPath string, normCategories []string) bool {
	for _, nCategory := range normCategories {
		if normPath == nCategory || isPathUnderNormalized(nCategory, normPath) {
			return true
		}
	}
	return false
}
