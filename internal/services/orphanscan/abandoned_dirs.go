// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// categoryPaths returns the on-disk destination of every qBittorrent category,
// resolved the way qBittorrent resolves it.
func (s *Service) categoryPaths(ctx context.Context, d fsops.PathDialect, instanceID int, defaultSavePath string, useSubcategories bool) ([]string, error) {
	categories, err := s.sync.GetCategories(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to read qBittorrent categories: %w", err)
	}

	seen := make(map[string]struct{}, len(categories))
	for name := range categories {
		addAbsoluteScanRoot(d, seen, qbittorrent.CategorySavePath(d, name, categories, defaultSavePath, useSubcategories))
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

// abandonedDirCandidates narrows directories to those safe to remove. deleted
// are the orphans this run removes, so a directory holding only those counts as
// empty, and one inside a deleted disc unit goes with the unit. Deepest first,
// so removing them in order never meets a non-empty directory.
func abandonedDirCandidates(
	ctx context.Context,
	dirs []AbandonedDir,
	deleted []OrphanFile,
	scanRoots, ignorePaths, categoryPaths []string,
	gracePeriod time.Duration,
	backend fsops.Backend,
) []OrphanFile {
	if len(dirs) == 0 {
		return nil
	}

	d := backend.Paths()
	// The protected sets are the same for every candidate, so normalize them
	// once rather than once per directory.
	normRoots := normalizePaths(d, scanRoots)
	normCategories := normalizePaths(d, categoryPaths)
	// Keyed like kept, by the spelling the walk produced: case-folding here
	// would let a surviving case-twin pass for the deleted orphan.
	deletedPaths := make(map[string]struct{}, len(deleted))
	for i := range deleted {
		deletedPaths[deleted[i].Path] = struct{}{}
	}

	kept := make(map[string]struct{}, len(dirs))
	out := make([]OrphanFile, 0, len(dirs))

	for _, dir := range dirs {
		if underDeletedPath(d, dir.Path, deletedPaths) {
			continue
		}
		normDir := normalizePath(d, dir.Path)
		if slices.Contains(normRoots, normDir) {
			continue
		}
		if isIgnoredPath(d, dir.Path, ignorePaths) {
			continue
		}
		if isCategoryDestinationNormalized(d, normDir, normCategories) {
			continue
		}
		if !dir.ModTime.IsZero() && time.Since(dir.ModTime) < gracePeriod {
			continue
		}
		if !childrenAllKept(ctx, dir.Path, kept, deletedPaths, backend) {
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
// already kept and the orphans the file pass deletes, so a file the run leaves
// behind, an ignored subtree or a symlink leaves it alone.
func childrenAllKept(ctx context.Context, dir string, kept, deletedPaths map[string]struct{}, backend fsops.Backend) bool {
	entries, err := backend.ReadDir(ctx, dir)
	if err != nil {
		log.Debug().Err(err).Str("dir", dir).Msg("orphanscan: could not read directory, not treating it as abandoned")
		return false
	}

	d := backend.Paths()
	for _, entry := range entries {
		// The walk never reports a symlink, so one can never be in deletedPaths.
		if entry.IsSymlink {
			return false
		}
		child := d.Join(dir, entry.Name)
		if _, gone := deletedPaths[child]; gone {
			continue
		}
		if !entry.IsDir {
			return false
		}
		if _, ok := kept[child]; !ok {
			return false
		}
	}
	return true
}

// underDeletedPath reports whether dir is, or sits inside, a path the file pass
// removes. Only a disc unit is a directory-shaped orphan, and the file pass
// removes the whole unit, so nothing below it is left for the directory pass.
func underDeletedPath(d fsops.PathDialect, dir string, deletedPaths map[string]struct{}) bool {
	for {
		if _, gone := deletedPaths[dir]; gone {
			return true
		}
		parent := d.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// normalizePaths normalizes a whole list once, for repeated membership tests.
func normalizePaths(d fsops.PathDialect, paths []string) []string {
	normalized := make([]string, len(paths))
	for i, p := range paths {
		normalized[i] = normalizePath(d, p)
	}
	return normalized
}

// isCategoryDestinationNormalized reports whether normPath is a category
// destination or holds one below it. Both stay: qBittorrent will save into them
// again. Callers normalize once and test many paths against the same set.
func isCategoryDestinationNormalized(d fsops.PathDialect, normPath string, normCategories []string) bool {
	for _, nCategory := range normCategories {
		if normPath == nCategory || isPathUnderNormalized(d, nCategory, normPath) {
			return true
		}
	}
	return false
}
