// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
)

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

func abandonedDirCandidates(
	ctx context.Context,
	dirs []AbandonedDir,
	deleted []OrphanFile,
	scanRoots, categoryPaths []string,
	protected *protectedPaths,
	gracePeriod time.Duration,
	backend fsops.Backend,
) []OrphanFile {
	if len(dirs) == 0 {
		return nil
	}

	d := backend.Paths()
	normRoots := normalizePaths(d, scanRoots)
	normCategories := normalizePaths(d, categoryPaths)
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
		normDir := fsops.NormalizePath(d, dir.Path)
		if slices.Contains(normRoots, normDir) {
			continue
		}
		if protected.ignored(dir.Path) {
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

func childrenAllKept(ctx context.Context, dir string, kept, deletedPaths map[string]struct{}, backend fsops.Backend) bool {
	entries, err := backend.ReadDir(ctx, dir)
	if err != nil {
		log.Debug().Err(err).Str("dir", dir).Msg("orphanscan: could not read directory, not treating it as abandoned")
		return false
	}

	d := backend.Paths()
	for _, entry := range entries {
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

func normalizePaths(d fsops.PathDialect, paths []string) []string {
	normalized := make([]string, len(paths))
	for i, p := range paths {
		normalized[i] = fsops.NormalizePath(d, p)
	}
	return normalized
}

func isCategoryDestinationNormalized(d fsops.PathDialect, normPath string, normCategories []string) bool {
	for _, nCategory := range normCategories {
		if normPath == nCategory || isPathUnderNormalized(d, nCategory, normPath) {
			return true
		}
	}
	return false
}

func torrentDirs(d fsops.PathDialect, files []string, fileDirs map[string]struct{}, saveRoots ...string) []string {
	seen := make(map[string]struct{})
	var dirs []string
	for _, file := range files {
		dir := d.Dir(file)
		if _, onDisk := fileDirs[dir]; onDisk {
			continue
		}
		dirs = addUpToRoot(d, dir, seen, dirs)
	}
	for _, root := range saveRoots {
		dirs = addUpToRoot(d, root, seen, dirs)
	}
	return dirs
}

func reachedDirs(d fsops.PathDialect, result *buildFileMapResult, fileDirs map[string]struct{}) []string {
	return torrentDirs(d, result.torrentPaths, fileDirs, slices.Concat(result.metadataRoots, result.skippedRoots)...)
}

func addUpToRoot(d fsops.PathDialect, dir string, seen map[string]struct{}, added []string) []string {
	for {
		if _, ok := seen[dir]; ok {
			return added
		}
		seen[dir] = struct{}{}
		added = append(added, dir)
		parent := d.Dir(dir)
		if parent == dir {
			return added
		}
		dir = parent
	}
}

func dropDirsTorrentsReach(ctx context.Context, dirs []OrphanFile, reached *inUseIDs, backend fsops.Backend) []OrphanFile {
	paths := make([]string, len(dirs))
	for i := range dirs {
		paths[i] = dirs[i].Path
	}
	infos, errs := fsops.LstatAll(ctx, backend, paths)

	d := backend.Paths()
	kept := make(map[string]struct{})
	for i, p := range paths {
		err := errs[i]
		if err == nil {
			var ok bool
			if ok, err = reached.reaches(infos[i]); err == nil && !ok {
				continue
			}
		}
		log.Debug().Err(err).Str("dir", p).Msg("orphanscan: a torrent may use this directory under another path, not treating it as abandoned")
		addUpToRoot(d, p, kept, nil)
	}
	if len(kept) == 0 {
		return dirs
	}

	out := make([]OrphanFile, 0, len(dirs))
	for _, dir := range dirs {
		if _, ok := kept[dir.Path]; !ok {
			out = append(out, dir)
		}
	}
	return out
}
