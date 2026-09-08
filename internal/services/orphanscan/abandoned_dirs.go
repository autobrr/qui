// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"fmt"
	"path/filepath"
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
// It returns the destinations to scan, and a superset to protect. The two differ
// only for a category whose name needs converting: qBittorrent's exact rule is
// not fully pinned down here, so both spellings are protected. Over-protecting
// leaves a directory in place, while under-protecting deletes a live one.
func (s *Service) categoryPaths(ctx context.Context, instanceID int, defaultSavePath string, useSubcategories bool) (destinations, protected []string, err error) {
	categories, err := s.getCategories(ctx, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read qBittorrent categories: %w", err)
	}

	dests := make(map[string]struct{}, len(categories))
	prot := make(map[string]struct{}, len(categories))
	for name := range categories {
		addAbsoluteScanRoot(dests, resolveCategoryPath(name, categories, defaultSavePath, useSubcategories, categoryDirName))
		addAbsoluteScanRoot(prot, resolveCategoryPath(name, categories, defaultSavePath, useSubcategories, categoryDirName))
		// The unconverted spelling, in case the server converts less than we do.
		addAbsoluteScanRoot(prot, resolveCategoryPath(name, categories, defaultSavePath, useSubcategories, func(segment string) string { return segment }))
	}

	return sortedRoots(dests), sortedRoots(prot), nil
}

// joinSegments applies dirName to each slash-separated segment of a category
// name, keeping the slashes so the result stays a relative slash path.
func joinSegments(name string, dirName func(string) string) string {
	segments := strings.Split(name, "/")
	for i, segment := range segments {
		segments[i] = dirName(segment)
	}
	return strings.Join(segments, "/")
}

// categoryDirNameInvalid are the characters qBittorrent replaces when it turns a
// category name into a directory name. "/" is absent: it separates
// subcategories and is consumed before a segment reaches here.
const categoryDirNameInvalid = `\:?"*<>|`

// categoryDirName converts one category name segment into the directory name
// qBittorrent creates for it, replacing characters invalid in a path with a
// space, so a category called "movies:hd" is protected at "movies hd".
func categoryDirName(segment string) string {
	if !strings.ContainsAny(segment, categoryDirNameInvalid) {
		return segment
	}
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(categoryDirNameInvalid, r) {
			return ' '
		}
		return r
	}, segment)
}

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
// The parent walk terminates on its own: each step drops a "/" segment, so the
// recursion is bounded by the name itself. Capping it would silently drop
// protection for a deeply nested category, which is the dangerous direction.
func resolveCategoryPath(name string, categories map[string]qbt.Category, defaultSavePath string, useSubcategories bool, dirName func(string) string) string {
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
			parent := resolveCategoryPath(name[:i], categories, defaultSavePath, useSubcategories, dirName)
			if parent == "" {
				return ""
			}
			return filepath.Join(parent, dirName(name[i+1:]))
		}
	}

	if defaultSavePath == "" {
		return ""
	}
	return filepath.Join(defaultSavePath, filepath.FromSlash(joinSegments(name, dirName)))
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

// normalizePaths normalizes a whole list once, for repeated membership tests.
func normalizePaths(paths []string) []string {
	normalized := make([]string, len(paths))
	for i, p := range paths {
		normalized[i] = normalizePath(p)
	}
	return normalized
}

// isScanRoot reports whether path is one of the roots the run walks. A scan root
// is a configured destination, so it stays even when empty.
func isScanRoot(path string, scanRoots []string) bool {
	return slices.Contains(normalizePaths(scanRoots), normalizePath(path))
}

// isCategoryDestination reports whether path is a category destination or holds
// one below it. Both stay: qBittorrent will save into them again.
func isCategoryDestination(path string, categoryPaths []string) bool {
	return isCategoryDestinationNormalized(normalizePath(path), normalizePaths(categoryPaths))
}

func isCategoryDestinationNormalized(normPath string, normCategories []string) bool {
	for _, nCategory := range normCategories {
		if normPath == nCategory || isPathUnderNormalized(nCategory, normPath) {
			return true
		}
	}
	return false
}
